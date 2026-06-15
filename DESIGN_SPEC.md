# fg 设计规格（v1.1 · 发布契约落地版）

> 目标：提供一个 Go 实现的轻量级 fuzzy file finder，既可作为 library 使用，也可通过极简 CLI 使用。
> 版本原则：**先跑通，再优化；可测试，可替换；纯 Go 优先；公开契约必须可执行**。

---

## 0. 非目标 (Non-Goals)

- **不**追求逐行对齐 Rust 源码的实现细节
- **不**默认绑 CGO / libgit2 / LMDB（有纯 Go 替代）
- **不**在 v1 写手写 AVX2 asm（先 bench 再决定）
- **不**做 GUI / TUI 前端（只提供 library + 一个极简 CLI）
- **不**自己写 `.gitignore` 解析器（复用成熟库）
- **不**在 CLI MVP 引入 Cobra/Viper；当前 flag 数量少，优先使用标准库 `flag`
- **不**在本阶段产品化持久化 frecency/combo；默认 API 仍使用内存 tracker

---

## 0.1 公开发布契约（v1.1）

### Go module

- Canonical module path: `github.com/superops-team/fg`
- 所有内部 import 必须使用该 module path。
- README 中的 library 示例必须与 `go.mod` 保持一致。

### Library API

顶层包 `github.com/superops-team/fg` 提供最小稳定入口：

```go
results, err := fg.Search(root, query, limit)
results, err := fg.SearchWith(fg.Options{Root: root, Query: query, Limit: limit})
```

行为契约：

- `limit <= 0` 时默认返回最多 20 条。
- `root == ""` 或空白字符串时使用当前工作目录。
- `root` 不存在或不是目录时返回 error。
- `Options.NowFunc` 只用于测试和确定性时间约束，不改变默认行为。

### CLI MVP

CLI 入口固定为 `cmd/fg`，构建命令必须可执行：

```bash
go build -o fg ./cmd/fg
```

CLI 使用标准库 `flag` 实现，MVP flags：

| 命令 | 行为 |
|------|------|
| `fg "query"` | 在当前目录执行文件搜索，stdout 每行输出一个 path |
| `fg -r ROOT "query"` | 在指定 root 执行文件搜索 |
| `fg --score "query"` | stdout 每行输出 `score<TAB>path` |
| `fg --grep TEXT` | 在 root 下执行内容 grep，默认最多输出 20 个文件命中 |
| `fg "file query" --grep TEXT` | 先执行文件搜索，再对搜索结果执行内容 grep |
| `fg --limit N` | 限制输出数量，默认 20 |
| `fg -h` / `fg --help` | 输出帮助，退出码 0 |

I/O 约定：

- 正常结果只写 stdout。
- 错误只写 stderr。
- 使用错误退出码 1。
- 参数错误退出码 2。

CLI MVP 不提供交互式 picker、不提供 shell completion、不提供配置文件。

---

## 0.2 查询语义与错误传播契约（v1.2）

### `status:*` lazy git filtering

- `Picker.Scan()` 只负责文件系统扫描、基础 metadata、binary 探测与 bigram 构建；**不得执行 `git status`**。
- `Picker.Search()` 在解析 query 后检查是否存在 `CGitStatus` 约束。
- 只有查询包含 `status:*` 时，`Search()` 才读取当前 root 的 `git status --porcelain` 输出，并且该状态只在本次搜索内参与过滤。
- 非 git 仓库、未安装 git、或 `git status` 执行失败时，`Search()` 必须返回带上下文的 error，不得把 `status:*` 退化为全量匹配。
- 支持值：`modified`、`added`、`deleted`、`renamed`、`untracked`、`clean`、`dirty`。未知值保持向后兼容：不参与过滤。

### `grep.SearchMany` partial failure contract

- `grep.SearchMany(paths, query, limitPerFile)` 并发搜索多个文件时，一个文件失败不得阻止其他文件返回命中结果。
- 每个文件失败时记录 `fmt.Errorf("%s: %w", path, err)`。
- 所有 goroutine 结束后，返回已收集的非空命中结果，并通过 `errors.Join(errs...)` 返回聚合错误。
- 调用方可用 `errors.Is` / `errors.As` 检查聚合错误中的底层错误。

---

## 0.3 可维护性契约（v1.3）

