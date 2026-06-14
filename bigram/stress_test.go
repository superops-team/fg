package bigram

import (
	"fmt"
	"testing"
)

// BenchmarkBigram_Build 衡量 10k 路径 bigram 构建成本
func BenchmarkBigram_Build(b *testing.B) {
	paths := make([]string, 10_000)
	for i := range paths {
		paths[i] = fmt.Sprintf("pkg%d/sub%d/file%d.go", i%10, i%100, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bg := NewBigram()
		bg.Build(paths)
		_ = bg.FileCount()
	}
}

// BenchmarkBigram_Candidates 衡量候选集生成
func BenchmarkBigram_Candidates(b *testing.B) {
	paths := make([]string, 10_000)
	for i := range paths {
		paths[i] = fmt.Sprintf("pkg%d/sub%d/file%d.go", i%10, i%100, i)
	}
	bg := NewBigram()
	bg.Build(paths)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bg.Candidates("pkg go")
	}
}

// 压力测试：10k 路径下 1000 次查询无 data race
func TestBigram_HighLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("-short 下跳过压力测试")
	}
	paths := make([]string, 10_000)
	for i := range paths {
		paths[i] = fmt.Sprintf("project%d/dir%d/file%d.go", i%20, i%200, i)
	}
	bg := NewBigram()
	bg.Build(paths)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for q := 0; q < 1000; q++ {
			_ = bg.Candidates("file go")
		}
	}()
	for q := 0; q < 1000; q++ {
		_ = bg.Candidates("project")
	}
	<-done
}
