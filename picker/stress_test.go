package picker

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
)

// 压力测试：生成 10k 文件的目录树，并发扫描+搜索，重复 3 次
func TestPicker_HighLoad(t *testing.T) {
	root := t.TempDir()
	total := 10_000
	dirs := []string{"src", "src/pkg", "docs", "test", "internal", "lib", "bin", "examples"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < total; i++ {
		d := dirs[i%len(dirs)]
		name := filepath.Join(root, d, "file_"+itoa(i)+".go")
		if err := os.WriteFile(name, []byte("package main"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	p := New(root, Options{})
	defer p.Close()
	if err := p.Scan(); err != nil {
		t.Fatal(err)
	}
	if p.FileCount() < total/2 {
		t.Fatalf("文件数太少：%d < %d", p.FileCount(), total/2)
	}

	// 3 轮并发搜索
	for round := 0; round < 3; round++ {
		var wg sync.WaitGroup
		workers := runtime.GOMAXPROCS(0) * 2
		errCh := make(chan error, workers)
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, err := p.Search("type:go", 100)
				if err != nil {
					errCh <- err
					return
				}
				if len(r) == 0 {
					t.Log("warning: zero results")
				}
			}()
		}
		wg.Wait()
		close(errCh)
		for e := range errCh {
			if e != nil {
				t.Fatal(e)
			}
		}
	}
}

// BenchmarkPicker_ColdBuild_10k 衡量扫描、元数据收集、path interning 和 bigram 构建成本。
func BenchmarkPicker_ColdBuild_10k(b *testing.B) {
	root := b.TempDir()
	writeBenchmarkFiles(b, root, 10_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(root, Options{})
		if err := p.Scan(); err != nil {
			b.Fatal(err)
		}
		_ = p.Close()
	}
}

// BenchmarkPicker_WarmSearch_10k 衡量已构建索引上的重复搜索成本。
func BenchmarkPicker_WarmSearch_10k(b *testing.B) {
	root := b.TempDir()
	writeBenchmarkFiles(b, root, 10_000)
	p := New(root, Options{})
	defer p.Close()
	if err := p.Scan(); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Search("type:go", 50); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPicker_ColdBuild_100k 是扩展大 corpus 基线；默认可运行，环境受限时可用 -run=^$ -bench=10k 只跑 10k 基线。
func BenchmarkPicker_ColdBuild_100k(b *testing.B) {
	root := b.TempDir()
	writeBenchmarkFiles(b, root, 100_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(root, Options{})
		if err := p.Scan(); err != nil {
			b.Fatal(err)
		}
		_ = p.Close()
	}
}

func writeBenchmarkFiles(tb testing.TB, root string, total int) {
	tb.Helper()
	dirs := []string{"src", "src/pkg", "docs", "test", "internal", "lib", "bin", "examples"}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			tb.Fatal(err)
		}
	}
	for i := 0; i < total; i++ {
		dir := dirs[i%len(dirs)]
		name := filepath.Join(root, dir, "file_"+strconv.Itoa(i)+".go")
		if err := os.WriteFile(name, []byte("package main"), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
}

// itoa 避免依赖 strconv 太频繁
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	buf := make([]byte, 0, 8)
	for i > 0 {
		buf = append(buf, byte('0'+i%10))
		i /= 10
	}
	if neg {
		buf = append(buf, '-')
	}
	// reverse
	for l, r := 0, len(buf)-1; l < r; l, r = l+1, r-1 {
		buf[l], buf[r] = buf[r], buf[l]
	}
	return string(buf)
}
