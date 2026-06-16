// Package picker 是 fff 的核心搜索路径：目录扫描 + bigram 预过滤 + frecency/querytracker 排序。
package picker

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/superops-team/fg/bigram"
	"github.com/superops-team/fg/core"
	"github.com/superops-team/fg/frecency"
	"github.com/superops-team/fg/queryparser"
	"github.com/superops-team/fg/querytracker"
)

// Options 控制 Picker 的行为
type Options struct {
	// IgnoreFn 可选：接受一个相对路径，true 表示忽略
	IgnoreFn func(relPath string) bool

	// NowFunc 可注入时间（测试用）
	NowFunc func() time.Time

	// FrecencyMode 决定 frecency 衰减半衰期
	FrecencyMode frecency.Mode
}

// Picker 是一个扫描 + 搜索 + 排名的控制器
type Picker struct {
	root string
	opts Options

	arena  *core.PathArena
	files  []core.FileItem
	fileMu sync.RWMutex // 保护 files 的并发访问（Scan 期间写入）

	bg      *bigram.Bigram
	overlay *bigram.BigramOverlay

	frec *frecency.FrecencyTracker
	qt   *querytracker.QueryTracker

	nowFn   func() time.Time
	scanned atomic.Bool // Scan 完成标志

	pagePool *core.PagePool
}

// Result 表示单条搜索结果
type Result struct {
	idx   uint32
	score int32
	p     *Picker
	path  string
}

// New 返回一个新的 Picker（使用默认内存 frecency/querytracker）。
func New(root string, opts Options) *Picker {
	nowFn := opts.NowFunc
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Picker{
		root:     root,
		opts:     opts,
		arena:    core.NewPathArena(4096),
		files:    make([]core.FileItem, 0, 4096),
		bg:       bigram.NewBigram(),
		overlay:  bigram.NewOverlay(),
		frec:     frecency.New(frecency.Options{Mode: opts.FrecencyMode, NowFunc: nowFn}),
		qt:       querytracker.New(),
		nowFn:    nowFn,
		pagePool: core.NewPagePool(16 * 1024),
	}
}

// FileCount 返回当前扫描到的文件总数
func (p *Picker) FileCount() int {
	p.fileMu.RLock()
	defer p.fileMu.RUnlock()
	return len(p.files)
}

// FileAt 返回第 i 个 FileItem 的只读引用（调用方不应修改）
func (p *Picker) FileAt(i int) *core.FileItem {
	p.fileMu.RLock()
	defer p.fileMu.RUnlock()
	if i < 0 || i >= len(p.files) {
		return nil
	}
	return &p.files[i]
}

// PathAt 返回第 i 个文件的绝对路径
func (p *Picker) PathAt(i int) string {
	p.fileMu.RLock()
	defer p.fileMu.RUnlock()
	if i < 0 || i >= len(p.files) {
		return ""
	}
	rel := p.arena.Get(p.files[i].Path)
	return filepath.Join(p.root, rel)
}

// TouchByIndex 标记第 i 个文件被访问，更新 frecency
func (p *Picker) TouchByIndex(i int) {
	p.fileMu.RLock()
	if i < 0 || i >= len(p.files) {
		p.fileMu.RUnlock()
		return
	}
	rel := p.arena.Get(p.files[i].Path)
	p.fileMu.RUnlock()
	_ = p.frec.Touch(rel)
	// 读取最新的 access score，更新 FileItem 内 AccessFrecencyScore（仅用于排名缓存）
	newScore := p.frec.GetAccessScore(rel)
	p.fileMu.Lock()
	if i < len(p.files) {
		// 注意：int16 原子操作需要 unsafe，这里用 int32 保存到 AccessFrecencyScore 并做转换
		if newScore > 32767 {
			newScore = 32767
		}
		if newScore < -32768 {
			newScore = -32768
		}
		// 直接写：Scan 完成后 fileMu 已经持锁
		p.files[i].AccessFrecencyScore = int16(newScore)
	}
	p.fileMu.Unlock()
}

