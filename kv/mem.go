package kv

import (
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
	keys := make([]string, 0, len(s.m))
	for k := range s.m {
		if len(prefix) == 0 || (len(k) >= len(prefix) && k[:len(prefix)] == prefix) {
			keys = append(keys, k)
		}
	}
	s.mu.RUnlock()
	sort.Strings(keys)
	for _, k := range keys {
		// 再次加锁读，避免外部写入同时遍历，但因我们传值拷贝，语义正确
		v, ok, err := s.Get(k)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if !fn(k, v) {
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