### `picker.Search` phase decomposition

`Picker.Search()` 是搜索主控制流，但不得把解析、候选集、过滤、打分、排序、结果映射全部堆在一个长函数内。实现必须拆分为私有阶段函数：

1. 确保索引已扫描。
2. 解析 query，得到 fuzzy text、constraints 和 lazy 依赖（例如 `status:*`）。
3. 根据 fuzzy text 生成候选集。
4. 对候选执行约束过滤与评分。
5. 按 score、modified、path 稳定排序。
6. 应用 limit 并映射为 `[]Result`。

拆分后的私有函数不扩大 public API；行为仍由现有 `Search()` 契约与测试保护。

### glob matching coverage

- `matchGlob` 必须用表驱动测试覆盖普通 glob、`**` 递归 glob、大小写归一、负例与非法 pattern。
- `**` 语义为“零个或多个目录层级”：`**/*.go` 必须同时匹配 `main.go` 和 `pkg/main.go`；`src/**/*.go` 必须同时匹配 `src/main.go` 和 `src/pkg/main.go`。
- 普通 `filepath.Match` 错误（例如非法字符类）必须安全返回 false，不得 panic。

---

## 1. 架构总览

```
                  ┌──────────────────────────────┐
                  │         picker (FilePicker)  │
                  │  Scanner + Watcher + Search  │
                  └──┬──────────┬──────────┬─────┘
                     │          │          │
         ┌───────────┴──┐  ┌────┴─────┐ ┌──┴──────────┐
         │   bigram     │  │  score   │ │  grep       │
         │ (预过滤)     │  │ (打分)   │ │ (内容搜索)  │
         └────────┬─────┘  └────┬─────┘ └──────┬──────┘
                  │              │               │
          ┌───────┴──────────────┴───────────────┴─────────┐
          │                    core                        │
          │  FileItem · DirItem · PathArena · GitStatus    │
          │  ignore · binary_detect · page_pool            │
          └───┬──────────────────────────┬─────────────────┘
              │                          │
     ┌────────┴───────┐        ┌─────────┴─────────┐
     │   frecency     │        │   querytracker    │
     │ (bbolt kv)      │        │ (bbolt kv)        │
     └─────────────────┘        └───────────────────┘
                                 ▲
                                 │
     ┌───────────────────────────┴──────────────────────────┐
     │                  queryparser                         │
     │  FFFQuery · FuzzyQuery(TaggedUnion) · Constraint(TaggedUnion) │
     └──────────────────────────────────────────────────────┘
                                 ▲
                                 │
     ┌───────────────────────────┴──────────────────────────┐
     │                   shared (可选)                      │
     │  SharedFilePicker · SharedFrecency · SharedQT        │
     └──────────────────────────────────────────────────────┘
```

**并发模型**：
- `FilePicker` 内部用 `sync.RWMutex` 保护 `files/dirs/arena/bigram`
- 搜索路径：**RLock** → bigram 过滤 → 并行打分 → 释放锁 → 外部排序
- 扫描 / 写 watcher 事件：**Lock** → 更新 → 释放
- 不引入第三方并发框架；goroutine 数量由 `runtime.GOMAXPROCS` 控制

---

## 2. 包结构与职责边界

