package storage

import (
	"context"
	"errors"
)

// Config selects and locates a storage backend.
type Config struct {
	// Driver names the backend (for example "sqlite").
	Driver string
	// Path is the history file for file-based drivers.
	Path string
}

// Sentinel errors returned by every implementation.
var (
	// ErrNotFound means no run has the requested ID.
	ErrNotFound = errors.New("run not found")
	// ErrDuplicateID means a run with that ID is already stored; the stored run is unchanged.
	ErrDuplicateID = errors.New("run id already stored")
	// ErrNewerSchema means the history file was written by a newer version and was left untouched.
	ErrNewerSchema = errors.New("history file was written by a newer version")
	// ErrBackendUnavailable means the configured backend is not available in this version.
	ErrBackendUnavailable = errors.New("storage backend not available")
)

// Store saves and reads run history. All callers use only this interface, so adding a backend
// needs no caller change. Implementations must be safe for concurrent use.
type Store interface {
	// SaveRun saves a complete run record atomically: it is stored whole or not at all.
	// It returns ErrDuplicateID if the ID exists, leaving the stored record unchanged.
	SaveRun(ctx context.Context, r RunRecord) error
	// GetRun returns the run with the given ID, or ErrNotFound.
	GetRun(ctx context.Context, id string) (RunRecord, error)
	// ListRunsByCommit returns the runs of a head commit, newest first (ties broken by ID).
	ListRunsByCommit(ctx context.Context, sha string) ([]RunRecord, error)
	// ListRecentRuns returns at most limit runs, newest first (ties broken by ID).
	ListRecentRuns(ctx context.Context, limit int) ([]RunRecord, error)
	// Close releases the store.
	Close() error
}
