<!--
Sync Impact Report (temporary; remove before commit)
- Version change: (blank template) -> 1.0.0
- Modified principles: none (initial ratification)
- Added principles: I. Single Static Binary; II. Statistical Honesty; III. Cost-Consciousness;
  IV. Vendor Neutrality; V. Simplicity First
- Added sections: Technical Constraints; Development Workflow & Quality Gates; Governance
- Removed sections: none
- Deferred TODOs: none
-->
# EvalCI Constitution

## Core Principles

### I. Single Static Binary, Zero Runtime Dependencies
EvalCI MUST ship as one static binary. End users MUST NOT need an install step for any
language runtime, package manager, shared library, or sidecar service (no npm/pip
equivalent). CGo MUST NOT be used unless a dependency strictly requires it and the
exception is recorded in the plan's complexity tracking. The dashboard, templates, and
any other assets MUST be embedded in the binary.

Rationale: EvalCI runs inside other people's CI. Anything the user must install first is
friction and a failure mode.

### II. Statistical Honesty
A build MUST NOT fail on a statistically unreliable signal. Whenever confidence is low
(for example a case's run-to-run spread exceeds the configured unreliable threshold, or a
drop does not exceed the configured minimum), the result MUST degrade to WARN or be
marked "unreliable". It MUST NOT become a FAIL. A FAIL is permitted only for a
regression that is both large enough and consistent enough to be real. Gate logic MUST
be deterministic given the same scores and configuration.

Rationale: a gate that cries wolf gets disabled. Trust in a FAIL is the product.

### III. Cost-Consciousness
EvalCI MUST NOT make more LLM API calls than necessary. Repeated sampling MUST be
skipped at temperature 0 (one run per side). Only eval suites affected by a PR's diff
MUST be re-tested; running the full set is an explicit opt-in. Features that add model
calls MUST state their worst-case call count in the spec or plan.

Rationale: every eval call costs money and CI minutes; wasted calls make the gate too
expensive to run on every pull request.

### IV. Vendor Neutrality
No LLM provider MAY be a built-in default. OpenAI, Anthropic, and Ollama MUST have
equal-footing support through one provider adapter interface, and core packages
(engine, assertions, gating) MUST NOT import or branch on a specific provider. A new
provider MUST be addable by implementing the interface alone.

Rationale: users choose their provider; the gate must not favor or depend on any one.

### V. Simplicity First
The simplest correct version of a feature MUST ship before any more sophisticated one.
Added complexity, such as formal statistical tests (bootstrap confidence intervals,
Wilcoxon signed-rank, Benjamini-Hochberg), MUST NOT be introduced ahead of a phase that
explicitly requests it. The current gating design (averaged scores, minimum-drop
threshold, spread-based "unreliable" marking) is intentional, not a placeholder.

Rationale: simple behavior is explainable to users and cheap to verify; sophistication
is added only when a phase justifies it.

## Technical Constraints

- Language and toolchain: Go 1.23+.
- Storage MUST sit behind a single `storage.Store` interface (SQLite pure-Go for solo
  mode, Postgres for shared mode); driver-specific branching MUST NOT occur outside the
  storage package.
- Configuration (`.evalci.yaml`, `*.eval.yaml`) MUST be validated strictly: unknown keys
  are rejected with an error naming the offending field.
- `context.Context` MUST be threaded through every provider call; no global mutable
  state; errors MUST be wrapped with `%w` and never swallowed.
- Out of scope until explicitly phased: web dashboard, RAG-specific assertions,
  multi-tenant or auth features.

## Development Workflow & Quality Gates

- Every spec, plan, and implementation MUST be checked against the five Core Principles
  before acceptance; plans MUST include a Constitution Check and justify any violation
  in complexity tracking.
- A phase is done only when `go build ./...`, `go vet ./...`, `go test ./... -race`, and
  golangci-lint are clean, and a working example in `examples/` demonstrates it
  end-to-end.
- Tests are table-driven; `internal/gating` and `internal/engine` MUST keep coverage
  above 80%.

## Governance

This constitution supersedes other practices. Amendments require a written change to
this file with rationale, a version bump, and review of dependent specs and plans for
conflicts. Versioning follows semantic versioning: MAJOR for removing or redefining a
principle, MINOR for adding a principle or materially expanding guidance, PATCH for
clarifications. All specs, plans, and reviews MUST verify compliance; unjustified
complexity MUST be rejected. Runtime development guidance lives in `claude.md`.

**Version**: 1.0.0 | **Ratified**: 2026-10-01 | **Last Amended**: 2026-10-01
