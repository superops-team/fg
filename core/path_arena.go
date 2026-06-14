package core

import "sync"

// PathArena 是 path 字符串的去重仓库。
// - Intern 追加或返回已有 index（线程安全）
// - Get 返回不可变的 string（arena 本身只追加，底层 byte slice 不重分配）
// 设计目标: 让 FileItem 仅保存 (uint32 index + uint32 filenameOffset)，
// 减少内存占用 & GC 压力，避免每个 FileItem 持有独立的 header+string 结构。
type PathArena struct {
	mu      sync.RWMutex
	strings []string
	intern  map[string]uint32
}

func NewPathArena(initialCap int) *PathArena {
	if initialCap <= 0 {
		initialCap = 16
	}
	return &PathArena{
		strings: make([]string, 0, initialCap),
		intern:  make(map[string]uint32, initialCap),
	}
}

// Intern 入或返回已有 ChunkedPath。对相同 path 幂等且线程安全。
func (a *PathArena) Intern(path string) ChunkedPath {
	// 先尝试在 RLock 下命中去重（热路径），命中则返回。
	a.mu.RLock()
	if idx, ok := a.intern[path]; ok {
		a.mu.RUnlock()
		return ChunkedPath{Index: idx, FilenameOffset: filenameOffset(path)}
	}
	a.mu.RUnlock()
	// 未命中：升级到写锁。需再检查一次（避免两个 goroutine 都走到这里时重复写入）
	a.mu.Lock()
	defer a.mu.Unlock()
	if idx, ok := a.intern[path]; ok {
		return ChunkedPath{Index: idx, FilenameOffset: filenameOffset(path)}
	}
	a.strings = append(a.strings, path)
	idx := uint32(len(a.strings) - 1)
	a.intern[path] = idx
	return ChunkedPath{Index: idx, FilenameOffset: filenameOffset(path)}
}

// Get 返回 ChunkedPath 对应原始 path 字符串。
// 读路径无锁（strings slice 只在 Intern 下 append，且底层数组扩容后旧引用仍有效；
// 但这里读 strings 长度/元素是非原子的，为安全起见仍需细粒度 —— 由于 arena 只追加，
// 元素一旦写入即可读，关键约束: **调用方**需确保底层 strings slice 不会被并发修改为更小长度。
// Intern 只追加，故 Get 只需持 RWMutex-style: 此处简化为不加锁 —— 如果上层 search 与 Intern
// 并发，上层应该用自己的 RWMutex 保证同一时刻没有 write+read。
// 为简单起见，也不在此函数做任何同步，让调用方负责。
func (a *PathArena) Get(cp ChunkedPath) string {
	a.mu.RLock()
	s := a.strings[cp.Index]
	a.mu.RUnlock()
	return s
}

func (a *PathArena) Filename(cp ChunkedPath) string {
	a.mu.RLock()
	s := a.strings[cp.Index]
	a.mu.RUnlock()
	return s[cp.FilenameOffset:]
}

func (a *PathArena) Dir(cp ChunkedPath) string {
	a.mu.RLock()
	s := a.strings[cp.Index]
	a.mu.RUnlock()
	if cp.FilenameOffset == 0 {
		return ""
	}
	return s[:cp.FilenameOffset-1]
}

func (a *PathArena) Len() int {
	a.mu.RLock()
	n := len(a.strings)
	a.mu.RUnlock()
	return n
}

// filenameOffset 返回 path 中最后一个 '/' 或 '\\' 的位置 + 1；若没有则返回 0。
// 纯函数，便于单测。
func filenameOffset(path string) uint32 {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return uint32(i + 1)
		}
	}
	return 0
}
