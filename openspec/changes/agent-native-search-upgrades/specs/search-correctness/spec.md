## ADDED Requirements

### Requirement: Candidate generation MUST distinguish unavailable, no-match, and candidates
The search engine MUST represent candidate-source output as three distinct states: unavailable, no-match, and candidates. A no-match state from an applicable candidate source MUST NOT be treated the same as an unavailable candidate source.

#### Scenario: Both candidate sources are unavailable
- **WHEN** a non-empty fuzzy query cannot produce usable candidate-index keys for the main index or overlay
- **THEN** the search engine uses the full scanned file set as the candidate set before applying constraints and scoring

#### Scenario: Applicable main index confirms no match
- **WHEN** the main candidate index can evaluate the fuzzy query and confirms no file can match
- **THEN** the search engine returns an empty candidate set unless another candidate source returns explicit candidates

#### Scenario: One candidate source returns candidates
- **WHEN** either the main index or overlay returns explicit candidates for a fuzzy query
- **THEN** the search engine uses the de-duplicated union of explicit candidates and does not fall back to the full file set

### Requirement: Fuzzy miss results MUST NOT be rescued by frecency scoring
The search engine MUST NOT return files that fail fuzzy candidate matching solely because modification or access frecency gives them a positive score.

#### Scenario: Unknown fuzzy query has no candidates
- **WHEN** a fuzzy query has usable candidate-index keys and no indexed path contains those keys
- **THEN** the search result is empty even if recently modified files have positive frecency scores

### Requirement: Public query syntax MUST match implemented parsing
The library, README, and CLI help MUST only advertise query syntaxes that the parser interprets as structured constraints. The `size:`-prefixed form is part of the public contract for this change.

#### Scenario: Size-prefixed greater-than query
- **WHEN** a caller searches with `size:>1KB`
- **THEN** the parser creates a size comparison constraint and the search engine excludes files with size less than or equal to 1KB

#### Scenario: Size-prefixed less-than query
- **WHEN** a caller searches with `size:<100B`
- **THEN** the parser creates a size comparison constraint and the search engine excludes files with size greater than or equal to 100 bytes

### Requirement: Structured constraint tests MUST include negative assertions
Every supported structured constraint used by file search MUST have regression tests that prove both inclusion of matching files and exclusion of non-matching files.

#### Scenario: Size constraint regression
- **WHEN** a regression test exercises a size comparison query
- **THEN** it asserts that matching files are present and non-matching files are absent

#### Scenario: Status, glob, and path-segment regression
- **WHEN** a regression test exercises `status:*`, glob, or path-segment filtering
- **THEN** it asserts both positive and negative outcomes rather than only checking for one expected result

### Requirement: Result ordering MUST remain deterministic
Search results MUST remain deterministic for the same index snapshot, query, clock input, and feedback state. Any top-k optimization MUST return the same first K results as the full sort under the ordering rule `score desc, modified desc, path asc`.

#### Scenario: Equal score tie-break
- **WHEN** multiple candidate files have equal score and equal modification time
- **THEN** the search engine orders those files by ascending path

#### Scenario: Top-k optimization preserves order
- **WHEN** an optimized top-k selection path is used for a query with more candidates than the requested limit
- **THEN** the returned results match the first K results produced by the full deterministic ordering
