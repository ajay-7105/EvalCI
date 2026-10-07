# Quickstart: Diff-Aware Suite Scoping and Run History validation

Proves the feature end to end, offline, using the mock Ollama server from `examples/mock` (no
model, no network, no keys). Contracts: [cli.md](contracts/cli.md),
[config-additions.md](contracts/config-additions.md), [store.md](contracts/store.md),
[sqlite-schema.md](contracts/sqlite-schema.md), [result.schema.json](contracts/result.schema.json).
Data rules: [data-model.md](data-model.md).

## Prerequisites

- Go 1.23+ to build; the resulting binary needs nothing else (no `git`, no database install).
- `git` is used only by this guide to build the demo repository; the tool itself does not need it.

## Build and test

```bash
CGO_ENABLED=0 go build -o evalci ./cmd/evalci
go build -o mockserver ./examples/mock/mockserver
go vet ./... && go test ./... -race && golangci-lint run
```

## Demo repository (shared setup)

`examples/scoping/setup.sh <dir> <history-db-path> [port]` creates a git repository with three
suites (`support`, `billing`, `legacy`) on `main`, each with its own prompt, a `.evalci.yaml`
pointing at the mock server with `storage.driver: sqlite` and a price for the mock model, then a
checked-out branch `feature/x`. Start the mock first: `./mockserver -addr 127.0.0.1:18080 &`.
`examples/scoping/run.sh` does all of this and asserts the outcomes of Scenarios 1-10 for you.

## Scenario 1: only the affected suite runs (User Story 1)

On `feature/x` edit only `prompts/support.txt`, commit, then:

```bash
./evalci run --base main
```

Expect: scope block shows `support  RUN  prompt file changed` and `billing`, `legacy` as
`SKIP  no related changes`; the table lists only `support`. Check the mock server's request log
or `--verbose` output: zero calls for the skipped suites (SC-001).

## Scenario 2: shared prompt, suite edit, add, delete, rename (User Story 1, edge cases)

Repeat with each change on its own commit: a prompt used by two suites (both run); a suite YAML
edit (that suite runs); a new suite (runs, status `NEW`, does not fail); a deleted suite
(`REMOVED`, not run); a renamed prompt (suites referencing either path run). Expected reasons
are in the data model.

## Scenario 3: nothing affected (FR-006)

Commit a change to `README.md` only. Expect `Decision: PASS (no suites affected)`, exit 0, zero
model calls, and a stored record with zero cost.

## Scenario 4: settings changes (FR-003)

Edit `runs_per_case` and commit: all suites run, reason `settings changed`. Edit only a price or
`storage.path`: no suites selected.

## Scenario 5: force a full run (User Story 3)

```bash
./evalci run --base main --full
```

Expect all suites run and reason `full run requested`.

## Scenario 6: history is saved and matches the output (User Story 2)

After any run:

```bash
sqlite3 .evalci/evalci.db "SELECT id, decision, exit_code, head_sha FROM runs ORDER BY started_at DESC LIMIT 3"
sqlite3 .evalci/evalci.db "SELECT suite, case_id, status, delta FROM run_cases WHERE run_id = '<id>'"
```

Expect a row per run, including FAIL, ERROR, and nothing-selected runs; case rows equal the
printed table; run the same commit twice and see two rows. Confirm no column holds responses,
prompts, or inputs. The JSON file's `run_id` equals the stored id.

## Scenario 7: save failures never change the gate (FR-016)

Make the history path unwritable (`chmod -w .evalci`) and rerun. Expect a `could not save run
history` warning on stderr, the same table, decision, and exit code as before.

## Scenario 8: refusals before any model call (FR-007a, FR-008, FR-025)

- `--base nope` -> exit 2, unresolved ref message.
- `--head other-branch` while `feature/x` is checked out -> exit 2 naming the checked-out branch.
- `--base main --baseline-dir ./baseline` -> exit 2, ambiguous.
- `storage.driver: postgres` -> exit 2, `backend "postgres" is not available`.
- Shallow clone without a common ancestor -> exit 2 suggesting fetching more history.

## Scenario 9: cost (FR-020 to FR-022)

With prices configured for the mock model, expect a `Cost:` line with token counts and an
estimate; remove the price and expect `unknown`, with tokens still shown and stored and
`cost_micros` NULL.

## Scenario 10: swapping the backend (User Story 5, SC-007)

`go test ./internal/storage/... -race` runs the shared contract suite against both the SQLite
and the in-memory implementations; both must pass with no caller change.
