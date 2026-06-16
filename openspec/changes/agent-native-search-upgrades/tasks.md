## 1. Spec and Test Contract Lockdown

- [x] 1.1 Update OpenSpec and README/help wording so `size:` behavior, no-hit behavior, runtime index scope, ignore scope, and benchmark scope match one another
- [x] 1.2 Add failing tests for tri-state candidate behavior: unavailable fallback, applicable no-match returns empty, explicit candidates do not fallback
- [x] 1.3 Add failing tests for `size:>1KB`, `size:<100B`, and negative assertions proving non-matching files are excluded
- [x] 1.4 Add deterministic ordering tests for `score desc, modified desc, path asc`, including equal-score tie cases

## 2. Search Correctness Implementation

- [x] 2.1 Replace implicit `nil`/empty candidate handling with explicit candidate outcome states in `bigram` and `picker`
- [x] 2.2 Implement or document `size:` parser behavior; MVP implements `size:` prefix parsing to match existing public docs
- [x] 2.3 Ensure fuzzy no-hit results cannot be rescued by frecency or modification score
- [x] 2.4 Run `go test ./bigram ./queryparser ./picker` and confirm all new correctness tests pass

## 3. Runtime Index MVP

- [x] 3.1 Define the minimal reusable index API: open, `SearchContext`, full `Refresh`, `Close`
- [x] 3.2 Implement legacy `Search` / `SearchWith` wrappers without changing default limit, empty root, and error semantics
- [x] 3.3 Add context cancellation and deadline tests for scan/search/git-status paths
- [x] 3.4 Implement complete snapshot refresh: search sees old or new snapshot, refresh failure preserves old snapshot
- [x] 3.5 Add race/stress tests for concurrent search and refresh

## 4. Repository-Aware Indexing MVP

- [x] 4.1 Add root-level `.gitignore` and `.ignore` matching to scan/index construction
- [x] 4.2 Preserve hardcoded skip directories `.git`, `.svn`, `.hg`, `.idea`, and `node_modules`
- [x] 4.3 Add tests for ignored file exclusion, non-ignored file inclusion, absent ignore files, and hardcoded skip compatibility
- [x] 4.4 Add tests for supported `status:*` filters in git repos and explicit errors outside git repos

## 5. Benchmark Baseline and Targeted Optimization

- [x] 5.1 Split or rename existing benchmarks so cold build and warm search labels match the timed work
- [x] 5.2 Add medium-corpus benchmarks around 10k paths for cold build and warm search
- [x] 5.3 Add extended large-corpus benchmark around 100k paths, with skip documentation if environment constraints prevent running it locally
- [x] 5.4 Add grep benchmarks for large-file and multi-file concurrency paths
- [x] 5.5 Only implement worker-pool or top-k optimization if benchmark output identifies the current path as a bottleneck and deterministic ordering tests remain green

## 6. Verification and Release Notes

- [x] 6.1 Run `go test ./...`
- [x] 6.2 Run `go test -race ./...` or targeted race coverage for packages modified by runtime index and refresh work
- [x] 6.3 Run `go test -bench=. -benchmem ./bigram ./grep ./picker` plus new benchmark packages as applicable
- [x] 6.4 Update `reports/` only with freshly generated outputs that match the current module path
- [x] 6.5 Run `openspec validate agent-native-search-upgrades --strict --json`

## 7. Development Schedule

- [x] 7.1 Phase 1, 0.5-1 day: lock correctness contract, add failing tests, implement tri-state and `size:` parser alignment
- [x] 7.2 Phase 2, 1.5-2 days: implement reusable runtime index, `SearchContext`, full snapshot refresh, compatibility wrappers, and race tests
- [x] 7.3 Phase 3, 1 day: add root-level ignore support and git-status error hardening
- [x] 7.4 Phase 4, 1 day: repair benchmark baselines and apply only benchmark-proven small optimizations
