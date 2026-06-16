## ADDED Requirements

### Requirement: Benchmark suite MUST separate cold build and warm search
The project SHALL maintain benchmarks that separately measure index construction and repeated search over an already-built index.

#### Scenario: Cold build benchmark
- **WHEN** the benchmark suite measures index construction
- **THEN** the benchmark includes scan, metadata collection, path interning, and bigram index construction time and allocation metrics

#### Scenario: Warm search benchmark
- **WHEN** the benchmark suite measures repeated search
- **THEN** the benchmark excludes one-time index construction from the timed loop and reports time, bytes, and allocations per search operation

### Requirement: Benchmark suite MUST cover at least two corpus sizes
Core file-search benchmarks SHALL include a medium corpus and a large corpus so growth behavior is visible.

#### Scenario: Medium corpus benchmark
- **WHEN** file-search benchmarks run
- **THEN** at least one benchmark uses a medium corpus of approximately 10k indexed file paths

#### Scenario: Large corpus benchmark
- **WHEN** file-search benchmarks run in the extended benchmark suite
- **THEN** at least one benchmark uses a larger corpus of approximately 100k indexed file paths or documents why the local environment skipped it

### Requirement: Grep performance MUST be measured independently
The project SHALL benchmark content-search paths independently from file-search paths so grep regressions do not hide behind aggregate search numbers.

#### Scenario: Large-file grep benchmark
- **WHEN** grep benchmarks are executed
- **THEN** they include at least one large-file case and report latency plus allocation metrics

#### Scenario: Multi-file grep benchmark
- **WHEN** grep concurrency behavior changes
- **THEN** the benchmark suite includes a multi-file case that exercises the configured concurrency path

### Requirement: Benchmark labels MUST match measured work
Benchmark names and comments SHALL describe the work inside the timed loop.

#### Scenario: Benchmark name claims scan cost
- **WHEN** a benchmark name or comment claims to measure scan or index build cost
- **THEN** scan or index build work occurs inside the timed section

#### Scenario: Benchmark name claims warm search cost
- **WHEN** a benchmark name or comment claims to measure warm search cost
- **THEN** index build work occurs before timer reset and only search work occurs inside the timed loop
