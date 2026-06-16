package fg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestIndex_RelativeRootRefreshRemainsBoundToOpenDirectory(t *testing.T) {
	root := t.TempDir()
	writeRootTestFile(t, root, "root.go", "package root\n", time.Time{})
	other := t.TempDir()
	writeRootTestFile(t, other, "other.go", "package other\n", time.Time{})

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	idx, err := Open(context.Background(), Options{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	if err := os.Chdir(other); err != nil {
		t.Fatal(err)
	}
	if err := idx.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	results, err := idx.SearchContext(context.Background(), "type:go", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || filepath.Base(results[0].Path) != "root.go" {
		t.Fatalf("relative root refresh should stay bound to open-time directory, got %+v", results)
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

func TestIndex_SearchContextRefreshAndCloseLifecycle(t *testing.T) {
	root := t.TempDir()
	writeRootTestFile(t, root, "old.go", "package old\n", time.Time{})

	idx, err := Open(context.Background(), Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	oldResults, err := idx.SearchContext(context.Background(), "old", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldResults) != 1 || filepath.Base(oldResults[0].Path) != "old.go" {
		t.Fatalf("expected old.go from opened snapshot, got %+v", oldResults)
	}

	writeRootTestFile(t, root, "new.go", "package new\n", time.Time{})
	staleResults, err := idx.SearchContext(context.Background(), "new", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(staleResults) != 0 {
		t.Fatalf("search before refresh must use old snapshot, got %+v", staleResults)
	}

	if err := idx.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	newResults, err := idx.SearchContext(context.Background(), "new", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(newResults) != 1 || filepath.Base(newResults[0].Path) != "new.go" {
		t.Fatalf("expected new.go after refresh, got %+v", newResults)
	}

	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("close should be idempotent: %v", err)
	}
	_, err = idx.SearchContext(context.Background(), "old", 10)
	if !errors.Is(err, ErrIndexClosed) {
		t.Fatalf("search after close error = %v, want ErrIndexClosed", err)
	}
}

func TestIndex_ContextCancellationReturnsNoPartialResults(t *testing.T) {
	root := t.TempDir()
	writeRootTestFile(t, root, "main.go", "package main\n", time.Time{})

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Open(canceled, Options{Root: root}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open canceled error = %v, want context.Canceled", err)
	}

	idx, err := Open(context.Background(), Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	results, err := idx.SearchContext(canceled, "main", 10)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SearchContext canceled error = %v, want context.Canceled", err)
	}
	if len(results) != 0 {
		t.Fatalf("canceled search must not return partial results, got %+v", results)
	}

	writeRootTestFile(t, root, "new.go", "package new\n", time.Time{})
	if err := idx.Refresh(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Refresh canceled error = %v, want context.Canceled", err)
	}
	newResults, err := idx.SearchContext(context.Background(), "new", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(newResults) != 0 {
		t.Fatalf("canceled refresh must preserve old snapshot, got %+v", newResults)
	}
}

func TestIndex_ContextDeadlineExceededReturnsNoPartialResults(t *testing.T) {
	root := t.TempDir()
	writeRootTestFile(t, root, "main.go", "package main\n", time.Time{})

	idx, err := Open(context.Background(), Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	deadline, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	results, err := idx.SearchContext(deadline, "main", 10)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SearchContext deadline error = %v, want context.DeadlineExceeded", err)
	}
	if len(results) != 0 {
		t.Fatalf("deadline-exceeded search must not return partial results, got %+v", results)
	}
}

func TestIndex_ConcurrentSearchAndRefresh(t *testing.T) {
	root := t.TempDir()
	writeRootTestFile(t, root, "main.go", "package main\n", time.Time{})
	idx, err := Open(context.Background(), Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				results, err := idx.SearchContext(context.Background(), "type:go", 10)
				if err != nil {
					t.Errorf("SearchContext: %v", err)
					return
				}
				if len(results) == 0 {
					t.Error("search should see a complete non-empty snapshot")
					return
				}
			}
		}()
	}
	for i := 0; i < 5; i++ {
		writeRootTestFile(t, root, "added-"+string(rune('a'+i))+".go", "package added\n", time.Time{})
		if err := idx.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
