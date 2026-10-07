# Feature Specification: Diff-Aware Suite Scoping and Run History

**Feature Branch**: `002-diff-scoping-storage`

**Created**: 2026-10-01

**Status**: Draft

**Input**: User description: "Specify git-diff-aware suite scoping and persistent storage for EvalCI, building on the merged MVP. Given a base branch and a PR branch, resolve which prompt files and eval-suite YAMLs changed, and run only the affected suites unless --full is passed. Persist every run (scores, per-case deltas, gate decision, commit SHA, timestamp, provider cost) to SQLite via a storage.Store interface, structured so a Postgres implementation can be added later without changing any caller."

## Clarifications

### Session 2026-10-01

- Q: Besides scores and cost, should a saved run also keep the model responses and prompt text? → A: No. Records hold scores, deltas, decision, commit, timestamp, and cost only; no responses and no prompt text. Keeping them could be added later as an opt-in without breaking existing records.
- Q: Should a change to the shared settings file always re-run every suite? → A: Only run-affecting settings (runs per case, the two thresholds, provider connection details) select all suites; model prices and storage settings select none.
- Q: If the named pull request branch is not the checked-out one, should EvalCI run that branch's files or refuse? → A: Refuse. The named branch must match the checked-out commit, otherwise the run stops with exit status 2 before any model call.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Run only the suites a pull request affects (Priority: P1)

A developer or CI job names a base branch and the pull request's branch. EvalCI works out
which prompt files and eval-suite files changed between them, runs only the suites those
changes affect, and skips the rest. The gate decision covers the suites that ran.

**Why this priority**: Every eval call costs money and CI time. Running the whole library on
every pull request is what makes teams stop running the gate. This is the core of the phase.

**Independent Test**: In a repository with several suites, change one prompt file used by one
suite, run with the two branch names, and confirm that only that suite makes model calls and
appears in the results, while the others are listed as skipped.

**Acceptance Scenarios**:

1. **Given** a pull request that edits one suite's prompt file, **When** the tool runs with the
   base and PR branches, **Then** only the suites using that prompt file are run and every other
   suite makes zero model calls.
2. **Given** a pull request that edits a suite's YAML file, **When** the tool runs, **Then**
   that suite is run and the others are skipped.
3. **Given** a prompt file shared by two suites, **When** that file changes, **Then** both
   suites are run.
4. **Given** a pull request that changes no prompt or suite files, **When** the tool runs,
   **Then** it reports that no suites were affected, runs no model calls, and exits successfully.

---

### User Story 2 - Keep a permanent record of every run (Priority: P1)

Every time the gate runs, EvalCI saves a complete record: when it ran, for which commit,
which suites ran, each case's baseline and candidate scores and the change between them, the
gate decision, and what the run cost in provider usage. Later phases and teams can look back
at how quality and cost changed over time.

**Why this priority**: Without history there is no trend, no audit trail, and no cost
visibility. It is also the foundation for later features such as a dashboard.

**Independent Test**: Run the gate on a known commit, then read the stored record back and
confirm it matches the printed result field for field.

**Acceptance Scenarios**:

1. **Given** storage is configured, **When** a run finishes with any decision (PASS, WARN,
   FAIL, or ERROR), **Then** exactly one record is saved containing the commit identifier,
   timestamp, decision, per-case results, and cost.
2. **Given** a saved record, **When** it is retrieved by its commit, **Then** its decision and
   per-case numbers equal those printed by that run.
3. **Given** the same commit is run twice, **When** both finish, **Then** two separate records
   exist and neither overwrites the other.
4. **Given** the history file cannot be written, **When** the run finishes, **Then** a warning
   is shown and the gate decision and exit status are unchanged.

---

### User Story 3 - Force a full run and see why suites were chosen (Priority: P2)

A developer can run every suite regardless of what changed (for example before a release or
when changing shared settings), and can always see which suites were selected or skipped and
the reason for each.

**Why this priority**: Scoping must be trustworthy. If people cannot see or override its
choices, they will not rely on it.

