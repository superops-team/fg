package bigram

import (
	"math"
	"strings"
	"sync"
)

// Bigram 是 64KB 指针表 + slice
type Bigram struct {
	mu        sync.RWMutex
	index     [math.MaxUint16 + 1][]uint32
	fileCount int
}

func NewBigram() *Bigram { return &Bigram{} }

func makeBigram(a, b byte) uint16 { return uint16(a)<<8 | uint16(b) }

func normalizeChar(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}

// Build 根据 path array 构建索引。
func (b *Bigram) Build(paths []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.index = [math.MaxUint16 + 1][]uint32{}
	b.fileCount = len(paths)
	for i, path := range paths {
		idx := uint32(i)
		p := strings.ToLower(path)
		if len(p) < 2 {
			continue
		}
		prev := uint16(0xFFFF)
		for j := 0; j < len(p)-1; j++ {
			bg := makeBigram(normalizeChar(p[j]), normalizeChar(p[j+1]))
			if bg == prev {
				continue
			}
			prev = bg
			list := b.index[bg]
			if len(list) > 0 && list[len(list)-1] == idx {
				continue
			}
			b.index[bg] = append(list, idx)
		}
	}
}

func (b *Bigram) FileCount() int {
	b.mu.RLock()
	n := b.fileCount
	b.mu.RUnlock()
	return n
}

// extractQueryBigrams 从查询串提取 unique bigram，跳过空白。
func extractQueryBigrams(q string) []uint16 {
	out := make([]uint16, 0, len(q))
	q = strings.ToLower(q)
	prev := uint16(0xFFFF)
	for i := 0; i < len(q)-1; i++ {
		a, b := q[i], q[i+1]
		if a == ' ' || a == '\t' || a == '\n' || b == ' ' || b == '\t' || b == '\n' {
			continue
		}
		bg := makeBigram(normalizeChar(a), normalizeChar(b))
		if bg == prev {
			continue
		}
		prev = bg
		out = append(out, bg)
	}
	return out
}

// Candidates 返回候选文件 index 集合（AND 语义）。
// 空查询返回 nil，未命中查询返回空切片。
func (b *Bigram) Candidates(query string) []uint32 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if query == "" || b.fileCount == 0 {
		return nil
	}
	qBigrams := extractQueryBigrams(query)
	if len(qBigrams) == 0 {
		return nil
	}
	var sets [][]uint32
	for _, bg := range qBigrams {
		s := b.index[bg]
		if len(s) == 0 {
			return []uint32{}
		}
		sets = append(sets, s)
	}
	// 以最小集合做 baseline，依次做 AND 交集
	minIdx := 0
	for i := 1; i < len(sets); i++ {
		if len(sets[i]) < len(sets[minIdx]) {
			minIdx = i
		}
	}
	sets[0], sets[minIdx] = sets[minIdx], sets[0]
	baseline := sets[0]
	rest := sets[1:]
	if len(rest) == 0 {
		out := make([]uint32, len(baseline))
		copy(out, baseline)
		return out
	}
	result := make([]uint32, 0, len(baseline))
	for _, v := range baseline {
		found := true
		for _, s := range rest {
			if !sortedContains(s, v) {
				found = false
				break
			}
		}
		if found {
			result = append(result, v)
		}
	}
	return result
}

// Matches 返回 file 是否在 or 语义上匹配查询（任意 bigram 命中即视为可能命中）。
func (b *Bigram) Matches(query string, fileIndex uint32) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	qBigrams := extractQueryBigrams(query)
	if len(qBigrams) == 0 {
		return true // 无 bigram ，放过
	}
	for _, bg := range qBigrams {
		if sortedContains(b.index[bg], fileIndex) {
			return true
		}
	}
	return false
}

// sortedContains 做二分查找。
func sortedContains(s []uint32, v uint32) bool {
	lo, hi := 0, len(s)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case s[mid] == v:
			return true
		case s[mid] < v:
			lo = mid + 1
		default:
			hi = mid - 1
		}
	}
	return false
}

// BigramOverlay 是增量索引（watcher 用）。
type BigramOverlay struct {
	mu        sync.RWMutex
	index     [math.MaxUint16 + 1][]uint32
	fileCount int
}

func NewOverlay() *BigramOverlay { return &BigramOverlay{} }

func (o *BigramOverlay) Add(fileIndex uint32, path string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.fileCount++
	p := strings.ToLower(path)
	if len(p) < 2 {
		return
	}
	prev := uint16(0xFFFF)
	for j := 0; j < len(p)-1; j++ {
		bg := makeBigram(normalizeChar(p[j]), normalizeChar(p[j+1]))
		if bg == prev {
			continue
		}
		prev = bg
		list := o.index[bg]
		if len(list) > 0 && list[len(list)-1] == fileIndex {
			continue
		}
		o.index[bg] = append(list, fileIndex)
	}
}

func (o *BigramOverlay) FileCount() int {
	o.mu.RLock()
	n := o.fileCount
	o.mu.RUnlock()
	return n
}

func (o *BigramOverlay) Candidates(query string) []uint32 {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if query == "" || o.fileCount == 0 {
		return nil
	}
	qBigrams := extractQueryBigrams(query)
	if len(qBigrams) == 0 {
		return nil
	}
	var sets [][]uint32
	for _, bg := range qBigrams {
		s := o.index[bg]
		if len(s) == 0 {
			return []uint32{}
		}
		sets = append(sets, s)
	}
	minIdx := 0
	for i := 1; i < len(sets); i++ {
		if len(sets[i]) < len(sets[minIdx]) {
			minIdx = i
		}
	}
	sets[0], sets[minIdx] = sets[minIdx], sets[0]
	baseline := sets[0]
	rest := sets[1:]
	if len(rest) == 0 {
		out := make([]uint32, len(baseline))
		copy(out, baseline)
		return out
	}
	result := make([]uint32, 0, len(baseline))
	for _, v := range baseline {
		found := true
		for _, s := range rest {
			if !sortedContains(s, v) {
				found = false
				break
			}
		}
		if found {
			result = append(result, v)
		}
	}
	return result
}

func (o *BigramOverlay) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.index = [math.MaxUint16 + 1][]uint32{}
	o.fileCount = 0
}
