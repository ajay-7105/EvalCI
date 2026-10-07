# Research: MVP Eval Gate

All Technical Context unknowns are resolved below.

## R1. How the baseline is supplied (spec FR-005, option A)

- **Decision**: Suites in `evals/` are the candidate. `--baseline-dir <dir>` points to a
  directory laid out like the repo (same `evals/*.eval.yaml` names and the prompt files they
  reference). For each suite, the baseline's prompt and model come from the same-named suite
  in that directory; the cases and assertions always come from the candidate suite so both
  sides are scored on identical inputs.
- **Rationale**: A literal "one baseline prompt/model flag" cannot work when several suites
  each have their own prompt. A mirrored directory is one flag, needs no git logic, and lets
  a CI job produce the baseline with a plain checkout.
- **Alternatives**: Per-suite `baseline:` blocks (more verbose, baseline drifts from main);
  git-revision baseline (git-aware, overlaps out-of-scope diff scoping).
- **Edge**: A suite with no same-named file in the baseline dir is an error naming the suite.

## R2. Semantic similarity needs embeddings

- **Decision**: A semantic-similarity assertion names an `embedding` block (`provider`,
  `model`). Score = cosine similarity clamped to [0,1] times 100. The provider interface has
  `Embed` and `SupportsEmbeddings() bool`. Pre-flight asks the provider, so no core package
  names a vendor (Constitution IV). Adapters without embeddings (Anthropic) return false and
  `ErrEmbeddingsUnsupported` from `Embed`; the problem is caught at validation, not at call
  time.
- **Rationale**: Anthropic has no embeddings endpoint; OpenAI and Ollama do. Vendor
  neutrality forbids a hidden default embedding provider, so it is always explicit.
- **Alternatives**: Bundled local embedding model (breaks single-binary size and
  simplicity); lexical similarity (not semantic).

## R3. llm-rubric judging

- **Decision**: The assertion names a `judge` block (`provider`, `model`) and a rubric text.
  The judge is asked for a JSON object `{"score": 0-100}`; non-parsable or out-of-range
  replies count as a failed run, not a zero.
- **Rationale**: A malformed judge answer is an infrastructure problem, not a quality signal
  (Constitution II). Judge calibration is explicitly later.
- **Alternatives**: Pass/fail judge (loses granularity for the drop threshold).

## R4. Scoring

- **Decision**: exact-match is 0 or 100 (trimmed comparison, optional `case_sensitive`,
  default true). Multiple assertions on a case are averaged equally.
- **Rationale**: Simplest rule that fits a 0-100 scale; weights can be added in a later phase.

## R5. Temperature and call count

- **Decision**: `temperature` is a required field of the suite's model block. When it equals
  0, run once per variant; otherwise `runs_per_case`.
- **Rationale**: Making it required removes the ambiguity of provider defaults, so "run once
  at temperature 0" is checkable. Spread is 0 for single runs, so UNRELIABLE cannot trigger.

## R6. Concurrency and rate limiting

- **Decision**: Per provider, a semaphore caps in-flight requests (`max_concurrent`, default
  4) and an optional `requests_per_minute` token bucket (`golang.org/x/time/rate`) paces
  starts. Work is fanned out with `errgroup`; each run takes both limits before calling.
- **Rationale**: Meets "per-provider rate limit" and the `claude.md` semaphore guidance.
- **Alternatives**: One global limit (punishes fast providers); retry-on-429 only (causes
  bursts and wasted calls).

## R7. Failures and exit codes

- **Decision**: Transient errors (network, 429, 5xx) are retried up to 2 times with backoff.
  A run that still fails is excluded from averages and counted in the result. A case where
  either side has no successful run gets status ERROR. If fewer than half of the planned runs
  succeeded on either side, the case is UNRELIABLE (clarified in spec FR-011a). Exit codes: `0` PASS or WARN, `1` FAIL,
  `2` configuration or infrastructure error (including any ERROR case when no critical case
  regressed). A critical regression plus an ERROR case is FAIL (exit 1): a confirmed
  regression is never masked by an unrelated outage.
- **Rationale**: Infrastructure failure must never look like a regression (FAIL) or a pass.
  This extends the spec's decision set (PASS/WARN/FAIL) with a non-gating error outcome.

## R8. Parsing and CLI libraries

- **Decision**: `yaml.v3` with `KnownFields(true)` for both YAML kinds; errors wrap file and
  field path. `cobra` for the command tree. `viper` is not used this phase.
- **Rationale**: Strict unknown-key rejection is native to yaml.v3 and awkward in viper;
  settings are a single file plus flags. Adding viper later is non-breaking.

## R9. Provider adapters

- **Decision**: Thin `net/http` clients behind `providers.Provider`
  (`Complete(ctx, Request) (Response, error)`, `Embed(ctx, model, text) ([]float32, error)`).
  Credentials are read from environment variables named in `.evalci.yaml` (`api_key_env`);
  keys are never stored in config files. Ollama takes a `base_url`.
- **Rationale**: No SDKs keeps the binary small and dependency-free, and tests can use
  `httptest`.

## R10. Prompt rendering

- **Decision**: System message plus user message by default; `{{input}}` substitution (single
  user message) when the placeholder is present; `\{{` escape; unknown `{{name}}` rejected at
  pre-flight; single-pass substitution so case inputs are never re-interpreted.
- **Rationale**: Tests the request shape production apps actually send, so the gate measures
  real behavior. Strict validation prevents a silently unfilled prompt from scoring the same
  on both sides. The named-placeholder form leaves room for later variables (for example,
  retrieved context) without breaking suites.
- **Alternatives**: Bare find-and-replace only (cannot express system/user roles, silent on
  typos, no escape); full template engine (more power than this phase needs).

## R11. JSON result and determinism

- **Decision**: One versioned result document (`schema_version: 1`), cases sorted by suite
  then case id, default path `evalci-result.json`, override with `--output`. Gating is a pure
  function of scores and settings.
- **Rationale**: Stable diffs, reproducible decisions (SC-006), easy for later phases to
  consume.
