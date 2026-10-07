# Research: Diff-Aware Suite Scoping and Run History

All Technical Context unknowns are resolved below.

## R1. Reading git history

- **Decision**: Use `go-git/v5` (pure Go) behind a small `diffscope.Repository` interface
  (resolve a ref to a commit, merge base, changed files with renames, read a file at a commit,
  current branch and checked-out commit, dirty check). Only one file imports go-git.
- **Rationale**: Constitution Principle I forbids requiring anything installed alongside the
  binary; shelling out to `git` makes the tool depend on it. go-git provides merge-base, tree
  diff with rename detection, and blob reads at a commit, which is everything needed. The
  interface keeps the selection logic pure and lets tests use real repos built in temp dirs.
- **Alternatives**: Shell out to `git` (simplest, battle-tested, but a runtime dependency and
  harder to test offline); libgit2 (CGo, rejected).
- **Edge handling**: shallow clones surface as "no common ancestor found"; the message says to
  fetch more history. Worktree status is used only for a best-effort dirty warning.

## R2. SQLite driver

- **Decision**: `modernc.org/sqlite` (pure Go), already named in `claude.md`. WAL mode,
  `busy_timeout` of 5 s, one open connection per process, every save inside a single
  transaction.
- **Rationale**: Keeps the binary CGo-free (Principle I). A single transaction gives
  all-or-nothing saves (FR-014). WAL plus busy timeout lets concurrent CI jobs write without
  corruption (FR-018, SC-010).
- **Alternatives**: `mattn/go-sqlite3` (CGo); bbolt or a JSON file (cannot be queried
  directly by users, no multi-process safety story, harder to swap behind the same schema).

## R3. Schema versioning and upgrades

- **Decision**: Version stored in SQLite's `user_version`. On open: read the version without
  writing (open read-only first when the file exists). If it is greater than the binary's
  version, return `ErrNewerSchema` and leave the file untouched. If lower, apply embedded
  forward-only migrations in one transaction. A new file is created at the current version.
- **Rationale**: Meets FR-018 (upgrade without losing records, never touch a newer file) with no
  migration library.

## R4. The `storage.Store` interface

- **Decision**: Domain types (`RunRecord`, `CaseRecord`, `SuiteDecision`, `UsageRecord`) live in
  package `storage` so callers never import a backend. Interface:
  `SaveRun`, `GetRun`, `ListRunsByCommit`, `ListRecentRuns`, `Close`. A factory
  `factory.Open(ctx, storage.Config)` in package `storage/factory` is the only place that
  branches on the driver; `postgres` returns `ErrBackendUnavailable`. It is a separate package
  because a factory inside `storage` would import the SQLite backend, which imports `storage`
  for the record types (an import cycle). A shared `storagetest` contract suite runs against SQLite and an
  in-memory implementation.
- **Rationale**: Satisfies FR-023 to FR-025 and SC-007 directly: a second implementation
  passing the same suite proves nothing in callers must change. `claude.md` already requires
  driver branching to stay inside this package.
- **Alternatives**: An ORM (heavy, hides SQL users want to query); exposing `*sql.DB` (leaks the
  backend, defeats the abstraction).

## R5. Records contain no sensitive text

- **Decision**: Record types have no field for responses, prompts, or case inputs; case rows are
  keyed by suite name and case id. A test uses reflection over the record types to forbid
  string fields named like content.
- **Rationale**: FR-013a (clarified): privacy and file size. Adding opt-in content later is an
  additive schema change.

## R6. Token usage through the provider interface

- **Decision**: `providers.Response` gains `Usage{InputTokens, OutputTokens int; Known bool}`;
  `Embed` returns `Embedding{Vector []float32; Usage Usage}`. Adapters map each API's usage
  fields (Ollama `prompt_eval_count`/`eval_count`; OpenAI `usage.prompt_tokens`/
  `completion_tokens`; Anthropic `usage.input_tokens`/`output_tokens`). A missing field gives
  `Known: false`. The engine's existing `limited` wrapper (which already sees every generation,
  judge, and embedding call) feeds a per-run `UsageMeter` keyed by `(provider, model)`.
- **Rationale**: One interception point covers all call kinds (FR-020) with no extra model
  calls. This changes the phase-1 provider interface; the adapters, the fake, and the semantic
  assertion are updated accordingly.
- **Alternatives**: Counting tokens locally (inaccurate, needs per-vendor tokenizers); parsing
  usage in the engine (would require vendor knowledge).

## R7. Cost estimation

- **Decision**: `.evalci.yaml` gains `prices` keyed by `"<provider>/<model>"` with
  `input_per_million` and `output_per_million` (decimals), and an optional `currency` label
  (default `USD`). Cost is computed with integer micro-units
  (`tokens * price_micro_per_million / 1_000_000`, rounded per model), stored as `int64`.
  No price or unknown tokens means cost is unknown (`NULL`), never 0; tokens are still stored.
  EvalCI ships no price list.
- **Rationale**: Prices change; a bundled list goes stale and misleads. Integer micro-units
  avoid float drift so SC-009 ("to the cent") holds.
- **Alternatives**: Built-in price table (stale); float arithmetic (rounding drift).

