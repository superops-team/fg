package picker

import (
	"os"
	"os/exec"
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

func TestMatchGlob_Table(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{
			name:    "plain glob matches file in same directory",
			pattern: "*.go",
			path:    "main.go",
			want:    true,
		},
		{
			name:    "plain glob does not cross directory separator",
			pattern: "*.go",
			path:    "pkg/main.go",
			want:    false,
		},
		{
			name:    "recursive glob matches root file",
			pattern: "**/*.go",
			path:    "main.go",
			want:    true,
		},
		{
			name:    "recursive glob matches nested file",
			pattern: "**/*.go",
			path:    "pkg/main.go",
			want:    true,
		},
		{
			name:    "recursive glob after prefix matches zero subdirectories",
			pattern: "src/**/*.go",
			path:    "src/main.go",
			want:    true,
		},
		{
			name:    "recursive glob after prefix matches nested subdirectories",
			pattern: "src/**/*.go",
			path:    "src/pkg/main.go",
			want:    true,
		},
		{
			name:    "recursive glob does not match wrong prefix",
			pattern: "src/**/*.go",
			path:    "pkg/main.go",
			want:    false,
		},
		{
			name:    "case insensitive match",
			pattern: "SRC/**/*.GO",
			path:    "src/pkg/main.go",
			want:    true,
		},
		{
			name:    "invalid pattern returns false",
			pattern: "[",
			path:    "main.go",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchGlob(tt.pattern, tt.path)
			if got != tt.want {
				t.Fatalf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
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

func TestPicker_StatusConstraintLoadsGitStatusLazily(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	mustWrite(t, root, "staged.go", "package staged\n")
	mustWrite(t, root, "untracked.go", "package untracked\n")
	runGit(t, root, "init")
	runGit(t, root, "add", "staged.go")

	p := New(root, Options{})
	defer p.Close()
	if err := p.Scan(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < p.FileCount(); i++ {
		if got := p.FileAt(i).GitStatus(); got != nil {
			t.Fatalf("Scan() 不应预加载 git status，index=%d status=%+v", i, got)
		}
	}

	res, err := p.Search("status:untracked", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("status:untracked 应只返回 1 条，got %d", len(res))
	}
	if filepath.Base(res[0].Path()) != "untracked.go" {
		t.Fatalf("status:untracked 应返回 untracked.go，got %s", res[0].Path())
	}
}

func TestPicker_UnknownStatusConstraintDoesNotRequireGit(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "main.go", "package main\n")

	p := New(root, Options{})
	defer p.Close()
	res, err := p.Search("status:unknown", 10)
	if err != nil {
		t.Fatalf("未知 status 值不应加载 git status 或返回错误: %v", err)
	}
	if len(res) != 1 || filepath.Base(res[0].Path()) != "main.go" {
		t.Fatalf("未知 status 值应保持向后兼容不参与过滤，got %+v", res)
	}
}

func TestPicker_StatusConstraintHandlesPathWithSpaces(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	mustWrite(t, root, "space name.go", "package spaced\n")
	runGit(t, root, "init")

	p := New(root, Options{})
	defer p.Close()
	res, err := p.Search("status:untracked", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || filepath.Base(res[0].Path()) != "space name.go" {
		t.Fatalf("status:untracked 应正确匹配带空格路径，got %+v", res)
	}
}

func TestPicker_StatusDeletedReturnsDeletedTrackedPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	mustWrite(t, root, "deleted.go", "package deleted\n")
	runGit(t, root, "init")
	runGit(t, root, "add", "deleted.go")
	runGit(t, root, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-m", "init")
	if err := os.Remove(filepath.Join(root, "deleted.go")); err != nil {
		t.Fatal(err)
	}

	p := New(root, Options{})
	defer p.Close()
	res, err := p.Search("status:deleted", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || filepath.Base(res[0].Path()) != "deleted.go" {
		t.Fatalf("status:deleted 应返回已删除 tracked path，got %+v", res)
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

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
