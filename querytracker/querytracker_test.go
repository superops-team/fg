package querytracker

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/superops-team/fg/kv"
)

func TestQueryTracker_Basic(t *testing.T) {
	q := New()
	defer q.Close()

	query := "main"
	path := "/project/src/main.go"

	if c := q.ComboCount(query, path); c != 0 {
		t.Fatalf("未记录时应返回 0， got %d", c)
	}
	// 记录 3 次
	for i := 0; i < 3; i++ {
		if err := q.Record(query, path); err != nil {
			t.Fatal(err)
		}
	}
	if c := q.ComboCount(query, path); c != 3 {
		t.Fatalf("应返回 3， got %d", c)
	}
	if b := q.ComboBoost(query, path); b != 20 {
		t.Fatalf("3 次应返回 boost 20， got %d", b)
	}
	if lq := q.LastQuery(); lq != query {
		t.Fatalf("LastQuery=%q, want %q", lq, query)
	}
}

func TestQueryTracker_TopCombos(t *testing.T) {
	q := New()
	defer q.Close()

	// 记录不同路径
	recs := []struct{ q, p string }{
		{"foo", "/a.go"},
		{"foo", "/b.go"},
		{"foo", "/a.go"},
		{"foo", "/a.go"},
		{"foo", "/c.go"},
		{"bar", "/x.go"},
	}
	for _, r := range recs {
		if err := q.Record(r.q, r.p); err != nil {
			t.Fatal(err)
		}
	}

	// top2 for "foo" 应是 /a.go(3) 和 /b.go(1)
	top := q.TopCombosForQuery("foo", 2)
	if len(top) != 2 {
		t.Fatalf("top2 应有 2 条， got %d (%v)", len(top), top)
	}
	if top[0] != "/a.go" {
		t.Fatalf("第 1 名应是 /a.go， got %q", top[0])
	}
	// top0 (n=0) 应返回 nil
	if q.TopCombosForQuery("foo", 0) != nil {
		t.Fatal("topN=0 应返回 nil")
	}
}

func TestQueryTracker_Empty(t *testing.T) {
	q := New()
	defer q.Close()

	// 两者都为空不应报错也不做事
	if err := q.Record("", ""); err != nil {
		t.Fatal(err)
	}
	if q.ComboCount("", "") != 0 {
		t.Fatal("空 combo 应返回 0")
	}
	if q.ComboBoost("", "") != 0 {
		t.Fatal("空 boost 应返回 0")
	}
	if q.LastQuery() != "" {
		t.Fatal("未记录时 LastQuery 应为空")
	}
}

func TestQueryTracker_ForEach(t *testing.T) {
	q := New()
	defer q.Close()

	_ = q.Record("hello", "/a")
	_ = q.Record("hello", "/b")
	_ = q.Record("world", "/c")

	count := 0
	if err := q.ForEach(func(q, p string, c uint32) bool {
		count++
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("ForEach 应返回 3 条， got %d", count)
	}
}

func TestQueryTracker_Concurrent(t *testing.T) {
	q := New()
	defer q.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			q := q // 捕获局部变量：实际是同一个，这里只是避免 go vet unused
			_ = q
			if err := q.Record("q"+string(rune('a'+idx%5)), "/p"+string(rune('A'+idx%5))); err != nil {
				t.Errorf("Record 失败: %v", err)
			}
			_ = q.ComboCount("qa", "/pA")
			_ = q.ComboBoost("qa", "/pA")
		}(i)
	}
	wg.Wait()
}

func TestQueryTracker_BoltPersistence(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "qt.db")

	store, err := kv.OpenBoltStore(dbPath, "querytracker", kv.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	q := Open(store)
	if err := q.Record("q1", "/foo"); err != nil {
		t.Fatal(err)
	}
	if err := q.Record("q1", "/foo"); err != nil {
		t.Fatal(err)
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}

	// 重新打开
	store2, err := kv.OpenBoltStore(dbPath, "querytracker", kv.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	q2 := Open(store2)
	defer q2.Close()

	if c := q2.ComboCount("q1", "/foo"); c != 2 {
		t.Fatalf("持久化后应读到 2， got %d", c)
	}
	if q2.LastQuery() != "q1" {
		t.Fatalf("LastQuery 应是 q1， got %q", q2.LastQuery())
	}
}
