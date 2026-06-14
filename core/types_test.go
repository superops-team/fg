package core

import (
	"sync"
	"testing"
)

// ========================================
// FileItem: 标志位测试
// ========================================

func TestFileItem_DefaultFlags(t *testing.T) {
	f := FileItem{}
	if f.IsDeleted() {
		t.Fatal("新 FileItem 不应是 deleted")
	}
	if f.IsBinary() {
		t.Fatal("新 FileItem 不应是 binary")
	}
	if f.IsOverflow() {
		t.Fatal("新 FileItem 不应是 overflow")
	}
}

func TestFileItem_SetDeleted(t *testing.T) {
	f := FileItem{}
	f.SetDeleted(true)
	if !f.IsDeleted() {
		t.Fatal("SetDeleted(true) 后应返回 true")
	}
	f.SetDeleted(false)
	if f.IsDeleted() {
		t.Fatal("SetDeleted(false) 后应返回 false")
	}
	// 其他标志位不应被污染
	if f.IsBinary() || f.IsOverflow() {
		t.Fatal("其他标志位被意外污染")
	}
}

func TestFileItem_SetBinary_Concurrent(t *testing.T) {
	f := FileItem{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.SetBinary(true)
		}()
	}
	wg.Wait()
	if !f.IsBinary() {
		t.Fatal("并发 SetBinary 后应为 true")
	}
}

func TestFileItem_SetOverflow(t *testing.T) {
	f := FileItem{}
	f.SetOverflow(true)
	if !f.IsOverflow() {
		t.Fatal("SetOverflow(true) 后应为 true")
	}
	f.SetOverflow(false)
	if f.IsOverflow() {
		t.Fatal("SetOverflow(false) 后应为 false")
	}
}

func TestFileItem_MultipleFlagsIndependent(t *testing.T) {
	f := FileItem{}
	f.SetDeleted(true)
	f.SetBinary(true)
	f.SetOverflow(true)
	if !f.IsDeleted() || !f.IsBinary() || !f.IsOverflow() {
		t.Fatal("三个标志位应同时为 true")
	}
	f.SetBinary(false)
	if !f.IsDeleted() || f.IsBinary() || !f.IsOverflow() {
		t.Fatal("SetBinary(false) 后仅 binary 应受影响")
	}
}

// ========================================
// FileItem: frecency 与其他方法
// ========================================

func TestFileItem_TotalFrecency(t *testing.T) {
	tests := []struct {
		name     string
		access   int16
		mod      int16
		expected int32
	}{
		{"zero", 0, 0, 0},
		{"正数", 10, 20, 30},
		{"负值", -5, -3, -8},
		{"边界 int16 max", 32767, 32767, 65534},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := FileItem{
				AccessFrecencyScore:       tt.access,
				ModificationFrecencyScore: tt.mod,
			}
			if got := f.TotalFrecency(); got != tt.expected {
				t.Fatalf("TotalFrecency()=%d, want %d", got, tt.expected)
			}
		})
	}
}

// ========================================
// GitStatus 原子指针
// ========================================

func TestFileItem_GitStatus_NilByDefault(t *testing.T) {
	f := FileItem{}
	if got := f.GitStatus(); got != nil {
		t.Fatalf("默认 GitStatus 应为 nil, got %+v", got)
	}
}

func TestFileItem_GitStatus_SetAndLoad(t *testing.T) {
	f := FileItem{}
	gs := &GitStatus{Kind: GitStatusModified}
	f.SetGitStatus(gs)
	got := f.GitStatus()
	if got == nil {
		t.Fatal("SetGitStatus 后不应为 nil")
	}
	if got.Kind != GitStatusModified {
		t.Fatalf("Kind=%v, want Modified", got.Kind)
	}
	// 再次设为 Clean
	f.SetGitStatus(&GitStatus{Kind: GitStatusClean})
	got = f.GitStatus()
	if got.Kind != GitStatusClean {
		t.Fatalf("第二次 Set 后 Kind=%v", got.Kind)
	}
}