## R8. Selecting affected suites

- **Decision**: Pure function over the change set. Per candidate suite, in order: full run
  requested -> selected ("full run requested"); suite file added -> selected as new; suite file
  changed/renamed -> selected ("suite file changed"); candidate or baseline prompt path in the
  change set (renames count under old and new) -> selected ("prompt file changed"); run-affecting
  settings changed -> selected ("settings changed"); otherwise skipped ("no related changes").
  Suites present at base but deleted at head are reported as removed.
- **Run-affecting settings**: `runs_per_case`, `min_drop_threshold`,
  `max_spread_before_unreliable`, and provider connection fields (`base_url`, `api_key_env`,
  `max_concurrent`, `requests_per_minute`). Detected by parsing the settings at base and at head
  and comparing those fields, so price or storage edits select nothing (spec FR-003).
- **Rationale**: Suite-level only, per spec; no dependency graph (Principle V). Comparing parsed
  values avoids false triggers from comments or key order.

## R9. Baseline from the base ref

- **Decision**: Export the base ref's `evals/*.eval.yaml` to a temp directory, parse them with
  the existing loader, then export each referenced prompt file. The temp dir is passed to the
  existing `ResolveBaseline`, so no baseline logic changes. The temp dir is removed when the run
  ends. `--base` and `--baseline-dir` are mutually exclusive; scoping applies only with `--base`
  (a `--baseline-dir` run runs everything, as in phase 1).
- **Rationale**: Reuses phase-1 code and tests unchanged. Reading blobs from the object database
  avoids touching the working tree.

## R9a. Baseline is read at the merge base

- **Decision**: The baseline suites, prompts, and settings are read at the merge base of the base
  and head, not at the base branch's tip. The change set is the same range (merge base to head).
- **Rationale**: Reading the tip would treat work that landed on the base branch after the pull
  request branched as differences made by the pull request (a suite added to `main` would look
  "removed", a changed prompt would look like a regression). The merge base is also what a
  pull request's own diff shows.
- **Alternatives**: Base tip (simpler to explain, wrong results on a moving base branch).

## R10. Suites with no baseline (new)

- **Decision**: A suite present at head but not at base runs candidate-only. Its cases get
  status `NEW`, which never causes FAIL and does not trigger WARN. If every candidate run fails,
  the case is `ERROR` as before.
- **Rationale**: Spec FR-009 and FR-011. There is nothing to regress from; candidate scores are
  still recorded for the future.

## R11. Which branch is being tested

- **Decision**: `--head` (default `HEAD`) must resolve to the currently checked-out commit,
  otherwise exit 2 before any model call. The recorded branch name is the symbolic branch if
  attached, else the `--head` value if it was not a raw SHA, else empty.
- **Rationale**: Spec clarification: candidate suites are read from the working directory, so
  selection and evaluation must describe the same code. Detached HEAD (common in CI) is handled.

## R12. Failure semantics for saving

- **Decision**: Backend selection errors (unknown or unavailable driver) are configuration
  errors: exit 2 before any model call. Failure to open (locked, read-only, newer schema) or to
  save is a warning on stderr; the decision, table, JSON, and exit status are unchanged. A
  cancelled run saves nothing because saving happens only after final results exist.
- **Rationale**: Spec FR-014, FR-016, FR-025, SC-008 and Principle II (non-quality problems never
  change the gate).

## R13. Run identity and time

- **Decision**: Run id is `YYYYMMDDTHHMMSSZ-<8 hex chars>` (UTC, random suffix from
  `crypto/rand`); the clock is injected so tests are deterministic. Timestamps are stored as UTC
  RFC 3339 text.
- **Rationale**: Sortable, collision-safe for concurrent runs, no dependency.

## R14. JSON result schema

- **Decision**: Bump `schema_version` to 2 with additive fields (`run_id`, `scope`, `cost`) and
  one new case status, `NEW`. Phase-1 consumers that ignore unknown fields keep working; the
  version bump signals the new status value.
- **Rationale**: The status enum widening is the only potentially breaking change for strict
  consumers, so it is versioned explicitly.

## R15. Run from the repository root; resolve symlinks

- **Decision**: With `--base`, evalci refuses to run unless the working directory is the
  repository root (exit 2, naming the root). `--evals` must be a relative directory inside the
  repository. Paths are compared after resolving symlinks on both sides.
- **Rationale**: Changed paths from git are repository-relative; suite and prompt paths come from
  the working directory. Any mismatch would silently skip suites, which is the worst failure for
  a gate. A real bug of this kind (a symlinked working directory such as `/var` -> `/private/var`
  made a settings change look "outside the repository") was found by the example script and now
  has a regression test.
- **Alternatives**: Computing a prefix to support subdirectories (more code, more ways to get
  paths wrong); not checking (silent mis-scoping).

## R16. Unparseable settings at the base

- **Decision**: If `.evalci.yaml` at the merge base cannot be parsed under the current strict
  schema, treat run-affecting settings as changed (select every suite) and warn on stderr.
- **Rationale**: Failing would block every pull request until someone fixed an old file; guessing
  "unchanged" could skip suites. Running more is the safe, honest fallback.
