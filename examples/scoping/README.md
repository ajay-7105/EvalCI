# examples/scoping

Diff-aware suite scoping and run history, end to end and offline. `run.sh` builds `evalci` and
the mock Ollama server, creates a demo git repository (`setup.sh`), and runs the scenarios from
`specs/002-diff-scoping-storage/quickstart.md`, asserting what ran, what was skipped, and what
was saved:

```bash
examples/scoping/run.sh        # needs Go and git (git only builds the demo repo); MOCK_PORT=18080
```

It checks that a prompt change runs only the suite using it (0 model calls for the others), that
a docs-only change runs nothing and passes, that new and deleted suites are handled, that
run-affecting settings select every suite while a price-only change selects none, that `--full`
runs everything, that a regression fails the gate against the base branch, that refusals happen
before any model call, and (when the `sqlite3` command is installed) that every run was saved
with its decision, cost, and no responses or prompts.

## Using it on your own repository

```bash
cd <repository root>
evalci run --base main            # only the suites this branch affects
evalci run --base main --full     # everything, regardless of changes
```

The baseline is the base branch as of the point where your branch left it. Add to
`.evalci.yaml` to keep history and estimate cost:

```yaml
storage:
  driver: sqlite
  path: .evalci/evalci.db   # default; keep it between CI runs (cache it) for history to build up

prices:                     # optional; no price means the cost is shown as unknown, never zero
  openai/gpt-4o-mini: {input_per_million: 0.15, output_per_million: 0.60}
```

## Reading the history

There is no history command; query the file directly:

```bash
sqlite3 -header -column .evalci/evalci.db \
  "SELECT id, decision, exit_code, mode, total_cost_micros/1e6 AS cost FROM runs ORDER BY started_at DESC LIMIT 10"
sqlite3 -header -column .evalci/evalci.db \
  "SELECT suite, case_id, status, delta FROM run_cases WHERE run_id = '<id>' ORDER BY delta DESC"
```

Records hold scores, deltas, decisions, commit, timestamp, token usage, and cost only: never
responses, prompt text, or case inputs. Columns are documented in
`specs/002-diff-scoping-storage/contracts/sqlite-schema.md`.