// ================================================================
// Scan: 目录扫描 + arena + bigram 构建
// ================================================================

// Scan 扫描 root 目录，构建文件索引
func (p *Picker) Scan() error {
	// 重置状态
	p.arena = core.NewPathArena(4096)
	p.fileMu.Lock()
	p.files = p.files[:0]
	p.fileMu.Unlock()

	// 收集相对路径
	var relPaths []string

	err := filepath.WalkDir(p.root, func(full string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(p.root, full)
		if relErr != nil {
			rel = full
		}
		// 忽略隐藏目录（.git, .svn, .hg）
		if d.IsDir() {
			name := d.Name()
			if name != "." && strings.HasPrefix(name, ".") {
				if name == ".git" || name == ".svn" || name == ".hg" || name == ".idea" || name == "node_modules" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if p.opts.IgnoreFn != nil && p.opts.IgnoreFn(rel) {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil || info == nil {
			return nil
		}
		// 基本判断：文件大小 < 1MB 且不在扩展黑名单，继续；大文件先标 binary
		isBin := false
		size := info.Size()
		if size > 0 && size <= 16*1024 {
			// 读前 16KB 检测 binary
			buf := p.pagePool.Get()
			f, openErr := os.Open(full)
			if openErr == nil {
				nRead, _ := f.Read(*buf)
				f.Close()
				isBin = core.DetectBinaryContent((*buf)[:nRead])
			}
			p.pagePool.Put(buf)
		} else if size > 1024*1024 {
			// 大于 1MB 默认标为 binary（减少搜索噪声）
			isBin = true
		}

		// 创建 FileItem
		p.fileMu.Lock()
		cp := p.arena.Intern(rel)
		fi := core.FileItem{
			Size:                      uint64(size),
			Modified:                  uint64(info.ModTime().Unix()),
			AccessFrecencyScore:       0,
			ModificationFrecencyScore: 0,
			ParentDirIndex:            0,
			Path:                      cp,
			Flags:                     0,
			GitStatusPtr:              nil,
		}
		if isBin {
			fi.SetBinary(true)
		}
		// 根据 modified 计算 mod frecency
		fi.ModificationFrecencyScore = p.frec.GetModificationScore(int64(fi.Modified), false)
		p.files = append(p.files, fi)
		relPaths = append(relPaths, p.arena.Get(cp)) // 相对路径
		p.fileMu.Unlock()

		return nil
	})
	if err != nil {
		return err
	}

	// 构建 bigram 索引
	if len(relPaths) > 0 {
		p.bg.Build(relPaths)
	}

	p.scanned.Store(true)
	return nil
}

// ================================================================
// Search: fuzzy 模糊搜索 + bigram 预过滤 + 排序
// ================================================================

const virtualDeletedIndex = ^uint32(0)

type searchPlan struct {
	query       string
	parsed      queryparser.FFFQuery
	fuzzyText   string
	fuzzyLower  string
	tokens      []string
	gitStatuses map[string]core.GitStatusKind
}

type scoredResult struct {
	idx      uint32
	score    int32
	path     string
	modified uint64
}

// Search 返回 top-N 结果。query 支持 "hello type:go size:>10KB modified:7d" 格式。
func (p *Picker) Search(query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return nil, nil
	}
	if err := p.ensureScanned(); err != nil {
		return nil, err
	}

	plan, err := p.prepareSearch(query)
	if err != nil {
		return nil, err
	}

	p.fileMu.RLock()
	defer p.fileMu.RUnlock()
	n := len(p.files)
	if n == 0 && plan.gitStatuses == nil {
		return nil, nil
	}

	candidates := p.searchCandidates(plan.fuzzyText, n)
	scoredBuf := p.scoreCandidates(candidates, plan)
	scoredBuf = append(scoredBuf, p.scoreDeletedGitCandidates(plan)...)
	p.sortScored(scoredBuf)
	return p.resultsFromScored(scoredBuf, limit), nil
}

func (p *Picker) ensureScanned() error {
	if p.scanned.Load() {
		return nil
	}
	return p.Scan()
}

func (p *Picker) prepareSearch(query string) (searchPlan, error) {
	parsed := queryparser.New().Parse(query)
	fuzzyText := normalizeFuzzy(parsed.Fuzzy)
	gitStatuses, err := p.gitStatusesForQuery(parsed.Constraints)
	if err != nil {
		return searchPlan{}, err
	}
	fuzzyLower := strings.ToLower(fuzzyText)
	return searchPlan{
		query:       query,
		parsed:      parsed,
		fuzzyText:   fuzzyText,
		fuzzyLower:  fuzzyLower,
		tokens:      strings.Fields(fuzzyLower),
		gitStatuses: gitStatuses,
	}, nil
}

func (p *Picker) searchCandidates(fuzzyText string, n int) []uint32 {
	if fuzzyText == "" {
		return makeRange(n)
	}
	main := p.bg.Candidates(fuzzyText)
	extra := p.overlay.Candidates(fuzzyText)
	if len(main) == 0 && len(extra) == 0 {
		return makeRange(n)
	}
	return append(main, extra...)
}

func (p *Picker) scoreCandidates(candidates []uint32, plan searchPlan) []scoredResult {
	n := len(p.files)
	scoredBuf := make([]scoredResult, 0, len(candidates))
	for _, idx := range candidates {
		if int(idx) >= n {
			continue
		}
		f := &p.files[idx]
		if f.IsDeleted() {
			continue
		}

		// 应用约束过滤（extension/type/size/modified/glob/pathsegment/not）
		relPath := p.arena.Get(f.Path)
		if !matchesConstraints(relPath, f, plan.parsed.Constraints, p.nowFn, plan.gitStatuses) {
			continue
		}

		// 基础分来自 TotalFrecency
		base := f.TotalFrecency()

		fuzzy := scoreFuzzyPath(relPath, plan, f.IsBinary())

		// combo boost
		combo := int32(p.qt.ComboBoost(plan.query, relPath))

		total := base + fuzzy + combo
		scoredBuf = append(scoredBuf, scoredResult{
			idx:      idx,
			score:    total,
			modified: f.Modified,
		})
	}
	return scoredBuf
}

func scoreFuzzyPath(relPath string, plan searchPlan, binary bool) int32 {
	if plan.fuzzyLower == "" {
		return 0
	}
	relLower := strings.ToLower(relPath)
	fuzzy := int32(0)
	if strings.Contains(relLower, plan.fuzzyLower) {
		fuzzy = 100
	} else if len(plan.tokens) > 1 {
		matched := 0
		for _, tok := range plan.tokens {
			if strings.Contains(relLower, tok) {
				matched++
			}
		}
		if matched == len(plan.tokens) {
			fuzzy = 60
		} else if matched > 0 {
			fuzzy = int32(10 * matched)
		}
	} else if containsAllTokens(relLower, plan.tokens) {
		fuzzy = 40
	}
	if binary {
		fuzzy -= 20
	}
	return fuzzy
}

func containsAllTokens(relLower string, tokens []string) bool {
	for _, tok := range tokens {
		if !strings.Contains(relLower, tok) {
			return false
		}
	}
	return true
}

func (p *Picker) scoreDeletedGitCandidates(plan searchPlan) []scoredResult {
	if plan.gitStatuses == nil {
		return nil
	}
	if !supportsVirtualDeletedConstraints(plan.parsed.Constraints) {
		return nil
	}
	scannedPaths := make(map[string]struct{}, len(p.files))
	for i := range p.files {
		scannedPaths[filepath.ToSlash(p.arena.Get(p.files[i].Path))] = struct{}{}
	}
	var deleted []scoredResult
	for relPath, status := range plan.gitStatuses {
		if status != core.GitStatusDeleted {
			continue
		}
		if _, ok := scannedPaths[filepath.ToSlash(relPath)]; ok {
			continue
		}
		f := core.FileItem{}
		if !matchesConstraints(relPath, &f, plan.parsed.Constraints, p.nowFn, plan.gitStatuses) {
			continue
		}
		fuzzy := scoreFuzzyPath(relPath, plan, false)
		combo := int32(p.qt.ComboBoost(plan.query, relPath))
		deleted = append(deleted, scoredResult{
			idx:   virtualDeletedIndex,
			score: fuzzy + combo,
			path:  filepath.Join(p.root, relPath),
		})
	}
	return deleted
}

func (p *Picker) sortScored(scoredBuf []scoredResult) {
	sort.SliceStable(scoredBuf, func(i, j int) bool {
		if scoredBuf[i].score != scoredBuf[j].score {
			return scoredBuf[i].score > scoredBuf[j].score
		}
		if scoredBuf[i].modified != scoredBuf[j].modified {
			return scoredBuf[i].modified > scoredBuf[j].modified
		}
		return p.scoredPath(scoredBuf[i]) < p.scoredPath(scoredBuf[j])
	})
}

func (p *Picker) scoredPath(s scoredResult) string {
	if s.path != "" {
		return s.path
	}
	if int(s.idx) >= len(p.files) {
		return ""
	}
	return filepath.Join(p.root, p.arena.Get(p.files[s.idx].Path))
}

func (p *Picker) resultsFromScored(scoredBuf []scoredResult, limit int) []Result {
	if len(scoredBuf) > limit {
		scoredBuf = scoredBuf[:limit]
	}
	out := make([]Result, len(scoredBuf))
	for i, s := range scoredBuf {
		out[i] = Result{idx: s.idx, score: s.score, p: p, path: s.path}
	}
	return out
}

// Result 的只读访问器
func (r Result) Path() string {
	if r.path != "" {
		return r.path
	}
	if r.p == nil {
		return ""
	}
	r.p.fileMu.RLock()
	defer r.p.fileMu.RUnlock()
	if int(r.idx) >= len(r.p.files) {
		return ""
	}
	rel := r.p.arena.Get(r.p.files[r.idx].Path)
	return filepath.Join(r.p.root, rel)
}

func (r Result) Score() int32 { return r.score }
func (r Result) Index() int {
	if r.idx == virtualDeletedIndex {
		return -1
	}
	return int(r.idx)
}

// Close 释放资源
func (p *Picker) Close() error {
	if p == nil {
		return nil
	}
	if p.frec != nil {
		_ = p.frec.Close()
	}
	if p.qt != nil {
		_ = p.qt.Close()
	}
	return nil
}

// makeRange 返回 [0, n)
func makeRange(n int) []uint32 {
	out := make([]uint32, n)
	for i := range out {
		out[i] = uint32(i)
	}
	return out
}

// normalizeFuzzy 把 FuzzyQuery 展平为字符串
func normalizeFuzzy(fq queryparser.FuzzyQuery) string {
	switch fq.Kind {
	case queryparser.FuzzyText:
		return fq.Text
	case queryparser.FuzzyParts:
		return strings.Join(fq.Parts, " ")
	}
	return ""
}

// matchesConstraints 检查文件是否匹配所有约束（AND 语义）
func matchesConstraints(relPath string, f *core.FileItem, constraints []queryparser.Constraint, nowFn func() time.Time, gitStatuses map[string]core.GitStatusKind) bool {
	for _, c := range constraints {
		if !matchOne(relPath, f, c, nowFn, gitStatuses) {
			return false
		}
	}
	return true
}

func matchOne(relPath string, f *core.FileItem, c queryparser.Constraint, nowFn func() time.Time, gitStatuses map[string]core.GitStatusKind) bool {
	switch c.Kind {
	case queryparser.CNot:
		if c.Child == nil {
			return true
		}
		if isNoopConstraint(*c.Child) {
			return true
		}
		return !matchOne(relPath, f, *c.Child, nowFn, gitStatuses)
	case queryparser.CExtension:
		// *.go -> 检查 ext
		ext := strings.ToLower(filepath.Ext(relPath))
		want := "." + strings.ToLower(strings.TrimPrefix(c.Value, "."))
		return ext == want
	case queryparser.CFileType:
		// type:go -> 映射到扩展名集合
		exts := queryparser.ExtensionFor(c.Value)
		if len(exts) == 0 {
			return true // 未知语言名：不滤
		}
		ext := strings.ToLower(filepath.Ext(relPath))
		for _, e := range exts {
			if ext == e {
				return true
			}
		}
		return false
	case queryparser.CSizeCmp:
		size := int64(f.Size)
		switch c.SizeOp {
		case queryparser.SizeEq:
			return size == c.SizeBytes
		case queryparser.SizeGt:
			return size > c.SizeBytes
		case queryparser.SizeLt:
			return size < c.SizeBytes
		case queryparser.SizeGte:
			return size >= c.SizeBytes
		case queryparser.SizeLte:
			return size <= c.SizeBytes
		default:
			return true
		}
	case queryparser.CGitStatus:
		if !isKnownGitStatusValue(c.Value) {
			return true
		}
		return matchGitStatus(relPath, c.Value, gitStatuses)
	case queryparser.CModifiedAgo:
		secs, ok := parseDur(c.Value)
		if !ok {
			return true
		}
		var now int64
		if nowFn != nil {
			now = nowFn().Unix()
		} else {
			now = time.Now().Unix()
		}
		return int64(f.Modified) >= now-secs && int64(f.Modified) <= now
	case queryparser.CPathSegment:
		// 在路径前后加上前导斜杠，确保匹配任意位置
		// 把 path 标准化为 /path/to/file 格式（添加前导和尾随斜杠）
		padded := "/" + strings.ToLower(relPath) + "/"
		seg := "/" + strings.ToLower(c.Value) + "/"
		return strings.Contains(padded, seg)
	case queryparser.CGlob:
		return matchGlob(c.Value, relPath)
	case queryparser.CText:
		// 纯文本约束：要求字符串包含在 relPath 中
		if c.Value == "" {
			return true
		}
		return strings.Contains(strings.ToLower(relPath), strings.ToLower(c.Value))
	}
	return true // 未知类型：不滤
}

func isNoopConstraint(c queryparser.Constraint) bool {
	switch c.Kind {
	case queryparser.CGitStatus:
		return !isKnownGitStatusValue(c.Value)
	case queryparser.CFileType:
		return len(queryparser.ExtensionFor(c.Value)) == 0
	case queryparser.CModifiedAgo:
		_, ok := parseDur(c.Value)
		return !ok
	}
	return false
}

func supportsVirtualDeletedConstraints(constraints []queryparser.Constraint) bool {
	for _, c := range constraints {
		if !supportsVirtualDeletedConstraint(c) {
			return false
		}
	}
	return true
}

func supportsVirtualDeletedConstraint(c queryparser.Constraint) bool {
	switch c.Kind {
	case queryparser.CNot:
		if c.Child == nil {
			return true
		}
		return supportsVirtualDeletedConstraint(*c.Child)
	case queryparser.CSizeCmp, queryparser.CModifiedAgo:
		return false
	default:
		return true
	}
}

// matchGlob 支持 "**/*.go" / "src/**/*.rs" 这样的 glob。** 匹配任意层级目录。
func matchGlob(pattern, path string) bool {
	lowerPat := strings.ToLower(pattern)
	lowerPath := strings.ToLower(filepath.ToSlash(path))

	if !strings.Contains(lowerPat, "**") {
		ok, err := filepath.Match(lowerPat, lowerPath)
		return err == nil && ok
	}
	return matchGlobSegments(splitGlobPath(lowerPat), splitGlobPath(lowerPath))
}

func splitGlobPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return []string{}
	}
	return strings.Split(trimmed, "/")
}

