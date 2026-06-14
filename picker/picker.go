// Package picker 是 fff 的核心搜索路径：目录扫描 + bigram 预过滤 + frecency/querytracker 排序。
package picker

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yourname/fg/bigram"
	"github.com/yourname/fg/core"
	"github.com/yourname/fg/frecency"
	"github.com/yourname/fg/queryparser"
	"github.com/yourname/fg/querytracker"
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
	root       string
	opts       Options

	arena      *core.PathArena
	files      []core.FileItem
	fileMu     sync.RWMutex          // 保护 files/dirs 的并发访问（Scan 期间写入）
	dirs       []core.DirItem

	bg         *bigram.Bigram
	overlay    *bigram.BigramOverlay

	frec       *frecency.FrecencyTracker
	qt         *querytracker.QueryTracker

	nowFn      func() time.Time
	scanned    atomic.Bool           // Scan 完成标志

	// 静态 buffer 池
	pagePool   *core.PagePool
}

// Result 表示单条搜索结果
type Result struct {
	idx   uint32
	score int32
	p     *Picker
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
		dirs:     make([]core.DirItem, 0, 64),
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
	p.dirs = p.dirs[:0]
	p.fileMu.Unlock()

	// 收集相对路径
	var relPaths []string

	err := filepath.WalkDir(p.root, func(full string, d os.DirEntry, err error) error {
		if err != nil {
			return err
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
			// 目录也加入 dirs
			p.fileMu.Lock()
			di := core.DirItem{Path: p.arena.Intern(rel), MaxAccessFrecency: 0}
			p.dirs = append(p.dirs, di)
			p.fileMu.Unlock()
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

// Search 返回 top-N 结果。query 支持 "hello type:go size:>10KB modified:7d" 格式。
func (p *Picker) Search(query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return nil, nil
	}
	// 没扫描：先扫一次（方便使用）
	if !p.scanned.Load() {
		if err := p.Scan(); err != nil {
			return nil, err
		}
	}

	// 解析 query 为 fuzzy text + constraints
	qp := queryparser.New()
	parsed := qp.Parse(query)
	fuzzyText := normalizeFuzzy(parsed.Fuzzy)

	p.fileMu.RLock()
	defer p.fileMu.RUnlock()
	n := len(p.files)
	if n == 0 {
		return nil, nil
	}

	// 候选集：如果 fuzzy 有意义，用 bigram；否则全量
	var candidates []uint32
	if fuzzyText != "" {
		main := p.bg.Candidates(fuzzyText)
		extra := p.overlay.Candidates(fuzzyText)
		if len(main) == 0 && len(extra) == 0 {
			candidates = makeRange(n)
		} else {
			candidates = append(main, extra...)
		}
	} else {
		candidates = makeRange(n)
	}

	// 评分 + 排序
	type scored struct {
		idx   uint32
		score int32
	}
	scoredBuf := make([]scored, 0, len(candidates))
	fuzzyLower := strings.ToLower(fuzzyText)
	tokens := strings.Fields(fuzzyLower)

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
		if !matchesConstraints(relPath, f, parsed.Constraints) {
			continue
		}

		// 基础分来自 TotalFrecency
		base := f.TotalFrecency()

		// fuzzy 匹配分
		fuzzy := int32(0)
		if fuzzyLower != "" {
			relLower := strings.ToLower(relPath)
			// 完整子串（最强）
			if strings.Contains(relLower, fuzzyLower) {
				fuzzy = 100
			} else if len(tokens) > 1 {
				// 多 token：全部包含
				allContain := true
				for _, tok := range tokens {
					if !strings.Contains(relLower, tok) {
						allContain = false
						break
					}
				}
				if allContain {
					fuzzy = 60
				} else {
					// 部分命中
					matched := 0
					for _, tok := range tokens {
						if strings.Contains(relLower, tok) {
							matched++
						}
					}
					if matched > 0 {
						fuzzy = int32(10 * matched)
					}
				}
			} else {
				// 单 token 且未命中完整子串
				allContain := true
				for _, tok := range tokens {
					if !strings.Contains(relLower, tok) {
						allContain = false
						break
					}
				}
				if allContain {
					fuzzy = 40
				}
			}
			if f.IsBinary() {
				fuzzy -= 20
			}
		}

		// combo boost
		combo := int32(p.qt.ComboBoost(query, relPath))

		total := base + fuzzy + combo
		scoredBuf = append(scoredBuf, scored{idx: idx, score: total})
	}

	// 排序：score desc, modified desc, path lex asc
	sort.SliceStable(scoredBuf, func(i, j int) bool {
		if scoredBuf[i].score != scoredBuf[j].score {
			return scoredBuf[i].score > scoredBuf[j].score
		}
		fi := p.files[scoredBuf[i].idx]
		fj := p.files[scoredBuf[j].idx]
		if fi.Modified != fj.Modified {
			return fi.Modified > fj.Modified
		}
		return p.arena.Get(fi.Path) < p.arena.Get(fj.Path)
	})

	if len(scoredBuf) > limit {
		scoredBuf = scoredBuf[:limit]
	}

	out := make([]Result, len(scoredBuf))
	for i, s := range scoredBuf {
		out[i] = Result{idx: s.idx, score: s.score, p: p}
	}
	return out, nil
}

// Result 的只读访问器
func (r Result) Path() string {
	if r.p == nil || int(r.idx) >= r.p.FileCount() {
		return ""
	}
	rel := ""
	r.p.fileMu.RLock()
	if int(r.idx) < len(r.p.files) {
		rel = r.p.arena.Get(r.p.files[r.idx].Path)
	}
	r.p.fileMu.RUnlock()
	return filepath.Join(r.p.root, rel)
}

func (r Result) Score() int32 { return r.score }
func (r Result) Index() int   { return int(r.idx) }

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
func matchesConstraints(relPath string, f *core.FileItem, constraints []queryparser.Constraint) bool {
	for _, c := range constraints {
		if !matchOne(relPath, f, c) {
			return false
		}
	}
	return true
}

func matchOne(relPath string, f *core.FileItem, c queryparser.Constraint) bool {
	switch c.Kind {
	case queryparser.CNot:
		if c.Child == nil {
			return true
		}
		return !matchOne(relPath, f, *c.Child)
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
		}
	case queryparser.CModifiedAgo:
		// modified:7d —— 文件在 7 天内被修改
		secs, ok := parseDur(c.Value)
		if !ok {
			return true
		}
		now := time.Now().Unix()
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

// matchGlob 支持 "**/*.go" / "src/**/*.rs" 这样的 glob。** 匹配任意层级目录。
// 简化实现：把 ** 替换为一个特殊匹配，其余用 filepath.Match 语义。
func matchGlob(pattern, path string) bool {
	lowerPat := strings.ToLower(pattern)
	lowerPath := strings.ToLower(filepath.ToSlash(path))

	// 如果没有 **，直接用 filepath.Match
	if !strings.Contains(lowerPat, "**") {
		ok, err := filepath.Match(lowerPat, lowerPath)
		return err == nil && ok
	}

	// 有 **：用 greedy 分段匹配
	// 把 pattern 按 ** 分割成片段 [prefix, middle..., suffix]
	// 每段必须在 path 中按顺序出现，每段之间匹配任意内容
	parts := strings.Split(lowerPat, "**")
	pos := 0

	for i, part := range parts {
		if part == "" {
			continue
		}
		// 去掉 part 可能的开头 /
		part = strings.TrimLeft(part, "/")
		if i == 0 {
			// 开头片段：必须匹配 path 的前缀（到下一个 /）
			// 用 '/' 分割检查第一段是否匹配
			pathParts := strings.SplitN(lowerPath[pos:], "/", 2)
			// 第一段可能含多个组件；先尝试整个剩余路径匹配 part
			// 简化：从 pos 开始在 path 中找 part 的匹配
			ok, _ := filepath.Match(part, pathParts[0])
			if !ok {
				// 第一段也可能是带 / 的复合模式，尝试 "part/x" 更长
				// 简化：如果 part 不是路径，直接尝试在 path 尾部找
				if strings.Contains(part, "/") {
					// 前缀多段匹配：取前 N 个组件
					segCount := strings.Count(part, "/") + 1
					pathSegs := strings.SplitN(lowerPath[pos:], "/", segCount+1)
					if len(pathSegs) < segCount {
						return false
					}
					joined := strings.Join(pathSegs[:segCount], "/")
					ok, _ := filepath.Match(part, joined)
					if !ok {
						return false
					}
					pos += len(joined) + 1
					if pos > len(lowerPath) {
						pos = len(lowerPath)
					}
					continue
				}
				return false
			}
			pos += len(pathParts[0]) + 1
			if pos > len(lowerPath) {
				pos = len(lowerPath)
			}
			continue
		}
		if i == len(parts)-1 {
			// 最后一段：必须匹配 path 尾部（后缀）
			remainder := lowerPath[pos:]
			// part 可能是 "*.go" 这样的模式，匹配路径末尾
			// 尝试：找到最后一个 '/' 之后的部分匹配 part
			lastSlash := strings.LastIndex(remainder, "/")
			var tail string
			if lastSlash >= 0 {
				tail = remainder[lastSlash+1:]
			} else {
				tail = remainder
			}
			// 如果 part 不含 /，直接匹配文件名
			if !strings.Contains(part, "/") {
				ok, _ := filepath.Match(part, tail)
				return ok
			}
			// 含 / 的后缀：从后向前匹配
			partSegs := strings.Split(part, "/")
			remSegs := strings.Split(remainder, "/")
			if len(partSegs) > len(remSegs) {
				return false
			}
			startIdx := len(remSegs) - len(partSegs)
			matched := true
			for j := range partSegs {
				ok, _ := filepath.Match(partSegs[j], remSegs[startIdx+j])
				if !ok {
					matched = false
					break
				}
			}
			return matched
		}
		// 中间片段：必须在剩余 path 中某位置匹配
		// 简化：跳过到下一个匹配点
		remainder := lowerPath[pos:]
		partSegs := strings.Split(part, "/")
		remSegs := strings.Split(remainder, "/")
		if len(partSegs) > len(remSegs) {
			return false
		}
		found := false
		for startIdx := 0; startIdx <= len(remSegs)-len(partSegs); startIdx++ {
			ok := true
			for j := range partSegs {
				ok2, _ := filepath.Match(partSegs[j], remSegs[startIdx+j])
				if !ok2 {
					ok = false
					break
				}
			}
			if ok {
				// 找到：移动 pos
				joined := strings.Join(remSegs[:startIdx+len(partSegs)], "/")
				pos += len(joined) + 1
				if pos > len(lowerPath) {
					pos = len(lowerPath)
				}
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func splitComponents(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "/")
}

// parseDur 解析 "7d" / "24h" 为秒
func parseDur(v string) (int64, bool) {
	return queryparser.ParseDurationAgo(v)
}


