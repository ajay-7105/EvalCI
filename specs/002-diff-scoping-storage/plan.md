# Implementation Plan: Diff-Aware Suite Scoping and Run History

**Branch**: `002-diff-scoping-storage` | **Date**: 2026-10-01 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/002-diff-scoping-storage/spec.md`

## Summary

Extend `evalci run` so that, given `--base <ref>`, it (1) computes the files changed between the
common ancestor of base and head and the checked-out head, (2) selects only the suites those
changes affect (suite file, referenced prompt, or run-affecting settings), (3) materializes the
base branch's suites and prompts into a temporary directory so the existing baseline machinery
works unchanged, and (4) runs only the selected suites. After the run, one complete record
(scores, per-case deltas, decision, commit, timestamp, scope decisions, token usage and
estimated cost) is saved through a `storage.Store` interface, with SQLite as the first backend.
Provider calls now report token usage so cost can be estimated from user-supplied prices.
Decisions are in [research.md](research.md).

## Technical Context

**Language/Version**: Go 1.26 (module's current toolchain; constitution minimum 1.23)

**Primary Dependencies**: existing (cobra, yaml.v3, x/sync, x/time) plus
`github.com/go-git/go-git/v5` (pure-Go git access) and `modernc.org/sqlite` (pure-Go SQLite).
No CGo, no `git` binary, no `sqlite3` library required at runtime.

**Storage**: SQLite single file behind `storage.Store`; default path `.evalci/evalci.db`;
opt-in via `storage.driver` in `.evalci.yaml`. `postgres` is a recognized but unavailable
driver in this phase.

**Testing**: standard `testing`, table-driven, `-race`. Git behavior is tested against real
repositories built in temp directories with go-git (no network); storage behavior by one shared
contract suite run against SQLite and an in-memory implementation; the provider usage path by
`httptest`. The mock Ollama server from `examples/mock` backs the quickstart.

**Target Platform**: macOS and Linux, static binary, `CGO_ENABLED=0`.

**Project Type**: CLI

**Performance Goals**: scope resolution < 2 s for 1,000 changed files (SC-005); saving a run
< 1 s for 500 cases (SC-006).

**Constraints**: zero runtime dependencies (no git or database install); unselected suites make
0 model calls (SC-001); a storage failure never changes the decision or exit status (SC-008);
records contain no responses, prompts, or case inputs (FR-013a).

**Scale/Scope**: tens of suites, up to hundreds of cases per run, thousands of stored runs per
history file.

**Worst-case model calls (Constitution III)**: unchanged per selected suite from phase 1
(cases x 2 variants x R generations, plus judge and embedding calls). Unselected suites: 0. New
suites (no baseline) run the candidate variant only: cases x R generations. Usage tracking adds
no calls.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Basis |
|-----------|--------|-------|
| I. Single static binary | Pass | go-git and modernc SQLite are pure Go; no `git` binary, no CGo; `go list` check retained |
| II. Statistical honesty | Pass | Storage failures and missing prices never alter the decision; NEW suites never FAIL; unknown cost is shown as unknown, not zero |
| III. Cost-consciousness | Pass | The feature's purpose: unselected suites make 0 calls; usage and cost are recorded and shown; no extra model calls |
| IV. Vendor neutrality | Pass | Usage is reported through the provider interface; prices are keyed by configured `provider/model` strings; core packages still name no vendor |
| V. Simplicity first | Pass | Suite-level scoping from changed files only (no dependency graph, no case-level scoping); one table family; integer micro-units for cost; no retention, history command, or dashboard |

Constitution Technical Constraints: storage sits behind a single `storage.Store` interface and
driver branching happens only inside the `storage` package.

Post-design re-check (after Phase 1): still Pass. No Complexity Tracking entries needed.

## Project Structure

### Documentation (this feature)

```text
specs/002-diff-scoping-storage/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── cli.md
│   ├── config-additions.md
│   ├── store.md
│   ├── sqlite-schema.md
│   └── result.schema.json
└── tasks.md             # created later by /speckit-tasks
```

### Source Code (repository root)

```text
cmd/evalci/
  run.go                 # orchestration: scope -> baseline -> run -> report -> save
configs/                 # + prices, currency, storage.path; ParseSettings from bytes
internal/
  diffscope/             # NEW: Repository interface, go-git implementation, change set,
                         #      suite selection (pure), base-ref export to a temp dir
  storage/               # NEW: Store interface and record types (imports no backend)
    factory/             #      Open(driver): the only place that branches on the driver
    sqlite/              #      modernc SQLite implementation, migrations
    memory/              #      in-memory implementation (tests, contract proof)
    storagetest/         #      shared contract suite run against every implementation
  cost/                  # NEW: price table, integer micro-unit cost computation
  providers/             # + Usage on Response; Embed returns Embedding{Vector, Usage}
  engine/                # + usage meter in the limited wrapper; candidate-only (new) suites
  gating/                # + StatusNew
  assertions/            # semantic uses the new Embed result
  reporters/             # + scope block, cost line, JSON schema v2
examples/
  scoping/               # NEW: runnable example (git repo built by a script, mock server)
```

**Structure Decision**: Follow the `claude.md` layout, adding the `diffscope` and `storage`
packages it already names, plus a small `cost` package. `diffscope`'s selection logic is pure
over a `Repository` interface so it is table-testable without git; the go-git implementation is
the only file that imports go-git. Likewise only `storage/sqlite` imports SQLite, and callers
depend solely on `storage.Store`. The driver factory is a separate package (`storage/factory`)
because a factory inside `storage` would import `sqlite`, which imports `storage` for the record
types: an import cycle.

## Complexity Tracking

No constitution violations; no entries. (The two new third-party dependencies are pure Go and
justified in research R1 and R2; both replace external binaries that Principle I forbids
requiring.)