```
github.com/superops-team/fg/                ← 顶层 library API
├── core/                    ← 数据结构 + 轻量工具；零第三方依赖
│   ├── types.go             FileItem, DirItem, FileFlags, PaginationArgs
│   ├── git_status.go        GitStatus enum + 可扩展字段
│   ├── path_arena.go        PathArena + ChunkedPath（第一版用 string，v1.1 unsafe）
│   ├── binary_detect.go     16KB NUL 探测（纯 Go, 走 bytes.IndexByte）
│   ├── ignore.go            interface IgnoreMatcher + 适配器
│   └── page_pool.go         sync.Pool 封装（[]byte / []int）
├── queryparser/             ← 纯函数式解析；零依赖
│   ├── parser.go            QueryParser.Parse(string) FFFQuery
│   ├── fuzzy_query.go       FuzzyQuery 类型定义 (tagged union)
│   ├── constraint.go        Constraint 类型定义 (tagged union)
│   ├── constraint_parse.go  各约束 token 解析器
│   └── config.go            ParserConfig
├── bigram/                  ← 预过滤；零依赖
│   ├── bigram_filter.go     BigramFilter（64KB 指针表 + slices）
│   └── bigram_overlay.go    OverflowBigram（watcher 增量）
├── score/                   ← 纯打分；零依赖
│   ├── score.go             TotalScore / FuzzyMatch / sub-scorers
│   └── sort.go              TopK 部分排序
├── picker/                  ← 有依赖 (fsnotify / bbolt adapter / go-git)
│   ├── picker.go            FilePicker 主结构 + 生命周期
│   ├── options.go           FilePickerOptions + 合理默认
│   ├── scan.go              Walk + arena build + sort
│   ├── watcher.go           fsnotify 递归监听 + debounce(30ms) + overflow
│   ├── search.go            Search() 主入口 + parallel score
│   └── git_cache.go         GitStatusCache（1 次/分钟刷新）
├── frecency/                ← 有依赖 (bbolt)
│   └── frecency.go          FrecencyTracker + decay 算法 + in-mem cache
├── querytracker/            ← 有依赖 (bbolt)
│   └── querytracker.go      QueryTracker + combo_boost 规则
├── grep/                    ← 有依赖 (标准 regexp)
│   ├── grep.go              GrepOptions + 统一入口
│   ├── plain.go             bytes 纯文本（大小写不敏感预处理）
│   ├── aho_corasick.go      多模式 AC 自动机
│   └── regexp.go            regexp 回退
├── shared/                  ← 可选；RWMutex 封装
│   └── shared.go
├── kv/                      ← 内部 KV 抽象（便于切换 bbolt <-> map 测试）
│   ├── kv.go                interface KVStore { Get/Put/ForEach/Close }
│   ├── bolt.go              bbolt 实现
│   └── mem.go               map 实现（test stub）
└── cmd/
    └── fff/                 ← demo CLI，不是 library 一部分
        └── main.go
```

### 依赖清单（最小集）

| 用途 | 库 | 何时引入 |
|------|----|---------|
| 文件递归监听 | `github.com/fsnotify/fsnotify` v1.7+ | picker/watcher.go |
| KV 持久化 | `go.etcd.io/bbolt` | frecency + querytracker |
| gitignore 解析 | `github.com/go-git/go-git/v5/.../gitignore` | core/ignore_go_git.go（可替换实现） |
| git status | `os/exec` 调 `git status --porcelain=v2 -z`（第一版） | picker/git_cache.go（v1.2 可切 go-git） |
| 测试断言 | `github.com/stretchr/testify`（开发期） | 测试文件 |

**规则**：`core/`、`queryparser/`、`bigram/`、`score/` 四个包 **严格零第三方依赖**；其余包按需。

---

## 3. 核心类型细化（消除歧义 & 统一风格）

### 3.1 FileItem / DirItem

```go
// core/types.go

type fileFlags uint32          // 32 位足够；用 atomic.Bits 原子读写
const (
    flagBinary    fileFlags = 1 << 0
    flagDeleted   fileFlags = 1 << 1
    flagOverflow  fileFlags = 1 << 2
)

// FileItem 是单个索引项。ByteSize ≈ 8+8+2+2+4+4+8+4+4 = ~44 字节，
// 10 万文件约 4.4MB，完全可放入 CPU L3。
type FileItem struct {
    Size                       uint64
    Modified                   uint64   // unix seconds
    AccessFrecencyScore        int16
    ModificationFrecencyScore  int16
    ParentDirIndex             uint32   // 索引到 picker.dirs[]；MaxUint32 表示未关联
    Path                       ChunkedPath
    // 原子位字段，watcher 并发写 / search 并发读
    flags                      atomic.Uint32
    // gitStatus 用 atomic.Value 存 *GitStatus（nil 表 clean/unknown），
    // 避免 search 在读路径上拿锁
    gitStatus                  atomic.Pointer[GitStatus]
}

func (f *FileItem) TotalFrecency() int32 {
    return int32(f.AccessFrecencyScore) + int32(f.ModificationFrecencyScore)
}

func (f *FileItem) IsDeleted() bool { return f.flags.Load()&uint32(flagDeleted) != 0 }
func (f *FileItem) SetDeleted(v bool) {
    if v { f.flags.Or(uint32(flagDeleted)) } else { f.flags.And(^uint32(flagDeleted)) }
}
func (f *FileItem) IsBinary() bool  { return f.flags.Load()&uint32(flagBinary) != 0 }
func (f *FileItem) IsOverflow() bool { return f.flags.Load()&uint32(flagOverflow) != 0 }

// DirItem：路径+最大子文件 frecency
type DirItem struct {
    Path              ChunkedPath
    LastSegmentOffset uint32   // 不压缩到 uint16；避免路径极深溢出
    MaxAccessFrecency atomic.Int32
}

// PaginationArgs：分页参数；Limit=0 表无上限（仍受 SearchOptions.MaxResults 控制）
type PaginationArgs struct {
    Offset int
    Limit  int
}
```

