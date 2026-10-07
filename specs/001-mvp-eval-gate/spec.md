# Feature Specification: MVP Eval Gate

**Feature Branch**: `001-mvp-eval-gate`

**Created**: 2026-10-01

**Status**: Draft

**Input**: User description: "Specify the MVP for EvalCI: a CLI tool that runs an LLM eval suite defined in YAML against two variants of a prompt/model configuration — a baseline and a candidate — and produces a merge-gate decision."

## Clarifications

### Session 2026-10-01

- Q: When a case can't be evaluated at all (for example every model call for it fails), should the run end with a separate error outcome? → A: Yes. A separate ERROR outcome with its own non-zero exit code (2), distinct from FAIL (1).
- Q: How is a case's score computed when it has several assertions? → A: Plain average of all its assertion scores (0-100); weights and must-pass checks are deferred to a later phase.
- Q: If some of a case's runs fail but others succeed, should it still be scored? → A: Yes, from the successful runs, but the case is UNRELIABLE if fewer than half of the planned runs succeeded on either side.
- Q: When one case is REGRESSED and another is ERROR, which outcome wins? → A: FAIL (exit 1). A confirmed critical regression is never masked by an unrelated ERROR case; the ERROR case is still reported.
- Q: Where do semantic-similarity and llm-rubric checks get their embedding or judge model? → A: Each check names its own model in the suite file; there is no default.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Get a merge-gate decision for a prompt change (Priority: P1)

A developer who changed a prompt or model configuration runs one command locally. EvalCI
runs every eval suite against both the baseline and the candidate variant, compares the
results, and prints a clear PASS / WARN / FAIL decision. The exit status tells a CI
pipeline whether to block the merge.

**Why this priority**: This is the whole product. Without a trustworthy decision there is
nothing to gate a merge on.

**Independent Test**: With one suite and a few cases, run the tool where the candidate is
clearly worse than the baseline and confirm a FAIL decision and non-zero exit; run it where
the candidate is equal and confirm PASS and zero exit.

**Acceptance Scenarios**:

1. **Given** a suite where the candidate's average score is lower than the baseline's by more
   than the minimum-drop threshold and neither side is noisy, **When** the tool runs, **Then**
   that case is REGRESSED, the suite decision is FAIL, and the exit status is non-zero.
2. **Given** a suite where no case drops beyond the threshold, **When** the tool runs,
   **Then** every case is PASS, the suite decision is PASS, and the exit status is zero.
3. **Given** a suite where the only problem case is unreliable and none are regressed,
   **When** the tool runs, **Then** the decision is WARN and the exit status is zero.

---

### User Story 2 - Trust the gate only when the signal is real (Priority: P1)

A developer with a non-deterministic model needs the gate to avoid blocking merges on noise.
Each case is run several times per side, scores are averaged, and a case whose scores vary
too widely on either side is reported as UNRELIABLE instead of being passed or failed.

**Why this priority**: A gate that fails on noise gets turned off. This is a core promise of
the product.

**Independent Test**: Feed a case whose run-to-run scores swing widely on one side with a
large apparent drop; confirm it is UNRELIABLE, not REGRESSED, and does not fail the suite.

**Acceptance Scenarios**:

1. **Given** a case where either side's highest and lowest run scores differ by more than the
   unreliable-spread threshold, **When** gated, **Then** the case is UNRELIABLE regardless of
   how the averages compare.
2. **Given** a model configured with temperature 0, **When** the case runs, **Then** it runs
   exactly once per side and is never marked UNRELIABLE due to spread.
3. **Given** no runs-per-case setting, **When** the tool runs, **Then** it uses 5 runs per
   case per side.

---

### User Story 3 - Define suites and assertions in files (Priority: P2)

A developer writes eval suites as YAML files in an `evals/` folder. Each suite names a prompt
file, a model, and a list of cases; each case has an input and one or more assertions of type
exact-match, semantic-similarity, or llm-rubric. Repo-wide settings live in `.evalci.yaml`.

**Why this priority**: Authoring is how users get value in, but it can be exercised with a
single hand-written suite once the run and gate logic exist.

**Independent Test**: Author a suite using each assertion type and confirm each produces a
0-100 score per run; author an invalid suite and confirm a clear error naming the bad field.

**Acceptance Scenarios**:

1. **Given** a case with several assertions, **When** a run is scored, **Then** a single
   0-100 score results from averaging them.
2. **Given** a suite or settings file with an unknown or invalid key, **When** loaded,
   **Then** the tool stops with a message naming the offending field and file.
