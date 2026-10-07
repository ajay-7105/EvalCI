# Implementation Plan: MVP Eval Gate

**Branch**: `001-mvp-eval-gate` | **Date**: 2026-10-01 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/001-mvp-eval-gate/spec.md`

## Summary

A standalone `evalci run` command loads `.evalci.yaml` and `evals/*.eval.yaml`, runs every
case against a candidate (the suites as written) and a baseline (an alternate directory
holding the baseline prompt/model), scores each run 0-100 through its assertions, gates each
case with the simple rule (average drop above threshold => REGRESSED, spread above limit =>
UNRELIABLE), and emits a table, a JSON file, and an exit code. The approach follows the
constitution: one static Go binary, simple gating, minimal model calls, three providers
behind one adapter interface. Decisions are in [research.md](research.md).

## Technical Context

**Language/Version**: Go 1.23+

**Primary Dependencies**: cobra (CLI), gopkg.in/yaml.v3 (strict parsing), golang.org/x/sync
(errgroup) and golang.org/x/time/rate (per-provider pacing). Provider adapters use the
standard library `net/http`; no vendor SDKs.

**Storage**: N/A this phase. The `storage` driver setting is parsed and validated only;
nothing is persisted. Results go to a JSON file.

**Testing**: standard `testing`, table-driven, `-race`; fake provider and `httptest` servers
for adapters; no network in tests.

**Target Platform**: macOS and Linux (amd64, arm64), static binary, CGo disabled.

**Project Type**: CLI

**Performance Goals**: Orchestration overhead negligible against model latency; a suite's
wall time is bounded by provider limits, not by EvalCI.

**Constraints**: Zero runtime dependencies; worst-case model calls stated below; gate
decision deterministic for identical scores and settings.

**Scale/Scope**: Tens of suites, hundreds of cases per run.

**Worst-case model calls (Constitution III)**: per suite, cases x 2 variants x R generation
calls (R = `runs_per_case`, or 1 at temperature 0), plus per generated output one judge call
per llm-rubric assertion and one embedding call per semantic-similarity assertion. Each
call is retried at most twice on transient errors.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Basis |
|-----------|--------|-------|
| I. Single static binary | Pass | Go, `CGO_ENABLED=0`, stdlib HTTP clients, no SDKs, no runtime installs |
| II. Statistical honesty | Pass | UNRELIABLE checked before REGRESSED; infrastructure errors get a separate exit code, never FAIL |
| III. Cost-consciousness | Pass | One run per variant at temperature 0 (temperature is required, so unambiguous); worst-case calls stated above |
| IV. Vendor neutrality | Pass | No default provider or model; engine/assertions/gating import only the `providers.Provider` interface |
| V. Simplicity first | Pass | Averages, threshold, spread only; no formal tests; no storage, diff-scoping, or calibration |

Post-design re-check (after Phase 1): still Pass. No entries needed in Complexity Tracking.

## Project Structure

### Documentation (this feature)

```text
specs/001-mvp-eval-gate/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── cli.md
│   ├── eval-suite.schema.md
│   ├── evalci-config.schema.md
│   └── result.schema.json
└── tasks.md             # created later by /speckit-tasks
```

### Source Code (repository root)

```text
cmd/evalci/              # main: wires cobra root + `run` command
configs/                 # .evalci.yaml and *.eval.yaml parsing + strict validation
internal/
  engine/                # loads work, worker pool, per-provider limiter, run-N-times
  providers/             # Provider interface + openai, anthropic, ollama adapters, fake for tests
  assertions/            # exact-match, semantic-similarity, llm-rubric; score combiner
  gating/                # per-case status and suite decision (pure functions)
  reporters/             # stdout table, JSON file, exit-code mapping
examples/
  basic/                 # candidate evals/ + baseline dir, runnable end-to-end
go.mod
```

**Structure Decision**: Single Go module following the layout in `claude.md`, limited to the
packages this phase needs. `storage`, `dashboard`, `diffscope`, and `calibration` are not
created. `gating` is kept pure (no I/O, no providers) so its >80% coverage is cheap and its
behavior is deterministic.

## Complexity Tracking

No constitution violations; no entries.
