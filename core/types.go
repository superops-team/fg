package core

import "sync/atomic"

// ========================================
// ChunkedPath: 路径引用（v1 = PathArena 中的 index + filename 偏移量
// 值类型，可安全复制/放 slice
// ========================================

type ChunkedPath struct {
	Index          uint32 // 在 PathArena.strings 中的 index
	FilenameOffset uint32 // 相对路径内最后一个 '/' 之后的字节位置；没有斜杠时为 0
}

// ========================================
// 标志位常量（FileItem.flags 用 uint32，避免 noCopy 字段）
// 位 0: deleted 位 1: binary 位 2: overflow
// ========================================

const (
	flagDeleted  uint32 = 1 << 0
	flagBinary   uint32 = 1 << 1
	flagOverflow uint32 = 1 << 2
)

// ========================================
// FileItem: 单个索引文件项
// 设计要点:
//   - 无 noCopy 字段，所以可放入 []FileItem 并 append（可扩容复制
//   - flags 用 atomic 包函数操作，保证搜索路径读时无需持锁
//   - gitStatusPtr 用普通指针字段：无锁读取，写入时由 RWMutex 保护
// ========================================

type FileItem struct {
	Size                       uint64
	Modified                   uint64 // unix seconds
	AccessFrecencyScore       int16
	ModificationFrecencyScore int16
	ParentDirIndex             uint32 // 索引到 []DirItem；MaxUint32 表示未关联
	Path                       ChunkedPath
	Flags                      uint32 // 用 atomic 操作，位掩码见 flag* 常量
	GitStatusPtr             *GitStatus // 读路径直接读，写路径需外部持锁
}

// IsDeleted 返回文件是否被标记为已删除（tombstone）。
func (f *FileItem) IsDeleted() bool { return atomic.LoadUint32(&f.Flags)&flagDeleted != 0 }

// SetDeleted 设置 deleted 标记。
func (f *FileItem) SetDeleted(v bool) {
	if v {
		atomic.OrUint32(&f.Flags, flagDeleted)
	} else {
		atomic.AndUint32(&f.Flags, ^flagDeleted)
	}
}

func (f *FileItem) IsBinary() bool   { return atomic.LoadUint32(&f.Flags)&flagBinary != 0 }
func (f *FileItem) SetBinary(v bool) {
	if v { atomic.OrUint32(&f.Flags, flagBinary) } else { atomic.AndUint32(&f.Flags, ^flagBinary) }
}

func (f *FileItem) IsOverflow() bool  { return atomic.LoadUint32(&f.Flags)&flagOverflow != 0 }
func (f *FileItem) SetOverflow(v bool) {
	if v { atomic.OrUint32(&f.Flags, flagOverflow) } else { atomic.AndUint32(&f.Flags, ^flagOverflow) }
}

// TotalFrecency 返回访问 frecency 分数之和
func (f *FileItem) TotalFrecency() int32 {
	return int32(f.AccessFrecencyScore) + int32(f.ModificationFrecencyScore)
}

// GitStatus 返回当前 GitStatus（nil = 未探测）
func (f *FileItem) GitStatus() *GitStatus { return f.GitStatusPtr }

// SetGitStatus 设置 GitStatus。nil 表示"未探测"状态。
func (f *FileItem) SetGitStatus(gs *GitStatus) { f.GitStatusPtr = gs }

// ========================================
// DirItem: 目录项
// MaxAccessFrecency 用 atomic 读写，保证并发安全 max 更新
// ========================================

type DirItem struct {
	Path              ChunkedPath
	LastSegmentOffset uint32
	MaxAccessFrecency int32
}

func (d *DirItem) GetMaxAccessFrecency() int32 { return atomic.LoadInt32(&d.MaxAccessFrecency) }

// UpdateFrecencyIfLarger 仅在 v 大于当前值时原子地更新最大值
func (d *DirItem) UpdateFrecencyIfLarger(v int32) {
	for {
		cur := atomic.LoadInt32(&d.MaxAccessFrecency)
		if v <= cur {
			return
		}
		if atomic.CompareAndSwapInt32(&d.MaxAccessFrecency, cur, v) {
			return
		}
	}
}

// ========================================
// PaginationArgs: 搜索分页参数
// ========================================

type PaginationArgs struct {
	Offset int
	Limit  int // 0 表示无上限
}
