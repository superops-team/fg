# fg — fuzzy file finder

基于 Go 的轻量级模糊文件搜索工具，采用 SDD（规格驱动开发）+ TDD（测试驱动开发）实现。

## 功能

- **模糊文件搜索**：`fg "type:go main util"` 按文件名 + 相对路径做模糊匹配
- **bigram 预过滤**：10k 级文件规模下先定位候选集，再精细匹配（性能 ~10×）
- **约束解析**：`type:go`、`*.go`、`size:>1KB`、`modified:7d`、`/src/`、`**/*.go`、`!foo`
- **frecency 评分**：基于访问衰减 + 修改时间 + 历史选择（combo boost）
- **行内 grep**：`fg --grep "TODO"` 对命中文件做行内匹配
- **持久化 frecency/combo**：通过 `kv` 抽象（bbolt）记录用户选择历史

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

# 搜索：当前目录下 .go 文件中含 "main"
./fg "type:go main"

# 搜索 + 显示分数
./fg --score "type:go main"

# 行内内容 grep
./fg --grep "TODO"

# 在指定目录
./fg -r ./pkg "modified:1d"
```

作为库使用：

```go
import "github.com/superops-team/fg"

results, err := fg.Search(".", "type:go main", 20)
for _, r := range results {
    fmt.Printf("%d  %s\n", r.Score(), r.Path())
}
```

## 测试与覆盖率

所有单元测试 + 压力测试（高并发 / 大文件 / 多轮 race 检测）全部绿色。

```bash
# 基础运行
go test ./...

# race + 3 轮（推荐）
go test -race -count=3 ./...

# 基准测试
go test -bench=. -benchmem -run=^$ ./bigram ./grep ./picker

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

### 基准测试摘要（Intel Xeon 8582C，GOMAXPROCS=2）

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| `BenchmarkBigram_Build-2` | 2,926,377 | 4,143,508 | 1,614 |
| `BenchmarkBigram_Candidates-2` | 992,066 | 41,120 | 4 |
| `BenchmarkGrep_Search-2` | 5,218,101 | 491,398 | 1,173 |
| `BenchmarkPicker_Scan-2` | 915,350 | 262,520 | 10,009 |

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
3. 按 TDD 先写测试 → 实现 → `go vet`/`go test -race -count=3` 三轮稳定 → 提交
4. 每次 PR 之前运行 `bash scripts/pressure.sh` 做压力测试

## License

MIT
