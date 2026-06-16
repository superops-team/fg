// Package fg 提供简洁的顶层 API：
//
//	results, err := fg.Search(root, "query type:go size:>1KB", 20)
//	for _, r := range results {
//	    fmt.Println(r.Path(), r.Score())
//	}
package fg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/superops-team/fg/picker"
)

// Options 控制搜索行为
type Options struct {
	Root    string           // 搜索根目录（默认当前目录）
	Query   string           // 查询（fuzzy text + type/size/modified/glob 约束）
	Limit   int              // 返回结果上限（默认 20）
	NowFunc func() time.Time // 注入时间（测试用）
}

// Result 是搜索结果（只读）
type Result struct {
	Path  string
	Score int32
}

var ErrIndexClosed = errors.New("fg index is closed")

type Index struct {
	mu      sync.RWMutex
	root    string
	opts    Options
	picker  *picker.Picker
	closed  bool
	refresh sync.Mutex
}

// Search 在 root 下搜索 query，返回最多 limit 条结果。
func Search(root, query string, limit int) ([]Result, error) {
	return SearchWith(Options{Root: root, Query: query, Limit: limit})
}

// SearchWith 用 Options 执行搜索（更灵活）。
func SearchWith(opts Options) ([]Result, error) {
	idx, err := Open(context.Background(), opts)
	if err != nil {
		return nil, err
	}
	defer idx.Close()
	return idx.SearchContext(context.Background(), opts.Query, opts.Limit)
}

func Open(ctx context.Context, opts Options) (*Index, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	root, err := normalizeRoot(opts.Root)
	if err != nil {
		return nil, err
	}
	opts.Root = root
	idx := &Index{root: root, opts: opts}
	if err := idx.Refresh(ctx); err != nil {
		return nil, err
	}
	return idx, nil
}

func normalizeRoot(root string) (string, error) {
	if root == "" || strings.TrimSpace(root) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get cwd: %w", err)
		}
		root = cwd
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("abs %s: %w", root, err)
	}
	root = filepath.Clean(absRoot)
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", root)
	}
	return root, nil
}

func (idx *Index) SearchContext(ctx context.Context, query string, limit int) ([]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	p, err := idx.snapshot()
	if err != nil {
		return nil, err
	}
	raw, err := p.SearchContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return mapResults(raw), nil
}

func (idx *Index) Refresh(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	idx.refresh.Lock()
	defer idx.refresh.Unlock()

	idx.mu.RLock()
	closed := idx.closed
	idx.mu.RUnlock()
	if closed {
		return ErrIndexClosed
	}

	p := picker.New(idx.root, pickerOptions(idx.opts))
	if err := p.ScanContext(ctx); err != nil {
		_ = p.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("scan %s: %w", idx.root, err)
	}
	idx.mu.Lock()
	if idx.closed {
		idx.mu.Unlock()
		_ = p.Close()
		return ErrIndexClosed
	}
	old := idx.picker
	idx.picker = p
	idx.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

func (idx *Index) Close() error {
	idx.mu.Lock()
	if idx.closed {
		idx.mu.Unlock()
		return nil
	}
	idx.closed = true
	p := idx.picker
	idx.picker = nil
	idx.mu.Unlock()
	if p != nil {
		return p.Close()
	}
	return nil
}

func (idx *Index) snapshot() (*picker.Picker, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if idx.closed {
		return nil, ErrIndexClosed
	}
	if idx.picker == nil {
		return nil, fmt.Errorf("fg index has no snapshot")
	}
	return idx.picker, nil
}

func mapResults(raw []picker.Result) []Result {
	out := make([]Result, len(raw))
	for i, r := range raw {
		out[i] = Result{Path: r.Path(), Score: r.Score()}
	}
	return out
}

func pickerOptions(opts Options) picker.Options {
	out := picker.Options{}
	if opts.NowFunc != nil {
		out.NowFunc = opts.NowFunc
	}
	return out
}
