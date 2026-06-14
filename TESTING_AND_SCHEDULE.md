# go-fff — 测试规划与开发排期

> 测试原则：**纯函数 → 白盒 100%；有 I/O → 全通过 interface 替换；并发 → race detector**。
> 排期原则：**每周产出一个可独立运行的增量**，MVP 在第 4 周末可跑。

---

## 一、分层任务拆解（按依赖顺序）

```
L1 — 基础设施         core/
L2 — 解析与预过滤     queryparser/ + bigram/
L3 — 打分与排序       score/
L4 — KV 抽象          kv/ (bolt + mem)
L5 — 持久化追踪       frecency/ + querytracker/
L6 — 搜索主引擎       picker/ (scan + watcher + search + git_cache)
L7 — 内容搜索         grep/ (plain + aho + regexp)
L8 — 顶层 API + CLI   shared/ + cmd/fff
```

### 详细任务（T1.1 表示 Layer 1 的第 1 个任务）

#### L1 · core （Week 1，约 1.5 人日）
- T1.1 `core/types.go`：FileItem, DirItem, PaginationArgs + 方法（IsDeleted / TotalFrecency / ...）
- T1.2 `core/path_arena.go`：PathArena + ChunkedPath（string 版）
- T1.3 `core/git_status.go`：GitStatus / GitStatusKind / IsDirty
- T1.4 `core/binary_detect.go`：detectBinary([]byte) bool
- T1.5 `core/ignore.go`：interface IgnoreMatcher + PassthroughIgnoreMatcher（"从不忽略"）
- T1.6 `core/page_pool.go`：`*PagePool`（一个 `sync.Pool` 包装，给 grep 重用 buf）

#### L2 · queryparser + bigram （Week 1-2，约 2 人日）
- T2.1 `queryparser/fuzzy_query.go`：FuzzyQuery 类型 + 常量
- T2.2 `queryparser/constraint.go`：Constraint 类型 + 常量
- T2.3 `queryparser/constraint_parse.go`：单个 token → 一个/零个 Constraint（识别 `*.rs` / `/src/` / `status:` / `>1MB` / `!xxx` 等）
- T2.4 `queryparser/parser.go`：`QueryParser{}.Parse(raw string) FFFQuery` — tokenize → 对每个 token 尝试 constraint 解析；余下的合并为 FuzzyText / FuzzyParts
- T2.5 `queryparser/config.go`：ParserConfig（预留扩展）
- T2.6 `bigram/bigram_filter.go`：BigramFilter.Build([]FileItem, arena) / .Candidates(queryTokens) []uint32
- T2.7 `bigram/bigram_overlay.go`：BigramOverlay（追加式，watcher 用）

#### L3 · score （Week 2，约 1.5 人日）
- T3.1 `score/score.go`：`TotalScore(file, query, arena, frecencyCache, qtCache) int32`
  - sub: fuzzyMatchBase / frecencyAccess / frecencyMod / dirBonus / gitBoost / combo / depthPenalty / binaryPenalty
- T3.2 `score/sort.go`：`TopK(items []ScoredFile, k int) []ScoredFile`（部分排序，避免全量 sort）

#### L4 · kv （Week 3，约 1 人日）
- T4.1 `kv/kv.go`：`type KVStore interface { Get(key []byte) []byte; Put(key, val []byte); ForEach(prefix []byte, fn func(k, v []byte) bool); Close() error }`
- T4.2 `kv/mem.go`：MemKV（纯 map，测试 stub）
- T4.3 `kv/bolt.go`：BoltKV（bbolt impl）

#### L5 · frecency + querytracker （Week 3，约 2 人日）
- T5.1 `frecency/frecency.go`：FrecencyTracker（Open/Close/Touch/GetAccessScore/GetModScore）
- T5.2 `querytracker/querytracker.go`：QueryTracker（Open/Close/Record/ComboCount）

#### L6 · picker （Week 3-4，约 3 人日）
- T6.1 `picker/options.go`：FilePickerOptions / FilePickerMode / SearchOptions
- T6.2 `picker/picker.go`：FilePicker 主结构 + 生命周期（New / Cancel / WaitForScan）
- T6.3 `picker/scan.go`：扫描（WalkDir → 收集 FileItem → Intern arena → 构建 BigramFilter）
- T6.4 `picker/watcher.go`：fsnotify 监听 + 30ms debounce + overflow 合并
- T6.5 `picker/search.go`：主入口 Search() — bigram 过滤 → 并行打分 → topK
- T6.6 `picker/git_cache.go`：GitStatusCache（exec `git status --porcelain=v2 -z` + 1 分钟 TTL）

