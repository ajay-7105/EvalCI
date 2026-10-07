# Data Model: Diff-Aware Suite Scoping and Run History

New and changed entities. Phase-1 entities (Suite, Case, Assertion, Run, Settings) are as in
`specs/001-mvp-eval-gate/data-model.md` unless noted here.

## Settings additions (`.evalci.yaml`)

| Field | Type | Default | Rule |
|-------|------|---------|------|
| prices | map `"<provider>/<model>"` -> Price | none | key must be `provider/model` with a known provider |
| prices.<k>.input_per_million | decimal | required | `>= 0` |
| prices.<k>.output_per_million | decimal | required | `>= 0` |
| currency | string | `USD` | non-empty label, informational only |
| storage.driver | string | none (no persistence) | `sqlite` or `postgres`; `postgres` is rejected as unavailable |
| storage.path | string | `.evalci/evalci.db` | used by `sqlite`; parent directory is created |

Unknown keys are still rejected with file and field named.

## Change Set

| Field | Meaning |
|-------|---------|
| Base | resolved base commit (SHA) and the ref name as given. The baseline suites, prompts, and settings are read at the merge base, so changes that landed on the base branch after the pull request branched are neither regressions nor removals |
| Head | resolved head commit (SHA), the ref name as given, and the branch name if known |
| MergeBase | common ancestor SHA |
| Files | list of `{Status: added|modified|deleted|renamed, Path, OldPath (renames)}` |

Rules: a rename is reported under both `OldPath` and `Path` when matching suites and prompts.
Head must equal the checked-out commit.

## ScopeDecision

| Field | Meaning |
|-------|---------|
| Suite | suite name |
| File | suite file path (`evals/x.eval.yaml`) |
| Action | `run`, `skip`, or `removed` |
| Reason | `full run requested`, `new suite`, `suite file changed`, `prompt file changed`, `settings changed`, `no related changes`, `suite removed`, or `baseline directory given` (every suite runs when `--baseline-dir` is used) |
| Files | the changed paths that caused selection (may be empty) |

Selection order (first match wins): full -> new -> suite file changed -> prompt changed
(candidate or baseline prompt path) -> settings changed -> skip. A suite present at base and
absent at head is `removed` (never run, never fails).

`settings changed` is true only when a run-affecting field differs between the merge base and the head (a base copy that cannot be parsed counts as changed, with a warning, so no suite is skipped on a guess; a settings file outside the repository never counts):
`runs_per_case`, `min_drop_threshold`, `max_spread_before_unreliable`, or any provider's
`base_url`, `api_key_env`, `max_concurrent`, `requests_per_minute`. Differences only in
`prices`, `currency`, or `storage` do not count. A settings file added or deleted counts as
changed.

## Case status addition

`NEW`: the suite has no baseline counterpart. Candidate runs only. Never causes FAIL or WARN.
All candidate runs failing gives `ERROR` as in phase 1. Evaluated:

1. candidate has no successful run => ERROR
2. otherwise => NEW

Suite decision rules from phase 1 are unchanged (NEW is ignored by `Decide`).

## Usage

| Field | Meaning |
|-------|---------|
| InputTokens | prompt tokens reported by the provider |
| OutputTokens | completion tokens (0 for embeddings) |
| Known | false when the provider reported no usage for the call |

`UsageMeter` aggregates per `(provider, model)`: calls, input tokens, output tokens, and
`UnknownCalls` (calls with `Known == false`).

## CostSummary

Per `(provider, model)`: `Calls`, `InputTokens`, `OutputTokens`, `UnknownCalls`, and
`CostMicros *int64` (nil = unknown). Cost is known only when a price exists and
`UnknownCalls == 0`:

```text
CostMicros = round( input_tokens  * input_price_micro_per_million  / 1_000_000
                  + output_tokens * output_price_micro_per_million / 1_000_000 )
```

Run totals: sum of tokens; `TotalCostMicros` is the sum of known costs and is flagged
`Partial` when any model's cost is unknown.

## Storage records (package `storage`)

All timestamps are UTC. No record has a field for responses, prompts, or case inputs
(FR-013a).

**RunRecord**

| Field | Type | Rule |
|-------|------|------|
| ID | string | unique; `YYYYMMDDTHHMMSSZ-<8 hex>` |
| SchemaVersion | int | record format version (currently 1) |
| StartedAt | time | UTC |
| FinishedAt | time | UTC, `>= StartedAt` |
| HeadSHA | string | required; `unknown` when the run is not inside a git repository (`--baseline-dir` outside git) |
| HeadBranch | string | may be empty (detached) |
| BaseRef | string | empty when `--baseline-dir` was used |
| BaseSHA | string | empty when `--baseline-dir` was used |
| Mode | enum | `scoped`, `full`, `baseline-dir` |
| Decision | enum | PASS, WARN, FAIL, ERROR |
| ExitCode | int | 0, 1, or 2 consistent with Decision |
| Thresholds | struct | `RunsPerCase`, `MinDropThreshold`, `MaxSpreadBeforeUnreliable` |
| Currency | string | label from settings |
| TotalInputTokens, TotalOutputTokens | int64 | sums over Usage |
| TotalCostMicros | *int64 | nil when no cost is known |
| CostPartial | bool | true when some model's cost is unknown |
| Suites | []SuiteDecision | one per candidate suite and removed suite |
| Cases | []CaseRecord | one per case of a run suite |
| Usage | []UsageRecord | one per provider/model |

**SuiteDecision**: `Suite`, `File`, `Action`, `Reason`.

**CaseRecord**: `Suite`, `Case`, `Critical`, `Status` (PASS, REGRESSED, UNRELIABLE, ERROR, NEW),
`BaselineRuns`, `BaselineFailed`, `BaselineAverage *float64`, `BaselineSpread *float64`,
`CandidateRuns`, `CandidateFailed`, `CandidateAverage *float64`, `CandidateSpread *float64`,
`Delta *float64` (baseline minus candidate; nil when either side has no data).

**UsageRecord**: `Provider`, `Model`, `Calls`, `InputTokens`, `OutputTokens`, `UnknownCalls`,
`CostMicros *int64`.

Validation on save: ID non-empty and not already stored; `ExitCode` matches `Decision`; at least
the head SHA present; case suites must appear in `Suites` with action `run`.

### Store interface (behavioral contract in `contracts/store.md`)

`SaveRun(ctx, RunRecord) error` (atomic), `GetRun(ctx, id) (RunRecord, error)`,
`ListRunsByCommit(ctx, sha) ([]RunRecord, error)` (newest first),
`ListRecentRuns(ctx, limit) ([]RunRecord, error)` (newest first), `Close() error`.
Errors: `ErrNotFound`, `ErrDuplicateID`, `ErrNewerSchema`, `ErrBackendUnavailable`.

## State flow of a run

```text
resolve (settings, store config) -> [scope: refs, head check, change set, baseline export]
  -> select suites -> preflight -> run engine (usage metered) -> gate -> report
  -> save record (best effort; failure = warning) -> exit
```

Cancellation at any point before "save" saves nothing.
