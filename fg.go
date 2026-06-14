// Package fg 提供简洁的顶层 API：
//
//	results, err := fg.Search(root, "query type:go size:>1KB", 20)
//	for _, r := range results {
//	    fmt.Println(r.Path(), r.Score())
//	}
package fg

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yourname/fg/picker"
)

// Options 控制搜索行为
type Options struct {
	Root        string        // 搜索根目录（默认当前目录）
	Query       string        // 查询（fuzzy text + type/size/modified/glob 约束）
	Limit       int           // 返回结果上限（默认 20）
	NowFunc     func() time.Time // 注入时间（测试用）
}

// Result 是搜索结果（只读）
type Result struct {
	Path  string
	Score int32
}

// Search 在 root 下搜索 query，返回最多 limit 条结果。
func Search(root, query string, limit int) ([]Result, error) {
	return SearchWith(Options{Root: root, Query: query, Limit: limit})
}

// SearchWith 用 Options 执行搜索（更灵活）。
func SearchWith(opts Options) ([]Result, error) {
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	root := opts.Root
	if root == "" || strings.TrimSpace(root) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get cwd: %w", err)
		}
		root = cwd
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	p := picker.New(root, pickerOptions(opts))
	defer p.Close()

	if err := p.Scan(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	raw, err := p.Search(opts.Query, opts.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]Result, len(raw))
	for i, r := range raw {
		out[i] = Result{Path: r.Path(), Score: r.Score()}
	}
	return out, nil
}

func pickerOptions(opts Options) picker.Options {
	out := picker.Options{}
	if opts.NowFunc != nil {
		out.NowFunc = opts.NowFunc
	}
	return out
}
