// Package grep 是轻量级行内文本匹配器。
// 设计目标：
//   - 子串 / 多 token 模糊匹配
//   - 跳过 binary 文件（前 16KB 含 NUL 字节）
//   - 并发扫描多文件（GrepMany）
//   - 大文件限制 MaxBytes 上限
package grep

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/superops-team/fg/core"
)

// MatchRange 表示单个行内 match 的 [start,end) 字节区间
type MatchRange struct {
	Start int
	End   int
}

// LineResult 表示单行匹配结果
type LineResult struct {
	Lineno  int          // 1-based
	Text    string       // 行内容（不含换行）
	Matches []MatchRange // 该行内所有 match 区间
}

// FileResult 表示单文件的匹配结果
type FileResult struct {
	Path  string
	Lines []LineResult
}

type indexedFileResult struct {
	idx int
	FileResult
}

type indexedError struct {
	idx int
	err error
}

// Options 控制 GrepMatcher 行为
type Options struct {
	CaseInsensitive bool // 大小写不敏感（默认：查询不含大写字母时自动不敏感）
	IncludeBinary   bool // 是否也搜索 binary 文件（默认跳过）
	MaxBytes        int  // 单文件最大扫描字节（默认 2MB）
	Concurrency     int  // GrepMany 并发数（默认 4）
}

// GrepMatcher 是线程安全的匹配器
type GrepMatcher struct {
	opts Options
	pool *core.PagePool
}

// New 返回一个 GrepMatcher
func New(opts Options) *GrepMatcher {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = 2 * 1024 * 1024 // 2MB
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	return &GrepMatcher{
		opts: opts,
		pool: core.NewPagePool(16 * 1024),
	}
}

// SearchFile 在单文件内匹配 query。返回命中行（最多 limit 行。
func (g *GrepMatcher) SearchFile(path, query string, limit int) ([]LineResult, error) {
	return g.SearchFileContext(context.Background(), path, query, limit)
}

func (g *GrepMatcher) SearchFileContext(ctx context.Context, path, query string, limit int) ([]LineResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if query == "" || limit <= 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// 先读前 16KB 判断是否为 binary
	headBuf := g.pool.Get()
	n, readErr := f.Read(*headBuf)
	_, seekErr := f.Seek(0, io.SeekStart)
	isBinary := core.DetectBinaryContent((*headBuf)[:n])
	g.pool.Put(headBuf)
	if isBinary && !g.opts.IncludeBinary {
		return nil, nil
	}
	if readErr != nil && n == 0 {
		return nil, fmt.Errorf("read %s: %w", path, readErr)
	}
	if seekErr != nil {
		return nil, fmt.Errorf("seek %s: %w", path, seekErr)
	}

	// 解析 tokens（支持 "foo bar" -> ["foo","bar"]）
	caseInsensitive := g.opts.CaseInsensitive || isAllLower(query)
	var tokens []string
	if caseInsensitive {
		tokens = strings.Fields(strings.ToLower(query))
	} else {
		tokens = strings.Fields(query)
	}
	if len(tokens) == 0 {
		return nil, nil
	}

	results := make([]LineResult, 0, 8)
	if err := g.searchReaderLinesContext(ctx, f, tokens, caseInsensitive, limit, &results); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return results, err
	}
	return results, nil
}

func (g *GrepMatcher) searchReaderLines(r io.Reader, tokens []string, caseInsensitive bool, limit int, results *[]LineResult) error {
	return g.searchReaderLinesContext(context.Background(), r, tokens, caseInsensitive, limit, results)
}

func (g *GrepMatcher) searchReaderLinesContext(ctx context.Context, r io.Reader, tokens []string, caseInsensitive bool, limit int, results *[]LineResult) error {
	reader := bufio.NewReader(r)
	totalRead := 0
	lineno := 0
	for len(*results) < limit && totalRead < g.opts.MaxBytes {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readAny, err := readLinePrefix(reader, g.opts.MaxBytes-totalRead)
		if err != nil {
			return err
		}
		if !readAny {
			break
		}
		totalRead += len(line)
		lineno++
		line = trimLineEnding(line)
		g.searchLine(lineno, line, tokens, caseInsensitive, results)
	}
	return nil
}

func readLinePrefix(reader *bufio.Reader, remaining int) (string, bool, error) {
	if remaining <= 0 {
		return "", false, nil
	}
	line := make([]byte, 0, min(remaining, 64*1024))
	for remaining > 0 {
		fragment, err := reader.ReadSlice('\n')
		if len(fragment) > remaining {
			fragment = fragment[:remaining]
		}
		line = append(line, fragment...)
		remaining -= len(fragment)
		if err == nil {
			return string(line), true, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			return string(line), len(line) > 0, nil
		}
		return string(line), len(line) > 0, err
	}
	return string(line), len(line) > 0, nil
}

func trimLineEnding(line string) string {
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line
}

func (g *GrepMatcher) searchLine(lineno int, line string, tokens []string, caseInsensitive bool, results *[]LineResult) {
	// 归一化用于匹配
	hay := line
	if caseInsensitive {
		hay = strings.ToLower(line)
	}
	// 多 token 模式：所有 token 必须都在该行里
	allMatched := true
	var allMatches []MatchRange
	for _, tok := range tokens {
		// 在归一化后的 hay 里找所有出现（多 token 每个 token 全包含才命中，但 match 区间记录的是首次出现位置）
		// 如果 tok 多次出现也要记录多次（用于高亮）——但仅单 token 情况记录多次出现
		// 检查 tok 是否在 hay 里
		if !strings.Contains(hay, tok) {
			allMatched = false
			break
		}
		// 单 token 模式（tokens=1）：记录所有出现
		// 多 token 模式：记录首次出现
		if len(tokens) == 1 {
			allMatches = append(allMatches, findAllMatches(line, tok, caseInsensitive)...)
		} else {
			// 多 token：记录 token 在原始字符串的首次出现
			match := findFirstMatch(line, tok, caseInsensitive)
			if match.Start >= 0 {
				allMatches = append(allMatches, match)
			}
		}
	}
	if allMatched && len(tokens) > 0 {
		*results = append(*results, LineResult{Lineno: lineno, Text: line, Matches: allMatches})
	}
}

