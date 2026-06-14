package core

import "sync"

// PagePool 是一个简单的 []byte 缓冲池，用于 grep 扫文件时的读缓冲重用，
// 减少频繁的小分配。容量可在 New 时指定。
type PagePool struct {
	pool *sync.Pool
	size int
}

func NewPagePool(bufSize int) *PagePool {
	if bufSize <= 0 {
		bufSize = 64 * 1024
	}
	return &PagePool{
		size: bufSize,
		pool: &sync.Pool{
			New: func() any {
				b := make([]byte, bufSize)
				return &b
			},
		},
	}
}

// Get 从池中取出一个 *[]byte（长度固定为 bufSize）。
// 调用方用完后必须 Put 回去。
func (p *PagePool) Get() *[]byte { return p.pool.Get().(*[]byte) }

// Put 将缓冲还回池。
func (p *PagePool) Put(b *[]byte) {
	if b == nil || len(*b) != p.size {
		return
	}
	p.pool.Put(b)
}

// BufSize 返回每个缓冲的字节大小。
func (p *PagePool) BufSize() int { return p.size }
