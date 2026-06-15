package frecency

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/superops-team/fg/kv"
)

// =======================================================
// decayFactor 纯算法测试（确定常数，不需要状态）
// =======================================================
func TestDecayFactor(t *testing.T) {
	// 0 小时 -> 1.0
	if f := decayFactor(0, 168); f != 1.0 {
		t.Fatalf("decay(0)=%f, want 1.0", f)
	}
	// 1 half-life -> 0.5
	if f := decayFactor(168, 168); f < 0.499 || f > 0.501 {
		t.Fatalf("decay(half-life)=%f, want 0.5", f)
	}
	// 2 half-life -> 0.25
	if f := decayFactor(336, 168); f < 0.249 || f > 0.251 {
		t.Fatalf("decay(2*half-life)=%f, want 0.25", f)
	}
	// 非常大 -> ~0
	if f := decayFactor(168*1000, 168); f > 0.01 {
		t.Fatalf("decay(very large)=%f, want ~0", f)
	}
}

// =======================================================
// FrecencyTracker 行为测试（注入时间，deterministic）
// =======================================================
func fixedTime(y, mo, d, h, mi, s int) func() time.Time {
	t := time.Date(y, time.Month(mo), d, h, mi, s, 0, time.UTC)
	return func() time.Time { return t }
}

func TestFrecencyTracker_AccessScoreDeterministic(t *testing.T) {
	// 固定时间: 2025-01-15 12:00:00 UTC
	now := fixedTime(2025, 1, 15, 12, 0, 0)
	tr := New(Options{Mode: ModeNeovim, NowFunc: now})
	defer tr.Close()

	path := "/project/src/main.go"
	// 之前未访问，应为 0
	if s := tr.GetAccessScore(path); s != 0 {
		t.Fatalf("未访问过应为 0， got %d", s)
	}
	// 一次访问
	if err := tr.Touch(path); err != nil {
		t.Fatal(err)
	}
	// 立刻读：应为 10 (1.0 * 1 * 10)
	if s := tr.GetAccessScore(path); s != 10 {
		t.Fatalf("刚访问后应为 10， got %d", s)
	}

	// 多次访问（10 次）应累加但受 clamp 限制
	for i := 0; i < 10; i++ {
		if err := tr.Touch(path); err != nil {
			t.Fatal(err)
		}
	}
	// count=11 -> score = 11*1*10 = 110, clamp 到 100
	if s := tr.GetAccessScore(path); s != 100 {
		t.Fatalf("11 次访问应 clamp 到 100， got %d", s)
	}
}

func TestFrecencyTracker_MultiplePaths(t *testing.T) {
	now := fixedTime(2025, 1, 15, 12, 0, 0)
	tr := New(Options{Mode: ModeNeovim, NowFunc: now})
	defer tr.Close()

	for _, p := range []string{"/a", "/b", "/c"} {
		if err := tr.Touch(p); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"/a", "/b", "/c"} {
		if s := tr.GetAccessScore(p); s != 10 {
			t.Fatalf("%s: score=%d, want 10", p, s)
		}
	}
	// 未存在的路径返回 0
	if s := tr.GetAccessScore("/unknown"); s != 0 {
		t.Fatalf("未知 path 应返回 0， got %d", s)
	}
}

func TestFrecencyTracker_EmptyPath(t *testing.T) {
	tr := New(Options{})
	defer tr.Close()
	if err := tr.Touch(""); err == nil {
		t.Fatal("空串 Touch 应返回错误")
	}
	if s := tr.GetAccessScore(""); s != 0 {
		t.Fatal("空串 GetAccessScore 应返回 0")
	}
}

func TestFrecencyTracker_ModificationScore(t *testing.T) {
	now := fixedTime(2025, 1, 15, 12, 0, 0)
	tr := New(Options{Mode: ModeNeovim, NowFunc: now})
	defer tr.Close()

	// Unix 秒换算
	day := 24 * 3600
	nowUnix := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC).Unix()

	tests := []struct {
		name      string
		modUnix   int64
		isDirty   bool
		wantRange [2]int16 // [min, max]
	}{
		{"刚修改", nowUnix, false, [2]int16{70, 80}},
		{"6 天前", nowUnix - int64(6*day), false, [2]int16{30, 50}},
		{"20 天前", nowUnix - int64(20*day), false, [2]int16{5, 15}},
		{"1 年前", nowUnix - int64(400*day), false, [2]int16{-20, -20}},
		{"脏状态 + 刚修改", nowUnix, true, [2]int16{80, 80}},
		{"脏状态 + 3 天前", nowUnix - int64(3*day), true, [2]int16{60, 80}},
		{"0 表示未知", 0, false, [2]int16{0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tr.GetModificationScore(tt.modUnix, tt.isDirty)
			if s < tt.wantRange[0] || s > tt.wantRange[1] {
				t.Fatalf("modUnix=%d isDirty=%v → score=%d, want [%d,%d]",
					tt.modUnix, tt.isDirty, s, tt.wantRange[0], tt.wantRange[1])
			}
		})
	}
}

func TestFrecencyTracker_Concurrent(t *testing.T) {
	now := fixedTime(2025, 1, 15, 12, 0, 0)
	tr := New(Options{Mode: ModeNeovim, NowFunc: now})
	defer tr.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := "path_" + string(rune('a'+idx%26))
			_ = tr.Touch(path)
			_ = tr.GetAccessScore(path)
			_ = tr.GetModificationScore(1000, false)
		}(i)
	}
	wg.Wait()
}

// =======================================================
// bbolt 持久化：Close 后再 Open 能读到数据
// =======================================================
func TestFrecencyTracker_BoltPersistence(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "frecency.db")

	now1 := fixedTime(2025, 1, 15, 12, 0, 0)
	store, err := kv.OpenBoltStore(path, "frecency", kv.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	tr := Open(store, Options{Mode: ModeNeovim, NowFunc: now1})
	// 访问 3 次
	for i := 0; i < 3; i++ {
		if err := tr.Touch("foo.go"); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}

	// 重新打开，使用同样时间点
	store2, err := kv.OpenBoltStore(path, "frecency", kv.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	tr2 := Open(store2, Options{Mode: ModeNeovim, NowFunc: now1})
	defer tr2.Close()

	if s := tr2.GetAccessScore("foo.go"); s < 20 || s > 50 {
		t.Fatalf("重新打开后 score=%d, want [20,50]", s)
	}
}
