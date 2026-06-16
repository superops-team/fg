## Context

当前 `fg` 的核心控制流集中在 `fg.SearchWith()` 与 `picker.Search()`：顶层搜索每次创建新的 `Picker` 并执行 `Scan()`，见 `fg.go:38`、`fg.go:58`、`fg.go:61`；候选生成依赖 `bigram.Candidates()`，但 `picker.searchCandidates()` 在主索引与 overlay 都为空时直接 `makeRange(n)`，见 `picker/picker.go:310`、`picker/picker.go:316`、`picker/picker.go:317`。这会让“索引确认无命中”和“索引不可用”被同一种 `len()==0` 判断混淆。

公开查询合同也存在偏差：README 宣传 `size:>1KB`，见 `README.md:9`，顶层注释同样使用 `size:>1KB`，见 `fg.go:3`，但 parser 的 `key:value` 分支只处理 `status`、`type`、`modified`，见 `queryparser/constraint_parse.go:48` 到 `queryparser/constraint_parse.go:61`。现有测试覆盖了 `size:>1KB`，但只断言 big file 出现，未断言 small file 被排除，因此不能保护公开合同。

本设计的目标是先收紧最小可执行合同。它不是一次性重写，也不是引入新搜索后端。它用小而明确的改动，让 `fg` 成为 AI agent 可以稳定调用的本地搜索内核。

## Goals / Non-Goals

**Goals:**
- 修复搜索假阳性：无命中 fuzzy 查询不得因为 frecency 或 modified score 返回无关文件。
- 对齐公开语法：`size:` 前缀要么被 parser 正确解析，要么从 README/CLI help/注释中移除；本方案选择实现解析。
- 提供最小 context-first runtime：可复用 index 对象支持 `SearchContext`、`Refresh`、`Close`，并保持 legacy wrapper 兼容。
- 支持 root-level repo-aware ignore：`.gitignore` 与 `.ignore` 参与扫描过滤，现有硬编码跳过规则继续生效。
- 建立可重复 benchmark 基线：cold build、warm search、grep 分开测，避免口径混淆。

**Non-Goals:**
- 不做 watcher、daemon、服务端分片、Tree-sitter、symbol graph、embedding/vector search。
- 不做文件级增量 refresh；第一版只有完整 refresh + 原子快照替换。
- 不产品化 selection feedback 持久化写回；现有 `querytracker` 保留，新增反馈 API 移到后续 change。
- 不强制 top-k 替代全量排序；排序优化必须先证明不会破坏稳定排序。
- 不支持 `.rgignore`、嵌套 `.gitignore` 继承或复杂 ignore precedence。

## Decisions

### 1. Candidate generation uses explicit tri-state outcomes
- 决策：把 candidate source 结果从隐式 `nil/empty slice` 升级为显式三态：`Unavailable`、`NoMatch`、`Candidates`。
- 合并规则：
  - `main=Unavailable` 且 `overlay=Unavailable`：允许 full-scan fallback。
  - 任一 source 返回 `Candidates`：使用所有 candidates 的去重合集。
  - 至少一个 applicable source 返回 `NoMatch`，且没有任何 source 返回 `Candidates`：返回空候选，不得 fallback 全量。
- 原因：这是解决无命中假阳性的最小改动，不需要改 scoring 模型。
- 备选方案：在 `scoreCandidates` 中过滤 fuzzy score 为 0 的结果。放弃原因是它会把 candidate 语义和 scoring 语义混在一起，且会影响短 query fallback。

### 2. Runtime object is additive and snapshot-based
- 决策：新增一个最小可复用 index 对象，命名保持单一，例如 `Index`。MVP API 只包含：`Open`、`SearchContext`、`Refresh`、`Close`。
- 兼容路径：`fg.Search` / `fg.SearchWith` 保留，内部可以 open/search/close，但 public 行为保持当前契约。
- 并发语义：Search 获取一个完整 snapshot；Refresh 在新 snapshot 构建成功后原子替换；Refresh 失败时旧 snapshot 继续可用。
- 原因：它解决 agent 连续查询重复扫描的问题，同时把并发模型控制在可测试范围内。
- 备选方案：只给现有 `SearchWith` 增加 context。放弃原因是重复扫描仍然存在，无法支撑 agent 会话复用。

