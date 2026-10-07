package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver

	"github.com/ajay-7105/EvalCI/internal/storage"
)

// timeLayout is fixed width and UTC, so text comparison orders times correctly.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

// Store is the SQLite implementation of storage.Store.
type Store struct {
	db *sql.DB
	// hook, when set by tests, is called after each table is written during SaveRun and may
	// return an error to simulate a mid-save failure.
	hook func(stage string) error
}

// Open opens or creates the history file at path. A file written by a newer version returns
// storage.ErrNewerSchema without being modified; an older file is migrated forward.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("sqlite history path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, fmt.Errorf("creating directory for %s: %w", abs, err)
	}
	if _, err := os.Stat(abs); err == nil {
		v, err := readVersion(ctx, abs)
		if err != nil {
			return nil, err
		}
		if v > currentVersion() {
			return nil, fmt.Errorf("%w: %s has schema version %d, this version supports up to %d",
				storage.ErrNewerSchema, abs, v, currentVersion())
		}
	}

	db, err := sql.Open("sqlite", dsn(abs, false))
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", abs, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("opening %s: %w", abs, err)
	}
	if err := migrate(ctx, db, migrations); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("preparing %s: %w", abs, err)
	}
	return &Store{db: db}, nil
}

func dsn(abs string, readOnly bool) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	q := url.Values{}
	if readOnly {
		q.Set("mode", "ro")
	} else {
		q.Add("_pragma", "busy_timeout(5000)")
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "foreign_keys(1)")
		q.Set("_txlock", "immediate")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// readVersion reads user_version through a read-only connection so a newer file is never written.
func readVersion(ctx context.Context, abs string) (int, error) {
	db, err := sql.Open("sqlite", dsn(abs, true))
	if err != nil {
		return 0, fmt.Errorf("opening %s read-only: %w", abs, err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("reading schema version of %s: %w", abs, err)
	}
	return v, nil
}

// Close implements storage.Store.
func (s *Store) Close() error { return s.db.Close() }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nf(p *float64) sql.NullFloat64 {
	if p == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *p, Valid: true}
}

