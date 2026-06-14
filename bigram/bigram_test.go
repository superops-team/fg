package bigram

import (
	"math"
	"sort"
	"sync"
	"testing"
)

// ========================================
// 测试工具
// ========================================

func equalUnsorted(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := append([]uint32(nil), a...), append([]uint32(nil), b...)
	sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
	sort.Slice(y, func(i, j int) bool { return y[i] < y[j] })
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// ========================================
// Bigram: 基础功能
// ========================================

func TestBigram_BuildAndCandidates_Basic(t *testing.T) {
	paths := []string{
		"src/main.go",
		"src/util.go",
		"docs/README.md",
		"test/main_test.go",
	}
	b := NewBigram()
	b.Build(paths)
	if b.FileCount() != 4 {
		t.Fatalf("FileCount=%d, want 4", b.FileCount())
	}

	// "main" 出现在 path 0 和 3
	got := b.Candidates("main")
	want := []uint32{0, 3}
	if !equalUnsorted(got, want) {
		t.Fatalf("Candidates(main)=%v, want %v", got, want)
	}

	// "util" 只在 path 1
	got = b.Candidates("util")
	if !equalUnsorted(got, []uint32{1}) {
		t.Fatalf("Candidates(util)=%v, want [1]", got)
	}

	// "readme" 只在 path 2
	got = b.Candidates("readme")
	if !equalUnsorted(got, []uint32{2}) {
		t.Fatalf("Candidates(readme)=%v, want [2]", got)
	}
}

func TestBigram_Candidates_EmptyOrShort(t *testing.T) {
	b := NewBigram()
	b.Build([]string{"foo", "bar"})
	if got := b.Candidates(""); got != nil {
		t.Fatalf("空查询应返回 nil, got %v", got)
	}
	// 单字符：返回 nil（没有 bigram）
	if got := b.Candidates("a"); got != nil {
		t.Fatalf("单字符应返回 nil, got %v", got)
	}
}

func TestBigram_Candidates_NoMatch(t *testing.T) {
	b := NewBigram()
	b.Build([]string{"src/main.go", "docs/README.md"})
	// "zzzz" 不匹配任何 bigram
	got := b.Candidates("zzzz")
	if got == nil {
		t.Fatal("应有非空但为 zero-length 切片")
	}
	if len(got) != 0 {
		t.Fatalf("Candidates(zzzz)=%v, want []", got)
	}
}

func TestBigram_Candidates_CaseInsensitive(t *testing.T) {
	b := NewBigram()
	b.Build([]string{"src/Main.go"})
	// 大小写查询都应命中
	for _, q := range []string{"main", "MAIN", "Main"} {
		if got := b.Candidates(q); len(got) != 1 {
			t.Fatalf("Candidates(%q)=%v, want len 1", q, got)
		}
	}
}

func TestBigram_Candidates_SlashAndDot(t *testing.T) {
	// 测试包含非字母的特殊字符是否能生成 bigram
	b := NewBigram()
	b.Build([]string{"a/b/c.go"})
	// "a/b" 包含 '/' → 应能命中
	got := b.Candidates("a/b")
	if len(got) != 1 {
		t.Fatalf("Candidates(a/b)=%v, want len 1", got)
	}
}

func TestBigram_Matches(t *testing.T) {
	b := NewBigram()
	b.Build([]string{"src/main.go", "docs/README.md"})
	if !b.Matches("main", 0) {
		t.Fatal("file 0 应匹配 main")
	}
	if b.Matches("main", 1) {
		t.Fatal("file 1 (README) 不应匹配 main")
	}
	if !b.Matches("readme", 1) {
		t.Fatal("file 1 应匹配 readme")
	}
}

func TestBigram_ConcurrentRead(t *testing.T) {
	b := NewBigram()
	paths := make([]string, 100)
	for i := range paths {
		paths[i] = "src/pkg_" + "foo" + "/file_" + string(rune('a'+(i%26))) + ".go"
	}
	b.Build(paths)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.Candidates("file")
			_ = b.Matches("foo", 0)
			_ = b.FileCount()
		}()
	}
	wg.Wait()
}