### 3. Context cancellation returns errors, not partial results
- 决策：`SearchContext`、`Refresh` 和 grep context 路径在 context cancel/deadline 时返回 `context.Canceled` 或 `context.DeadlineExceeded`，不返回 partial results。
- 原因：agent 工具调用更需要确定性失败，而不是半截结果被误认为完整上下文。
- 备选方案：返回 partial results + error。放弃原因是调用方更难判断结果可用性，也更容易被大模型误读。

### 4. Repository-aware ignore starts with root-level rules only
- 决策：MVP 只读取 root 下 `.gitignore` 与 `.ignore`，并保留当前硬编码跳过目录：`.git`、`.svn`、`.hg`、`.idea`、`node_modules`。
- Precedence：硬编码跳过目录优先；其后应用 `.ignore` 与 `.gitignore` 的匹配结果。若 ignore 文件不存在，行为退回现有硬编码过滤。
- 原因：这覆盖最常见噪音来源，避免一次性实现完整 gitignore 继承语义。
- 备选方案：完整实现嵌套 `.gitignore`。放弃原因是范围明显扩大，且不是当前高 ROI 核心。

### 5. Benchmark before optimization
- 决策：本 change 必须先修正 benchmark 口径并记录基线；top-k、锁缩小、worker pool 属于 benchmark 驱动优化。
- 排序要求：如果实现 top-k，前 K 结果必须与当前全量排序在 `score desc, modified desc, path asc` 规则下保持一致。
- 原因：搜索引擎的可重复性比单次微优化更重要。
- 备选方案：直接替换为 heap/top-k。放弃原因是容易破坏稳定排序，且当前没有完整基线证明收益。

## Risks / Trade-offs

- [结果集变化] → no-hit 返回空、`size:` 变结构化约束都会改变少量非预期行为；用 regression tests 和 changelog/README 明确说明。
- [runtime API 增加维护面] → 只新增一个 `Index` 抽象，不新增 parallel API family；legacy wrappers 保持原样。
- [ignore 改变扫描 corpus] → MVP 限制 root-level ignore，并保留硬编码 skip；若用户需要搜索 ignored 文件，作为后续显式 opt-in 功能处理。
- [Refresh 并发复杂度] → 使用完整 snapshot 原子替换，不做文件级增量更新。
- [性能优化影响排序] → top-k 只有在测试证明与全量排序一致时才能合入。

## SDD / TDD Fit

- SDD 顺序：先更新 OpenSpec requirement，再写 failing tests，再实现，再运行 `openspec validate --strict`。
- TDD 顺序：每个阶段先写至少一个负向测试和一个兼容测试。尤其是 no-hit、`size:`、Refresh 失败、context cancel、ignore 排除。
- 完成标准：每个 requirement 至少对应一个可定位测试；tasks 中每个实现任务必须有前置或同批测试任务。

## Development Schedule

### Phase 1: Correctness Contract，0.5-1 天
- 修 tri-state candidate 合同。
- 修 `size:` parser 或文档，本方案实现 parser。
- 补 no-hit、size 负向断言、排序稳定性测试。

### Phase 2: Runtime Index MVP，1.5-2 天
- 新增最小 `Index` API。
- 接入 `SearchContext`、`Refresh`、`Close`。
- 保持 `Search` / `SearchWith` 兼容。
- 补 context cancel、Refresh 原子快照、Refresh 失败保留旧 snapshot 的测试。

### Phase 3: Repo-aware Indexing，1 天
- 接 root-level `.gitignore` / `.ignore`。
- 保留硬编码 skip。
- 补 ignored/non-ignored 文件搜索测试。

### Phase 4: Benchmark Baseline and Targeted Optimization，1 天
- 修正现有 benchmark 命名与测量内容。
- 补 cold build、warm search、grep、多规模 benchmark。
- 仅在 benchmark 证明必要时引入 worker pool 或 top-k。

## Migration Plan

1. 先提交 spec 与测试收敛，不改 public behavior。
2. 实现 correctness 修复与 `size:` 解析，更新 README/CLI help。
3. 增加 `Index` API，legacy wrappers 委托新实现但保持调用语义。
4. 接入 root-level ignore 与 full Refresh。
5. 更新 benchmark baseline，基于数据决定是否做小范围性能优化。
6. 如 runtime API 或 ignore 行为出现兼容问题，可回滚 wrapper 委托路径；correctness 测试不得回滚。

## Open Questions

- `Index` 构造函数命名最终采用 `Open`、`OpenIndex` 还是 `NewIndex`？建议 `Open` 用于可能持有资源的 runtime object。
- ignored 文件搜索是否需要 opt-in flag？建议不放入本 change，避免扩大 CLI 兼容面。
