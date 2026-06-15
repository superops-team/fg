package core

import (
	"sync"
	"testing"
)

// ========================================
// PathArena: 功能 TDD 测试
// ========================================

func TestPathArena_BasicInternAndGet(t *testing.T) {
	a := NewPathArena(4)
	cp := a.Intern("src/main.go")
	if got := a.Get(cp); got != "src/main.go" {
		t.Fatalf("Get=%q, want src/main.go", got)
	}
	if cp.FilenameOffset != 4 { // "src/" 的长度 = 4
		t.Fatalf("FilenameOffset=%d, want 4", cp.FilenameOffset)
	}
}

func TestPathArena_Deduplication(t *testing.T) {
	a := NewPathArena(4)
	cp1 := a.Intern("foo/bar.go")
	cp2 := a.Intern("foo/bar.go")
	if cp1.Index != cp2.Index {
		t.Fatalf("相同路径应去重，index %d vs %d", cp1.Index, cp2.Index)
	}
	if cp1.FilenameOffset != cp2.FilenameOffset {
		t.Fatalf("FilenameOffset 应相同")
	}
	if a.Len() != 1 {
		t.Fatalf("去重后 arena 长度应为 1, got %d", a.Len())
	}
}

func TestPathArena_FilenameOffset_NoSlash(t *testing.T) {
	a := NewPathArena(4)
	cp := a.Intern("main.go")
	if cp.FilenameOffset != 0 {
		t.Fatalf("无斜杠时 FilenameOffset 应为 0, got %d", cp.FilenameOffset)
	}
	if got := a.Get(cp); got != "main.go" {
		t.Fatalf("Get=%q, want main.go", got)
	}
}

func TestPathArena_FilenameOffset_DeepPath(t *testing.T) {
	a := NewPathArena(4)
	cp := a.Intern("a/b/c/d/e.go")
	filename := a.Filename(cp)
	if filename != "e.go" {
		t.Fatalf("Filename=%q, want e.go", filename)
	}
	dir := a.Dir(cp)
	if dir != "a/b/c/d" {
		t.Fatalf("Dir=%q, want a/b/c/d", dir)
	}
}

func TestPathArena_FilenameOffset_BackslashWindows(t *testing.T) {
	a := NewPathArena(4)
	cp := a.Intern("src\\util\\foo.go")
	filename := a.Filename(cp)
	if filename != "foo.go" {
		t.Fatalf("Windows 路径 Filename=%q, want foo.go", filename)
	}
}

func TestPathArena_Dir_NoSlash(t *testing.T) {
	a := NewPathArena(4)
	cp := a.Intern("hello.go")
	if got := a.Dir(cp); got != "" {
		t.Fatalf("Dir=%q, want empty", got)
	}
}

func TestPathArena_InternMany(t *testing.T) {
	a := NewPathArena(16)
	paths := []string{
		"src/main.go", "src/util/foo.go", "src/util/bar.go",
		"test/main_test.go", "docs/README.md", "README.md",
	}
	for _, p := range paths {
		cp := a.Intern(p)
		if got := a.Get(cp); got != p {
			t.Fatalf("Get(%q)=%q", p, got)
		}
	}
	if a.Len() != len(paths) {
		t.Fatalf("Len=%d, want %d", a.Len(), len(paths))
	}
}

// ========================================
// PathArena: 并发安全
// ========================================

func TestPathArena_ConcurrentIntern(t *testing.T) {
	a := NewPathArena(32)
	paths := []string{"a.go", "b.go", "c/d.go", "c/e.go", "f/g/h.go", "i.go"}
	var wg sync.WaitGroup
	for round := 0; round < 10; round++ {
		for _, p := range paths {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				cp := a.Intern(path)
				if got := a.Get(cp); got != path {
					t.Errorf("并发 Intern 后 Get 返回 %q", got)
				}
			}(p)
		}
	}
	wg.Wait()
	if a.Len() != len(paths) {
		t.Fatalf("并发 Intern 去重后应为 %d, got %d", len(paths), a.Len())
	}
}