### 3.2 ChunkedPath / PathArena（两阶段设计，关键）

```go
// core/path_arena.go

// ChunkedPath 是 PathArena 中的一个引用。
// v1：Arena 存储独立 string；FileItem 只存一个 string 的 index。
//     这种设计的要点：arena 永远**只追加、不 realloc**，所以 string 指向的
//     字节地址在 FilePicker 生命周期内稳定 → 可安全用 unsafe.String(if v2)。
// v1.1 优化点：把 []string 换成一个 []byte + 一个 (offset,len) 表，进一步减少 GC 根。
//              这个切换对上层透明，只需改 ChunkedPath 字段。

type ChunkedPath struct {
    // v1：string 的 index 到 arena.strings[]
    Index uint32
    // FilenameOffset：相对路径字符串内，最后一个 '/' 之后的字节位置
    // （若相对路径中没有 '/'，= 0）
    FilenameOffset uint32
}

type PathArena struct {
    mu      sync.Mutex
    strings []string
    // dedup map: path string -> index
    intern  map[string]uint32
}

func NewPathArena(initialCap int) *PathArena {
    return &PathArena{
        strings: make([]string, 0, initialCap),
        intern:  make(map[string]uint32, initialCap),
    }
}

// Intern 追加或返回已有索引。对相同 path 幂等。
func (a *PathArena) Intern(path string) ChunkedPath {
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

// Get 返回相对路径字符串。返回值在 arena 生命周期内有效（不可变）。
func (a *PathArena) Get(cp ChunkedPath) string { return a.strings[cp.Index] }

// Filename 返回相对路径中"文件名段"（最后一段）。
func (a *PathArena) Filename(cp ChunkedPath) string {
    s := a.strings[cp.Index]
    return s[cp.FilenameOffset:]
}

// Dir 返回相对路径中"目录段"（不含尾斜杠）。空路径返回 ""。
func (a *PathArena) Dir(cp ChunkedPath) string {
    s := a.strings[cp.Index]
    if cp.FilenameOffset == 0 { return "" }
    return s[:cp.FilenameOffset-1]
}

func filenameOffset(s string) uint32 {
    for i := len(s) - 1; i >= 0; i-- {
        if s[i] == '/' || s[i] == '\\' { return uint32(i + 1) }
    }
    return 0
}

// Len 返回 arena 当前路径数。
func (a *PathArena) Len() int { a.mu.Lock(); defer a.mu.Unlock(); return len(a.strings) }
```

### 3.3 GitStatus（统一为 struct，可扩展，非指针可选）

```go
// core/git_status.go

// GitStatus 表示一个文件在 git 工作区中的状态。
// 只用一个 uint8 足以表达状态，但保留 struct 以便 v1.1 加 rename-from 等字段。
type GitStatus struct {
    Kind GitStatusKind
}

type GitStatusKind uint8
const (
    GitStatusClean GitStatusKind = iota   // 明确的 clean（区别于 "unknown"）
    GitStatusModified
    GitStatusAdded
    GitStatusDeleted
    GitStatusRenamed
    GitStatusUntracked
    GitStatusIgnored
)

// IsDirty 返回 true 表示该文件存在于工作区更改中（影响打分 boost）。
func (g GitStatus) IsDirty() bool {
    switch g.Kind {
    case GitStatusModified, GitStatusAdded, GitStatusDeleted, GitStatusRenamed:
        return true
    }
    return false
}
```

> FileItem.gitStatus 用 `atomic.Pointer[GitStatus]`：nil = "未探测"。
> 显式的 GitStatusClean 与 nil 的区别：前者已执行过 git 查询，后者还没查 — 这样 watcher 增量时可以避免重复查 "肯定干净" 的文件。

