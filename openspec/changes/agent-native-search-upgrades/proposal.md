## Why

`fg` 已经具备 fuzzy file search、bigram 预过滤、grep、frecency、querytracker 和 bbolt 基础能力，但当前公开契约与实现存在几处会直接误导 AI agent 的问题：无命中 fuzzy 查询可能退化为全量候选，`size:>1KB` 被 README 与顶层注释宣传但 parser 只支持裸 `>1KB` 形式，顶层 `SearchWith` 每次都会重新 `Scan()`，并且长任务缺少 `context.Context` 取消能力。现在要先用小改动补齐“可信搜索内核”的基础契约，避免后续 runtime API、repo-aware indexing、性能优化建立在模糊语义上。

## What Changes

### MVP Changes
- 修正搜索正确性：显式区分 candidate source 的 `Unavailable`、`NoMatch`、`Candidates` 三态，禁止索引确认无命中时回退全量候选。
- 修正公开查询合同：正式支持 `size:>1KB` / `size:<100B` / `size:>=10KB`，或同步收回文档宣传；本 change 默认选择“实现 `size:` 前缀”，因为 README 已经公开宣传该语法。
- 增加最小 agent runtime API：新增可复用 index 对象，支持 `SearchContext`、`Refresh`、`Close`；保留 `Search` / `SearchWith` 兼容入口。
- 增加 repo-aware indexing MVP：只支持 root 级 `.gitignore` 与 `.ignore`，保留现有硬编码跳过目录，不引入 watcher、增量刷新或 `.rgignore`。
- 补齐 benchmark 基线：明确区分 cold build、warm search、grep，大中两档 corpus，修正现有 benchmark 命名与测量口径不一致问题。

### Deferred / Post-MVP
- 不在本 change 中实现 embedding/vector search、Tree-sitter、symbol graph、LSP、daemon、分片索引、watcher 或文件级增量刷新。
- 不在本 change 中产品化持久化 selection feedback；只保留现有 frecency/querytracker 能力，反馈写回作为后续独立 change。
- 不在本 change 中强制实现 top-k 与复杂性能 gate；先补基线，只有 benchmark 证明必要时再做小范围优化。
- 不在本 change 中定义 machine-readable benchmark artifact 或 time-to-first-result 指标；当前 API 非 streaming，先记录总延迟与分配数据。

## Capabilities

### New Capabilities
- `search-correctness`: 定义 agent 搜索必须满足的确定性语义、查询合同、排序稳定性与负向回归测试。
- `agent-runtime-api`: 定义面向 AI agent 的最小 context-first 搜索 API、索引生命周期、兼容包装器与快照刷新语义。
- `repository-aware-indexing`: 定义 root-level ignore、完整快照刷新、git status 错误显式化与兼容边界。
- `performance-benchmarks`: 定义当前阶段必须维护的 cold/warm/grep benchmark 口径和回归基线。

### Modified Capabilities
- 无。当前仓库此前没有 OpenSpec baseline spec，本 change 以新增 capability 形式记录未来行为合同。

## Impact

- 受影响代码：`fg.go`、`picker/picker.go`、`bigram/bigram.go`、`grep/grep.go`、`queryparser/constraint_parse.go`、`core/ignore.go`、`cmd/fg/main.go`。
- 受影响测试：`picker/*_test.go`、`queryparser/*_test.go`、`bigram/*_test.go`、`grep/*_test.go`、`cmd/fg/*_test.go`、`*_stress_test.go`。
- 兼容性策略：`fg.Search` / `fg.SearchWith` 保持可用，`limit <= 0` 默认 20、空 root 使用 cwd、错误写法不改变；新增 runtime API 采用 additive 方式。
- 行为变化注意事项：无命中查询返回空结果会修复当前假阳性，但可能改变依赖“总能返回最近文件”的非预期调用方；`size:` 从 fuzzy 文本变成结构化约束也会改变结果集，但这是与公开文档对齐的修复。
- 依赖变化：允许引入一个成熟 ignore 解析实现；不得引入重型搜索后端或 daemon 运行时。
