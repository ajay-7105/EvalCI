# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Status
Phase 1 (MVP eval gate, `specs/001-mvp-eval-gate/`) and phase 2 (diff-aware scoping and run history,
`specs/002-diff-scoping-storage/`) are implemented: `configs`, `cmd/evalci`, and
`internal/{engine,providers,assertions,gating,reporters,diffscope,storage,cost}`, with runnable examples in
`examples/`. Not built yet (later phases): `dashboard`, `calibration`, a Postgres backend. Spec Kit
artifacts live in `specs/` and `.specify/` (constitution in `.specify/memory/constitution.md`).

Non-obvious points:
- Two ways to give a baseline, mutually exclusive. `--base <ref>`: the baseline is the base branch as of
  the merge base (not its tip), and only suites affected by the change set run unless `--full`; run from the
  repository root. `--baseline-dir <dir>`: a repo-shaped directory supplies the baseline prompt and model and
  every suite runs. Either way the candidate is `evals/` in the cwd and cases/assertions always come from it.
- Scoping is suite-level: a suite is selected by its own file, a prompt it references, or run-affecting
  settings (`runs_per_case`, the two thresholds, provider connection fields; not prices/currency/storage).
  Unparseable base settings count as changed. Compare paths only after resolving symlinks (a real bug:
  `/var` vs `/private/var` silently hid a settings change).
- A suite with no baseline counterpart runs candidate-only and its cases are `NEW` (never FAIL/WARN).
- History is opt-in (`storage.driver`). Callers use only `storage.Store`; only `storage/factory` branches on the
  driver and only `storage/sqlite` imports the SQLite driver (`storage/boundary_test.go` enforces it).
  Records hold scores, deltas, decision, commit, timestamp, tokens, and cost; never responses, prompts, or
  case inputs. Open/save failures only warn; an unavailable backend (`postgres`) exits 2 before any model call.
- Cost: providers report token usage through `providers.Usage`; cost is estimated from user-supplied
  `prices` in integer micro-units. No price, or any call with unreported usage, means cost is `nil`/NULL
  (unknown), never zero. Phase 1's `Embed` now returns `providers.Embedding`.
- Exit codes: 0 PASS/WARN, 1 FAIL, 2 config error or an unevaluable case. A critical regression plus an
  ERROR case is FAIL.
- Config fields where absence differs from zero are pointers (`model.temperature`, `critical`,
  `case_sensitive`); keep them as pointers. Unknown record values are pointers too (`*float64`, `*int64`).
- Core packages (`engine`, `assertions`, `gating`, `cost`, `diffscope`, `storage`) must not name a vendor;
  `engine/boundary_test.go` and `storage/boundary_test.go` enforce this. Provider capabilities come from
  the adapter (`SupportsEmbeddings()`).
- Examples: `examples/mock/run.sh` and `examples/scoping/run.sh` run offline (mock Ollama server; scoping needs
  `git` only to build its demo repo). `examples/basic`, `examples/noisy`, `examples/providers/ollama` need a
  local Ollama server with `llama3.2:1b`; `examples/providers/hosted` needs API keys.

## Commands
- Build / vet: `go build ./...` and `go vet ./...` (static binary: `CGO_ENABLED=0 go build -o evalci ./cmd/evalci`)
- Test (all): `go test ./... -race` (add `-short` to skip the performance checks)
- Test (single): `go test ./internal/gating -race -run TestName`
- Coverage (must stay >80% on `internal/gating` and `internal/engine`):
  `go test ./internal/gating ./internal/engine -race -cover`
- Lint: `golangci-lint run`
- Offline end-to-end checks: `examples/mock/run.sh`, `examples/scoping/run.sh`

## Project
EvalCI — a Go-native, single-binary CI gate for LLM prompt/RAG pipeline quality.
Runs an eval suite on every pull request, compares a candidate (the PR's
prompt/model/retrieval config) against the last known-good baseline, and blocks
merges only on regressions large enough and consistent enough to be real.

## Why this exists
Prompt and RAG changes are usually treated as riskless text edits in CI. EvalCI
treats them as behavior changes, gated the same way code changes are: run tests,
compare against baseline, block on regression.

## Tech stack
- Go 1.23+, no CGo unless a dependency strictly requires it
- CLI: cobra + viper for commands/config
- Storage: SQLite (modernc.org/sqlite, pure Go) for solo mode; Postgres (pgx) for
  shared mode — both behind one `storage.Store` interface, never branch on driver
  outside that package
- Dashboard: embedded web UI via Go's `embed.FS`, plain templates — no separate
  frontend build step
- Concurrency: goroutine worker pool with a semaphore for per-provider rate limits
- Testing: standard `testing`, table-driven, `-race` always on in CI
- Lint: golangci-lint, strict default config

## Architecture
CLI -> Eval Engine -> Provider Adapters (OpenAI / Anthropic / Ollama, one interface)
Eval Engine -> Assertions -> Statistical Gating -> Gate Decision -> {Reporters, Storage}
Storage -> Dashboard

RAG mode (later phase) adds: Corpus Snapshot Manager, Indexer Adapter, Vector DB
Adapter, Retriever/Reranker Adapter — and splits gating into Layer 1 (retrieval,
deterministic, no LLM call) and Layer 2 (generation, LLM-judged).

## Repo layout
evalci/
  cmd/evalci/            # CLI entrypoint
  internal/
    engine/              # eval runner, worker pool, run-N-times logic
    providers/           # OpenAI, Anthropic, Ollama adapters (common interface)
    assertions/          # exact-match, semantic, llm-rubric, tool-call
    gating/              # average + threshold + spread check
    diffscope/           # git-diff-aware suite resolution
    calibration/         # judge calibration logic + storage
    storage/             # SQLite/Postgres backends behind one interface
    dashboard/           # embedded web UI
    reporters/           # GitHub PR comment, exit codes, JSON output
  configs/
    schema.go            # .evalci.yaml + *.eval.yaml parsing/validation

## Statistical gating — current approach, DO NOT "improve" without being asked
Ship the SIMPLE version deliberately: run each case 5 times per side (1 run if
temperature == 0), average the scores, flag a drop only if it exceeds
`min_drop_threshold`, and mark a case "unreliable" instead of pass/fail if either
side's spread exceeds `max_spread_before_unreliable`. Do NOT implement bootstrap
confidence intervals, Wilcoxon signed-rank tests, or Benjamini-Hochberg correction
unless a phase explicitly scopes it — the simple version is the intended design
for early phases, not a placeholder awaiting a "real" statistics implementation.

## Config files
- `.evalci.yaml` — repo-level settings (runs_per_case, thresholds, providers, storage)
- `evals/*.eval.yaml` — per-suite test case definitions
Validate strictly: reject unknown keys, fail with a message naming the bad field.

## Coding conventions
- Every exported symbol has a doc comment
- Errors wrapped with `fmt.Errorf("...: %w", err)`, never swallowed
- No global mutable state — pass dependencies explicitly via constructors
- Table-driven tests for every package; >80% coverage required on
  `internal/gating` and `internal/engine` specifically (trust-critical paths)
- No `panic` outside `main()` and genuine programmer errors
- `context.Context` threaded through every provider call for cancellation

## What NOT to build yet
- No web dashboard before its scoped phase
- No RAG-specific assertions until that phase is explicitly specified
- No multi-tenant / auth — this is a CLI invoked by CI, not a hosted service

## Definition of done, every phase
- `go build ./...` and `go vet ./...` clean
- `go test ./... -race` passes
- golangci-lint clean
- A working example in `examples/` demonstrating the phase end-to-end