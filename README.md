# fg — fuzzy file finder

`fg` 是一个基于 Go 的轻量级模糊文件搜索与内容 grep 工具，也提供可复用的库 API，适合交互式搜索、脚本调用和 agent/runtime 场景。

## 当前能力概览

- **模糊文件搜索**：`fg "type:go main util"` 按文件名 + 相对路径排序返回候选文件
- **内容 grep**：`fg --grep "TODO"` 在扫描出的文件集中做逐行匹配
- **搜索 + grep 组合**：`fg "type:go main" --grep "TODO"` 先筛文件，再只在命中文件内 grep
- **约束语法**：支持 `type:go`、`*.go`、`size:>1KB`、`modified:7d`、`/src/`、`**/*.go`、`!vendor`
- **确定性候选语义**：candidate index 可证明“无模糊命中”时直接返回空结果，不再退化为全量扫描结果
- **仓库感知扫描**：固定跳过 `.git`、`.svn`、`.hg`、`.idea`、`node_modules`，并读取 root-level `.gitignore` / `.ignore`
- **上下文感知库 API**：支持 `Open` / `SearchContext` / `Refresh` / `Close`，可复用运行时索引并传递取消/超时
- **取消安全与原子刷新**：`Refresh` 与 `ScanContext` 失败或取消时保留旧快照，不破坏在线索引
- **frecency + combo boost**：结合访问历史、修改时间和 query/path 组合选择记录排序

## 目录结构

```
.
├── core/                基础类型（FileItem/DirItem/ChunkedPath/PathArena/...）
├── bigram/              2-byte 倒排索引（Bigram / BigramOverlay）
├── queryparser/         约束解析器（type / size / modified / extension / glob / path segment）
├── querytracker/        (query, path) 选择记录 + combo boost
├── frecency/            访问/修改 frecency 评分
├── kv/                  KVStore 抽象（MemStore / BoltStore）
├── picker/              扫描 + bigram + fuzzy + 约束过滤 + 排序（主控制流）
├── grep/                行内内容匹配
├── cmd/fg/              CLI 入口
├── scripts/             压力/验证脚本
└── reports/             覆盖率 + 基准测试报告
```

## 快速开始

```bash
# 构建
go build -o fg ./cmd/fg

# 查看帮助
./fg --help

# 搜索：当前目录下 .go 文件中含 "main"
./fg "type:go main"

# 搜索 + 显示分数
./fg --score "type:go main"

# 在全部扫描文件中做 grep
./fg --grep "TODO"

# 先筛出 Go 文件，再只在结果内 grep
./fg "type:go main" --grep "TODO"

# 在指定目录
./fg -r ./pkg "modified:1d"
```

## CLI 使用说明

### 用法

```text
fg [flags] [query]
```

### 三种运行模式

1. **模糊文件搜索**

   ```bash
   fg "type:go main"
   ```

2. **文件内容 grep**

   ```bash
   fg --grep "TODO"
   ```

3. **模糊搜索 + grep**

   ```bash
   fg "type:go main" --grep "TODO"
   ```

### Flags

| Flag | 说明 |
|---|---|
| `-r`, `--root` | 指定搜索根目录，默认当前目录 |
| `--limit` | 最多返回多少条结果，默认 `20` |
| `--score` | 在文件搜索结果前打印 score |
| `--grep` | 对文件内容做逐行匹配 |
| `-h`, `--help` | 输出帮助信息 |

### 查询语法

| 语法 | 含义 | 示例 |
|---|---|---|
| `type:go` | 按语言/扩展族过滤 | `fg "type:go main"` |
| `*.go` | 扩展 glob | `fg "*.go"` |
| `size:>1KB` | 文件大小比较 | `fg "type:go size:>1KB"` |
| `modified:7d` | 最近修改时间窗口 | `fg "modified:7d"` |
| `/src/` | 路径段过滤 | `fg "/src/ type:go"` |
| `**/*.go` | 路径 glob | `fg "**/*.go"` |
| `!vendor` | 否定条件 | `fg "type:go !vendor"` |

### Ignore 行为

- 始终跳过：`.git`、`.svn`、`.hg`、`.idea`、`node_modules`
- 读取根目录 `.gitignore` 与 `.ignore`
- grep 无 file query 时，同样遵守上述 ignore 规则
- 若 bigram candidate index 明确判断无匹配，`fg` 返回空结果，而不是退回到全量文件

作为库使用：

