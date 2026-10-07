# Contract: SQLite schema (version 1)

`PRAGMA user_version = 1`. Users may query the file directly (there is no history command in this
phase), so column names are part of the contract. Times are UTC RFC 3339 text; costs are integer
micro-units of the run's currency (1,000,000 = 1 unit); NULL means unknown, never zero.

```sql
CREATE TABLE runs (
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
CREATE INDEX runs_head_sha ON runs(head_sha, started_at DESC);
CREATE INDEX runs_started  ON runs(started_at DESC);

CREATE TABLE run_suites (
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  suite  TEXT NOT NULL,
  file   TEXT NOT NULL,
  action TEXT NOT NULL CHECK (action IN ('run','skip','removed')),
  reason TEXT NOT NULL,
  PRIMARY KEY (run_id, suite)
);

CREATE TABLE run_cases (
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

CREATE TABLE run_usage (
  run_id        TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  provider      TEXT NOT NULL,
  model         TEXT NOT NULL,
  calls         INTEGER NOT NULL,
  input_tokens  INTEGER NOT NULL,
  output_tokens INTEGER NOT NULL,
  unknown_calls INTEGER NOT NULL,
  cost_micros   INTEGER,
  PRIMARY KEY (run_id, provider, model)
);
```

Notes:
- There are deliberately no columns for responses, prompts, or case inputs.
- Connection settings: WAL journal, `busy_timeout = 5000`, `foreign_keys = ON`, one open
  connection per process; each `SaveRun` is one transaction.
- Upgrades: a lower `user_version` is migrated forward in one transaction; a higher one is
  rejected without writing.

Example queries a user can run:

```sql
SELECT id, started_at, decision, total_cost_micros / 1e6 AS cost FROM runs ORDER BY started_at DESC LIMIT 10;
SELECT suite, case_id, delta FROM run_cases WHERE run_id = ? ORDER BY delta DESC;
```