func matchGlobSegments(patternParts, pathParts []string) bool {
	memo := make(map[[2]int]bool)
	var match func(patternIdx, pathIdx int) bool
	match = func(patternIdx, pathIdx int) bool {
		key := [2]int{patternIdx, pathIdx}
		if ok, cached := memo[key]; cached {
			return ok
		}
		var ok bool
		defer func() { memo[key] = ok }()

		if patternIdx == len(patternParts) {
			ok = pathIdx == len(pathParts)
			return ok
		}
		if patternParts[patternIdx] == "**" {
			if match(patternIdx+1, pathIdx) {
				ok = true
				return ok
			}
			for i := pathIdx; i < len(pathParts); i++ {
				if match(patternIdx+1, i+1) {
					ok = true
					return ok
				}
			}
			return false
		}
		if pathIdx == len(pathParts) {
			return false
		}
		matched, err := filepath.Match(patternParts[patternIdx], pathParts[pathIdx])
		if err != nil || !matched {
			return false
		}
		ok = match(patternIdx+1, pathIdx+1)
		return ok
	}
	return match(0, 0)
}

func (p *Picker) gitStatusesForQuery(constraints []queryparser.Constraint) (map[string]core.GitStatusKind, error) {
	if !hasGitStatusConstraint(constraints) {
		return nil, nil
	}
	statuses, err := loadGitStatuses(p.root)
	if err != nil {
		return nil, fmt.Errorf("load git status: %w", err)
	}
	return statuses, nil
}

