## ADDED Requirements

### Requirement: Repository scans MUST honor root-level ignore rules
Repository indexing SHALL honor root-level `.gitignore` and `.ignore` files during scan. MVP ignore support is limited to files located directly under the configured repository root.

#### Scenario: Root-level ignored file is present on disk
- **WHEN** a file path matches a root-level `.gitignore` or `.ignore` rule during indexing
- **THEN** that file is excluded from the indexed search corpus by default

#### Scenario: Non-ignored file is present on disk
- **WHEN** a file path does not match hardcoded skip directories or root-level ignore rules
- **THEN** that file remains eligible for indexing and search

### Requirement: Existing hardcoded skip directories MUST remain active
Repository-aware indexing SHALL keep the existing hardcoded skip behavior for `.git`, `.svn`, `.hg`, `.idea`, and `node_modules` while adding root-level ignore parsing.

#### Scenario: Hardcoded skipped directory is present
- **WHEN** scan encounters a hardcoded skipped directory
- **THEN** scan skips that directory even if no ignore file exists

#### Scenario: Ignore file is absent
- **WHEN** neither `.gitignore` nor `.ignore` exists at the configured root
- **THEN** scan behavior falls back to existing hardcoded skip behavior

### Requirement: Runtime index refresh MUST be full-refresh only in MVP
The reusable runtime index SHALL support explicit full refresh. MVP refresh SHALL rebuild the complete index snapshot and SHALL NOT require file-level incremental updates or watcher integration.

#### Scenario: Caller requests full refresh
- **WHEN** a caller invokes refresh after files change on disk
- **THEN** subsequent searches observe a complete refreshed snapshot after refresh succeeds

#### Scenario: Caller does not refresh
- **WHEN** files change on disk but the caller has not requested refresh
- **THEN** the runtime index continues serving the previous complete snapshot

### Requirement: Git-backed filters MUST fail explicitly
Searches that rely on git status data MUST return explicit errors when git status cannot be collected. Unknown status values remain non-contractual and MUST NOT be advertised as supported filters.

#### Scenario: Status filter in non-git directory
- **WHEN** a query includes a supported git-status constraint and the target root is not a usable git repository
- **THEN** the search operation returns an error that identifies git status collection as the failing dependency

#### Scenario: Status filter succeeds
- **WHEN** a query includes a supported git-status constraint in a valid git repository
- **THEN** the search operation filters results using repository status data gathered for that search

#### Scenario: Unsupported git-status value
- **WHEN** a query includes a git-status value outside the supported set
- **THEN** public documentation does not describe that value as supported behavior