```go
import "github.com/superops-team/fg"

results, err := fg.Search(".", "type:go main", 20)
for _, r := range results {
    fmt.Printf("%d  %s\n", r.Score, r.Path)
}

idx, err := fg.Open(context.Background(), fg.Options{Root: "."})
if err != nil {
    return err
}
defer idx.Close()
results, err = idx.SearchContext(context.Background(), "type:go size:>1KB", 20)
if err != nil {
    return err
}

// 文件变化后刷新运行时索引；取消/失败不会破坏旧快照
if err := idx.Refresh(context.Background()); err != nil {
    return err
}
```

### 顶层库 API

| API | 说明 |
|---|---|
| `fg.Search(root, query, limit)` | 一次性打开、搜索、关闭 |
| `fg.SearchWith(opts)` | 基于 `fg.Options` 的一次性调用 |
| `fg.Open(ctx, opts)` | 打开可复用索引 |
| `(*Index).SearchContext(ctx, query, limit)` | 带 `context` 的查询 |
| `(*Index).Refresh(ctx)` | 原子刷新快照 |
| `(*Index).Close()` | 幂等关闭 |

## 测试与覆盖率

当前开发能力已由单元测试、race 检测与基准测试覆盖验证，包括：

- CLI 帮助输出与组合模式
- root-level ignore / bare directory ignore
- candidate index no-match 语义
- `Open` / `SearchContext` / `Refresh` / `Close` 生命周期
- grep 在取消/超时时不返回 partial results
- 索引刷新在取消时保留旧快照

```bash
# 基础运行
go test ./...

# race 检测
go test -race ./...

# 关键基准测试
go test -run=^$ -bench 'BenchmarkPicker_(ColdBuild_10k|WarmSearch_10k)|BenchmarkGrep_(MultiFileConcurrency|LargeFile)' ./picker ./grep

# 一键压力（见 scripts/pressure.sh）
bash scripts/pressure.sh
```

### 覆盖率汇总（go test -coverprofile=reports/coverage.out）

| 包 | 覆盖率 |
|---|---|
| `core` | 96.6% |
| `querytracker` | 93.0% |
| `kv` | 88.2% |
| `bigram` | 88.1% |
| `grep` | 89.2% |
| `frecency` | 84.7% |
| `queryparser` | 80.4% |
| `picker` | 67.8% |
| **整体** | **77.2%** |

详细按函数覆盖率：[reports/coverage_func.txt](reports/coverage_func.txt)。

### 基准测试摘要

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| `BenchmarkBigram_Build-2` | 2,926,377 | 4,143,508 | 1,614 |
| `BenchmarkBigram_Candidates-2` | 992,066 | 41,120 | 4 |
| `BenchmarkGrep_MultiFileConcurrency-*` | 多文件 grep 并发路径 |
| `BenchmarkGrep_LargeFile-*` | 单个大文件 grep 路径 |
| `BenchmarkPicker_ColdBuild_10k-*` | 10k corpus 冷构建：scan + metadata + path arena + bigram |
| `BenchmarkPicker_WarmSearch_10k-*` | 10k corpus 已构建索引上的重复搜索 |
| `BenchmarkPicker_ColdBuild_100k-*` | 100k corpus 扩展冷构建基线 |

完整输出：[reports/bench.txt](reports/bench.txt)

## 架构要点

1. **分层职责清晰**：`kv` 是底层；`frecency`/`querytracker` 依赖它；`picker` 组合所有组件；`cmd/fg` 是最上层。
2. **并发安全**：`FileItem.Flags` / `DirItem.MaxAccessFrecency` 用 `atomic`；`PathArena` / `Bigram` / `FilePicker` 用 `sync.RWMutex`。`go test -race` 三轮无告警。
3. **小接口抽象**：`kv.KVStore{Put/Get/ForEach/Close}`，便于替换实现（测试用 `MemStore`，生产用 `BoltStore`）。
4. **评分加性模型**：`fuzzy + frecency + combo_boost`，稳定可解释，便于后续扩展权重文件。
5. **最小实现原则**：仅引入 `bbolt` 一个外部依赖；其他全部 stdlib，构建 0 配置。

## 开发流程

1. `DESIGN_SPEC.md`：架构与各包的类型/算法规格
2. `TESTING_AND_SCHEDULE.md`：分层任务拆解、测试目标与时间规划
3. 按 TDD 先写测试 → 实现 → `go test ./...` + `go test -race ./...` 稳定后提交
4. 每次 PR 之前运行 `bash scripts/pressure.sh` 做压力测试

## License

MIT
