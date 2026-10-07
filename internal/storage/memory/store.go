package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/ajay-7105/EvalCI/internal/storage"
)

// Store is an in-memory storage.Store. It copies records in and out so callers cannot mutate
// stored data.
type Store struct {
	mu   sync.Mutex
	runs map[string]storage.RunRecord
}

// New returns an empty in-memory store.
func New() *Store { return &Store{runs: map[string]storage.RunRecord{}} }

// SaveRun implements storage.Store.
func (s *Store) SaveRun(ctx context.Context, r storage.RunRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.runs[r.ID]; exists {
		return storage.ErrDuplicateID
	}
	s.runs[r.ID] = r.Clone()
	return nil
}

// GetRun implements storage.Store.
func (s *Store) GetRun(_ context.Context, id string) (storage.RunRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return storage.RunRecord{}, storage.ErrNotFound
	}
	return r.Clone(), nil
}

// ListRunsByCommit implements storage.Store.
func (s *Store) ListRunsByCommit(_ context.Context, sha string) ([]storage.RunRecord, error) {
	return s.list(func(r storage.RunRecord) bool { return r.HeadSHA == sha }, -1), nil
}

// ListRecentRuns implements storage.Store.
func (s *Store) ListRecentRuns(_ context.Context, limit int) ([]storage.RunRecord, error) {
	if limit < 0 {
		limit = 0
	}
	return s.list(func(storage.RunRecord) bool { return true }, limit), nil
}

func (s *Store) list(keep func(storage.RunRecord) bool, limit int) []storage.RunRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []storage.RunRecord{}
	for _, r := range s.runs {
		if keep(r) {
			out = append(out, r.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].ID > out[j].ID
	})
	if limit >= 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Close implements storage.Store.
func (s *Store) Close() error { return nil }
