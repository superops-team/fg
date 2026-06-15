package frecency

import "github.com/superops-team/fg/kv"

// newMemStore 返回一个内存 KVStore（用于测试/无持久化场景）
func newMemStore() *kv.MemStore { return kv.NewMemStore() }
