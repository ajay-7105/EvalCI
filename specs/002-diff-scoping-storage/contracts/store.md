# Contract: `storage.Store`

Package `internal/storage`. Callers import this package for the interface and types, and
`internal/storage/factory` to open one; they never import a backend.

```go
type Store interface {
    SaveRun(ctx context.Context, r RunRecord) error
    GetRun(ctx context.Context, id string) (RunRecord, error)
    ListRunsByCommit(ctx context.Context, sha string) ([]RunRecord, error) // newest first
    ListRecentRuns(ctx context.Context, limit int) ([]RunRecord, error)    // newest first
    Close() error
}

// package factory (internal/storage/factory): the only place that branches on the driver
func Open(ctx context.Context, cfg storage.Config) (storage.Store, error)

var (
    ErrNotFound           = errors.New("run not found")
    ErrDuplicateID        = errors.New("run id already stored")
    ErrNewerSchema        = errors.New("history file was written by a newer version")
    ErrBackendUnavailable = errors.New("storage backend not available")
)
```

Record types are defined in `data-model.md`.

## Behavioral guarantees (the shared contract suite)

Every implementation MUST pass `storagetest.RunContract`, which checks:

1. **Round trip**: `SaveRun` then `GetRun` returns a record equal to the one saved, including
   nil (unknown) cost and nil averages.
2. **Atomicity**: a save that fails midway (forced error) leaves no partial record visible to
   `GetRun`, `ListRunsByCommit`, or `ListRecentRuns`.
3. **Immutability and duplicates**: saving an existing ID returns `ErrDuplicateID` and leaves
   the stored record unchanged; two runs of the same commit with different IDs both exist.
4. **Queries**: `ListRunsByCommit` returns only that commit, newest first; `ListRecentRuns(n)`
   returns at most `n`, newest first; unknown ID gives `ErrNotFound`; empty result is an empty
   slice, not an error.
5. **Ordering ties** are broken deterministically by ID.
6. **Concurrency**: parallel `SaveRun` calls from several goroutines (and, for file backends,
   several handles on the same file) all succeed and every record is complete.
7. **No content fields**: the record types contain no field intended for responses, prompts, or
   case inputs (reflection check).

Backend-specific tests (SQLite only): schema upgrade from an older version keeps records; a
newer-version file returns `ErrNewerSchema` and is byte-for-byte unchanged; a read-only or locked
file returns an error rather than panicking.

## Factory rules

- `storage.Config{Driver, Path}`; `Driver == ""` is not valid for `factory.Open` (callers skip
  storage when unconfigured).
- `sqlite`: opens or creates the file (creating parent directories), applies migrations.
- `postgres` and any other value: `ErrBackendUnavailable` wrapped with the driver name.

## Caller rules

- The CLI calls `factory.Open` before the run starts to surface configuration errors, saves once after
  final results exist, and treats open or save failures as warnings.
- No caller may type-assert the `Store` to a concrete type.
