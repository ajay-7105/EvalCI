---

description: "Task list for Diff-Aware Suite Scoping and Run History"
---

# Tasks: Diff-Aware Suite Scoping and Run History

**Input**: Design documents from `/specs/002-diff-scoping-storage/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md; the
merged phase-1 MVP (`specs/001-mvp-eval-gate/`).

**Tests**: Included. The constitution requires table-driven tests per package and >80% coverage
on `internal/gating` and `internal/engine`. Write each test task before the implementation it
covers and confirm it fails first.

**Organization**: Grouped by user story. Unlike phase 1, the story order is not a strict
dependency order: running from a base branch needs git access and baseline export, so those live
in Phase 2 (Foundational). After Phase 2, `evalci run --base <ref>` already works as a full run
against the base ref; User Story 1 then adds selection.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: US1-US5 map to the user stories in spec.md
- Paths are relative to the repository root `.` (the repository root)

## Conventions (apply to every task)

- Every exported symbol has a doc comment; errors wrapped with `fmt.Errorf("...: %w", err)`; no
  global mutable state; `context.Context` first on I/O functions.
- Core packages (`engine`, `assertions`, `gating`, `diffscope` selection, `cost`) never import a
  concrete provider, SQLite, or go-git. Only `internal/diffscope/gitrepo.go` imports go-git and
  only `internal/storage/sqlite` imports the SQLite driver (enforced by T060).
- Run `gofmt`, `go vet ./...`, and `golangci-lint run` before marking a phase done.
- Records never contain responses, prompts, or case inputs (FR-013a).

---

## Phase 1: Setup

**Purpose**: Dependencies and package skeletons

- [X] T001 Add dependencies with `go get github.com/go-git/go-git/v5 modernc.org/sqlite` then `go mod tidy` in `go.mod`; confirm `CGO_ENABLED=0 go build ./...` still succeeds
- [X] T002 [P] Add package `doc.go` files for the new packages: `internal/diffscope/doc.go`, `internal/storage/doc.go`, `internal/storage/factory/doc.go`, `internal/storage/sqlite/doc.go`, `internal/storage/memory/doc.go`, `internal/storage/storagetest/doc.go`, `internal/cost/doc.go`; create the empty `examples/scoping/` directory

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Cross-cutting changes every story needs: provider usage reporting, config additions, the `NEW` status, git access, baseline export from a base ref, and CLI flag plumbing.

**CRITICAL**: No user story work starts until this phase is complete.

### Provider usage (Research R6)

- [X] T003 In `internal/providers/provider.go` add `type Usage struct{ InputTokens, OutputTokens int; Known bool }` (`Known` is false when the provider reported no usage for the call), add `Usage Usage` to `Response`, add `type Embedding struct{ Vector []float32; Usage Usage }`, and change `Provider.Embed` to return `(Embedding, error)`; update the interface doc comments
- [X] T004 [P] Update the fake provider in `internal/providers/fake/fake.go` for the new signatures: `CompleteFn` may return usage, add a `UsagePerCall providers.Usage` default applied when a reply carries none, and `EmbedFn` returns `providers.Embedding`
- [X] T005 [P] Update the Ollama adapter `internal/providers/ollama/ollama.go` to read `prompt_eval_count` and `eval_count` from `/api/chat` and `prompt_eval_count` from `/api/embed` (use pointers: absent field means `Known: false`), and return `Embedding`; extend `internal/providers/ollama/ollama_test.go`
- [X] T006 [P] Update the OpenAI adapter `internal/providers/openai/openai.go` to read `usage.prompt_tokens` and `usage.completion_tokens` (chat) and `usage.prompt_tokens` (embeddings), and return `Embedding`; extend `internal/providers/openai/openai_test.go`
- [X] T007 [P] Update the Anthropic adapter `internal/providers/anthropic/anthropic.go` to read `usage.input_tokens` and `usage.output_tokens`, and keep `Embed` returning `ErrEmbeddingsUnsupported` with the new return type; extend `internal/providers/anthropic/anthropic_test.go`
- [X] T008 Update `internal/assertions/semantic.go` and `internal/assertions/semantic_test.go` to use `Embedding.Vector`, and fix the `Embed` method of the `limited` wrapper in `internal/engine/limiter.go` for the new signature so `go build ./...` and all phase-1 tests pass again

### Config additions (contracts/config-additions.md)

- [X] T009 In `configs/schema.go` add `Settings.Prices map[string]Price`, `Settings.Currency string`, `StorageSettings.Path string`, and `type Price struct{ InputPerMillion, OutputPerMillion float64 }`; constraints verbatim from data-model.md: price key must be `provider/model` with a known provider, `input_per_million` required `>= 0`, `output_per_million` required `>= 0`, `currency` default `USD` non-empty, `storage.driver` `sqlite` or `postgres`, `storage.path` default `.evalci/evalci.db`
- [X] T010 In `configs/load.go` parse and validate the new fields with the existing strict unknown-key rejection and file+field error messages; extract `ParseSettings(name string, data []byte) (Settings, error)` from `LoadSettings` so settings can be parsed from a base-ref blob; apply the `currency` and `storage.path` defaults
- [X] T011 [P] Table-driven tests for the new settings fields in `configs/load_test.go`: valid prices, key without provider, unknown provider in key, negative price, missing `output_per_million`, `currency` default, `storage.path` default, `ParseSettings` from bytes names the supplied file name in errors

### Gating

- [X] T012 In `internal/gating/types.go` add `StatusNew`; in `internal/gating/gate.go` add `EvaluateNew(suite, caseID string, critical bool, candidate Side) CaseResult` (rules: candidate has no successful run => `ERROR`, otherwise `NEW`; `Drop` is 0); `Decide` must ignore `NEW`; add table tests in `internal/gating/gate_test.go` including `NEW` never causing `FAIL` or `WARN` and a mix of `NEW` with `ERROR`

### Git access and baseline export (Research R1, R9, R11)

- [X] T013 Define the git abstraction in `internal/diffscope/repo.go`: `Repository` interface (`Resolve(ref) (Commit, error)`, `CheckedOut() (Commit, string /*branch*/, error)`, `MergeBase(a, b Commit) (Commit, error)`, `ChangedFiles(from, to Commit) ([]FileChange, error)`, `FileAt(c Commit, path string) ([]byte, error)`, `Dirty() (bool, error)`), `type FileChange struct{ Status string /*added|modified|deleted|renamed*/; Path, OldPath string }`, `type ChangeSet struct{ Base, Head, MergeBase Commit; BaseRef, HeadRef, HeadBranch string; Files []FileChange }`, and sentinel errors `ErrNotARepository`, `ErrRefNotFound`, `ErrNoCommonAncestor`, `ErrHeadNotCheckedOut`
- [X] T014 Implement the go-git backed repository in `internal/diffscope/gitrepo.go` (the only file importing go-git): open the repository from a directory, resolve branch/tag/SHA refs, report the checked-out commit and branch (empty when detached), compute the merge base, diff trees with rename detection (renames reported with both `Path` and `OldPath`), read a blob at a commit, report a dirty working tree; no merge base returns `ErrNoCommonAncestor` with a message suggesting fetching more history
- [X] T015 Tests for the go-git implementation in `internal/diffscope/gitrepo_test.go` using real repositories built in `t.TempDir()` with go-git: added/modified/deleted/renamed files, merge base across a branch point, head equal and not equal to the checked-out commit, detached HEAD, unresolvable ref, not a repository, unrelated histories giving `ErrNoCommonAncestor`, dirty working tree detection, `FileAt` for a path missing at that commit
- [X] T016 Implement baseline export in `internal/diffscope/export.go`: `ExportBase(repo Repository, base Commit, evalsDir string) (dir string, suites []configs.Suite, cleanup func(), err error)` writes the base commit's `<evalsDir>/*.eval.yaml` into a temp dir, parses them with `configs.LoadSuites`, then writes each referenced prompt file at its path relative to the repo root; a prompt missing at base is an error naming the suite; `cleanup` removes the temp dir
- [X] T017 [P] Tests for `ExportBase` in `internal/diffscope/export_test.go`: exports suites and prompts exactly as at the base commit (not the working tree), missing prompt at base names the suite, no `evals/` at base returns `configs.ErrNoSuites`, cleanup removes the directory, files outside `evals/` and referenced prompts are not exported

### CLI plumbing (contracts/cli.md)

- [X] T018 In `cmd/evalci/run.go` add flags `--base`, `--head` (default `HEAD`), `--full`; validate before any other work: exactly one of `--base` or `--baseline-dir` (both, or neither, exit 2 with an "ambiguous"/"required" message), `--full` or `--head` without `--base` exit 2; with `--base`: open the repository, resolve base and head, require head equal to the checked-out commit (else exit 2 naming the checked-out branch and saying to check the branch out), compute the merge base and change set, call `ExportBase` into a temp dir used as the baseline root (cleaned up on exit); add `openRepo` to `runDeps` for tests; with `--base` and no selection logic yet, all suites run
- [X] T019 Tests in `cmd/evalci/run_test.go` for flag validation and refusals using the fake provider and real temp git repos: both or neither flags, `--full`/`--head` without `--base`, unresolvable `--base`, `--head` not checked out, no common ancestor, not a repository; each exits 2 with zero model calls; plus `--base` running all suites against the base commit's prompts

**Checkpoint**: `evalci run --base <ref>` works as a full run with a git-sourced baseline; all phase-1 tests still pass.

---

## Phase 3: User Story 1 - Run only the suites a pull request affects (Priority: P1) MVP

**Goal**: Select suites from the change set and skip the rest, with reasons.

**Independent Test**: In a repo with several suites, change one suite's prompt, run `evalci run --base main`; only that suite makes model calls and appears in results; others are listed as skipped (quickstart Scenarios 1-3).

### Tests for User Story 1

- [X] T020 [P] [US1] Table-driven tests for suite selection in `internal/diffscope/selection_test.go` covering the ordered rules in data-model.md (full -> new -> suite file changed -> prompt changed (candidate or baseline path) -> settings changed -> skip): one prompt edit selects only referencing suites, a prompt shared by two suites selects both, a renamed prompt selects suites referencing the old or the new path, an edited suite file, an added suite is `new suite`, a suite deleted at head is `removed` and never `run`, only unrelated files changed selects nothing, and every decision carries its reason and the files that caused it
- [X] T021 [P] [US1] Tests for run-affecting settings detection in `internal/diffscope/settingsdiff_test.go`: `runs_per_case`, `min_drop_threshold`, `max_spread_before_unreliable`, and each provider's `base_url`, `api_key_env`, `max_concurrent`, `requests_per_minute` count; differences only in `prices`, `currency`, or `storage` do not; a settings file added or deleted counts as changed; comments and key order do not
- [X] T022 [P] [US1] Golden tests for the scope block in `internal/reporters/scope_test.go` matching contracts/cli.md: `RUN`/`SKIP`/`REMOVED` rows with reasons, the `--full` header `full run requested; 3 of 3 suites selected`, and the nothing-selected output
- [X] T023 [US1] End-to-end scoping tests in `cmd/evalci/scope_test.go` with real temp git repos and a fake provider that counts calls per model name: one prompt change makes calls only for its suite and 0 for the others (SC-001), no relevant change prints `Decision: PASS (no suites affected)` with exit 0, zero calls, and still writes the JSON file, a shared prompt runs both suites, a changed suite file runs only that suite

### Implementation for User Story 1

- [X] T024 [P] [US1] Implement run-affecting settings comparison in `internal/diffscope/settingsdiff.go`: `RunAffectingChanged(base, head *configs.Settings) bool` over exactly the fields listed in data-model.md (nil on either side means the file was added or deleted and counts as changed)
- [X] T025 [US1] Implement selection in `internal/diffscope/selection.go`: `type ScopeDecision struct{ Suite, File, Action, Reason string; Files []string }` and a pure `Select(in SelectInput) []ScopeDecision` (inputs: candidate suites, base suites, change set, settings changed flag, full flag) following the data-model order and reason strings exactly (`full run requested`, `new suite`, `suite file changed`, `prompt file changed`, `settings changed`, `no related changes`, `suite removed`)
- [X] T026 [P] [US1] Implement the scope block writer in `internal/reporters/scope.go`: `WriteScope(w, ScopeSummary)` printing the header (`Scope: base <ref> (<sha7>) -> head <ref> (<sha7>); N of M suites selected`, or the `--full` form) and one `RUN`/`SKIP`/`REMOVED` row per suite with its reason
- [X] T027 [US1] Wire selection into `cmd/evalci/run.go`: read `.evalci.yaml` at the base commit via `FileAt` and `ParseSettings` to compute the settings-changed flag, call `Select`, run only the selected pairs, print the scope block before the table, handle the nothing-selected case (print scope block and `Decision: PASS (no suites affected)`, make no model calls, write the JSON file, exit 0), and warn on stderr when the working tree is dirty (`warning: working tree has uncommitted changes; they are not part of the comparison`)
- [X] T028 [US1] Create the runnable example `examples/scoping/setup.sh` (builds a git repository in a given directory with suites `support`, `billing`, `legacy`, each with its own prompt and `temperature: 0`, a `.evalci.yaml` pointing at the mock Ollama server on port 18080, branch `main` and branch `feature/x`) and `examples/scoping/run.sh` (builds `evalci` and `examples/mock/mockserver`, runs the scoped scenarios from quickstart 1-3, asserts which suites ran and the decision and exit code, exits non-zero on mismatch) plus `examples/scoping/README.md`

**Checkpoint**: User Story 1 works end to end and is demonstrable on its own.

---

## Phase 4: User Story 2 - Keep a permanent record of every run (Priority: P1)

**Goal**: Save one complete record per finished run to SQLite through `storage.Store`, with token usage and estimated cost.

**Independent Test**: Run on a known commit, read the record back by commit, and confirm it matches the printed result field for field (quickstart Scenarios 6, 7, 9).

### Tests for User Story 2

- [X] T029 [P] [US2] Table-driven tests for cost computation in `internal/cost/cost_test.go`: integer micro-unit math `tokens * price_micro_per_million / 1_000_000` rounded per model, no price gives `nil` cost (never 0) with tokens kept, `UnknownCalls > 0` gives `nil` even with a price, partial run totals set the partial flag, run total tokens equal the sum of per-call usage and cost equals tokens times prices to the cent (SC-009)
- [X] T030 [P] [US2] Tests for the usage meter in `internal/engine/usage_test.go`: aggregates per `(provider, model)` across generation, judge, and embedding calls made through the limited wrapper, counts `Known == false` calls as `UnknownCalls`, is safe under concurrent calls with `-race`, and total tokens equal the sum of fake per-call usage
- [X] T031 [P] [US2] Write the shared behavior suite `storagetest.RunContract(t, newStore func(t *testing.T) storage.Store)` in `internal/storage/storagetest/contract.go` implementing the seven checks in contracts/store.md: round trip including nil cost and nil averages, atomicity (forced mid-save error leaves nothing visible), duplicate ID gives `ErrDuplicateID` and leaves the record unchanged while two runs of one commit with different IDs both exist, query ordering and limits and `ErrNotFound`, deterministic tie-break by ID, parallel saves all complete, and no content fields in the record types (reflection)
- [X] T032 [P] [US2] SQLite tests in `internal/storage/sqlite/sqlite_test.go`: pass `storagetest.RunContract`; a newer `user_version` returns `ErrNewerSchema` and leaves the file byte-for-byte unchanged; an older or empty database is migrated without losing records; a read-only location returns an error; two handles on one file save concurrently without corruption (SC-010); table and column names match contracts/sqlite-schema.md; NULL costs round-trip as nil
- [X] T033 [P] [US2] In-memory store tests in `internal/storage/memory/memory_test.go`: pass `storagetest.RunContract`
- [X] T034 [US2] End-to-end history tests in `cmd/evalci/history_test.go` with real temp git repos, a fake provider, and a real SQLite file: record decision, exit code, commit, per-case baseline/candidate averages, spreads, delta, status, critical flag, thresholds, scope decisions, and usage equal what was printed (SC-004), retrievable by commit; a FAIL, an ERROR, and a nothing-selected run each save exactly one record (SC-003); the same commit twice gives two records; storage not configured saves nothing and creates no file; an injected failing store prints `warning: could not save run history` and leaves the decision and exit code unchanged (SC-008); a cancelled run saves nothing

### Implementation for User Story 2

- [X] T035 [P] [US2] Define the record types in `internal/storage/types.go` exactly per data-model.md: `RunRecord`, `SuiteDecision`, `CaseRecord`, `UsageRecord`, `Thresholds`, enums `Mode` (`scoped`, `full`, `baseline-dir`) and `Decision`, pointer fields for unknown values (`TotalCostMicros *int64`, `CostMicros *int64`, `BaselineAverage *float64` and the other averages, spreads, and `Delta`), and `RunRecord.Validate()` enforcing: ID non-empty, `ExitCode` consistent with `Decision` (PASS/WARN 0, FAIL 1, ERROR 2), head SHA present, `FinishedAt >= StartedAt`, case suites appear in `Suites` with action `run`; no field may hold responses, prompts, or case inputs
- [X] T036 [US2] Define the interface in `internal/storage/store.go`: `Store` (`SaveRun`, `GetRun`, `ListRunsByCommit` newest first, `ListRecentRuns` newest first, `Close`), `Config{Driver, Path}`, and errors `ErrNotFound`, `ErrDuplicateID`, `ErrNewerSchema`, `ErrBackendUnavailable`
- [X] T037 [P] [US2] Implement the in-memory store in `internal/storage/memory/store.go` (mutex-guarded, copies records in and out so callers cannot mutate stored data)
- [X] T038 [US2] Implement the SQLite store in `internal/storage/sqlite/store.go` and `internal/storage/sqlite/migrate.go` per contracts/sqlite-schema.md and Research R2/R3: read `user_version` through a read-only connection first and return `ErrNewerSchema` without writing when it exceeds the supported version, create parent directories, WAL journal, `busy_timeout = 5000`, `foreign_keys = ON`, one open connection, forward-only embedded migrations in one transaction, `SaveRun` as a single transaction, times stored as UTC RFC 3339 text, costs as integer micro-units with NULL for unknown
- [X] T039 [P] [US2] Implement the driver factory in `internal/storage/factory/factory.go`: `Open(ctx, storage.Config) (storage.Store, error)` is the only place that branches on the driver; `sqlite` opens the store at `Config.Path`; `postgres` and any other value return `storage.ErrBackendUnavailable` wrapped with the driver name; an empty driver is invalid
- [X] T040 [P] [US2] Implement cost computation in `internal/cost/cost.go`: `Compute(usage []Usage, prices map[string]Price) Summary` where `Usage{Provider, Model string; Calls, InputTokens, OutputTokens, UnknownCalls int64}` and `Price{InputPerMillion, OutputPerMillion float64}` are plain structs defined in the package (the CLI converts from `engine.ModelUsage` and `configs.Price`), using integer micro-units (`tokens * price_micro_per_million / 1_000_000`, rounded per model), `nil` when no price exists or any call's usage was unknown, with `Summary{Models []ModelCost, TotalInput, TotalOutput int64, TotalCostMicros *int64, Partial bool}`; the package must not import a provider or engine package (take plain structs)
- [X] T041 [US2] Implement the usage meter in `internal/engine/usage.go` and call it from the `limited` wrapper in `internal/engine/limiter.go` for every `Complete` and `Embed` call; expose `Engine.Usage() []ModelUsage` (`Provider`, `Model`, `Calls`, `InputTokens`, `OutputTokens`, `UnknownCalls`) aggregated per `(provider, model)`; no extra model calls
- [X] T042 [US2] Build and save records in `cmd/evalci/record.go`: `buildRecord` assembles a `storage.RunRecord` from the results, scope decisions, settings, change set, usage, and cost summary (mode `scoped`, `full`, or `baseline-dir`; head branch name per Research R11; `BaseRef`/`BaseSHA` empty for `baseline-dir`), with an injected clock and run ID `YYYYMMDDTHHMMSSZ-<8 hex>` from `crypto/rand`
- [X] T043 [US2] Wire persistence into `cmd/evalci/run.go`: when `storage.driver` is set, call `factory.Open` before any model call (an unavailable backend such as `postgres` exits 2 naming it; any other open failure is only a warning and the run continues unsaved), save once after final results exist and never for a cancelled run, treat any save failure as `warning: could not save run history: <reason>` on stderr with the decision, table, JSON, and exit code unchanged; add a `store` seam to `runDeps` for tests
- [X] T044 [US2] Update `internal/reporters/table.go` and `internal/reporters/json.go` for schema version 2 per `contracts/result.schema.json` and contracts/cli.md: add the cost line (`Cost: <in> input / <out> output tokens, est. <amount> <currency>` with `unknown` and `(cost unknown for N model(s))` forms; omitted when nothing ran), and the JSON fields `run_id` (null when nothing was saved), `scope`, `cost`, and the `NEW` case status; update `schema_version` to 2
- [X] T045 [P] [US2] Tests for the cost line and JSON v2 in `internal/reporters/table_test.go` and `internal/reporters/json_test.go`: golden cost line (known, unknown, partial, omitted), JSON validates against `specs/002-diff-scoping-storage/contracts/result.schema.json` using the existing schema validator helper, `run_id` equals the stored ID, and point the existing phase-1 schema tests at the version-2 schema
- [X] T046 [US2] Make the mock server report token usage so cost can be demonstrated offline: in `examples/mock/mockserver/main.go` return `prompt_eval_count` (word count of the request messages) and `eval_count` (word count of the reply) in `/api/chat` responses, and an `/api/embed` handler returning a fixed vector plus `prompt_eval_count`; keep `examples/mock/run.sh` passing
- [X] T047 [US2] Extend `examples/scoping/run.sh` with history assertions (record exists per run, the stored decision equals the printed one, same commit twice gives two rows, a price in `.evalci.yaml` yields a non-NULL `total_cost_micros`, removing it yields NULL) using the `sqlite3` CLI when available and skipping those checks with a message otherwise; document the example queries in `examples/scoping/README.md`

**Checkpoint**: User Stories 1 and 2 together deliver scoped runs with permanent history.

---

## Phase 5: User Story 3 - Force a full run and see why suites were chosen (Priority: P2)

**Goal**: `--full` and complete, visible reasons everywhere.

**Independent Test**: Run with `--full` and confirm every suite runs; run without it and confirm each skipped suite is listed with its reason (quickstart Scenarios 4-5).

- [X] T048 [P] [US3] End-to-end tests in `cmd/evalci/full_test.go` with real temp git repos: `--full` runs every suite with reason `full run requested` and the stored mode is `full`; a scoped run lists every suite as run or skipped with its reason in the printed output, in the JSON `scope.suites`, and in the stored `run_suites` rows; a change to `runs_per_case` selects all suites with reason `settings changed`; a change only to `prices` or `storage.path` selects none
- [X] T049 [US3] Make selection reasons complete across all outputs: ensure `Select` results flow into the scope block, JSON `scope`, and the stored record without loss (suite, file, action, reason, files) in `cmd/evalci/record.go` and `internal/reporters/json.go`; set the record `Mode` to `full` when `--full` is given
- [X] T050 [US3] Extend `examples/scoping/run.sh` with quickstart Scenarios 4-5 (settings change selects all, price-only change selects none, `--full` runs all) and add them to `examples/scoping/README.md`

---

## Phase 6: User Story 4 - Compare against the base branch automatically (Priority: P2)

**Goal**: No manual baseline directory; new suites run candidate-only; explicit `--baseline-dir` keeps working.

**Independent Test**: Run with `--base` and no baseline directory; baseline scores come from the base branch's prompt and model; a suite added on the PR branch is reported `NEW` and does not fail (quickstart Scenario 2).

- [X] T051 [P] [US4] Engine tests for candidate-only suites in `internal/engine/engine_test.go`: a pair with no baseline makes only candidate calls (no baseline calls), cases get status `NEW`, scores and failed runs are still recorded, all candidate runs failing gives `ERROR`, and `Decide` ignores `NEW`
- [X] T052 [P] [US4] End-to-end baseline tests in `cmd/evalci/baseline_test.go` with real temp git repos and a fake provider that answers from the system prompt: baselines use the prompt and model as they exist on the base branch (not the working tree), a suite added on the PR branch is `NEW` and exit 0, a suite deleted on the PR branch is `REMOVED` and exit 0, an explicit `--baseline-dir` behaves exactly as in phase 1 (all suites run, no scope block, stored mode `baseline-dir`)
- [X] T053 [US4] Support candidate-only suites in `internal/engine/baseline.go` and `internal/engine/engine.go`: `SuitePair.Baseline` becomes optional (`HasBaseline bool`), `ResolveBaseline` takes an `allowMissing` flag so a missing counterpart yields a no-baseline pair instead of an error when running from a base ref (phase-1 `--baseline-dir` behavior unchanged), the runner schedules only candidate runs for such pairs and produces results with `gating.EvaluateNew`, and `Preflight` in `internal/engine/preflight.go` skips baseline checks for them
- [X] T054 [US4] Show `NEW` rows in `internal/reporters/table.go` (baseline `-`, drop `-`, status `NEW`) and in the JSON writer (baseline side with `runs` 0, empty `scores`, null `average` and `spread`); add golden and schema-validation cases in `internal/reporters/table_test.go` and `internal/reporters/json_test.go`
- [X] T055 [US4] Extend `examples/scoping/run.sh` with a new-suite scenario and a deleted-suite scenario and document them in `examples/scoping/README.md`

---

## Phase 7: User Story 5 - Swap the storage backend without touching callers (Priority: P3)

**Goal**: Prove the abstraction: another backend needs no caller change, and unavailable backends are rejected early.

**Independent Test**: The same contract suite passes against SQLite and the in-memory store; a CLI run works identically with either injected (quickstart Scenarios 8, 10).

- [X] T056 [P] [US5] Tests for the driver factory in `internal/storage/factory/factory_test.go`: `sqlite` opens and saves a record, `postgres` returns `storage.ErrBackendUnavailable` and the message names `postgres`, an unknown driver does the same, an empty driver is rejected
- [X] T057 [P] [US5] CLI tests in `cmd/evalci/storage_test.go`: `storage.driver: postgres` exits 2 with `storage backend "postgres" is not available in this version` and makes zero model calls; the same full run produces identical decision, exit code, and per-case record values when the injected store is the in-memory implementation and when it is SQLite, with no change to any caller code
- [X] T058 [P] [US5] Dependency-boundary test in `internal/storage/boundary_test.go`: only `internal/storage/sqlite` imports `modernc.org/sqlite`; only `internal/diffscope/gitrepo.go` imports go-git; `cmd/evalci`, `internal/engine`, `internal/reporters`, and `internal/cost` import `internal/storage` (and `internal/storage/factory` for `cmd/evalci` only) but never `internal/storage/sqlite` or `internal/storage/memory`; `internal/engine`, `internal/assertions`, `internal/gating`, and `internal/cost` still contain no vendor name literals

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T059 [P] Re-verify the static-binary rule (Constitution I): `CGO_ENABLED=0 go build -o evalci ./cmd/evalci` succeeds and `CGO_ENABLED=0 go list -f '{{if .CgoFiles}}{{.ImportPath}}{{end}}' -deps ./...` prints nothing, now including go-git and modernc SQLite
- [X] T060 [P] Enforce coverage: `go test ./internal/gating ./internal/engine -race -cover` must stay above 80% for both; also confirm `internal/diffscope`, `internal/storage/...`, and `internal/cost` are above 80% and add tests where they are not
- [X] T061 [P] Performance checks from the spec in `internal/diffscope/perf_test.go` and `internal/storage/sqlite/perf_test.go`: resolving selection for a 1,000-file change set under 2 seconds (SC-005) and saving a 500-case record under 1 second (SC-006); skip under `-short`
- [X] T062 Run `go build ./...`, `go vet ./...`, `go test ./... -race`, `golangci-lint run`, `examples/mock/run.sh`, and `examples/scoping/run.sh`; fix findings
- [X] T063 Walk through all ten scenarios in `specs/002-diff-scoping-storage/quickstart.md` against `examples/scoping` and correct the example or quickstart where they disagree
- [X] T064 Update `claude.md` Status and Commands to describe phase 2 (new packages, `--base`/`--head`/`--full`, storage opt-in and path, prices, unavailable `postgres`, the cost-unknown rule, records never hold responses or prompts) and mention `examples/scoping`

---

## Dependencies & Execution Order

### Phase Dependencies

- Setup (1) -> Foundational (2) -> user stories -> Polish (8).
- Foundational blocks every story. Within it: provider usage (T003-T008) must land together to
  keep the build green; config (T009-T011), gating (T012), and git access (T013-T017) are
  independent of each other; CLI plumbing (T018-T019) needs all of them.

### User Story Dependencies

- **US1**: needs Phase 2 only. MVP.
- **US2**: needs Phase 2; builds on US1's scope data for the stored suites, so start the record
  builder (T042) after US1's selection (T025) and wiring (T027); the storage packages
  (T035-T039) and cost (T040) are independent of US1.
- **US3**: needs US1 and US2 (it verifies reasons across printed output, JSON, and records).
- **US4**: needs Phase 2; touches `internal/engine` and `internal/reporters` shared with US1/US2,
  so sequence edits to `engine.go`, `table.go`, and `json.go`.
- **US5**: needs US2 (the abstraction and factory); mostly tests.

### Within Each Story

Tests first (confirm they fail), then pure packages (`gating`, `diffscope` selection, `cost`,
`storage` types) before the engine and CLI wiring; wiring in `cmd/evalci/run.go` is sequential
across stories (same file).

### Parallel Opportunities

- Phase 2: T004-T007 together after T003; config (T009-T011) and gating (T012) in parallel with
  each other; after T013, the implementations T014 and T016 in parallel.
- US1: tests T020-T022 together (T023 after the pieces it exercises exist); T024 and T026 in
  parallel.
- US2: tests T029-T033 together; then T037, T038, T039, and T040 in parallel (different
  packages) once T035-T036 exist.
- US4 and US5 tests together once their prerequisites exist.
- Polish: T059-T061 together.

### Parallel Example: User Story 2 storage packages

```text
Launch together after the types and interface exist:
  internal/storage/memory/store.go
  internal/storage/sqlite/store.go + migrate.go
  internal/storage/factory/factory.go
  internal/cost/cost.go
```

---

## Implementation Strategy

### MVP First (User Story 1)

1. Phases 1-2, then US1. Validate with quickstart Scenarios 1-3 (`examples/scoping/run.sh`).
2. At this point CI cost drops immediately: unchanged suites make no model calls.

### Incremental Delivery

1. Add US2 (history and cost): the audit trail and cost visibility.
2. Add US3 (`--full`, visible reasons), US4 (new suites, baseline guarantees), US5 (backend
   swap proof).
3. Polish and the final quickstart walkthrough.

### Notes

- A phase is done only when `go build ./...`, `go vet ./...`, `go test ./... -race`, and
  golangci-lint are clean and `examples/scoping` demonstrates it (constitution Quality Gates).
- Do not add a dashboard, pull-request comments, retention or pruning, a Postgres
  implementation, a history browsing command, or case-level scoping (spec FR-026 and Out of
  scope).