### 3.4 FuzzyQuery / Constraint（消除"一个 Value string 装天下"的扩展瓶颈）

```go
// queryparser/fuzzy_query.go

type FuzzyQueryKind int
const (
    FuzzyEmpty FuzzyQueryKind = iota
    FuzzyText           // 单 token
    FuzzyParts          // 多 token 列表
)

type FuzzyQuery struct {
    Kind  FuzzyQueryKind
    Text  string     // FuzzyText 时有效
    Parts []string   // FuzzyParts 时有效
}

// queryparser/constraint.go

type ConstraintKind int
const (
    CText         ConstraintKind = iota
    CNot
    CExtension      // *.rs    → Value="rs"
    CGlob           // **/*.rs → Value="**/*.rs"
    CPathSegment    // /src/   → Value="src"
    CGitStatus      // status:modified → Value="modified"
    CFileType       // type:go → Value="go"
    CSizeCmp        // >1MB / <10KB / =1KB → Op + Value
    CModifiedAgo    // modified:7d  → Value="7d"
)

// SizeOp 用于 CSizeCmp
type SizeOp int
const ( SizeEq SizeOp = iota; SizeGt; SizeLt; SizeGte; SizeLte )

type Constraint struct {
    Kind  ConstraintKind

    // 文本型约束共享字段
    Value string

    // 取反（Not 本身也是一个 ConstraintKind，通过 Children[0] 嵌套表达）
    // 例：!*.rs → Constraint{Kind: CNot, Children: [{Kind: CExtension, Value: "rs"}]}
    Children []Constraint

    // CSizeCmp 专用
    SizeOp   SizeOp
    SizeBytes int64   // 解析后字节数（1MB → 1048576）
}

// FFFQuery 是解析的最终结果。
type FFFQuery struct {
    Fuzzy       FuzzyQuery
    Constraints []Constraint
}
```

**关键设计决策**：`Constraint` 是一个 "带 tag 的 union" — 所有字段都在一个 struct 里，按 Kind 决定哪些字段有效。这避免了 Go 中 interface-tag 的类型断言开销，也让 JSON 序列化更自然。缺点是字段多，但对 99% 场景够用（v2 再决定是否重构为 interface-tag）。

---

## 4. 评分算法（有实际数字，不再空谈）

```
total = base
      + frecency_access
      + frecency_modification
      + dir_bonus
      + git_dirty_boost
      + combo_boost
      - depth_penalty
      - is_binary_penalty
```

**base（fuzzy match 基础分）**：
- 每个 fuzzy token 在相对路径中按 0-100 分匹配，取最小值（AND 语义）
- 子串命中 = 100；前缀命中 = 90；后缀命中 = 85；其他字符级编辑距离按比例扣分
- 查询 token 数 > 0 但一个都不命中 → 直接排除

**frecency_access（-50 ~ +100）**：
- 按 Rust 版衰减算法：`score = sum_over_each_access( exp(-hours_since / half_life_hours) * 10 )`
- `half_life_hours`：Neovim 模式 168h（一周），AI 模式 720h（30 天）
- 取对数压缩后 clamp 到 [-50, +100]

**frecency_modification（-20 ~ +80）**：
- `modified_ago_days < 1 → +80`
- `modified_ago_days < 7 → +40`
- `modified_ago_days < 30 → +10`
- `modified_ago_days >= 365 → -20`
- 二进制文件不享受此项

**dir_bonus（+0 ~ +50）**：
- fuzzy token 命中父目录名（DirItem.LastSegment） → +30/命中
- 命中完整路径的中间段 → +10/命中

**git_dirty_boost（+0 ~ +40）**：
- `IsDirty() → +40`
- `GitStatusUntracked → +15`（鼓励发现新文件，但不优先于已修改文件）

**combo_boost（+0 ~ +∞，但 clamp 上限 +200）**：
- `min(combo_count * 20, 200)`，combo_count = QueryTracker 中 "该 query 选中该 file" 的次数
- 目的：用户行为学习 — 反复从同个 query 里选同个文件 → 下次直接置顶

**depth_penalty（0 ~ -80）**：
- 相对路径段数 `segments = count('/')+1`
- `penalty = max(0, segments - 3) * 10`，封顶 -80（9 层以下保持 -80）

