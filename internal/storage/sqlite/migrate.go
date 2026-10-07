package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// migrations holds forward-only schema steps; migrations[i] upgrades version i to i+1. The
// database version is SQLite's user_version pragma.
var migrations = []string{
	// version 1: see specs/002-diff-scoping-storage/contracts/sqlite-schema.md
	`CREATE TABLE IF NOT EXISTS runs (
  id                  TEXT PRIMARY KEY,
  schema_version      INTEGER NOT NULL,
  started_at          TEXT NOT NULL,
  finished_at         TEXT NOT NULL,
  head_sha            TEXT NOT NULL,
  head_branch         TEXT NOT NULL,
  base_ref            TEXT NOT NULL,
  base_sha            TEXT NOT NULL,
  mode                TEXT NOT NULL CHECK (mode IN ('scoped','full','baseline-dir')),
  decision            TEXT NOT NULL CHECK (decision IN ('PASS','WARN','FAIL','ERROR')),
  exit_code           INTEGER NOT NULL,
  runs_per_case       INTEGER NOT NULL,
  min_drop_threshold  REAL NOT NULL,
  max_spread          REAL NOT NULL,
  currency            TEXT NOT NULL,
  total_input_tokens  INTEGER NOT NULL,
  total_output_tokens INTEGER NOT NULL,
  total_cost_micros   INTEGER,
  cost_partial        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS runs_head_sha ON runs(head_sha, started_at DESC);
CREATE INDEX IF NOT EXISTS runs_started  ON runs(started_at DESC);

CREATE TABLE IF NOT EXISTS run_suites (
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  suite  TEXT NOT NULL,
  file   TEXT NOT NULL,
  action TEXT NOT NULL CHECK (action IN ('run','skip','removed')),
  reason TEXT NOT NULL,
  PRIMARY KEY (run_id, suite)
);

CREATE TABLE IF NOT EXISTS run_cases (
  run_id            TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  suite             TEXT NOT NULL,
  case_id           TEXT NOT NULL,
  critical          INTEGER NOT NULL,
  status            TEXT NOT NULL CHECK (status IN ('PASS','REGRESSED','UNRELIABLE','ERROR','NEW')),
  baseline_runs     INTEGER NOT NULL,
  baseline_failed   INTEGER NOT NULL,
  baseline_average  REAL,
  baseline_spread   REAL,
  candidate_runs    INTEGER NOT NULL,
  candidate_failed  INTEGER NOT NULL,
  candidate_average REAL,
  candidate_spread  REAL,
  delta             REAL,
  PRIMARY KEY (run_id, suite, case_id)
);

CREATE TABLE IF NOT EXISTS run_usage (
  run_id        TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  provider      TEXT NOT NULL,
  model         TEXT NOT NULL,
  calls         INTEGER NOT NULL,
  input_tokens  INTEGER NOT NULL,
  output_tokens INTEGER NOT NULL,
  unknown_calls INTEGER NOT NULL,
  cost_micros   INTEGER,
  PRIMARY KEY (run_id, provider, model)
);`,
}

// currentVersion is the schema version this binary reads and writes.
func currentVersion() int { return len(migrations) }

// migrate upgrades the database to len(steps) in one transaction, re-reading the version
// inside the (immediate) transaction so concurrent first runs do not apply a step twice.
func migrate(ctx context.Context, db *sql.DB, steps []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var v int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}
	if v >= len(steps) {
		return nil
	}
	for i := v; i < len(steps); i++ {
		if _, err := tx.ExecContext(ctx, steps[i]); err != nil {
			return fmt.Errorf("migrating to version %d: %w", i+1, err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", len(steps))); err != nil {
		return fmt.Errorf("recording schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing migration: %w", err)
	}
	return nil
}
