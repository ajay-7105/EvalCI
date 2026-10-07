# Data Model: MVP Eval Gate

Entities from the spec, with fields and rules. No persistence in this phase; these exist in
memory and in the JSON result.

## Settings (`.evalci.yaml`)

| Field | Type | Default | Rule |
|-------|------|---------|------|
| runs_per_case | int | 5 | >= 1 |
| min_drop_threshold | float | 5.0 | >= 0, score points |
| max_spread_before_unreliable | float | 20.0 | >= 0, score points |
| providers.<name> | map | none | name in {openai, anthropic, ollama}; see below |
| storage.driver | string | none | `sqlite` or `postgres`; validated, unused this phase |

Provider entry: `api_key_env` (env var name, required for openai/anthropic), `base_url`
(required for ollama, optional otherwise), `max_concurrent` (int, default 4),
`requests_per_minute` (int, optional, 0 = unlimited). Unknown keys are rejected.

## Suite (`evals/*.eval.yaml`)

| Field | Type | Rule |
|-------|------|------|
| name | string | unique across suites |
| prompt | path | file must exist, relative to the repo (or baseline dir) root; rendered per the Prompt rendering rules below |
| model | Model | see below |
| cases | list of Case | at least one |

**Model**: `provider` (openai/anthropic/ollama), `name` (string), `temperature` (float,
required: a missing key is a validation error naming `model.temperature`; an explicit `0`
is valid), optional `max_tokens`. The provider must be configured in Settings.

## Case

| Field | Type | Rule |
|-------|------|------|
| id | string | unique within suite |
| input | string | the case input; sent as the user message, or substituted for `{{input}}` |
| critical | bool | default true |
| assertions | list of Assertion | at least one |

## Assertion

`type` is one of:

- `exact-match`: `expected` (string), optional `case_sensitive` (default true).
- `semantic-similarity`: `expected` (string), `embedding` {provider, model}. The named
  provider MUST support embeddings (reported by its adapter); otherwise it is a validation
  error.
- `llm-rubric`: `rubric` (string), `judge` {provider, model}.

## Prompt rendering

Applied identically to both variants:

| Prompt file contains | Request sent |
|----------------------|--------------|
| no placeholder | system message = file contents; user message = case input |
| `{{input}}` | one user message = file contents with `{{input}}` replaced by the case input |

- `\{{` is an escape and renders as a literal `{{`.
- Any other `{{name}}` (including a misspelled `{{imput}}`) is a validation error naming
  file and line, raised before any model call. Further variable names are reserved for later
  phases.
- Substitution is single-pass: text inside the case input is never re-scanned for
  placeholders.

## Variant

`baseline` or `candidate`; resolves to a prompt and Model. Both variants of a suite share
the same cases and assertions.

## Run

`suite`, `case`, `variant`, `index`, `score` (0-100) or `error` (string). A run with an error
has no score and is excluded from averages.

## CaseResult

| Field | Meaning |
|-------|---------|
| baseline / candidate | `{runs, failed_runs, scores[], average, spread}` |
| drop | baseline average minus candidate average |
| status | PASS, REGRESSED, UNRELIABLE, or ERROR |
| critical | copied from the case |

### Status rules (evaluated in order)

1. Either side has no successful run => ERROR (there is nothing to compare).
2. Either side's max - min score > max_spread_before_unreliable, or fewer than half of the
   planned runs succeeded on either side => UNRELIABLE.
3. drop > min_drop_threshold (strictly) => REGRESSED.
4. Otherwise PASS.

Single-run sides have spread 0.

## SuiteDecision

Computed over all CaseResults:

1. Any REGRESSED case with critical = true => FAIL (exit 1). A confirmed regression is never
   masked by an unrelated ERROR case; the ERROR case is still shown.
2. Else any ERROR case => exit code 2 (decision reported as ERROR, not gated).
3. Else any UNRELIABLE case (critical or not) => WARN (exit 0).
4. Else PASS (exit 0).

A REGRESSED case with critical = false is shown but does not cause FAIL.