3. **Given** suites that select different providers (OpenAI, Anthropic, Ollama), **When**
   run, **Then** each is executed with the provider its model configuration names.

---

### User Story 4 - Read results as a person and as a machine (Priority: P2)

A developer reads a human-readable table in the terminal; a CI job or later tooling reads a
machine-readable JSON result file.

**Why this priority**: The decision is only useful if people can understand it and pipelines
can consume it.

**Independent Test**: Run a suite and confirm the table lists every case with baseline
average, candidate average, and status, and that the JSON file contains the same data plus
the suite decision.

**Acceptance Scenarios**:

1. **Given** a completed run, **When** it finishes, **Then** a per-case table and the suite
   decision are printed to standard output.
2. **Given** a completed run, **When** it finishes, **Then** a JSON result file is written
   containing per-case scores, statuses, and the suite decision.

---

### User Story 5 - Mark a case as non-blocking (Priority: P3)

A developer marks a known-flaky or experimental case with `critical: false` so a regression
there is visible but does not fail the suite.

**Why this priority**: Useful escape hatch, but the gate works without it.

**Independent Test**: Regress a `critical: false` case and confirm it is shown as REGRESSED
while the suite decision is not FAIL on its account.

**Acceptance Scenarios**:

1. **Given** a REGRESSED case marked `critical: false` and no other regressed case, **When**
   the suite is decided, **Then** it does not cause FAIL.
2. **Given** a case with no `critical` setting, **When** it is REGRESSED, **Then** it is
   treated as critical and causes FAIL.

---

### Edge Cases

- A provider call fails or times out for some runs: the case is scored from the successful
  runs (and UNRELIABLE if under half succeeded on either side); a case where either side has
  no successful run cannot be gated; it is reported as ERROR, never as PASS, and (unless a
  critical case also regressed) the run exits with the distinct error exit code (2), not the
  FAIL code.
- A critical case is REGRESSED while another case is ERROR: the decision is FAIL (exit 1) and
  the ERROR case is still shown.
- Baseline and candidate average exactly at the drop threshold: only a drop strictly greater
  than the threshold is REGRESSED.
- The candidate scores higher than the baseline: never REGRESSED.
- A case is both over the drop threshold and over the spread threshold: UNRELIABLE wins.
- No suites found in `evals/`: the tool stops with a clear message instead of reporting PASS.
- Provider rate limits are hit: runs are paced, not failed.
- A prompt file contains an unknown `{{name}}` placeholder (including a misspelled
  `{{input}}`): the tool stops before any model call and names the file and line, rather than
  sending an unfilled prompt.
- A prompt file contains literal `{{ }}` text, such as a JSON example: it is escaped as `\{{`
  and sent unchanged.
- Missing credentials for a provider a suite needs: the tool stops before making any
  model calls and names the provider.
- Suite with a `critical: false` case that is UNRELIABLE: still contributes to WARN.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST load every suite file matching `evals/*.eval.yaml`, each naming
  a prompt file, a model configuration, and a list of cases.
- **FR-002**: Each case MUST have an input and one or more assertions; supported assertion
  types are exact-match, semantic-similarity, and llm-rubric.
- **FR-002a**: Each semantic-similarity and llm-rubric assertion MUST name its own embedding
  or judge model and provider; no default is assumed. A combination the named provider cannot
  serve (for example, embeddings from a provider without them) MUST be rejected before any
  model call.
- **FR-002b**: The system MUST build each model request from the suite's prompt file and the
  case input identically for baseline and candidate. By default the prompt file is sent as the
  system message and the case input as the user message. If the prompt file contains the
  `{{input}}` placeholder, the case input replaces it and the result is sent as a single user
  message. `\{{` writes a literal `{{`. Any other `{{name}}` placeholder MUST be rejected
  before any model call, naming the file and line.
- **FR-003**: The system MUST load repo settings from `.evalci.yaml`: runs per case (default
  5), minimum drop threshold (default 5.0), maximum spread before unreliable (default 20.0),
  provider credentials, and storage driver.
- **FR-004**: The system MUST reject unknown keys and invalid values in any configuration
  file with a message naming the file and field.
- **FR-005**: The system MUST evaluate each case against both a baseline variant and a
  candidate variant of the prompt/model configuration. The suite files describe the candidate;
  a command-line option points to the location of the baseline's prompt/model configuration.
- **FR-006**: The system MUST run each case `runs per case` times per variant, or exactly once
  per variant when the model's temperature is 0.
