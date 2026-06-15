// Package querytracker 追踪查询 -> 文件 的 combo 选择记录。
// 用于对搜索结果按历史选择做加权。
package querytracker

import (
	"encoding/binary"
	"strings"
	"sync"

	"github.com/superops-team/fg/kv"
)

// QueryTracker 维护 (query, path) -> 选择次数 的映射。
type QueryTracker struct {
	mu    sync.Mutex
	store interface {
		Put(key string, value []byte) error
		Get(key string) ([]byte, bool, error)
		ForEach(prefix string, fn func(key string, value []byte) bool) error
		Close() error
	}
}

const comboPrefix = "c:"
const lastQueryKey = "last_query"

// New 返回内存 tracker（不持久化）
func New() *QueryTracker {
	return &QueryTracker{store: kv.NewMemStore()}
}

// Open 返回持久化 tracker
func Open(store interface {
	Put(key string, value []byte) error
	Get(key string) ([]byte, bool, error)
	ForEach(prefix string, fn func(key string, value []byte) bool) error
	Close() error
}) *QueryTracker {
	return &QueryTracker{store: store}
}

func comboKey(query, path string) string {
	return comboPrefix + query + "\x00" + path
}

// Record 记录一次 (query, path) 选择事件。
// 两者都为空时不执行。
func (q *QueryTracker) Record(query, path string) error {
	if query == "" && path == "" {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	key := comboKey(query, path)
	raw, ok, err := q.store.Get(key)
	if err != nil {
		return err
	}
	count := uint32(1)
	if ok && len(raw) == 4 {
		c := binary.LittleEndian.Uint32(raw)
		if c < 0x7fffffff {
			count = c + 1
		}
	}
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], count)
	if err := q.store.Put(key, buf[:]); err != nil {
		return err
	}
	if query != "" {
		// 同时存一份最近 query
		_ = q.store.Put(lastQueryKey, []byte(query))
	}
	return nil
}

// ComboCount 返回 (query, path) 被选择的次数。
// 未记录过返回 0。
func (q *QueryTracker) ComboCount(query, path string) uint32 {
	if query == "" || path == "" {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	raw, ok, err := q.store.Get(comboKey(query, path))
	if err != nil || !ok || len(raw) != 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(raw)
}

// ComboBoost 返回应用于该 (query, path) 的评分 boost（约 0 ~ 30）。
//   - 未命中: 0
//   - 命中 1 次: 10
//   - 命中 3 次: 20
//   - 命中 >= 5 次: 30
func (q *QueryTracker) ComboBoost(query, path string) int16 {
	c := q.ComboCount(query, path)
	switch {
	case c == 0:
		return 0
	case c < 2:
		return 10
	case c < 5:
		return 20
	default:
		return 30
	}
}

// LastQuery 返回最近一次 Record 记录的 query（空串表示无）。
func (q *QueryTracker) LastQuery() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	raw, ok, err := q.store.Get(lastQueryKey)
	if err != nil || !ok {
		return ""
	}
	return string(raw)
}

// TopCombosForQuery 返回最近若干命中最高的 path。最多 topN 个。
func (q *QueryTracker) TopCombosForQuery(query string, topN int) []string {
	if query == "" || topN <= 0 {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	prefix := comboPrefix + query + "\x00"
	// 遍历并收集
	type entry struct {
		path  string
		count uint32
	}
	entries := make([]entry, 0, 8)
	_ = q.store.ForEach(prefix, func(k string, v []byte) bool {
		if len(k) < len(prefix) || k[:len(prefix)] != prefix {
			return true
		}
		p := k[len(prefix):]
		var c uint32
		if len(v) == 4 {
			c = binary.LittleEndian.Uint32(v)
		}
		entries = append(entries, entry{p, c})
		return true
	})
	// 按 count 降序、path 字典序排列（小范围，不需要优先队列）
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].count > entries[i].count ||
				(entries[j].count == entries[i].count && entries[j].path < entries[i].path) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
	if len(entries) > topN {
		entries = entries[:topN]
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.path
	}
	return out
}

// ForEach 遍历所有 combo 记录（前缀 comboPrefix）。用于调试与导出。
func (q *QueryTracker) ForEach(fn func(query, path string, count uint32) bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.store.ForEach(comboPrefix, func(kvKey string, val []byte) bool {
		rest := strings.TrimPrefix(kvKey, comboPrefix)
		sep := strings.Index(rest, "\x00")
		if sep < 0 {
			return true
		}
		query, path := rest[:sep], rest[sep+1:]
		var c uint32
		if len(val) == 4 {
			c = binary.LittleEndian.Uint32(val)
		}
		return fn(query, path, c)
	})
}

// Close 释放资源
func (q *QueryTracker) Close() error {
	if q == nil || q.store == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.store.Close()
}