func ni(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func pf(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func pi(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

// isDuplicate reports a primary-key or unique constraint failure.
func isDuplicate(err error) bool {
	var coded interface{ Code() int }
	if errors.As(err, &coded) {
		c := coded.Code()
		return c == 1555 || c == 2067 // SQLITE_CONSTRAINT_PRIMARYKEY, SQLITE_CONSTRAINT_UNIQUE
	}
	return false
}

// SaveRun implements storage.Store. Everything is written in one transaction.
func (s *Store) SaveRun(ctx context.Context, r storage.RunRecord) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.Validate(); err != nil {
		return err
	}
	r = r.Clone()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting save: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	stage := func(name string) error {
		if s.hook != nil {
			return s.hook(name)
		}
		return nil
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO runs (id, schema_version, started_at, finished_at, head_sha, head_branch,
		base_ref, base_sha, mode, decision, exit_code, runs_per_case, min_drop_threshold, max_spread, currency,
		total_input_tokens, total_output_tokens, total_cost_micros, cost_partial)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.SchemaVersion, r.StartedAt.Format(timeLayout), r.FinishedAt.Format(timeLayout), r.HeadSHA, r.HeadBranch,
		r.BaseRef, r.BaseSHA, string(r.Mode), string(r.Decision), r.ExitCode, r.Thresholds.RunsPerCase,
		r.Thresholds.MinDropThreshold, r.Thresholds.MaxSpreadBeforeUnreliable, r.Currency,
		r.TotalInputTokens, r.TotalOutputTokens, ni(r.TotalCostMicros), b2i(r.CostPartial)); err != nil {
		if isDuplicate(err) {
			return storage.ErrDuplicateID
		}
		return fmt.Errorf("saving run: %w", err)
	}
	if err = stage("runs"); err != nil {
		return err
	}
	for _, su := range r.Suites {
		if _, err = tx.ExecContext(ctx, `INSERT INTO run_suites (run_id, suite, file, action, reason) VALUES (?,?,?,?,?)`,
			r.ID, su.Suite, su.File, su.Action, su.Reason); err != nil {
			return fmt.Errorf("saving suite %s: %w", su.Suite, err)
		}
	}
	if err = stage("suites"); err != nil {
		return err
	}
	for _, c := range r.Cases {
		if _, err = tx.ExecContext(ctx, `INSERT INTO run_cases (run_id, suite, case_id, critical, status,
			baseline_runs, baseline_failed, baseline_average, baseline_spread,
			candidate_runs, candidate_failed, candidate_average, candidate_spread, delta)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.ID, c.Suite, c.Case, b2i(c.Critical), c.Status, c.BaselineRuns, c.BaselineFailed, nf(c.BaselineAverage),
			nf(c.BaselineSpread), c.CandidateRuns, c.CandidateFailed, nf(c.CandidateAverage), nf(c.CandidateSpread),
			nf(c.Delta)); err != nil {
			return fmt.Errorf("saving case %s/%s: %w", c.Suite, c.Case, err)
		}
	}
	if err = stage("cases"); err != nil {
		return err
	}
	for _, u := range r.Usage {
		if _, err = tx.ExecContext(ctx, `INSERT INTO run_usage (run_id, provider, model, calls, input_tokens, output_tokens,
			unknown_calls, cost_micros) VALUES (?,?,?,?,?,?,?,?)`,
			r.ID, u.Provider, u.Model, u.Calls, u.InputTokens, u.OutputTokens, u.UnknownCalls, ni(u.CostMicros)); err != nil {
			return fmt.Errorf("saving usage %s/%s: %w", u.Provider, u.Model, err)
		}
	}
	if err = stage("usage"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing run: %w", err)
	}
	return nil
}

// GetRun implements storage.Store.
func (s *Store) GetRun(ctx context.Context, id string) (storage.RunRecord, error) {
	var r storage.RunRecord
	var started, finished, mode, decision string
	var cost sql.NullInt64
	var partial int
	err := s.db.QueryRowContext(ctx, `SELECT id, schema_version, started_at, finished_at, head_sha, head_branch, base_ref,
		base_sha, mode, decision, exit_code, runs_per_case, min_drop_threshold, max_spread, currency, total_input_tokens,
		total_output_tokens, total_cost_micros, cost_partial FROM runs WHERE id = ?`, id).Scan(
		&r.ID, &r.SchemaVersion, &started, &finished, &r.HeadSHA, &r.HeadBranch, &r.BaseRef, &r.BaseSHA, &mode, &decision,
		&r.ExitCode, &r.Thresholds.RunsPerCase, &r.Thresholds.MinDropThreshold, &r.Thresholds.MaxSpreadBeforeUnreliable,
		&r.Currency, &r.TotalInputTokens, &r.TotalOutputTokens, &cost, &partial)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.RunRecord{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.RunRecord{}, fmt.Errorf("reading run %s: %w", id, err)
	}
	if r.StartedAt, err = time.Parse(timeLayout, started); err != nil {
		return storage.RunRecord{}, fmt.Errorf("run %s started_at: %w", id, err)
	}
	if r.FinishedAt, err = time.Parse(timeLayout, finished); err != nil {
		return storage.RunRecord{}, fmt.Errorf("run %s finished_at: %w", id, err)
	}
	r.Mode, r.Decision = storage.Mode(mode), storage.Decision(decision)
	r.TotalCostMicros, r.CostPartial = pi(cost), partial != 0

	if r.Suites, err = s.suites(ctx, id); err != nil {
		return storage.RunRecord{}, err
	}
	if r.Cases, err = s.cases(ctx, id); err != nil {
		return storage.RunRecord{}, err
	}
	if r.Usage, err = s.usage(ctx, id); err != nil {
		return storage.RunRecord{}, err
	}
	return r.Clone(), nil
}

func (s *Store) suites(ctx context.Context, id string) ([]storage.SuiteDecision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT suite, file, action, reason FROM run_suites WHERE run_id = ? ORDER BY rowid`, id)
	if err != nil {
		return nil, fmt.Errorf("reading suites of %s: %w", id, err)
	}
	defer rows.Close()
	out := []storage.SuiteDecision{}
	for rows.Next() {
		var d storage.SuiteDecision
		if err := rows.Scan(&d.Suite, &d.File, &d.Action, &d.Reason); err != nil {
			return nil, fmt.Errorf("reading suites of %s: %w", id, err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) cases(ctx context.Context, id string) ([]storage.CaseRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT suite, case_id, critical, status, baseline_runs, baseline_failed,
		baseline_average, baseline_spread, candidate_runs, candidate_failed, candidate_average, candidate_spread, delta
		FROM run_cases WHERE run_id = ? ORDER BY rowid`, id)
	if err != nil {
		return nil, fmt.Errorf("reading cases of %s: %w", id, err)
	}
	defer rows.Close()
	out := []storage.CaseRecord{}
	for rows.Next() {
		var c storage.CaseRecord
		var crit int
		var ba, bs, ca, cs, d sql.NullFloat64
		if err := rows.Scan(&c.Suite, &c.Case, &crit, &c.Status, &c.BaselineRuns, &c.BaselineFailed, &ba, &bs,
			&c.CandidateRuns, &c.CandidateFailed, &ca, &cs, &d); err != nil {
			return nil, fmt.Errorf("reading cases of %s: %w", id, err)
		}
		c.Critical = crit != 0
		c.BaselineAverage, c.BaselineSpread, c.CandidateAverage, c.CandidateSpread, c.Delta = pf(ba), pf(bs), pf(ca), pf(cs), pf(d)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) usage(ctx context.Context, id string) ([]storage.UsageRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider, model, calls, input_tokens, output_tokens, unknown_calls, cost_micros
		FROM run_usage WHERE run_id = ? ORDER BY rowid`, id)
	if err != nil {
		return nil, fmt.Errorf("reading usage of %s: %w", id, err)
	}
	defer rows.Close()
	out := []storage.UsageRecord{}
	for rows.Next() {
		var u storage.UsageRecord
		var cost sql.NullInt64
		if err := rows.Scan(&u.Provider, &u.Model, &u.Calls, &u.InputTokens, &u.OutputTokens, &u.UnknownCalls, &cost); err != nil {
			return nil, fmt.Errorf("reading usage of %s: %w", id, err)
		}
		u.CostMicros = pi(cost)
		out = append(out, u)
	}
	return out, rows.Err()
}

// ListRunsByCommit implements storage.Store.
func (s *Store) ListRunsByCommit(ctx context.Context, sha string) ([]storage.RunRecord, error) {
	return s.list(ctx, `SELECT id FROM runs WHERE head_sha = ? ORDER BY started_at DESC, id DESC`, sha)
}

// ListRecentRuns implements storage.Store.
func (s *Store) ListRecentRuns(ctx context.Context, limit int) ([]storage.RunRecord, error) {
	if limit < 0 {
		limit = 0
	}
	return s.list(ctx, `SELECT id FROM runs ORDER BY started_at DESC, id DESC LIMIT ?`, limit)
}

func (s *Store) list(ctx context.Context, query string, args ...any) ([]storage.RunRecord, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing runs: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("listing runs: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("listing runs: %w", err)
	}
	_ = rows.Close() // release the single connection before the per-run reads below
	out := []storage.RunRecord{}
	for _, id := range ids {
		r, err := s.GetRun(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
