// Package kv 提供持久化 KV 存储抽象，便于测试时替换为内存实现
package kv

import (
	"time"
)

// Entry 表示一个 KV 条目
type Entry struct {
	Key   string
	Value []byte
}

// KVStore 是 key-value 存储的统一接口
//   - Put(key, value): 写入；value 为 nil 表示删除
//   - Get(key): 返回 value；不存在返回 nil, false
//   - ForEach(prefix, fn): 遍历前缀匹配的 key；fn 返回 false 则停止
//   - Close(): 释放资源
type KVStore interface {
	Put(key string, value []byte) error
	Get(key string) ([]byte, bool, error)
	ForEach(prefix string, fn func(key string, value []byte) bool) error
	Close() error
}

// Bucket 分桶的 KV 接口（可选）：语义上等同于多个 namespace
type Bucket interface {
	KVStore
	Bucket(name string) (Bucket, error)
}

// Options 控制 KV 打开行为
type Options struct {
	Timeout time.Duration // 默认 5s
	ReadOnly bool
}

// DefaultOptions 返回默认配置
func DefaultOptions() Options { return Options{Timeout: 5 * time.Second} }