- **FR-007**: Runs MUST execute concurrently while never exceeding a per-provider rate limit.
- **FR-008**: The system MUST use the provider (OpenAI, Anthropic, or Ollama) selected by the
  suite's model configuration, with identical behavior across providers and no provider
  assumed by default.
- **FR-009**: Each run MUST be scored on a 0-100 scale as the plain average of its
  assertions' 0-100 results.
- **FR-010**: For each case the system MUST compute the average score of the baseline runs and
  of the candidate runs.
- **FR-011**: A case MUST be UNRELIABLE when the difference between the highest and lowest
  run score on either side exceeds the maximum spread threshold, regardless of averages.
- **FR-011a**: Runs that fail (for example, a timeout) MUST be excluded from averages and
  counted in the results. A case MUST also be UNRELIABLE when fewer than half of the planned
  runs succeeded on either side.
- **FR-012**: A case that is not UNRELIABLE MUST be REGRESSED when baseline average minus
  candidate average is strictly greater than the minimum drop threshold; otherwise PASS.
- **FR-013**: The suite decision MUST be FAIL if any critical case is REGRESSED (a case not
  marked `critical: false`). Otherwise it MUST be ERROR if any case is ERROR. Otherwise WARN
  if any case is UNRELIABLE. Otherwise PASS.
- **FR-014**: Cases MUST be critical by default; `critical: false` makes a case non-blocking.
- **FR-015**: The system MUST print a human-readable table of per-case results and the suite
  decision to standard output.
- **FR-016**: The system MUST write a machine-readable JSON result file containing per-case
  scores, statuses, and the suite decision.
- **FR-017**: The system MUST exit with status 0 on PASS or WARN, 1 on FAIL, and 2 on an
  error outcome (an unevaluable case, or a configuration or credential problem). A run with
  both a critical regression and an unevaluable case exits 1.
- **FR-018**: The system MUST work as a standalone local run, with no stored history,
  dashboard, or pull-request integration required.
- **FR-019**: The system MUST NOT make more model calls than required: one run per variant at
  temperature 0, and no repeated sampling beyond the configured runs per case.
- **FR-020**: The system MUST report, not hide, cases where either side has no successful
  run, marking them ERROR. An ERROR case MUST NOT count as PASS and MUST NOT be
  reported as FAIL; it produces an error outcome with its own exit status (see FR-017).

### Key Entities

- **Suite**: A named collection of cases sharing a prompt file and model configuration.
- **Case**: One test input with its assertions and an optional non-blocking flag.
- **Assertion**: A check on a model output (exact-match, semantic-similarity, or llm-rubric)
  that yields a 0-100 result.
- **Variant**: The baseline or candidate form of the prompt/model configuration under test.
- **Run**: One model response to a case for one variant, with its 0-100 score.
- **Case Result**: Baseline and candidate averages, spreads, and a status (PASS, REGRESSED,
  UNRELIABLE, ERROR).
- **Suite Decision**: The overall PASS, WARN, FAIL, or ERROR with its exit status.
- **Settings**: Repo-wide thresholds, runs per case, provider credentials, and storage driver.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A user with a valid suite and settings can go from invoking the tool to seeing
  a final decision without any extra installation steps.
- **SC-002**: On a reference set of cases with known outcomes, 100% of cases are assigned the
  status the gating rules define (PASS, REGRESSED, UNRELIABLE, or ERROR).
- **SC-003**: Any case with spread above the threshold is never reported as REGRESSED or
  PASS (0 false FAILs from noisy signals).
- **SC-004**: At temperature 0, the number of model calls per case equals exactly one per
  variant.
- **SC-005**: 100% of configuration errors identify the offending file and field.
- **SC-006**: Re-running the gate over the same recorded scores always yields the same
  decision.
- **SC-007**: The exit status matches the suite decision in 100% of runs (0 for PASS/WARN,
  1 for FAIL, 2 for ERROR).

## Assumptions

- Out of scope for this phase: git-diff-aware suite scoping, persistent run history, judge
  calibration, the dashboard, pull-request comments, and formal statistical tests.
- The storage driver setting is read and validated but nothing is stored in this phase.
- Scores for exact-match are 0 or 100; semantic-similarity and llm-rubric map their result
  onto 0-100.
- Thresholds are in score points (0-100 scale), not percentages.
- Users supply their own provider credentials. Suites, cases, and runs execute concurrently,
  bounded only by each provider's rate limit.
- The JSON output path defaults to a fixed filename and can be overridden by the user.
