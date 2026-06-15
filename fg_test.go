package fg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeRootTestFile(t *testing.T, root, name, content string, modTime time.Time) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if !modTime.IsZero() {
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestSearch_DefaultLimit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 25; i++ {
		writeRootTestFile(t, root, "file-"+string(rune('a'+i))+".go", "package main\n", time.Time{})
	}

	results, err := Search(root, "type:go", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 20 {
		t.Fatalf("default limit should return 20 results, got %d", len(results))
	}
}

func TestSearchWith_EmptyRootUsesCurrentWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	writeRootTestFile(t, root, "main.go", "package main\n", time.Time{})
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	results, err := SearchWith(Options{Query: "type:go", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result from cwd, got %d", len(results))
	}
	if filepath.Base(results[0].Path) != "main.go" {
		t.Fatalf("expected main.go, got %q", results[0].Path)
	}
}

func TestSearchWith_InvalidRootErrors(t *testing.T) {
	tests := []struct {
		name string
		root func(t *testing.T) string
		want string
	}{
		{
			name: "missing root",
			root: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") },
			want: "stat",
		},
		{
			name: "file root",
			root: func(t *testing.T) string {
				return writeRootTestFile(t, t.TempDir(), "not-dir.txt", "x", time.Time{})
			},
			want: "not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SearchWith(Options{Root: tt.root(t), Query: "x", Limit: 1})
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q should contain %q", err.Error(), tt.want)
			}
		})
	}
}

func TestSearchWith_NowFuncAffectsModifiedConstraint(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	writeRootTestFile(t, root, "recent.go", "package main\n", now.Add(-2*time.Hour))
	writeRootTestFile(t, root, "old.go", "package main\n", now.AddDate(0, 0, -30))

	results, err := SearchWith(Options{
		Root:    root,
		Query:   "type:go modified:1d",
		Limit:   10,
		NowFunc: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one recent result, got %d", len(results))
	}
	if filepath.Base(results[0].Path) != "recent.go" {
		t.Fatalf("expected recent.go, got %q", results[0].Path)
	}
}

func TestSearch_ResultMapping(t *testing.T) {
	root := t.TempDir()
	writeRootTestFile(t, root, "main.go", "package main\n", time.Time{})

	results, err := Search(root, "main type:go", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if results[0].Path == "" {
		t.Fatal("result path should not be empty")
	}
	if filepath.Base(results[0].Path) != "main.go" {
		t.Fatalf("expected main.go, got %q", results[0].Path)
	}
	if results[0].Score == 0 {
		t.Fatal("result score should be mapped from picker result")
	}
}
