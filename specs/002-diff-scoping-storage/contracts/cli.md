# Contract: CLI (changes to `evalci run`)

Extends `specs/001-mvp-eval-gate/contracts/cli.md`. Anything not listed is unchanged.

```text
evalci run (--base <ref> [--head <ref>] [--full] | --baseline-dir <dir>)
           [--config .evalci.yaml] [--evals evals] [--output evalci-result.json] [--verbose]
```

| Flag | Default | Meaning |
|------|---------|---------|
| `--base` | none | Base branch or ref. Baseline comes from it; enables scoping |
| `--head` | `HEAD` | Pull request branch or ref. MUST resolve to the checked-out commit |
| `--full` | off | Run every suite regardless of changes (only meaningful with `--base`) |
| `--baseline-dir` | none | Phase-1 explicit baseline directory; all suites run |

With `--base`, run from the repository root (`--evals` and `--config` are relative to it; `--evals`
must be inside the repository). Exactly one of `--base` or `--baseline-dir` is required. Both, or neither, exit 2 with a message.
`--full` or `--head` without `--base` exits 2.

## Output

stdout, in order: scope block (only with `--base`), results table, decision line, cost line.

```text
Scope: base main (a1b2c3d) -> head feature/x (d4e5f6a); 1 of 3 suites selected
  support   RUN   prompt file changed: prompts/support.txt
  billing   SKIP  no related changes
  legacy    SKIP  no related changes

SUITE    CASE      BASELINE  CANDIDATE  DROP  STATUS
support  refund-1  92.0      90.0       2.0   PASS

Decision: PASS (1 cases)
Cost: 12,400 input / 3,100 output tokens, est. 0.0123 USD (cost unknown for 1 model)
```

- Scope actions: `RUN`, `SKIP`, `REMOVED`. Reason strings are those in the data model.
- `--full`: `Scope: ... full run requested; 3 of 3 suites selected`.
- Nothing selected: scope block, then `Decision: PASS (no suites affected)`; no table.
- `NEW` cases show `-` for baseline and drop and status `NEW`.
- Cost line: omitted when no provider reported usage and no run happened; estimated cost shows
  `unknown` when no price applies, and `(cost unknown for N model(s))` when partial.
- With `--baseline-dir` there is no scope block (all suites run).

stderr: warnings and diagnostics only, including:
- `warning: working tree has uncommitted changes; they are not part of the comparison`
- `warning: could not save run history: <reason>`
- failure reasons for failed runs (unchanged)

## Exit codes

Unchanged: 0 PASS or WARN, 1 FAIL, 2 error. Additional exit-2 cases, all before any model call:

- both or neither of `--base` / `--baseline-dir`; `--full` or `--head` without `--base`
- not a git repository, not run from the repository root, `--evals` outside the repository, or
  `--base` / `--head` does not resolve
- `--head` does not equal the checked-out commit (message names the checked-out branch and says
  to check the branch out)
- no common ancestor (message suggests fetching more history)
- `storage.driver` names an unavailable backend (message names it)
- any phase-1 pre-flight failure for a selected suite

A storage open or save failure never changes the exit code (warning only).

## JSON file

`--output` JSON follows `result.schema.json` (schema version 2): adds `run_id` (null when
nothing was saved), `scope`, `cost`, and the `NEW` case status.
