// Package grep 是轻量级行内文本匹配器。
// 设计目标：
//   - 子串 / 多 token 模糊匹配
//   - 跳过 binary 文件（前 16KB 含 NUL 字节）
//   - 并发扫描多文件（GrepMany）
//   - 大文件限制 MaxBytes 上限
package grep

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/yourname/fg/core"
)

// MatchRange 表示单个行内 match 的 [start,end) 字节区间
type MatchRange struct {
	Start int
	End   int
}

// LineResult 表示单行匹配结果
type LineResult struct {
	Lineno   int          // 1-based
	Text     string       // 行内容（不含换行）
	Matches  []MatchRange // 该行内所有 match 区间
}

// FileResult 表示单文件的匹配结果
type FileResult struct {
	Path  string
	Lines []LineResult
}

// Options 控制 GrepMatcher 行为
type Options struct {
	CaseInsensitive bool // 大小写不敏感（默认：查询不含大写字母时自动不敏感）
	IncludeBinary     bool // 是否也搜索 binary 文件（默认跳过）
	MaxBytes          int  // 单文件最大扫描字节（默认 2MB）
	Concurrency       int  // GrepMany 并发数（默认 4）
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
	n, _ := f.Read(*headBuf)
	// 重置位置
	_, _ = f.Seek(0, io.SeekStart)
	isBinary := core.DetectBinaryContent((*headBuf)[:n])
	g.pool.Put(headBuf)
	if isBinary && !g.opts.IncludeBinary {
		return nil, nil
	}

	// 解析 tokens（支持 "foo bar" -> ["foo","bar"]）
	var tokens []string
	if g.opts.CaseInsensitive || isAllLower(query) {
		// 大小写不敏感处理：归一化为小写
		tokens = strings.Fields(strings.ToLower(query))
	} else {
		tokens = strings.Fields(query)
	}
	if len(tokens) == 0 {
		return nil, nil
	}
	caseInsensitive := g.opts.CaseInsensitive || isAllLower(query)

	// 逐行扫描
	scanner := bufio.NewScanner(f)
	// 大文件限制
	totalRead := 0
	results := make([]LineResult, 0, 8)
	lineno := 0
	for scanner.Scan() && len(results) < limit {
		line := scanner.Text()
		lineno++
		totalRead += len(line) + 1
		if totalRead > g.opts.MaxBytes {
			break
		}
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
				// 找所有出现
				rest := hay
				offset := 0
				for {
					idx := strings.Index(rest, tok)
					if idx < 0 {
						break
					}
					allMatches = append(allMatches, MatchRange{Start: offset + idx, End: offset + idx + len(tok)})
					if idx+len(tok) >= len(rest) {
						break
					}
					rest = rest[idx+len(tok):]
					offset += idx + len(tok)
				}
			} else {
				// 多 token：记录 token 在原始字符串的首次出现
				origIdx := findCaseInsensitive(line, tok)
				if origIdx >= 0 {
					allMatches = append(allMatches, MatchRange{Start: origIdx, End: origIdx + len(tok)})
				}
			}
		}
		if allMatched && len(tokens) > 0 {
			results = append(results, LineResult{Lineno: lineno, Text: line, Matches: allMatches})
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, bufio.ErrTooLong) {
		return results, err
	}
	return results, nil
}

// SearchMany 并发在多个文件里搜索，返回每个文件的结果（空结果不返回）。
func (g *GrepMatcher) SearchMany(paths []string, query string, limitPerFile int) ([]FileResult, error) {
	if query == "" || limitPerFile <= 0 || len(paths) == 0 {
		return nil, nil
	}

	sem := make(chan struct{}, g.opts.Concurrency)
	var wg sync.WaitGroup
	resultsCh := make(chan FileResult, len(paths))

	for _, p := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release
			lines, err := g.SearchFile(path, query, limitPerFile)
			if err != nil {
				return
			}
			if len(lines) == 0 {
				return
			}
			resultsCh <- FileResult{Path: path, Lines: lines}
		}(p)
	}
	wg.Wait()
	close(resultsCh)

	out := make([]FileResult, 0, len(paths))
	for fr := range resultsCh {
		out = append(out, fr)
	}
	return out, nil
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

// findCaseInsensitive 在 line 中查找 tok 的首次出现位置（不区分大小写）。
// 返回 line 中的字节偏移。使用 rune 边界扫描，确保非 ASCII 内容的偏移也正确。
func findCaseInsensitive(line, tok string) int {
	if tok == "" || len(line) < len(tok) {
		return -1
	}
	// 快速路径：精确子串匹配
	if idx := strings.Index(line, tok); idx >= 0 {
		return idx
	}
	lowerTok := strings.ToLower(tok)
	// 在 line 的每个 rune 起点检查小写前缀是否匹配
	for i := 0; i <= len(line)-len(tok); {
		rest := line[i:]
		// 取 rest 前 len(tok) 字节做小写化比较
		checkLen := len(tok)
		if checkLen > len(rest) {
			break
		}
		if strings.HasPrefix(strings.ToLower(rest[:checkLen]), lowerTok) {
			return i
		}
		// 按 rune 步进
		_, size := utf8.DecodeRuneInString(rest)
		if size <= 0 {
			size = 1
		}
		i += size
	}
	return -1
}
