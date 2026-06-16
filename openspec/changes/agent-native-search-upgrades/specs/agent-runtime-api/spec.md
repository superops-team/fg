## ADDED Requirements

### Requirement: Agent-facing runtime API MUST be context-first
The library SHALL expose context-aware runtime methods so agent callers can cancel scan, search, refresh, grep, and git-status work through `context.Context`.

#### Scenario: Search is canceled by caller
- **WHEN** the caller cancels the context during scan, candidate generation, filtering, scoring, grep, or git status collection
- **THEN** the operation stops and returns an error that matches `context.Canceled`

#### Scenario: Timeout budget is exceeded
- **WHEN** the caller provides a context deadline and the search work exceeds that deadline
- **THEN** the operation returns an error that matches `context.DeadlineExceeded`

#### Scenario: Canceled search does not return partial results
- **WHEN** a search operation is canceled before completion
- **THEN** the operation returns no result set that could be mistaken for a complete answer

### Requirement: Runtime API MUST provide a reusable index object
The library SHALL provide one reusable runtime index object for a repository root. The object SHALL support open, context-aware search, full refresh, and close lifecycle operations.

#### Scenario: Repeated queries reuse index state
- **WHEN** a caller opens a runtime index for one repository and performs multiple searches through it
- **THEN** the implementation reuses the existing index snapshot and does not require a full rescan before every query

#### Scenario: Runtime index is closed
- **WHEN** a caller closes the runtime index
- **THEN** later operations on that closed index return an explicit closed-index error instead of using released resources

### Requirement: Legacy one-shot API MUST remain compatible
The existing one-shot `Search` and `SearchWith` APIs SHALL remain available and keep their documented defaults while internally reusing the new implementation where practical.

#### Scenario: Existing Search call succeeds
- **WHEN** an existing caller invokes `Search(root, query, limit)`
- **THEN** the call succeeds without requiring the caller to construct a runtime index

#### Scenario: Existing defaults are preserved
- **WHEN** an existing caller passes `limit <= 0` or an empty root to `SearchWith`
- **THEN** the library preserves the current default behavior of limit 20 and current working directory root

### Requirement: Search and refresh MUST use complete snapshots
Runtime index searches SHALL observe one complete index snapshot. Refresh SHALL build a complete replacement snapshot and publish it atomically only after successful completion.

#### Scenario: Search overlaps with refresh
- **WHEN** a search starts while a refresh is running
- **THEN** that search observes either the previous complete snapshot or the next complete snapshot, never a partially built snapshot

#### Scenario: Refresh fails
- **WHEN** refresh fails because scanning, ignore parsing, metadata loading, or context cancellation fails
- **THEN** the previous successful snapshot remains available for subsequent searches

### Requirement: Selection feedback is deferred from MVP
The runtime API MUST NOT require callers to record selection feedback to obtain deterministic search behavior. Selection feedback writeback SHALL be treated as a post-MVP extension unless introduced by a separate accepted OpenSpec change.

#### Scenario: No selection feedback API is used
- **WHEN** a caller performs searches without recording result selection
- **THEN** search ordering remains deterministic using fuzzy, metadata, and existing frecency signals
