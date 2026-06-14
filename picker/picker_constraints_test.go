package picker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 测试约束：type:go 应只保留 .go 文件
func TestPicker_ConstraintType(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "src/main.go", "package main\n")
	mustWrite(t, root, "src/util.js", "var x = 1\n")
	mustWrite(t, root, "README.md", "# readme\n")

	p := New(root, Options{})
	defer p.Close()
	if err := p.Scan(); err != nil {
		t.Fatal(err)
	}
	res, err := p.Search("type:go", 10)
	if err != nil {
		t.Fatal(err)
	}
	// 应只返回 main.go
	if len(res) != 1 {
		t.Fatalf("type:go 应返回 1 条结果，got %d: %v", len(res), res)
	}
	if filepath.Base(res[0].Path()) != "main.go" {
		t.Fatalf("应返回 main.go，got %s", res[0].Path())
	}
}

// 测试约束：*.md 扩展名
func TestPicker_ConstraintExtension(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "README.md", "# readme\n")
	mustWrite(t, root, "notes.md", "notes\n")
	mustWrite(t, root, "main.go", "package main\n")

	p := New(root, Options{})
	defer p.Close()
	if err := p.Scan(); err != nil {
		t.Fatal(err)
	}
	res, err := p.Search("*.md", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("*.md 应返回 2 条，got %d", len(res))
	}
}

// 测试 glob 约束：**/*.go
func TestPicker_ConstraintGlob(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "pkg/a/main.go", "package main\n")
	mustWrite(t, root, "pkg/b/util.go", "package util\n")
	mustWrite(t, root, "README.md", "# readme\n")

	p := New(root, Options{})
	defer p.Close()
	if err := p.Scan(); err != nil {
		t.Fatal(err)
	}
	res, err := p.Search("**/*.go", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("**/*.go 应返回 2 条，got %d", len(res))
	}
}

// 测试 size 约束：文件大小
func TestPicker_ConstraintSize(t *testing.T) {
	root := t.TempDir()
	// 小文件
	mustWrite(t, root, "small.txt", "1KB here")
	// 大文件（大于 1KB）
	big := make([]byte, 2048)
	for i := range big {
		big[i] = 'x'
	}
	mustWrite(t, root, "big.txt", string(big))

	p := New(root, Options{})
	defer p.Close()
	if err := p.Scan(); err != nil {
		t.Fatal(err)
	}

	// 查 >1KB：应只返回 big.txt
	res, err := p.Search("size:>1KB", 10)
	if err != nil {
		t.Fatal(err)
	}
	gotBig := false
	for _, r := range res {
		if filepath.Base(r.Path()) == "big.txt" {
			gotBig = true
			break
		}
	}
	if !gotBig {
		t.Fatalf("size:>1KB 应返回 big.txt，got %d 条: %v", len(res), res)
	}

	// 查 <100B：应只返回 small.txt
	res2, err := p.Search("size:<100B", 10)
	if err != nil {
		t.Fatal(err)
	}
	gotSmall := false
	for _, r := range res2 {
		if filepath.Base(r.Path()) == "small.txt" {
			gotSmall = true
			break
		}
	}
	if !gotSmall {
		t.Fatalf("size:<100B 应返回 small.txt，got %d 条: %v", len(res2), res2)
	}
}

// 测试 modified 约束
func TestPicker_ConstraintModified(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "recent.txt", "x")
	// 设置 recent.txt 修改时间为 1 小时前
	recentPath := filepath.Join(root, "recent.txt")
	oneHourAgo := time.Now().Add(-time.Hour)
	os.Chtimes(recentPath, oneHourAgo, oneHourAgo)

	// old.txt 设置为 100 天前
	mustWrite(t, root, "old.txt", "x")
	oldPath := filepath.Join(root, "old.txt")
	ancient := time.Now().Add(-100 * 24 * time.Hour)
	os.Chtimes(oldPath, ancient, ancient)

	p := New(root, Options{})
	defer p.Close()
	if err := p.Scan(); err != nil {
		t.Fatal(err)
	}

	res, err := p.Search("modified:1d", 10)
	if err != nil {
		t.Fatal(err)
	}
	// 应返回 recent.txt，但不返回 old.txt
	gotRecent := false
	for _, r := range res {
		if filepath.Base(r.Path()) == "recent.txt" {
			gotRecent = true
		}
		if filepath.Base(r.Path()) == "old.txt" {
			t.Fatal("modified:1d 不应返回 old.txt")
		}
	}
	if !gotRecent {
		t.Fatal("modified:1d 应返回 recent.txt")
	}
}

// 测试 pathsegment：/src/
func TestPicker_ConstraintPathSegment(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "src/a.go", "package a")
	mustWrite(t, root, "pkg/b.go", "package b")
	p := New(root, Options{})
	defer p.Close()
	if err := p.Scan(); err != nil {
		t.Fatal(err)
	}
	res, err := p.Search("/src/", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || filepath.Base(res[0].Path()) != "a.go" {
		t.Fatalf("/src/ 应返回 a.go，got %d 条", len(res))
	}
}

// helper
func mustWrite(t *testing.T, root, relPath, content string) {
	t.Helper()
	fp := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