**Independent Test**: Run with the full-run option and confirm every suite runs; run without
it and confirm the output lists each skipped suite with its reason.

**Acceptance Scenarios**:

1. **Given** a pull request touching one suite, **When** the full-run option is used, **Then**
   all suites run.
2. **Given** a scoped run, **When** it finishes, **Then** the output and the saved record list
   every suite as run or skipped, each with a reason (for example "prompt file changed" or
   "no related changes").
3. **Given** a run-affecting setting in the shared settings file changed (runs per case, a
   threshold, or provider connection details), **When** a scoped run starts, **Then** all
   suites are run and the reason is shown.
4. **Given** only a price or the storage location changed in the settings file, **When** a
   scoped run starts, **Then** that change selects no suites.

---

### User Story 4 - Compare against the base branch automatically (Priority: P2)

Instead of preparing a separate baseline directory, the developer names the base branch and
EvalCI uses the prompts and suite definitions from that branch as the baseline.

**Why this priority**: It removes the manual setup that the first version required and makes
CI usage a single command.

**Independent Test**: Run with a base branch name and no baseline directory; confirm the
baseline scores come from the base branch's prompt and model.

**Acceptance Scenarios**:

1. **Given** a base branch and a pull request branch, **When** the tool runs without a baseline
   directory, **Then** each suite's baseline uses its prompt and model as they exist on the base
   branch.
2. **Given** a suite that exists only on the pull request branch, **When** the tool runs,
   **Then** it is reported as new with no baseline to compare against, and it does not fail
   the gate.
3. **Given** a baseline directory is supplied explicitly, **When** the tool runs, **Then** that
   directory is used exactly as in the first version.

---

### User Story 5 - Swap the storage backend without touching callers (Priority: P3)

A maintainer can add a different storage backend later (for example a shared server database)
by providing a new implementation of the storage abstraction; no code that saves or reads run
history needs to change.

**Why this priority**: It protects the investment in this phase, but delivers no end-user
behavior by itself.

**Independent Test**: Run the same set of behavior checks against two implementations of the
storage abstraction (the real one and a simple in-memory one) and confirm both pass without
changing any caller.

**Acceptance Scenarios**:

1. **Given** two implementations of the storage abstraction, **When** the same behavior checks
   run against each, **Then** both pass.
2. **Given** a configuration naming a backend that is declared but not yet available, **When**
   the tool starts, **Then** it stops with a clear message before making any model call.

---

### Edge Cases

- A prompt file is renamed or moved: both the old and new paths count as changed, so any suite
  that referenced either is selected.
- A suite file is deleted on the pull request branch: it is reported as removed and not run;
  it does not fail the gate.
- A suite file is added: it is selected and reported as new (no baseline).
- The base and pull request branches point to the same commit: no changes, so no suites run.
- The base branch name cannot be resolved, the directory is not a git repository, or history is
  too shallow to find the common ancestor: the tool stops before any model call with a message
  that says what to fix (for example, fetch more history).
- The named pull request branch differs from the checked-out commit: the run stops before any
  model call (exit 2), so selection and the evals can never describe different code.
- The tool is started from a subdirectory of the repository (or `--evals` points outside it):
  it stops with a message naming the repository root, so suites cannot be skipped by a path
  mismatch.
- The working tree has uncommitted changes: they are not part of the comparison; a warning
  says so.
- A run is interrupted before it finishes: nothing is saved for it, so incomplete numbers
  never appear as history.
- The history file is locked by another run, missing, read-only, or from a newer version of the
  tool: the tool reports it clearly; a newer-version file is never modified.
- Two CI jobs write history at the same time: neither corrupts the other and each record is
  saved completely or not at all.
- A model's price is not configured: usage (tokens) is still saved and the cost for that model
  is shown as unknown, never as zero.
- A run that selects no suites still saves a record (decision PASS, zero cost).
- A case's input, the model's response, and the prompt text never appear in the history, so
  the history file cannot leak customer text from them.
