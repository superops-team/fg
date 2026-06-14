package core

import (
	"sync"
	"testing"
)

func TestPagePool_GetPut(t *testing.T) {
	p := NewPagePool(1024)
	b1 := p.Get()
	if len(*b1) != 1024 {
		t.Fatalf("len(*b1)=%d, want 1024", len(*b1))
	}
	p.Put(b1)
	// 再 get 一个，应与之前同大小
	b2 := p.Get()
	if len(*b2) != 1024 {
		t.Fatalf("len(*b2)=%d, want 1024", len(*b2))
	}
	p.Put(b2)
	if p.BufSize() != 1024 {
		t.Fatalf("BufSize=%d, want 1024", p.BufSize())
	}
}

func TestPagePool_Concurrent(t *testing.T) {
	p := NewPagePool(4096)
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := p.Get()
			// 写一点数据模拟使用
			for j := range *b {
				(*b)[j] = 1
			}
			p.Put(b)
		}()
	}
	wg.Wait()
}

func TestPagePool_DefaultSize(t *testing.T) {
	p := NewPagePool(0)
	b := p.Get()
	if len(*b) != 64*1024 {
		t.Fatalf("默认 bufSize=%d, want %d", len(*b), 64*1024)
	}
	p.Put(b)
}
