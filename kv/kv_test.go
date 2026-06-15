package kv

import (
	"os"
	"path/filepath"
	"testing"
)

// 测试一个具体实现：通过构造工厂注入
func runStoreTests(t *testing.T, name string, factory func() (KVStore, func(), error)) {
	t.Helper()
	t.Run(name+"/PutGet", func(t *testing.T) {
		store, cleanup, err := factory()
		if err != nil {
			t.Fatalf("打开失败: %v", err)
		}
		defer cleanup()

		// 空 key 读
		if v, ok, err := store.Get("missing"); err != nil || ok || v != nil {
			t.Fatalf("missing key 应返回 (nil,false,nil)， got (%v,%v,%v)", v, ok, err)
		}
		// 写入
		if err := store.Put("foo", []byte("bar")); err != nil {
			t.Fatalf("Put 失败: %v", err)
		}
		// 再读
		v, ok, err := store.Get("foo")
		if err != nil || !ok || string(v) != "bar" {
			t.Fatalf("Get(foo)=(%q,%v,%v)", v, ok, err)
		}
		// 覆盖
		if err := store.Put("foo", []byte("baz")); err != nil {
			t.Fatal(err)
		}
		v, ok, _ = store.Get("foo")
		if !ok || string(v) != "baz" {
			t.Fatalf("覆盖写入后应返回 baz， got (%q,%v)", v, ok)
		}
		// 删除
		if err := store.Put("foo", nil); err != nil {
			t.Fatal(err)
		}
		_, ok, _ = store.Get("foo")
		if ok {
			t.Fatal("Put(nil) 应删除 key")
		}
	})

	t.Run(name+"/ForEach", func(t *testing.T) {
		store, cleanup, err := factory()
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()

		// 准备 10 个 key
		for i := 0; i < 10; i++ {
			if err := store.Put("k"+string(rune('0'+i)), []byte{byte(i)}); err != nil {
				t.Fatal(err)
			}
		}
		// 遍历前缀 "k"
		count := 0
		if err := store.ForEach("k", func(key string, value []byte) bool {
			count++
			return true
		}); err != nil {
			t.Fatalf("ForEach 失败: %v", err)
		}
		if count != 10 {
			t.Fatalf("ForEach(k) 应返回 10 条， got %d", count)
		}
		// 测试中断：仅取前 3 条
		count = 0
		if err := store.ForEach("k", func(key string, value []byte) bool {
			count++
			return count < 3
		}); err != nil {
			t.Fatal(err)
		}
		if count != 3 {
			t.Fatalf("提前中止应返回 3， got %d", count)
		}
		// 空前缀应遍历全部
		count = 0
		if err := store.ForEach("", func(key string, value []byte) bool {
			count++
			return true
		}); err != nil {
			t.Fatal(err)
		}
		if count != 10 {
			t.Fatalf("空前缀应返回 10， got %d", count)
		}
	})

	t.Run(name+"/EmptyKey", func(t *testing.T) {
		store, cleanup, err := factory()
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()
		// 空 key 应该能正常读写
		if err := store.Put("", []byte("empty")); err != nil {
			t.Fatal(err)
		}
		v, ok, err := store.Get("")
		if err != nil || !ok || string(v) != "empty" {
			t.Fatalf("空 key 读取失败: (%q,%v,%v)", v, ok, err)
		}
	})

	t.Run(name+"/Concurrent", func(t *testing.T) {
		store, cleanup, err := factory()
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()
		// 并发写入 200 条
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := 0; i < 200; i++ {
				key := "c" + string(rune('a'+i%26)) + string(rune('0'+i/26%10))
				_ = store.Put(key, []byte{byte(i)})
			}
		}()
		// 并发读取
		for i := 0; i < 200; i++ {
			_, _, _ = store.Get("c" + string(rune('a'+i%26)))
		}
		<-done
	})
}

// === 内存实现测试 ===
func TestMemStore(t *testing.T) {
	runStoreTests(t, "Mem", func() (KVStore, func(), error) {
		s := NewMemStore()
		return s, func() { _ = s.Close() }, nil
	})
}

// === bbolt 实现测试 ===
func TestBoltStore(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过 bolt 持久化测试 (-short)")
	}
	runStoreTests(t, "Bolt", func() (KVStore, func(), error) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.db")
		s, err := OpenBoltStore(path, "test_bucket", DefaultOptions())
		if err != nil {
			return nil, func() {}, err
		}
		cleanup := func() {
			_ = s.Close()
			_ = os.Remove(path)
		}
		return s, cleanup, nil
	})
}

// 测试 bolt 持久性：Close 后重新 Open 还能读到数据
func TestBoltStore_Persistence(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.db")

	s, err := OpenBoltStore(path, "frecency", DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("key1", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// 重新打开
	s2, err := OpenBoltStore(path, "frecency", DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	v, ok, err := s2.Get("key1")
	if err != nil || !ok || string(v) != "v1" {
		t.Fatalf("重新打开后应读到 v1， got (%q,%v,%v)", v, ok, err)
	}
}

// ================================================================
// 回归: Overwrite 后 ok 必须为 true 且 value 被更新
//     Bug: 原实现中 ok 变量声明后未检查，导致覆盖写入的
//     ok 状态未验证。确保覆盖写入后 ok 为 true。
// ================================================================

func TestKV_OverwriteReturnsOk(t *testing.T) {
	stores := []struct {
		name    string
		factory func() (KVStore, func(), error)
	}{
		{"Mem", func() (KVStore, func(), error) {
			s := NewMemStore()
			return s, func() { _ = s.Close() }, nil
		}},
	}
	if !testing.Short() {
		stores = append(stores, struct {
			name    string
			factory func() (KVStore, func(), error)
		}{
			"Bolt", func() (KVStore, func(), error) {
				dir := t.TempDir()
				path := filepath.Join(dir, "overwrite.db")
				s, err := OpenBoltStore(path, "test", DefaultOptions())
				if err != nil {
					return nil, func() {}, err
				}
				return s, func() { _ = s.Close() }, nil
			},
		})
	}

	for _, st := range stores {
		t.Run(st.name, func(t *testing.T) {
			store, cleanup, err := st.factory()
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()

			if err := store.Put("k", []byte("v1")); err != nil {
				t.Fatalf("第一次 Put 失败: %v", err)
			}
			v, ok, err := store.Get("k")
			if err != nil || !ok || string(v) != "v1" {
				t.Fatalf("Get 1: (%q,%v,%v)", v, ok, err)
			}
			if err := store.Put("k", []byte("v2")); err != nil {
				t.Fatalf("第二次 Put 失败: %v", err)
			}
			v, ok, err = store.Get("k")
			if err != nil {
				t.Fatalf("覆盖后 Get 错误: %v", err)
			}
			if !ok {
				t.Fatal("覆盖后 ok 应为 true")
			}
			if string(v) != "v2" {
				t.Fatalf("覆盖后 value 应为 v2, got %q", v)
			}
		})
	}
}