// ========================================
// DirItem: frecency 原子更新
// ========================================

func TestDirItem_MaxAccessFrecency_InitZero(t *testing.T) {
	d := DirItem{}
	if got := d.GetMaxAccessFrecency(); got != 0 {
		t.Fatalf("初始值应为 0, got %d", got)
	}
}

func TestDirItem_UpdateFrecencyIfLarger(t *testing.T) {
	d := DirItem{}
	d.UpdateFrecencyIfLarger(10)
	if got := d.GetMaxAccessFrecency(); got != 10 {
		t.Fatalf("第一次更新后应为 10, got %d", got)
	}
	// 更小的值不应覆盖
	d.UpdateFrecencyIfLarger(5)
	if got := d.GetMaxAccessFrecency(); got != 10 {
		t.Fatalf("更小值不应覆盖, got %d", got)
	}
	// 更大的值应覆盖
	d.UpdateFrecencyIfLarger(20)
	if got := d.GetMaxAccessFrecency(); got != 20 {
		t.Fatalf("更大值应覆盖, got %d", got)
	}
}

func TestDirItem_UpdateFrecencyIfLarger_Concurrent(t *testing.T) {
	d := DirItem{}
	var wg sync.WaitGroup
	for i := 1; i <= 100; i++ {
		wg.Add(1)
		go func(v int32) {
			defer wg.Done()
			d.UpdateFrecencyIfLarger(v)
		}(int32(i))
	}
	wg.Wait()
	if got := d.GetMaxAccessFrecency(); got != 100 {
		t.Fatalf("并发写入后最大值应为 100, got %d", got)
	}
}

// ========================================
// FileItem: 可放入 slice（无 noCopy 字段）
// ========================================

func TestFileItem_CopyableInSlice(t *testing.T) {
	// 这个测试如果通过，说明 FileItem 无 noCopy 字段，可放入 []FileItem
	f1 := FileItem{Size: 100, Modified: 200}
	f2 := f1 // 复制
	if f2.Size != 100 || f2.Modified != 200 {
		t.Fatalf("复制后字段不匹配")
	}
	// 放到 slice 中，append（会触发扩容复制）
	s := make([]FileItem, 0, 1)
	for i := 0; i < 10; i++ {
		s = append(s, FileItem{Size: uint64(i)})
	}
	if len(s) != 10 {
		t.Fatalf("slice 长度应为 10, got %d", len(s))
	}
}

// ========================================
// PaginationArgs
// ========================================

func TestPaginationArgs_ZeroValue(t *testing.T) {
	p := PaginationArgs{}
	if p.Offset != 0 || p.Limit != 0 {
		t.Fatalf("零值应为 (0,0), got (%d,%d)", p.Offset, p.Limit)
	}
}

func TestPaginationArgs_SliceBounds(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	tests := []struct {
		name   string
		page   PaginationArgs
		expect []int
	}{
		{"first 3", PaginationArgs{Offset: 0, Limit: 3}, []int{1, 2, 3}},
		{"skip 5 take 3", PaginationArgs{Offset: 5, Limit: 3}, []int{6, 7, 8}},
		{"limit=0 => 取完", PaginationArgs{Offset: 7, Limit: 0}, []int{8, 9, 10}},
		{"offset 越界", PaginationArgs{Offset: 100, Limit: 5}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			off := tt.page.Offset
			if off >= len(items) {
				if tt.expect != nil {
					t.Fatalf("expect 应为 nil")
				}
				return
			}
			end := len(items)
			if tt.page.Limit > 0 && off+tt.page.Limit < end {
				end = off + tt.page.Limit
			}
			got := items[off:end]
			if len(got) != len(tt.expect) {
				t.Fatalf("got len=%d, want len=%d", len(got), len(tt.expect))
			}
			for i := range got {
				if got[i] != tt.expect[i] {
					t.Fatalf("items[%d]=%d, want %d", i, got[i], tt.expect[i])
				}
			}
		})
	}
}