func hasGitStatusConstraint(constraints []queryparser.Constraint) bool {
	for _, c := range constraints {
		if c.Kind == queryparser.CGitStatus && isKnownGitStatusValue(c.Value) {
			return true
		}
		if c.Kind == queryparser.CNot && c.Child != nil && hasGitStatusConstraint([]queryparser.Constraint{*c.Child}) {
			return true
		}
	}
	return false
}

func isKnownGitStatusValue(value string) bool {
	switch strings.ToLower(value) {
	case "modified", "added", "deleted", "renamed", "untracked", "clean", "dirty":
		return true
	default:
		return false
	}
}

func loadGitStatuses(root string) (map[string]core.GitStatusKind, error) {
	cmd := exec.Command("git", "-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git status --porcelain failed in %q: %w: %s", root, err, strings.TrimSpace(string(out)))
	}
	statuses := make(map[string]core.GitStatusKind)
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if len(field) < 4 {
			continue
		}
		kind, ok := parseGitPorcelainKind(field[0], field[1])
		if !ok {
			continue
		}
		pathField := string(field[3:])
		if isRenameOrCopy(field[0], field[1]) && i+1 < len(fields) {
			i++
		}
		statuses[filepath.ToSlash(pathField)] = kind
	}
	return statuses, nil
}