#### L7 · grep （Week 5，约 2 人日）
- T7.1 `grep/grep.go`：GrepOptions / GrepMatch / Search() 统一入口（按模式选 plain/aho/regexp）
- T7.2 `grep/plain.go`：PlainSearch（大小写不敏感；v1 用 `bytes.ToLower + bytes.Index`）
- T7.3 `grep/aho_corasick.go`：MultiGrep（AC trie，多模式同时搜）
- T7.4 `grep/regexp.go`：RegexSearch（用 std `regexp`；提供 MustCompile 包装）

#### L8 · shared + CLI （Week 5，约 1 人日）
- T8.1 `shared/shared.go`：SharedFilePicker / SharedFrecency / SharedQT（RWMutex 包装）
- T8.2 `cmd/fff/main.go`：demo CLI
- T8.3 `go.mod` / `go.sum` / `.gitignore`

---

## 二、测试规划（每个任务的测试目标）

### 单元测试（go test ./...）

| 包 | 覆盖点 | 测试策略 |
|----|--------|---------|
| `core` | PathArena.Intern 去重 / filenameOffset 边界 / binary detect / GitStatus.IsDirty | 表驱动 + 随机路径 |
| `queryparser` | 空串 / 单 token / 多 token / `*.rs` / `**/*.go` / `/src/` / `status:modified` / `>1MB` / `!test` / 组合查询 | table-driven；每个 token 形态至少一条 |
| `bigram` | Build 后 Candidate 能命中 / 大小写不敏感策略 / query 无 tokens 时返回空 | 构造固定文件集做 golden test |
| `score` | 各子项打分公式（用 fixture） / TopK 正确性 / 空结果 / 极大 k | 纯函数，大量表驱动 |
| `kv` | Put→Get 往返 / ForEach 前缀 / Close 后不可写 / mem 与 bolt 行为一致 | mem 版跑所有子测试；bolt 版用 `t.TempDir()` |
| `frecency` | Touch 后 score 上升 / 衰减算法数值正确性 / half_life 参数生效 | fixed clock（注入 Now 函数） |
| `querytracker` | Record 后 ComboCount 上升 / 不同 query 互不污染 | mem/bolt 双实现都跑 |
| `picker` | Scan 完成后文件数正确 / Search 返回 topN / watcher 创建新文件 → 下次搜索命中（短延迟） | `t.TempDir()` 造小型项目树；watcher 用 `testing/S` 短 poll |
| `grep` | plain 大小写不敏感 / aho 多模式无漏 / regex 基础 | 构造临时文件；重点测 "包含 0 匹配 / 多匹配行 / 二进制跳过" |

### 集成测试
- I1：完整生命周期测试 — `mkdir project → touch 10 个文件 → New picker → WaitForScan → Search → 结果含每个文件 → TriggerRescan 结果稳定`
- I2：watcher 集成 — `在测试中创建新文件 → short poll → 新文件出现在 Search 结果中`
- I3：grep 集成 — `写 3 个带 pattern 的测试源文件 → grep → 命中数精确`

### 并发 / race 测试
- C1：`go test -race ./...` 强制通过；所有包（尤其 picker/frecency/kv）的测试都要并发跑
- C2：`picker.search_test.go`：并发 50 个 Search goroutine + 1 个 watcher 写 goroutine；断言无崩溃、无 data race

### fuzz 测试（可选但推荐）
- F1：`queryparser` 的 Parse 做 fuzz：任何输入都不能 panic
- F2：`bigram` 的 Build 做 fuzz：任何 FileItem 集合都不能崩溃

### 基准测试
- B1：`bigram` BenchmarkBuild / BenchmarkCandidates （10k 文件，100k 文件两档）
- B2：`picker` BenchmarkSearch10k / BenchmarkSearch100k
- B3：`grep` BenchmarkPlainSearch10M （10MB 文件搜一个 token）

### 测试命令约定
```bash
go test ./...                       # 平时开发
go test -race ./...                 # PR 前必须
go test -bench=. -benchmem ./...    # 每次涉及 hot path 改动时
go test -fuzz=FuzzParse -fuzztime=10s ./queryparser/   # 偶尔跑
```

---

## 三、开发排期（5 周 MVP）

假设 **1 名主力开发**。