func findAllMatches(line, tok string, caseInsensitive bool) []MatchRange {
	var matches []MatchRange
	start := 0
	for start < len(line) {
		match := findFirstMatch(line[start:], tok, caseInsensitive)
		if match.Start < 0 {
			break
		}
		match.Start += start
		match.End += start
		matches = append(matches, match)
		start = match.End
	}
	return matches
}

func findFirstMatch(line, tok string, caseInsensitive bool) MatchRange {
	if !caseInsensitive {
		idx := strings.Index(line, tok)
		if idx < 0 {
			return MatchRange{Start: -1, End: -1}
		}
		return MatchRange{Start: idx, End: idx + len(tok)}
	}
	start, end := findCaseInsensitiveRange(line, tok)
	return MatchRange{Start: start, End: end}
}

// SearchMany 并发在多个文件里搜索，返回每个文件的结果（空结果不返回）。
func (g *GrepMatcher) SearchMany(paths []string, query string, limitPerFile int) ([]FileResult, error) {
	return g.SearchManyContext(context.Background(), paths, query, limitPerFile)
}

func (g *GrepMatcher) SearchManyContext(ctx context.Context, paths []string, query string, limitPerFile int) ([]FileResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if query == "" || limitPerFile <= 0 || len(paths) == 0 {
		return nil, nil
	}

	sem := make(chan struct{}, g.opts.Concurrency)
	var wg sync.WaitGroup
	resultsCh := make(chan indexedFileResult, len(paths))
	errsCh := make(chan indexedError, len(paths))

	for i, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		wg.Add(1)
		go func(idx int, path string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				errsCh <- indexedError{idx: idx, err: ctx.Err()}
				return
			}
			defer func() { <-sem }() // release
			lines, err := g.SearchFileContext(ctx, path, query, limitPerFile)
			if err != nil {
				errsCh <- indexedError{idx: idx, err: fmt.Errorf("%s: %w", path, err)}
				return
			}
			if len(lines) == 0 {
				return
			}
			resultsCh <- indexedFileResult{idx: idx, FileResult: FileResult{Path: path, Lines: lines}}
		}(i, p)
	}
	wg.Wait()
	close(resultsCh)
	close(errsCh)

	indexedResults := make([]indexedFileResult, 0, len(paths))
	for fr := range resultsCh {
		indexedResults = append(indexedResults, fr)
	}
	sort.Slice(indexedResults, func(i, j int) bool { return indexedResults[i].idx < indexedResults[j].idx })
	out := make([]FileResult, 0, len(indexedResults))
	for _, fr := range indexedResults {
		out = append(out, fr.FileResult)
	}
	indexedErrs := make([]indexedError, 0)
	for err := range errsCh {
		indexedErrs = append(indexedErrs, err)
	}
	sort.Slice(indexedErrs, func(i, j int) bool { return indexedErrs[i].idx < indexedErrs[j].idx })
	errs := make([]error, 0, len(indexedErrs))
	for _, indexedErr := range indexedErrs {
		errs = append(errs, indexedErr.err)
	}
	joinedErr := errors.Join(errs...)
	if errors.Is(joinedErr, context.Canceled) || errors.Is(joinedErr, context.DeadlineExceeded) {
		return nil, joinedErr
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return out, joinedErr
}

// ================================================================
// helpers
// ================================================================

// isAllLower 判断是否全部是小写/数字/符号（没有大写字母）
func isAllLower(s string) bool {
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			return false
		}
	}
	return true
}

func findCaseInsensitiveRange(line, tok string) (int, int) {
	if tok == "" {
		return -1, -1
	}
	// 快速路径：精确子串匹配
	if idx := strings.Index(line, tok); idx >= 0 {
		return idx, idx + len(tok)
	}
	lowerTok := strings.ToLower(tok)
	// 在 line 的每个 rune 起点检查小写后的前缀是否匹配 lowerTok。
	// 从每个 rune 起点开始，按 rune 扩展 end，直到累积的小写字节数 >= len(lowerTok)。
	// 使用 utf8.RuneCountInString 确保在处理前对齐 rune 边界，避免 Unicode 大小写折叠改变字节长度。
	// 策略：先在归一化的整行里找到 lowerTok 的位置（忽略字节长度变化），再映射回原 line 的字节位置。
	tokRuneCount := utf8.RuneCountInString(tok)
	// 直接按 rune 边界扫描 line，对每个起点截取 tokRuneCount 个 rune 后比较。
	for i := 0; i < len(line); {
		// 从位置 i 开始，收集 tokRuneCount 个 rune 的字节区间
		end := i
		runeCount := 0
		for runeCount < tokRuneCount && end < len(line) {
			_, size := utf8.DecodeRuneInString(line[end:])
			if size <= 0 {
				size = 1
			}
			end += size
			runeCount++
		}
		if runeCount < tokRuneCount {
			// 剩余不足 tokRuneCount 个 rune，结束
			break
		}
		if strings.ToLower(line[i:end]) == lowerTok {
			return i, end
		}
		// 前进到下一个 rune 起点
		_, size := utf8.DecodeRuneInString(line[i:])
		if size <= 0 {
			size = 1
		}
		i += size
	}
	return -1, -1
}
