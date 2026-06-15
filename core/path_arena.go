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
// 若索引越界（Arena 已重置或 ChunkedPath 来自不同 Arena），返回空串。
func (a *PathArena) Get(cp ChunkedPath) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if cp.Index >= uint32(len(a.strings)) {
		return ""
	}
	return a.strings[cp.Index]
}

func (a *PathArena) Filename(cp ChunkedPath) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if cp.Index >= uint32(len(a.strings)) {
		return ""
	}
	s := a.strings[cp.Index]
	if cp.FilenameOffset >= uint32(len(s)) {
		return ""
	}
	return s[cp.FilenameOffset:]
}

func (a *PathArena) Dir(cp ChunkedPath) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if cp.Index >= uint32(len(a.strings)) {
		return ""
	}
	s := a.strings[cp.Index]
	if cp.FilenameOffset == 0 || cp.FilenameOffset > uint32(len(s)) {
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
