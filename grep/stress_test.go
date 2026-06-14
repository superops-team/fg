package grep

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// BenchmarkGrep_Search 衡量并发 grep 100 个文件
func BenchmarkGrep_Search(b *testing.B) {
	dir := b.TempDir()
	for i := 0; i < 100; i++ {
		name := filepath.Join(dir, "file_"+strconv.Itoa(i)+".go")
		content := "package main\n// line\nfunc main(){}\n"
		// 让一些文件含 token
		if i%3 == 0 {
			content += "\n// TODO handle error\n"
		}
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	paths := make([]string, 0, 100)
	_ = filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err == nil && p != dir {
			paths = append(paths, p)
		}
		return nil
	})
	m := New(Options{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m.SearchMany(paths, "TODO", 5)
	}
}

// 压力测试：并发 grep 200 个大小文件
func TestGrep_HighLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("-short 下跳过压力测试")
	}
	dir := t.TempDir()
	for i := 0; i < 200; i++ {
		name := filepath.Join(dir, "f_"+strconv.Itoa(i)+".txt")
		content := "lorem ipsum dolor sit amet\n"
		if i%5 == 0 {
			content += "special_token_here\n"
		}
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	paths := make([]string, 0, 200)
	_ = filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err == nil && p != dir {
			paths = append(paths, p)
		}
		return nil
	})
	m := New(Options{})
	for round := 0; round < 5; round++ {
		res, err := m.SearchMany(paths, "special_token_here", 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(res) == 0 {
			t.Fatal("应命中 >0 个文件")
		}
	}
}
