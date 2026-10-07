---

description: "Task list for MVP Eval Gate"
---

# Tasks: MVP Eval Gate

**Input**: Design documents from `/specs/001-mvp-eval-gate/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included. The constitution requires table-driven tests per package and >80%
coverage on `internal/gating` and `internal/engine`. Write each test task before the
implementation it covers and confirm it fails first.

**Organization**: Grouped by user story. US1 is the end-to-end MVP; later stories extend it.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: US1-US5 map to the user stories in spec.md
- Paths are relative to the repository root `.` (the repository root)

## Conventions (apply to every task)

- Every exported symbol has a doc comment; errors wrapped with `fmt.Errorf("...: %w", err)`;
  no global mutable state; `context.Context` is the first parameter of every provider call.
- Core packages (`engine`, `assertions`, `gating`) import only the `providers.Provider`
  interface, never a concrete provider.
- Config errors name the file and the field path (e.g. `evals/support.eval.yaml: cases[2].assertions[0].type`).

---

## Phase 1: Setup

**Purpose**: Project initialization

- [X] T001 Create `go.mod` (module `github.com/ajay-7105/EvalCI`, Go 1.23) and the empty package directories `cmd/evalci/`, `configs/`, `internal/engine/`, `internal/providers/`, `internal/assertions/`, `internal/gating/`, `internal/reporters/`, `examples/basic/`
- [X] T002 [P] Add strict golangci-lint config in `.golangci.yml` (enable default linters plus `errorlint`, `godot`, `revive` exported-doc rule)
- [X] T003 [P] Add dependencies: `github.com/spf13/cobra`, `gopkg.in/yaml.v3`, `golang.org/x/sync`, `golang.org/x/time` via `go get`, then `go mod tidy`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared types and plumbing every story needs

**CRITICAL**: No user story work starts until this phase is complete

- [X] T004 Define config types in `configs/schema.go`: `Settings`, `ProviderSettings`, `StorageSettings`, `Suite`, `Model`, `Case`, `Assertion`, with defaults applied by a `Settings.WithDefaults()` method: `runs_per_case` 5 (must be `>= 1`), `min_drop_threshold` 5.0 (`>= 0`, score points), `max_spread_before_unreliable` 20.0 (`>= 0`, score points), `max_concurrent` 4, `critical` default true, `case_sensitive` default true (both are `*bool` with `IsCritical()` / `IsCaseSensitive()` accessors returning true when nil, because a plain bool cannot distinguish an absent key from `false`); `Model.Temperature` is `*float64` and `Model.Validate()` returns an error naming `model.temperature` when it is nil (a missing key must never decode as 0; an explicit `0` is valid and means a single run per variant)
- [X] T005 Implement strict loaders in `configs/load.go`: `LoadSettings(path)` and `LoadSuites(dir)` using `yaml.v3` with `KnownFields(true)`; every error is wrapped with the file and field path (FR-004); suite glob is `<dir>/*.eval.yaml` and zero matches returns a distinct `ErrNoSuites`
- [X] T006 [P] Table-driven tests for `configs/load.go` in `configs/load_test.go`: unknown key rejected with file+field named, defaults applied (`runs_per_case` 5, `min_drop_threshold` 5.0, `max_spread_before_unreliable` 20.0, `max_concurrent` 4, `critical` and `case_sensitive` true when absent and false when set to `false`), `runs_per_case: 0` rejected, `storage.driver` must be `sqlite` or `postgres`, provider names limited to `openai`, `anthropic`, `ollama`, no-suites error, suite without `model.temperature` rejected naming the field while `temperature: 0` is accepted
- [X] T007 [P] Define the adapter contract in `internal/providers/provider.go`: `Provider` interface with `Complete(ctx, Request) (Response, error)` and `Embed(ctx, model, text string) ([]float32, error)`; `Request` carries system message, user message, model name, temperature, max tokens; add `SupportsEmbeddings() bool` to the interface (pre-flight asks the provider; core packages never name a vendor); export `ErrEmbeddingsUnsupported` and an `IsTransient(err)` helper
- [X] T008 [P] Implement bounded retry in `internal/providers/retry.go`: up to 2 retries with exponential backoff on transient errors (network, 429, 5xx), honoring `ctx` cancellation; table-driven tests in `internal/providers/retry_test.go`
- [X] T009 [P] Implement a scriptable fake provider in `internal/providers/fake/fake.go` (per-call scripted responses/errors, call counter, optional embeddings) for tests and examples
- [X] T010 [P] Define result types in `internal/gating/types.go`: `Status` (`PASS`, `REGRESSED`, `UNRELIABLE`, `ERROR`), `Decision` (`PASS`, `WARN`, `FAIL`, `ERROR`), `Side{Runs, FailedRuns, Scores, Average, Spread}`, `CaseResult{Suite, Case, Critical, Baseline, Candidate, Drop, Status}`
- [X] T011 Create the CLI skeleton in `cmd/evalci/main.go` and `cmd/evalci/run.go`: cobra root plus `run` command with flags `--baseline-dir` (required), `--config` (default `.evalci.yaml`), `--evals` (default `evals`), `--output` (default `evalci-result.json`), `--verbose` (default off); the run body returns `not implemented` for now; `main()` maps returned errors to exit code 2

**Checkpoint**: Foundation ready; user stories can begin

---

## Phase 3: User Story 1 - Get a merge-gate decision for a prompt change (Priority: P1) MVP

**Goal**: One command runs suites against baseline and candidate and prints PASS/FAIL with a matching exit code, using exact-match scoring and a local Ollama model.

**Independent Test**: With the fake provider (and, via the example, Ollama): a clearly worse candidate gives `Decision: FAIL` and exit 1; an equal candidate gives `Decision: PASS` and exit 0 (quickstart Scenarios 1-2).

### Tests for User Story 1

- [X] T012 [P] [US1] Table-driven tests for case classification in `internal/gating/gate_test.go`: drop `>` threshold is REGRESSED, drop exactly at threshold is PASS (strictly greater), candidate higher is PASS, baseline average computed over scores
- [X] T013 [P] [US1] Tests for exact-match and the score combiner in `internal/assertions/assertions_test.go`: 0 or 100 per `case_sensitive` (default true), trimmed comparison, run score is the plain average of assertion scores (FR-009)
- [X] T014 [P] [US1] Engine tests with the fake provider in `internal/engine/engine_test.go`: each case runs `runs_per_case` times per variant, baseline and candidate both executed, per-provider `max_concurrent` never exceeded (use a counting fake), results sorted by suite then case id
- [X] T015 [P] [US1] Tests for the table reporter and exit-code mapping in `internal/reporters/table_test.go`: golden output matching the table in `specs/001-mvp-eval-gate/contracts/cli.md`; exit 0 for PASS, 1 for FAIL

### Implementation for User Story 1

- [X] T016 [US1] Implement `Classify(baseline, candidate Side, s Settings) Status` (REGRESSED when `baseline average - candidate average > min_drop_threshold`, else PASS) and `Decide([]CaseResult) Decision` (FAIL if any case with `Critical == true` is REGRESSED, else PASS) in `internal/gating/gate.go`; pure functions, no I/O
- [X] T017 [P] [US1] Implement the exact-match assertion and `Score(assertions, output)` averaging combiner in `internal/assertions/assertions.go`, with an `Assertion` interface taking `ctx` so later assertion types plug in
- [X] T018 [P] [US1] Implement the Ollama adapter in `internal/providers/ollama/ollama.go` using `net/http` against `base_url` (chat completion and embeddings), wrapped by the retry helper; `httptest`-based tests in `internal/providers/ollama/ollama_test.go`
- [X] T019 [P] [US1] Implement the per-provider limiter in `internal/engine/limiter.go`: a buffered-channel semaphore sized `max_concurrent` plus an optional `golang.org/x/time/rate` limiter from `requests_per_minute` (0 = unlimited); `Acquire(ctx)` returns a release func; tests in `internal/engine/limiter_test.go`
- [X] T020 [US1] Implement baseline resolution in `internal/engine/baseline.go`: for each candidate suite find the same-named suite under `--baseline-dir/evals`, take its prompt and model, reuse the candidate's cases and assertions; missing counterpart returns an error naming the suite
- [X] T021 [US1] Implement the runner in `internal/engine/engine.go`: fan out (suite, case, variant, run index) jobs with `errgroup` across all suites, acquire the provider limiter around each model call, build each request, score through `assertions`, collect `Run` records, then aggregate into `Side` values and call `gating`; populate `CaseResult.Critical` from `Case.IsCritical()`; accept an `Observer` interface (passed via the constructor, no globals) with `CallFinished(suite, case, variant, run, provider, model, duration, err)` so reporting stays outside the engine
- [X] T022 [US1] Implement the stdout table writer in `internal/reporters/table.go` per `contracts/cli.md` (columns SUITE, CASE, BASELINE, CANDIDATE, DROP, STATUS, then `Decision:` line) and exit-code mapping in `internal/reporters/exit.go`
- [X] T023 [US1] Wire the `run` command in `cmd/evalci/run.go`: load settings and suites, resolve baseline, build the provider registry (Ollama only for now), run the engine, print the table, return the mapped exit code
- [X] T024 [US1] Create the runnable example in `examples/basic/`: `.evalci.yaml` (Ollama), `evals/support.eval.yaml` with exact-match cases, `prompts/support.txt`, and a `baseline/` directory with its own `evals/` and `prompts/` (baseline and candidate prompts identical and `temperature: 0`, so the example is deterministic and PASSes; also ship `prompts/support-worse.txt` and a README line explaining that pointing the suite's `prompt:` at it demonstrates a FAIL, per quickstart Scenario 2)

**Checkpoint**: US1 works end to end and is demonstrable on its own (MVP)

---

## Phase 4: User Story 2 - Trust the gate only when the signal is real (Priority: P1)

**Goal**: Noise degrades to UNRELIABLE/WARN, never FAIL; temperature 0 runs once; failed runs are handled honestly; unevaluable cases become ERROR with exit code 2.

**Independent Test**: A case with a wide spread and a large apparent drop is UNRELIABLE and the suite is WARN (exit 0); a case whose calls all fail is ERROR and the run exits 2 (quickstart Scenarios 3-4).

### Tests for User Story 2

- [X] T025 [P] [US2] Extend `internal/gating/gate_test.go` with table-driven cases for the ordered rules in data-model.md: ERROR when either side has no successful run (baseline all failed, candidate all failed, or both); UNRELIABLE when `max - min > max_spread_before_unreliable` on either side regardless of averages; UNRELIABLE when fewer than half of planned runs succeeded on either side; UNRELIABLE wins over REGRESSED; single-run side has spread 0; repeated evaluation of identical inputs always yields identical output (SC-006); decision precedence: a critical REGRESSED case plus an ERROR case gives `FAIL`; ERROR alone gives `ERROR`; a non-critical REGRESSED plus ERROR gives `ERROR`; any UNRELIABLE with no REGRESSED or ERROR gives `WARN`
- [X] T026 [P] [US2] Engine tests in `internal/engine/engine_test.go`: `temperature: 0` makes exactly one fake call per case per variant (SC-004); failed runs are excluded from averages and counted in `FailedRuns`; all-failed case yields ERROR
- [X] T027 [P] [US2] Reporter tests in `internal/reporters/table_test.go`: exit code 2 for decision `ERROR`; 0 for `WARN`; the stderr observer always prints failure reasons (suite, case, variant, run, message) and prints one per-call line only when verbose

### Implementation for User Story 2

- [X] T028 [US2] Extend `Classify` in `internal/gating/gate.go` with the ordered rules from data-model.md (ERROR when either side has no successful run, then UNRELIABLE by spread or by under-half successful runs, then REGRESSED, then PASS) and `Decide` in the order FAIL (critical REGRESSED), then ERROR, then WARN, then PASS; keep functions pure and deterministic
- [X] T029 [US2] In `internal/engine/engine.go` make planned runs per variant `1` when the model `temperature` is `0`, otherwise `runs_per_case`; record failed runs instead of aborting the case; a context-cancel aborts the whole run; read the temperature through the `*float64` (never default a missing value to 0)
- [X] T030 [US2] Extend `internal/reporters/exit.go` and `internal/reporters/table.go` for ERROR and WARN: show `ERROR` rows and the failed-run note (`!N`) beside averages; decision line `Decision: WARN (...)`; map `ERROR` to exit 2; implement the stderr `Observer` in `internal/reporters/stderr.go` (failure reasons always, per-call lines when `--verbose`) and pass it from `cmd/evalci/run.go`
- [X] T031 [US2] Create a separate example `examples/noisy/` (own `.evalci.yaml`, `evals/noisy.eval.yaml` with high temperature and exact-match cases only since rubric judging arrives in US3, its prompt, and a `baseline/` counterpart), kept apart from `examples/basic/` so that example stays a deterministic PASS; the example demonstrates UNRELIABLE and WARN (quickstart Scenario 3)

**Checkpoint**: US1 and US2 together deliver the full gating rule set

---

## Phase 5: User Story 3 - Define suites and assertions in files (Priority: P2)

**Goal**: All three assertion types and all three providers work; prompt rendering and pre-flight validation catch mistakes before any model call.

**Independent Test**: A suite using each assertion type scores 0-100 per run; invalid suites fail with a message naming the file and field; no model calls occur on invalid input (quickstart Scenario 5).

### Tests for User Story 3

- [X] T032 [P] [US3] Tests for semantic-similarity in `internal/assertions/semantic_test.go`: cosine similarity clamped to [0,1] times 100 using fake embeddings; identical vectors score 100, orthogonal 0
- [X] T033 [P] [US3] Tests for llm-rubric in `internal/assertions/rubric_test.go`: judge reply `{"score": 0-100}` accepted; non-JSON or out-of-range reply is a failed run (error), not a zero
- [X] T034 [P] [US3] Table-driven tests for prompt rendering in `internal/engine/render_test.go`: no placeholder gives system message plus user message; `{{input}}` gives a single user message; `\{{` renders literal `{{`; unknown or misspelled `{{imput}}` is an error naming file and line; substitution is single pass (input containing `{{input}}` is not re-expanded)
- [X] T035 [P] [US3] Tests for pre-flight validation in `internal/engine/preflight_test.go`: missing prompt file, unconfigured provider, missing credentials env var for a used provider only, embedding provider whose `SupportsEmbeddings()` is false rejected (use the fake provider configured without embeddings), missing baseline counterpart, duplicate suite `name`, duplicate case `id` within a suite, zero cases, zero assertions per case
- [X] T036 [P] [US3] `httptest`-based adapter tests in `internal/providers/openai/openai_test.go` and `internal/providers/anthropic/anthropic_test.go`: request shape, auth header from the named env var, 429 and 5xx retried, Anthropic `Embed` returns `ErrEmbeddingsUnsupported`

### Implementation for User Story 3

- [X] T037 [P] [US3] Implement semantic-similarity in `internal/assertions/semantic.go` using the assertion's own `embedding` provider and model (no default, FR-002a)
- [X] T038 [P] [US3] Implement llm-rubric in `internal/assertions/rubric.go` using the assertion's own `judge` provider and model; strict JSON reply parsing
- [X] T039 [P] [US3] Implement the OpenAI adapter in `internal/providers/openai/openai.go` (`net/http`, key read from the env var named by `api_key_env`)
- [X] T040 [P] [US3] Implement the Anthropic adapter in `internal/providers/anthropic/anthropic.go` (`net/http`, key from `api_key_env`; `SupportsEmbeddings()` returns false and `Embed` returns `ErrEmbeddingsUnsupported`)
- [X] T041 [US3] Implement prompt rendering in `internal/engine/render.go` per the Prompt rendering rules in data-model.md, applied identically to baseline and candidate
- [X] T042 [US3] Implement pre-flight validation in `internal/engine/preflight.go` covering the list in `contracts/cli.md` (runs before any model call); wire it into `cmd/evalci/run.go` and extend the provider registry there to select by the suite's `model.provider`
- [X] T043 [US3] Create `examples/providers/` (own `.evalci.yaml`, baseline dir, semantic-similarity and llm-rubric cases, and OpenAI and Anthropic suite variants) and add a rubric-judged case to `examples/noisy/`; `examples/basic/` stays offline and deterministic (documented in the example README as requiring the matching API key env vars)

**Checkpoint**: Full authoring surface works across all three providers

---

## Phase 6: User Story 4 - Read results as a person and as a machine (Priority: P2)

**Goal**: A JSON result file matching the published schema, consistent with stdout and the exit status.

**Independent Test**: The JSON validates against `contracts/result.schema.json` and its `decision` and `exit_code` match the printed decision and process status (quickstart Scenario 7).

### Tests for User Story 4

- [X] T044 [P] [US4] Tests for the JSON reporter in `internal/reporters/json_test.go`: `schema_version` 1, cases sorted by suite then case id, `settings` echoed, `drop` and `average` null for ERROR cases, output validates against `specs/001-mvp-eval-gate/contracts/result.schema.json`
- [X] T045 [P] [US4] Test in `cmd/evalci/run_test.go` that stdout decision, JSON `decision`, JSON `exit_code`, and the returned exit status always agree across PASS, WARN, FAIL, and ERROR fixtures (SC-007), and that a run leaves no file in the working directory other than the `--output` JSON (FR-018: no stored history)

### Implementation for User Story 4

- [X] T046 [US4] Implement the JSON writer in `internal/reporters/json.go` (versioned document, deterministic ordering) and write it to `--output` in `cmd/evalci/run.go` even when the decision is FAIL or ERROR
- [X] T047 [US4] Keep stdout clean for machines: in `cmd/evalci/run.go` send diagnostics and progress to stderr only; table and `Decision:` line only on stdout (per `contracts/cli.md`)

**Checkpoint**: Output is consumable by people and pipelines

---

## Phase 7: User Story 5 - Mark a case as non-blocking (Priority: P3)

**Goal**: `critical: false` keeps a regression visible without failing the suite.

**Independent Test**: A regressed `critical: false` case is shown as REGRESSED `(non-critical)` and the suite is not FAIL on its account; a regressed case with no setting still fails (quickstart Scenario 6).

### Tests for User Story 5

- [X] T048 [P] [US5] Extend `internal/gating/gate_test.go`: REGRESSED non-critical only gives PASS (or WARN if an UNRELIABLE case exists); REGRESSED with `critical` unset gives FAIL; UNRELIABLE non-critical still yields WARN
- [X] T049 [P] [US5] Table reporter golden test in `internal/reporters/table_test.go` for the `(non-critical)` suffix

### Implementation for User Story 5

- [X] T050 [US5] Add an engine integration test in `internal/engine/engine_test.go` proving `critical: false` in a suite YAML reaches `CaseResult.Critical == false` and an absent key yields `true`; fix any gap found in the `Case.IsCritical()` plumbing from T004/T021
- [X] T051 [US5] Add the `(non-critical)` status suffix in `internal/reporters/table.go`; add a non-critical case to `examples/basic/evals/support.eval.yaml`

**Checkpoint**: All five user stories complete

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T052 [P] Verify the static-binary rule (Constitution I): `CGO_ENABLED=0 go build -o evalci ./cmd/evalci` succeeds and `go list -deps ./... ` shows no cgo packages; document the build command in `examples/basic/README.md`
- [X] T053 [P] Enforce coverage: run `go test ./internal/gating ./internal/engine -race -cover` and add tests until both exceed 80%
- [X] T054 [P] Add an import-boundary test in `internal/engine/boundary_test.go` asserting `engine`, `assertions`, and `gating` do not import `providers/openai`, `providers/anthropic`, or `providers/ollama`, and that none of those three packages' source contains the string literals `"openai"`, `"anthropic"`, or `"ollama"` (no branching on a vendor name; Constitution IV)
- [X] T055 Run `go build ./...`, `go vet ./...`, `go test ./... -race`, and `golangci-lint run`; fix findings
- [X] T056 Walk through all seven scenarios in `specs/001-mvp-eval-gate/quickstart.md` against `examples/basic` and correct the example or quickstart where they disagree
- [X] T057 Update `claude.md` Status section to reflect the implemented packages and remove the "no code yet" note

---

## Dependencies & Execution Order

### Phase Dependencies

- Setup (1) -> Foundational (2) -> user stories (3-7) -> Polish (8).
- Foundational blocks every story.

### User Story Dependencies

- **US1 (P1)**: starts after Foundational; no other story dependency. MVP.
- **US2 (P1)**: extends gating, engine, and reporters from US1, so start after US1's T016, T021, T022.
- **US3 (P2)**: needs US1's engine and assertion interface (T017, T021); independent of US2 except shared files `engine.go` and `gate_test.go` (sequence edits).
- **US4 (P2)**: needs US1's reporters and run command; US2 for ERROR rows in the JSON.
- **US5 (P3)**: needs US1's gating and engine; independent of US3 and US4.

### Within Each Story

Tests first (confirm failing), then implementation; pure packages (`gating`, `assertions`) before the engine; engine before the CLI wiring.

### Parallel Opportunities

- Foundational: T006-T010 in parallel after T004-T005 start.
- US1: T012-T015 together; then T017, T018, T019 in parallel; T016 is independent of them.
- US3: T032-T036 together; T037-T040 in parallel (four different files).
- US4 and US5 tests (T044, T045, T048, T049) in parallel once US1 is complete.
- Polish: T052-T054 in parallel.

### Parallel Example: User Story 3

```text
Launch together:
  T037 internal/assertions/semantic.go
  T038 internal/assertions/rubric.go
  T039 internal/providers/openai/openai.go
  T040 internal/providers/anthropic/anthropic.go
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phases 1-2, then Phase 3 (US1).
2. Stop and validate with quickstart Scenarios 1-2 on the Ollama example.

### Incremental Delivery

1. Add US2 (honest gating) - together with US1 this is the shippable gate.
2. Add US3 (full authoring and providers), then US4 (JSON output), then US5 (non-critical).
3. Polish and the final quickstart walkthrough.

### Notes

- A phase is done only when `go build ./...`, `go vet ./...`, `go test ./... -race`, and
  golangci-lint are clean and `examples/basic` demonstrates it (constitution, Quality Gates).
- Do not add storage, a dashboard, diff-aware scoping, judge calibration, or formal
  statistical tests; they are out of scope for this phase (constitution Principle V).