func TestBigram_Rebuild(t *testing.T) {
	b := NewBigram()
	b.Build([]string{"first.go", "second.go"})
	if b.FileCount() != 2 {
		t.Fatalf("first build count=%d, want 2", b.FileCount())
	}
	b.Build([]string{"one.rs", "two.rs", "three.rs"})
	if b.FileCount() != 3 {
		t.Fatalf("second build count=%d, want 3", b.FileCount())
	}
	// "first" 不应出现
	if got := b.Candidates("first"); len(got) != 0 {
		t.Fatalf("重建后不应有 first, got %v", got)
	}
	// "three" 应命中
	if got := b.Candidates("three"); len(got) != 1 {
		t.Fatalf("three 应命中 1, got %v", got)
	}
}

// ========================================
// BigramOverlay
// ========================================

func TestBigramOverlay_AddAndCandidates(t *testing.T) {
	o := NewOverlay()
	o.Add(100, "src/overlay/a.go")
	o.Add(101, "src/overlay/b.go")
	if o.FileCount() != 2 {
		t.Fatalf("FileCount=%d, want 2", o.FileCount())
	}
	got := o.Candidates("overlay")
	if len(got) != 2 {
		t.Fatalf("Candidates(overlay)=%v, want len 2", got)
	}
	got = o.Candidates("a.go")
	if len(got) != 1 || got[0] != 100 {
		t.Fatalf("Candidates(a.go)=%v, want [100]", got)
	}
}

func TestBigramOverlay_Reset(t *testing.T) {
	o := NewOverlay()
	o.Add(1, "foo.go")
	o.Reset()
	if o.FileCount() != 0 {
		t.Fatalf("Reset 后 FileCount=%d, want 0", o.FileCount())
	}
	if got := o.Candidates("foo"); len(got) != 0 {
		t.Fatalf("Reset 后 Candidates(foo)=%v, want empty", got)
	}
}

func TestBigramOverlay_Concurrent(t *testing.T) {
	o := NewOverlay()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx uint32) {
			defer wg.Done()
			o.Add(idx, "src/file_"+string(rune('a'+int(idx%26)))+".go")
		}(uint32(i))
	}
	wg.Wait()
	if o.FileCount() != 50 {
		t.Fatalf("FileCount=%d, want 50", o.FileCount())
	}
}

// ========================================
// Bigram: 空数组 vs 空 slice（边界）
// ========================================

func TestBigram_EmptyPaths(t *testing.T) {
	b := NewBigram()
	b.Build(nil)
	if b.FileCount() != 0 {
		t.Fatalf("FileCount=%d, want 0", b.FileCount())
	}
	if got := b.Candidates("anything"); got != nil {
		t.Fatalf("空索引应返回 nil, got %v", got)
	}
}

func TestBigram_SingleCharPath(t *testing.T) {
	b := NewBigram()
	b.Build([]string{"a", "b"})
	if b.FileCount() != 2 {
		t.Fatalf("FileCount=%d, want 2", b.FileCount())
	}
	// 单字符路径没有 bigram → 任何包含 bigram 的查询都不会命中
	got := b.Candidates("ab")
	if len(got) != 0 {
		t.Fatalf("Candidates(ab)=%v, want empty", got)
	}
}

// ========================================
// 中等规模：确保排序和交集逻辑正确
// ========================================

func TestBigram_Intersection(t *testing.T) {
	paths := []string{
		"src/util/config.go",
		"src/app/main.go",
		"src/util/helper.go",
		"docs/user-guide.md",
	}
	b := NewBigram()
	b.Build(paths)
	// "src util" 应包含 path 0 和 2
	got := b.Candidates("src util")
	if !equalUnsorted(got, []uint32{0, 2}) {
		t.Fatalf("Candidates(src util)=%v, want [0,2]", got)
	}
}

// ========================================
// sortedContains 二分查找的简单测试
// ========================================

func TestSortedContains(t *testing.T) {
	s := []uint32{1, 3, 5, 7, 9, 11, 13, 15}
	for _, v := range s {
		if !sortedContains(s, v) {
			t.Fatalf("应包含 %d", v)
		}
	}
	for _, v := range []uint32{0, 2, 4, 100, math.MaxUint32} {
		if sortedContains(s, v) {
			t.Fatalf("不应包含 %d", v)
		}
	}
}
