package kv

import (
	"bytes"
	"sort"
	"sync"
)

// MemStore 是内存 KV 实现。线程安全。
type MemStore struct {
	mu sync.RWMutex
	m  map[string][]byte
}

func NewMemStore() *MemStore {
	return &MemStore{m: make(map[string][]byte)}
}

func (s *MemStore) Put(key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if value == nil {
		delete(s.m, key)
	} else {
		// 复制一次，避免外部修改底层数组
		cp := make([]byte, len(value))
		copy(cp, value)
		s.m[key] = cp
	}
	return nil
}

func (s *MemStore) Get(key string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.m[key]; ok {
		cp := make([]byte, len(v))
		copy(cp, v)
		return cp, true, nil
	}
	return nil, false, nil
}

// ForEach 按字典序遍历 prefix 匹配的 key；fn 返回 false 停止。
func (s *MemStore) ForEach(prefix string, fn func(key string, value []byte) bool) error {
	s.mu.RLock()
	entries := make([]struct{ k, v []byte }, 0, len(s.m))
	for k, v := range s.m {
		if len(prefix) == 0 || (len(k) >= len(prefix) && k[:len(prefix)] == prefix) {
			cp := make([]byte, len(v))
			copy(cp, v)
			entries = append(entries, struct{ k, v []byte }{[]byte(k), cp})
		}
	}
	s.mu.RUnlock()
	// 排序（字典序即字节序）
	sort.Slice(entries, func(i, j int) bool { return bytes.Compare(entries[i].k, entries[j].k) < 0 })
	for _, e := range entries {
		if !fn(string(e.k), e.v) {
			break
		}
	}
	return nil
}

func (s *MemStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = nil
	return nil
}
