package kv

import (
	"time"

	"go.etcd.io/bbolt"
)

// BoltStore 基于 bbolt 的 KV 实现
type BoltStore struct {
	db       *bbolt.DB
	bucket   []byte
}

// OpenBoltStore 在 path 处打开一个 bbolt 文件，使用指定 bucket。
// bucket 不存在会自动创建。
func OpenBoltStore(path string, bucketName string, opts Options) (*BoltStore, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: timeout, ReadOnly: opts.ReadOnly})
	if err != nil {
		return nil, err
	}
	bucket := []byte(bucketName)
	// 预创建 bucket
	if !opts.ReadOnly {
		if err := db.Update(func(tx *bbolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists(bucket)
			return err
		}); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return &BoltStore{db: db, bucket: bucket}, nil
}

// normKey 归一化 key：bbolt 不允许空 key，所以将空串映射为 \x00。
// 这样与 MemStore 行为一致（外部都能读写空 key）。
func normKey(key string) []byte {
	if key == "" {
		return []byte{0x00}
	}
	return []byte(key)
}

func (s *BoltStore) Put(key string, value []byte) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(s.bucket)
		if b == nil {
			return nil
		}
		if value == nil {
			return b.Delete(normKey(key))
		}
		return b.Put(normKey(key), value)
	})
}

func (s *BoltStore) Get(key string) ([]byte, bool, error) {
	var (
		out []byte
		ok  bool
	)
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(s.bucket)
		if b == nil {
			return nil
		}
		v := b.Get(normKey(key))
		if v == nil {
			return nil
		}
		ok = true
		out = make([]byte, len(v))
		copy(out, v)
		return nil
	})
	return out, ok, err
}

func (s *BoltStore) ForEach(prefix string, fn func(key string, value []byte) bool) error {
	return s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(s.bucket)
		if b == nil {
			return nil
		}
		c := b.Cursor()
		// 前缀匹配：空前缀遍历全部；非空前缀按字符串前缀匹配
		hasPrefix := len(prefix) > 0
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if hasPrefix {
				// 字节层面的前缀比较，保持与 MemStore 的字典序一致
				if len(k) < len(prefix) || string(k[:len(prefix)]) != prefix {
					continue
				}
			}
			// 还原 key（如果是 \x00 表示空 key）
			key := string(k)
			if len(k) == 1 && k[0] == 0x00 {
				key = ""
			}
			cp := make([]byte, len(v))
			copy(cp, v)
			if !fn(key, cp) {
				return nil
			}
		}
		return nil
	})
}

func (s *BoltStore) Close() error { return s.db.Close() }