**is_binary_penalty（-30）**：非内容搜索模式下，二进制文件打 -30，避免误推荐 .exe / .so

---

## 5. Watcher / Overflow 机制（不再模糊）

```
事件流程：
  fsnotify.Event
    → debounce（30ms 滑动窗口；同路径合并 Create+Write）
    → dispatch:
         CREATE  → 查 stat + 新增 FileItem(flagOverflow=true) 到 overflow 槽
         WRITE   → 更新 FileItem.Size/Modified/GitStatus
         REMOVE  → SetDeleted(true)（保留槽位，避免 base 数组 reshuffle）
         RENAME  → 旧路径 SetDeleted，新路径 CREATE
  每 60s 做一次 overflow → base 合并（如果 overflow > 1000 个 或 搜索压力大）
```

**去抖实现**：`map[string]time.Time` + `time.AfterFunc(30ms)` 消费 goroutine，写回 picker 主态。
**递归监听**：监听目录时手动遍历子目录并 `watcher.Add`；收到 Create(目录) 时追加 Add。
**Windows 特殊处理**：`fsnotify` 在 Windows 上不支持递归（我们已手动处理）；长路径 `\\?\` 前缀在 `filepath.Abs` 之前手工处理。

---

## 6. 关键 API（可独立测试）

```go
// core.PathArena.Intern(string) ChunkedPath            ← 纯函数
// core.PathArena.Get(ChunkedPath) string               ← 纯函数
// queryparser.QueryParser.Parse(string) FFFQuery      ← 纯函数
// bigram.BigramFilter.Candidates(string) []uint32      ← 纯函数
// score.TotalScore(file, query) int32                  ← 纯函数
// picker.FilePicker.Search(FFFQuery, SearchOptions) SearchResult
// frecency.FrecencyTracker.Touch(path)
// grep.PlainSearch(files, pattern, opts) []GrepMatch
```

> **可测试性原则**：任何不含 I/O 的模块都必须是纯函数或可重入的；任何含 I/O 的模块必须通过 interface 注入依赖，方便测试替换。

---

## 7. 并发安全合同（明确写出来，避免歧义）

- `FilePicker` 的所有公共方法都是并发安全的（内部 RWMutex）
- `FileItem` 的 `flags` 和 `gitStatus` 是原子字段，**不持锁即可读**
- `PathArena` 的 `Intern` 是线程安全的（内部 Mutex），`Get` 只读，**无锁**（因为 arena 只追加不 realloc）
- `FrecencyTracker` / `QueryTracker` 都是并发安全的（内部用 bbolt 的事务锁）
- `BigramFilter` 一旦 build 完成就是只读；watcher 在做 overflow→base 合并时会短暂持 Lock 替换整个 filter

---

## 8. 版本迭代的兼容性合同

- **v1.0 → v1.x 最小承诺**：
  - 顶层导出的 struct 字段名不变（新增字段只能加尾部）
  - `ConstraintKind` 只在末尾加新值（不回收旧值编号）
  - `FFFQuery` / `FuzzyQuery` 的字段只增不减
  - bbolt bucket 名和 key 编码（见附录 A）一旦稳定永不改变
- **序列化**：任何需要写磁盘的结构都必须有显式版本号（`magic + version + payload`）
- **破坏性变更**：发 v2.0 时整个 import path 升级为 `/v2/`（Go modules 语义版本约定）

---

## 9. 风险清单与应对

| 风险 | 等级 | 应对 |
|------|------|------|
| fsnotify Windows 不递归 | 高 | 我们自己手动 Add 子目录（已明确） |
| unsafe.String 等 v1.1 再上 | 中 | v1 用纯 string arena，bench 数据证明瓶颈再切 |
| case-insensitive memmem 在 Go 里没 SIMD | 中 | 先 `bytes.ToLower + bytes.Index`；瓶颈就用 `bytes/bytes_amd64.go` 技巧 + build tag 注入汇编；不到万不得已不手写 asm |
| arena Intern 哈希冲突 | 低 | Go map 本身处理冲突；实测 100k 文件毫无问题 |
| git `exec.Command` 在无 git 环境下失败 | 中 | 显式探测 `git` 可执行性，不可用时降级为 `GitStatusUntracked` |
| bbolt 文件锁冲突（两个进程打开同一 db） | 中 | Open 时给一个明确的超时 + 错误信息 "database locked" |
| 大小写不敏感文件系统（macOS/Windows）上 path dedup 漏 | 低 | Intern 时把路径统一 `filepath.Clean`；可选：在 case-insensitive FS 上把 key 转 lowercase |

---

## 10. 附录 A：bbolt bucket 布局（持久化格式一旦冻结绝不改）

```
bucket "frecency"
  key: <abs_path_bytes>
  val: <varint_count><varint_last_access_unix_sec>   (9 字节典型值)