- Storage is not configured: the tool behaves as in the first version and saves nothing.

## Requirements *(mandatory)*

### Functional Requirements

**Scoping**

- **FR-001**: The system MUST accept a base branch and a pull request branch (defaulting to
  the current branch's committed state) and compute the files changed since their common
  ancestor, matching what a pull request shows.
- **FR-002**: A suite MUST be selected when its own file changed, was added, or when any prompt
  file it references changed (in either the candidate or baseline version). Renamed files MUST
  count under both paths.
- **FR-003**: A change to a run-affecting setting in the shared settings file MUST select all
  suites. Run-affecting settings are runs per case, the minimum drop threshold, the maximum
  spread threshold, and provider connection details. A change only to model prices or storage
  settings MUST select no suites.
- **FR-004**: Unselected suites MUST make no model calls and MUST NOT appear in gating, but MUST
  be listed as skipped with a reason.
- **FR-005**: A full-run option MUST select every suite regardless of changes.
- **FR-006**: When no suite is selected, the system MUST report that nothing was affected, make
  no model calls, and exit as PASS (status 0).
- **FR-007**: Every selected or skipped suite MUST be shown with its reason in the printed
  output and in the machine-readable result.
- **FR-007a**: The pull request branch, when named, MUST resolve to the currently checked-out commit; otherwise the run MUST stop before any model call with exit status 2, saying which branch is checked out and what to do.
- **FR-008**: Unresolvable branches, a non-repository directory, or insufficient history MUST
  stop the run before any model call, with exit status 2 and an actionable message.
- **FR-009**: A suite deleted on the pull request branch MUST be reported as removed and MUST
  NOT run or fail the gate; a suite added MUST run and be reported as new.
- **FR-010**: When a base branch is given and no baseline directory is, the system MUST take
  each suite's baseline prompt and model from the base branch. An explicit baseline directory
  MUST keep working as in the first version; supplying both MUST be rejected as ambiguous.
- **FR-011**: A new suite with no baseline MUST be reported as new and MUST NOT cause FAIL.

**History**

- **FR-012**: When storage is configured, the system MUST save one record for every finished
  run, whatever the decision (PASS, WARN, FAIL, or ERROR), including runs that selected no
  suites. A run that stops on a configuration, credential, or pre-flight error before any model
  call is not a finished run and saves nothing.
- **FR-013**: Each record MUST contain: a unique run identifier; start timestamp in UTC; the
  head commit identifier and its branch name; the base branch and its commit identifier; whether the run was
  scoped or full; the selected and skipped suites with reasons; for every case its baseline and
  candidate average and spread, the change between them (the delta), failed-run count, status,
  and critical flag; the thresholds in effect; the gate decision and exit status; and the
  provider usage and cost.
- **FR-013a**: A record MUST NOT contain model responses, prompt text, or case inputs; it holds only the fields listed in FR-013.
- **FR-014**: Saving MUST be all-or-nothing: a record is either complete or absent. An
  interrupted run MUST save nothing.
- **FR-015**: Re-running the same commit MUST create a new record; existing records MUST NOT
  be altered.
- **FR-016**: A failure to save MUST NOT change the gate decision or exit status; it MUST be
  reported as a warning.
- **FR-017**: Saved records MUST be retrievable by run identifier, by commit, and as the most
  recent runs, through the storage abstraction.
- **FR-018**: History MUST be written safely when several runs write at once, and when the
  history file was created by an older version it MUST be upgraded without losing records; a
  file from a newer version MUST be rejected and left untouched.
- **FR-019**: When storage is not configured, the system MUST save nothing and behave as in the
  first version.

**Cost**

- **FR-020**: The system MUST record token usage for every provider call (generation, judging,
  embedding) as reported by the provider, per provider and model.
- **FR-021**: The system MUST compute estimated cost from per-model prices set in the settings
  file. A model with no configured price MUST show its cost as unknown, not zero; its tokens
  MUST still be recorded.
- **FR-022**: The printed output MUST show the run's total estimated cost and token usage, and
  MUST flag when any part of the cost is unknown.

**Storage abstraction**

- **FR-023**: All saving and reading of history MUST go through one storage abstraction. No
  caller may depend on a specific database, so adding another backend requires no change to any
  caller.
- **FR-024**: The initial supported backend MUST be a single-file embedded database that needs
  no separate server and no external installation. The configuration MUST accept a location for
  it, defaulting to a hidden folder in the repository.
- **FR-025**: A backend named in the settings but not yet available MUST be rejected before any
  model call with a message naming the backend.
- **FR-026**: The system MUST NOT add a dashboard, pull-request comments, retention or deletion
  of old records, or a command for browsing history in this phase.

### Key Entities

- **Change Set**: The files that differ between the base and pull request branches, with
  renames identified.
- **Scope Decision**: For one suite, selected or skipped, with the reason (suite changed,
  prompt changed, settings changed, full run requested, new, removed, no related changes).
- **Run Record**: One finished gate run; owns the fields in FR-013.
- **Case Record**: The result of one case within a run: baseline and candidate averages and
  spreads, delta, failed-run counts, status, critical flag.
- **Usage Record**: Token counts and estimated cost for one provider and model within a run,
  with an unknown-cost marker when no price is configured.
- **Price Entry**: A per-model price in the settings used to estimate cost.
- **Store**: The abstraction through which records are saved and retrieved.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For a pull request that touches N of M suites, model calls are made only for
  those N suites: 0 calls for each of the M-N unaffected suites, in 100% of tested scenarios.
- **SC-002**: On a reference set of change scenarios (suite edit, prompt edit, shared prompt,
  rename, add, delete, run-affecting settings change, price-only settings change, no change), 100% of selection decisions and reasons
  match the rules.
- **SC-003**: 100% of finished runs, regardless of decision, produce exactly one complete
  record when storage is configured; 0% of interrupted runs produce a record.
- **SC-004**: Retrieving a run by its commit returns a decision and per-case numbers identical
  to the ones that run printed, in 100% of tests.
- **SC-005**: Resolving which suites are affected takes under 2 seconds for a change set of up
  to 1,000 files.
- **SC-006**: Saving a run adds under 1 second to the run for runs with up to 500 cases.
- **SC-007**: A second storage implementation passes the same behavior checks as the first
  without any change to code that saves or reads history.
- **SC-008**: A storage failure leaves the decision and exit status unchanged in 100% of
  tested cases.
- **SC-009**: The saved total token usage equals the sum of the individual call usages, and
  estimated cost equals tokens times configured prices, to the cent.
- **SC-010**: Two runs writing at the same time both finish with complete records in 100% of
  tested cases.

## Assumptions

- Out of scope for this phase: a dashboard, pull-request comments, using history as the
  baseline, retention or pruning of old records, a server database implementation, a command
  for browsing history, judge calibration, and selecting individual cases within a suite
  (scoping is per suite).
- Scoping compares committed states only. The comparison starts from the common ancestor of the
  base and pull request branches so it matches a pull request's own diff.
- A suite depends only on its own file and the prompt files it references; no other files
  affect selection except run-affecting settings in the shared settings file, which affect all
  suites. This errs on the side of running more rather than missing a regression.
- Storage is opt-in: a run saves history only when storage is configured, so the first
  version's standalone behavior is unchanged by default.
- Cost is estimated from prices the user supplies; EvalCI ships no price list, because prices
  change and a stale built-in list would be misleading.
- Providers report token usage; where a provider reports none for a call, that call's tokens
  are recorded as unknown.
- The first phase's explicit baseline directory option remains supported.
- History lives in the configured file. On CI machines that start from a clean checkout every
  time, the history survives only if that file is kept between runs (for example by caching it)
  or placed outside the checkout; history shared across machines is what a later server
  backend is for.
- Timestamps are stored in UTC.
- The run record format carries a schema version so later phases can evolve it.