func isRenameOrCopy(index, worktree byte) bool {
	return index == 'R' || worktree == 'R' || index == 'C' || worktree == 'C'
}

func parseGitPorcelainKind(index, worktree byte) (core.GitStatusKind, bool) {
	if index == '?' && worktree == '?' {
		return core.GitStatusUntracked, true
	}
	if index == 'R' || worktree == 'R' {
		return core.GitStatusRenamed, true
	}
	if index == 'D' || worktree == 'D' {
		return core.GitStatusDeleted, true
	}
	if index == 'A' || worktree == 'A' {
		return core.GitStatusAdded, true
	}
	if index == 'M' || worktree == 'M' {
		return core.GitStatusModified, true
	}
	return core.GitStatusClean, false
}

func matchGitStatus(relPath, want string, statuses map[string]core.GitStatusKind) bool {
	// 当没有加载 git status 数据时，status 约束不做过滤（由 caller 负责控制调用时机）。
	// 例如虚拟 deleted 文件的约束检查中，caller 会确保此时不包含其他需要文件元数据的约束。
	if statuses == nil {
		return true
	}
	status, dirty := statuses[filepath.ToSlash(relPath)]
	if !dirty {
		status = core.GitStatusClean
	}
	want = strings.ToLower(want)
	switch want {
	case "dirty":
		return core.GitStatus{Kind: status}.IsDirty()
	case "modified", "added", "deleted", "renamed", "untracked", "clean":
		return status.String() == want
	default:
		return true
	}
}

// parseDur 解析 "7d" / "24h" 为秒
func parseDur(v string) (int64, bool) {
	return queryparser.ParseDurationAgo(v)
}