// ========================================
// filenameOffset: 纯函数测试
// ========================================

func TestFilenameOffset_Cases(t *testing.T) {
	tests := []struct {
		input  string
		expect uint32
	}{
		{"", 0},
		{"main.go", 0},
		{"src/main.go", 4},
		{"a/b/c.go", 4},
		{"a/b/c/d/e.go", 8},
		{"src\\util\\foo.go", 9},
		{"foo/bar/baz/", 12}, // 末尾 / 的位置是 11，+1 = 12
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := filenameOffset(tt.input); got != tt.expect {
				t.Fatalf("filenameOffset(%q)=%d, want %d", tt.input, got, tt.expect)
			}
		})
	}
}

// ========================================
// 验证 FileItem 可放入 []FileItem，并正确解析
// ========================================

func TestFileItem_WithPathArena_Integration(t *testing.T) {
	a := NewPathArena(4)
	paths := []string{"src/main.go", "src/util/fs.go", "README.md"}
	files := make([]FileItem, len(paths))
	for i, p := range paths {
		files[i] = FileItem{Path: a.Intern(p)}
	}
	for i, f := range files {
		got := a.Get(f.Path)
		if got != paths[i] {
			t.Fatalf("file[%d].Path 解析为 %q, want %q", i, got, paths[i])
		}
		filename := a.Filename(f.Path)
		// 简单验证 filename 非空且长度 <= 原长度
		if len(filename) == 0 || len(filename) > len(paths[i]) {
			t.Fatalf("filename=%q 长度异常", filename)
		}
	}
}

// ========================================
// 回归: PathArena 越界访问保护
// ----------------------------------------
// Bug: 原始实现 a.strings[idx] / s[cp.FilenameOffset:] 在构造假 ChunkedPath
// 时会越界 panic。修复后应安全返回空串。
// ========================================

func TestPathArena_Get_OutOfRangeIndex(t *testing.T) {
	a := NewPathArena(4)
	_ = a.Intern("a.go")
	bad := ChunkedPath{Index: 99, FilenameOffset: 0}
	if got := a.Get(bad); got != "" {
		t.Fatalf("越界 index 应返回空串， got %q", got)
	}
}

func TestPathArena_Filename_OutOfRangeIndex(t *testing.T) {
	a := NewPathArena(4)
	_ = a.Intern("a.go")
	bad := ChunkedPath{Index: 1234, FilenameOffset: 0}
	if got := a.Filename(bad); got != "" {
		t.Fatalf("越界 Filename 应返回空串， got %q", got)
	}
}

func TestPathArena_Filename_OutOfRangeOffset(t *testing.T) {
	a := NewPathArena(4)
	cp := a.Intern("a.go")
	bad := ChunkedPath{Index: cp.Index, FilenameOffset: 999}
	if got := a.Filename(bad); got != "" {
		t.Fatalf("offset 越界应返回空串， got %q", got)
	}
}

func TestPathArena_Dir_OutOfRangeIndex(t *testing.T) {
	a := NewPathArena(4)
	_ = a.Intern("a.go")
	bad := ChunkedPath{Index: 42, FilenameOffset: 0}
	if got := a.Dir(bad); got != "" {
		t.Fatalf("Dir 越界 index 应返回空串， got %q", got)
	}
}

func TestPathArena_Dir_OutOfRangeOffset(t *testing.T) {
	a := NewPathArena(4)
	cp := a.Intern("foo/bar.go")
	bad := ChunkedPath{Index: cp.Index, FilenameOffset: 9999}
	if got := a.Dir(bad); got != "" {
		t.Fatalf("Dir 超界 offset 应返回空串， got %q", got)
	}
}

func TestPathArena_Dir_NoSlashStillWorks(t *testing.T) {
	a := NewPathArena(4)
	cp := a.Intern("src/util/main.go")
	dir := a.Dir(cp)
	if dir != "src/util" {
		t.Fatalf("Dir=%q, want src/util", dir)
	}
}