### Week 1：L1 core + L2 queryparser
| 日 | 任务 | 验收 |
|---|-------|------|
| D1 | T1.1-T1.3 types + path_arena + git_status | `go test ./core/...` 通过 |
| D2 | T1.4-T1.6 binary_detect + ignore + page_pool；T2.1-T2.2 fuzzy/constraint 类型 | 同上 |
| D3 | T2.3-T2.5 constraint_parse + parser | `go test ./queryparser/...`；至少 12 个查询形态正确 |
| D4 | T2.6 bigram_filter | `go test ./bigram/...` |
| D5 | T2.7 bigram_overlay + 补齐遗漏测试 + go vet | `go test ./...` |

### Week 2：L3 score + L4 kv + L5 frecency 起步
| 日 | 任务 | 验收 |
|---|------|------|
| D1-D2 | T3.1 score.go（所有子项打分） | 数值匹配 spec 公式 |
| D3 | T3.2 sort.go TopK + 大量 edge case | `go test ./score/...` |
| D4-D5 | T4.1 kv interface + mem impl + bolt impl | `go test ./kv/... -race` |

### Week 3：L5 frecency + querytracker + L6 picker 起步
| 日 | 任务 | 验收 |
|---|------|------|
| D1-D2 | T5.1 frecency（decay 算法 + bbolt 持久化 + fixed clock 测试） | `go test ./frecency/... -race` |
| D3 | T5.2 querytracker | `go test ./querytracker/... -race` |
| D4 | T6.1 options + T6.2 picker 生命周期骨架 | 能 New / Cancel |
| D5 | T6.3 scan（WalkDir + arena + bigram 构建） | `go test ./picker/...` 跑通 minimal case |

### Week 4：L6 picker 完成 + 第一版可跑
| 日 | 任务 | 验收 |
|---|------|------|
| D1-D2 | T6.4 watcher（fsnotify + debounce + overflow） | 并发测试通过 |
| D3 | T6.5 search（bigram 过滤 → 并行打分 → topK） | `go test -race ./picker/...` |
| D4 | T6.6 git_cache（exec git status + 1 分钟 TTL；无 git 时降级） | 手动在一个 git 仓库内跑 demo |
| D5 | 端到端手动验证 + 第一版 demo CLI 原型 | `go run ./cmd/fff --path ~/proj foo` 能出结果 |

### Week 5：L7 grep + L8 shared + 文档补齐
| 日 | 任务 | 验收 |
|---|------|------|
| D1-D2 | T7.1-T7.4 grep（plain + aho + regexp + 统一入口） | `go test ./grep/...` |
| D3 | T8.1 shared（SharedFilePicker / SharedFrecency / SharedQT） | 并发测试通过 |
| D4 | T8.2 cmd/fff demo CLI | 能用 3 种命令：files / grep / stats |
| D5 | 文档 + README + `DESIGN_SPEC.md` 更新到当前实现状态 + CI 配置（GitHub Actions 跑 `go test -race ./...`） | CI 绿灯 |

---

## 四、里程碑（Milestone）

| 里程碑 | 时间点 | 标志 |
|--------|--------|------|
| M1 可解析 | W1 末 | `queryparser` 覆盖所有 spec 中约束 token |
| M2 可打分 | W2 末 | `score.TotalScore` 公式对齐 spec |
| M3 可持久化 | W3 末 | `frecency + querytracker` 可写可读 |
| M4 MVP 可搜索 | W4 末 | `picker` 能扫描 + 搜索 + 响应新文件 |
| M5 MVP 完整 | W5 末 | `grep + CLI` 可用；CI 绿灯 |

---

## 五、验收标准（Done criteria）

每完成一个任务，必须同时满足：
1. ✅ `go build ./...` 通过
2. ✅ `go vet ./...` 无告警
3. ✅ `go test ./path/to/pkg -race` 通过
4. ✅ 单元测试 **至少覆盖** 该包的所有公共函数和 spec 中列出的每种形态
5. ✅ 同包内没有超过 500 行的单个 .go 文件（否则拆）
6. ✅ 不引入新的第三方依赖到 L1-L3 四个包（core/queryparser/bigram/score）

---

## 六、CI 配置（最小集）

```yaml
# .github/workflows/ci.yml
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: go build ./...
      - run: go vet ./...
      - run: go test -race -count=1 ./...
```

三平台测试（Linux / macOS / Windows）在 MVP 后追加（Week 6）。