bucket "frecency_meta"
  key: "schema_version"  val: "1"
  key: "half_life_hours"  val: ascii decimal string
  key: "mode"             val: "neovim" | "ai"

bucket "qt_by_query"  (query → [file_path → count])
  key: <query_bytes> + 0x00 + <file_path_bytes>
  val: <varint_count>

bucket "qt_meta"
  key: "schema_version"  val: "1"
```

> 约定：**所有 key 都带长度前缀或用 0x00 分隔且字段内容禁止含 0x00**（path 天然满足）。

---

## 11. 附录 B：搜索主入口的伪代码（对照实现）

```
func (fp *FilePicker) Search(query FFFQuery, opts SearchOptions) SearchResult:
    1) fp.mu.RLock(); files = fp.files; arena = fp.arena; bigram = fp.bigram
       fp.mu.RUnlock()   // bigram 一旦构建不再变，可安全在锁外读

    2) candidateIdx := bigram.Candidates(query.Fuzzy.TextOrJoined())
       若 candidateIdx 为空且 query.Fuzzy 非 Empty → 同时扫 overflow 列表

    3) 并行打分（goroutine pool，分块 N/numcpu）
       for chunk in candidateIdx 分块:
           for i in chunk:
               s := score.TotalScore(files[i], query, arena, fp.frecencyCache, fp.qtCache)
               if s > 0 → 进入临时结果堆

    4) topK := sort.TopK(scoredFiles, opts.Pagination.Offset+Limit)
       取 [Offset:Offset+Limit] 段
       填充 RelativePath / FileName（从 arena 取）

    5) return SearchResult{Items: topK, TotalMatched: len(scoredFiles)}
```

---

## 12. 附录 C：与 Rust 版的对照表（供参照）

| Rust 模块 | Go 包 | 差异/取舍 |
|-----------|-------|-----------|
| `types.rs` (FileItem) | `core/types.go` | 去掉 ChunkedString；用 string index arena |
| `bigram_filter.rs` | `bigram/bigram_filter.go` | v1 用 `[65536][]uint32`，不上 roaring |
| `frecency.rs` (LMDB) | `frecency/frecency.go` | bbolt 替代 LMDB；纯 Go |
| `query_tracker.rs` | `querytracker/querytracker.go` | 同上 |
| `scan.rs` | `picker/scan.go` | `filepath.WalkDir`（单线程）+ goroutine 排序/构建 |
| `background_watcher.rs` | `picker/watcher.go` | fsnotify + 30ms debounce + overflow |
| `grep.rs` (SIMD memmem) | `grep/plain.go` | v1 用 `bytes.ToLower+Index`；asm 留 v2 |
| `query_parser` crate | `queryparser/` | 零依赖，纯函数 |

---

## 13. 迭代优化点（标记 "持续优化写法" 的位置）

- [ ] `PathArena` v1.1：切到 `[]byte` arena + `unsafe.String`，减少 GC 根
- [ ] `bigram` v1.2：对 10w+ 文件，从 slice 切 roaring bitmap（bench 证明再做）
- [ ] `grep` v1.3：hot path 的 case-insensitive memmem 上汇编（有 bench 数据再做）
- [ ] `git_cache` v1.4：从 `exec git status` 切到 `go-git`（避免 fork 开销）
- [ ] `FilePicker` v2：支持多 base path 合并索引（当前单一 base）

---

## 14. 最小化测试脚手架（每个包都能跑）

见 `TESTING_PLAN.md`。

---

## 15. 一句话版电梯总结

> **"用纯 Go 实现的、模块化的 fff 搜索库：core + queryparser 无依赖可嵌入，picker + frecency 可选持久化，grep 可选启用；先跑通再优化，不写手写汇编，不绑 CGO。"**
