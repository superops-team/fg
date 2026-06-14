package frecency

import (
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"time"
)

// Mode 控制 frecency 的灵敏度：
//   - ModeNeovim: half-life = 168h (1 周)，对近期访问给高分
//   - ModeAI:     half-life = 720h (30 天)，更看重长期历史
type Mode int

const (
	ModeNeovim Mode = iota
	ModeAI
)

// FrecencyTracker 维护文件的 frecency 评分。
// 内部通过 kv.KVStore 持久化；可通过 SetClock 注入时间做 deterministic 测试。
type FrecencyTracker struct {
	store      interface {
		Put(key string, value []byte) error
		Get(key string) ([]byte, bool, error)
		Close() error
	}
	mu         sync.Mutex
	halfLifeH  float64
	nowFn      func() time.Time // 可注入时间，测试用
	mode       Mode
}

// Options 控制 FrecencyTracker 的行为
type Options struct {
	Mode     Mode
	NowFunc  func() time.Time // 可选：注入时间源；nil 时使用 time.Now
	// HalfLifeHours 可选：自定义衰减半衰期；0 时按 Mode 使用默认
	HalfLifeHours float64
}

// New 返回一个内存 frecency tracker（不持久化）
func New(opts Options) *FrecencyTracker {
	return newTracker(opts, newMemStore())
}

// Open 打开一个持久化 frecency tracker（需要 store 提供 Put/Get/Close）。
func Open(store interface {
	Put(key string, value []byte) error
	Get(key string) ([]byte, bool, error)
	Close() error
}, opts Options) *FrecencyTracker {
	return newTracker(opts, store)
}

func newTracker(opts Options, store interface {
	Put(key string, value []byte) error
	Get(key string) ([]byte, bool, error)
	Close() error
}) *FrecencyTracker {
	halfLife := opts.HalfLifeHours
	if halfLife <= 0 {
		switch opts.Mode {
		case ModeAI:
			halfLife = 720
		default:
			halfLife = 168
		}
	}
	nowFn := opts.NowFunc
	if nowFn == nil {
		nowFn = time.Now
	}
	return &FrecencyTracker{
		store:     store,
		halfLifeH: halfLife,
		nowFn:     nowFn,
		mode:      opts.Mode,
	}
}

// === 序列化格式（小端） ===
//   [0:4]  count (int32)
//   [4:12] lastAccessUnix (int64)
const recordSize = 4 + 8

func encode(count int32, lastUnix int64) []byte {
	b := make([]byte, recordSize)
	binary.LittleEndian.PutUint32(b[0:4], uint32(count))
	binary.LittleEndian.PutUint64(b[4:12], uint64(lastUnix))
	return b
}

func decode(b []byte) (count int32, lastUnix int64, ok bool) {
	if len(b) != recordSize {
		return 0, 0, false
	}
	count = int32(binary.LittleEndian.Uint32(b[0:4]))
	lastUnix = int64(binary.LittleEndian.Uint64(b[4:12]))
	if count < 0 {
		count = 0
	}
	return count, lastUnix, true
}

// Touch 记录一次对 path 的访问，更新评分状态
func (t *FrecencyTracker) Touch(path string) error {
	if path == "" {
		return errors.New("path is empty")
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	var count int32 = 1
	var lastUnix int64 = t.nowFn().Unix()

	if raw, ok, err := t.store.Get(path); err == nil && ok {
		if c, _, good := decode(raw); good {
			count = c + 1
			if count < 0 {
				count = 1
			}
			lastUnix = t.nowFn().Unix()
		}
	} else if err != nil {
		return err
	}
	return t.store.Put(path, encode(count, lastUnix))
}

// decayFactor 根据时长与半衰期返回衰减系数 [0, 1]
func decayFactor(hoursSince, halfLifeHours float64) float64 {
	if hoursSince <= 0 {
		return 1.0
	}
	// f = 2^(-hours / halfLife)
	// 用自然指数避免 2^x 在极端值精度问题
	return math.Exp(-hoursSince / halfLifeHours * math.Ln2)
}

// GetAccessScore 返回 path 的访问 frecency 评分（约 [-50, 100]）。
// path 未被 Touch 过返回 0。
func (t *FrecencyTracker) GetAccessScore(path string) int32 {
	if t == nil || path == "" {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	raw, ok, err := t.store.Get(path)
	if err != nil || !ok {
		return 0
	}
	count, lastUnix, ok := decode(raw)
	if !ok || count <= 0 {
		return 0
	}
	hoursSince := float64(t.nowFn().Unix()-lastUnix) / 3600.0
	if hoursSince < 0 {
		hoursSince = 0
	}
	factor := decayFactor(hoursSince, t.halfLifeH)
	score := float64(count) * factor * 10.0 // 比例系数，让常见值落在 [-50, 100]
	// clamp
	if score > 100 {
		score = 100
	}
	if score < -50 {
		score = -50
	}
	return int32(score)
}

// GetModificationScore 基于文件最后修改时间与 git status 返回评分（约 [-20, 80]）。
func (t *FrecencyTracker) GetModificationScore(modUnix int64, isDirty bool) int16 {
	if modUnix <= 0 {
		return 0
	}
	daysAgo := float64(t.nowFn().Unix()-modUnix) / 86400.0
	var score int
	switch {
	case daysAgo < 0:
		score = 50 // 时钟漂移：给安全值
	case daysAgo < 1:
		score = 80
	case daysAgo < 7:
		score = 40
	case daysAgo < 30:
		score = 10
	case daysAgo >= 365:
		score = -20
	default:
		// 30 ~ 365 天：线性衰减
		tNorm := (daysAgo - 30) / (365 - 30)
		score = int(10 - 30*tNorm)
	}
	if isDirty {
		score += 20
	}
	if score > 80 {
		score = 80
	}
	if score < -20 {
		score = -20
	}
	return int16(score)
}

func (t *FrecencyTracker) Close() error {
	if t == nil || t.store == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.store.Close()
}
