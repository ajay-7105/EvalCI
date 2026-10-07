# EvalCI

A CI gate for LLM prompt quality. EvalCI runs an eval suite against a **baseline** and a
**candidate** version of your prompt/model configuration and blocks a merge only when the
candidate is worse by an amount that is both large enough and consistent enough to be real.

Prompt edits look like harmless text changes, but they change behavior. EvalCI gates them the way
you gate code: run the tests, compare against the last known-good version, fail on regression.

- **One static binary.** Pure Go, no CGo, nothing to install alongside it (no `git`, no database).
- **Honest about noise.** A noisy signal degrades to a warning, never a false failure.
- **Cost-conscious.** Runs once at temperature 0, and only re-tests the suites a pull request
  actually affects.
- **Vendor neutral.** OpenAI, Anthropic and Ollama sit behind one adapter interface; no provider
  is a default.

## Install

Requires Go 1.26 or newer to build.

```bash
go install github.com/ajay-7105/EvalCI/cmd/evalci@latest
# or from a checkout:
CGO_ENABLED=0 go build -o evalci ./cmd/evalci
```

## Quick start

Try it with no model and no API key (needs Go, plus `git` only to build a demo repository):

```bash
examples/scoping/run.sh     # diff-aware scoping and run history, against a mock model server
examples/mock/run.sh        # PASS / FAIL / WARN scenarios
```

On your own repository, from the repository root:

```bash
evalci run --base main      # compare this branch with main; run only the affected suites
evalci run --base main --full
evalci run --baseline-dir ./baseline    # or supply the baseline directory yourself
```

## Writing a suite

`evals/support.eval.yaml`:

```yaml
name: support
prompt: prompts/support.txt        # system message; use {{input}} to substitute the case input instead
model:
  provider: ollama                 # openai | anthropic | ollama (no default)
  name: llama3.2:1b
  temperature: 0                   # required; 0 means one run per variant

cases:
  - id: late-order
    input: "Hi, my order 4821 has not arrived yet."
    assertions:
      - type: exact-match
        expected: "4821"
  - id: tone
    input: "This is the third time my parcel is late."
    critical: false                # visible when it regresses, but never blocks the merge
    assertions:
      - type: llm-rubric
        rubric: "Apologetic and offers a concrete next step."
        judge: { provider: ollama, model: "llama3.2:1b" }
```

Assertion types: `exact-match`, `semantic-similarity` (cosine similarity of embeddings), and
`llm-rubric` (a judge model scores 0-100). Each run's score is the average of its assertions.
Semantic and rubric assertions name their own embedding or judge model; there is no default.

`.evalci.yaml` (repository settings):

```yaml
runs_per_case: 5                    # runs per case per side (1 at temperature 0)
min_drop_threshold: 5.0             # average drop, in score points, that counts as a regression
max_spread_before_unreliable: 20.0  # max-min spread above this makes a case UNRELIABLE

providers:
  ollama: { base_url: "http://localhost:11434", max_concurrent: 2 }
  openai: { api_key_env: OPENAI_API_KEY, requests_per_minute: 60 }   # keys come from the environment

prices:                             # optional; no price means cost is shown as unknown, never zero
  openai/gpt-4o-mini: { input_per_million: 0.15, output_per_million: 0.60 }

storage:                            # optional; omit to save no history
  driver: sqlite
  path: .evalci/evalci.db
```

Unknown keys are rejected with the file and field named.

## How the gate decides

For each case, EvalCI averages the baseline runs and the candidate runs, then:

1. If either side has no successful run: **ERROR**.
2. If either side's max-min spread is above the limit, or fewer than half of its runs succeeded:
   **UNRELIABLE** (a warning, never a failure).
3. If baseline average minus candidate average is strictly greater than the threshold:
   **REGRESSED**.
4. Otherwise **PASS**. A suite with no baseline counterpart is **NEW** and never fails.

The suite decision is **FAIL** if any critical case regressed (even if another case errored),
otherwise **ERROR** if any case errored, otherwise **WARN** if any is unreliable, otherwise **PASS**.

| Exit code | Meaning |
|-----------|---------|
| 0 | PASS or WARN |
| 1 | FAIL: a critical case regressed |
| 2 | Configuration error, refusal, or an unevaluable case |

Output is a table on stdout and a JSON result file (`--output`, default `evalci-result.json`).

```text
Scope: base main (a1b2c3d) -> head feature/x (d4e5f6a); 1 of 3 suites selected
  support  RUN   prompt file changed: prompts/support.txt
  billing  SKIP  no related changes
  legacy   SKIP  no related changes

SUITE    CASE      BASELINE  CANDIDATE  DROP  STATUS
support  refund-1  92.0      90.0       2.0   PASS

Decision: PASS (1 cases)
Cost: 12,400 input / 3,100 output tokens, est. 0.0123 USD
```

## Diff-aware scoping

With `--base <ref>`, EvalCI reads the changes between the merge base of your branch and the base
branch and runs only affected suites: one whose file changed, whose referenced prompt changed
(renames count under both paths), or all of them when run-affecting settings changed (runs per
case, thresholds, provider connection details). Unselected suites make no model calls. The
baseline is the base branch as it was when your branch left it. `--head` must be the checked-out
commit, and the tool must be run from the repository root.

## Run history and cost

With `storage` configured, every finished run is saved: scores, per-case deltas, decision, commit,
timestamp, token usage and estimated cost. Records never contain responses, prompt text or case
inputs. Saving is best effort and never changes the gate result. There is no history command;
query the SQLite file directly:

```bash
sqlite3 -header -column .evalci/evalci.db \
  "SELECT id, decision, exit_code, total_cost_micros/1e6 AS cost FROM runs ORDER BY started_at DESC LIMIT 10"
```

History sits behind a `storage.Store` interface, so other backends (such as Postgres) can be
added without changing any caller. Only the SQLite backend exists today.

## Examples

| Example | Needs | Shows |
|---------|-------|-------|
| `examples/mock` | Go | PASS / FAIL / WARN against a mock server |
| `examples/scoping` | Go, git | Scoping, history and cost, end to end |
| `examples/basic` | Ollama (`llama3.2:1b`) | A real PASS, and how to flip it to FAIL |
| `examples/noisy` | Ollama | UNRELIABLE and WARN |
| `examples/providers/ollama` | Ollama | `{{input}}` prompts and an LLM judge |
| `examples/providers/hosted` | API keys | OpenAI and Anthropic suites |

## Development

```bash
go build ./... && go vet ./...
go test ./... -race            # add -short to skip the performance checks
golangci-lint run
```

Coverage is held above 80% on the trust-critical packages (`internal/gating`, `internal/engine`).
`claude.md` has a map of the architecture and the non-obvious design rules.

The project was built with [Spec Kit](https://github.com/github/spec-kit): the constitution is in
`.specify/memory/constitution.md`, and each phase has its spec, plan, tasks and contracts in
`specs/`.

```text
cmd/evalci/            CLI entry point and orchestration
configs/               .evalci.yaml and *.eval.yaml parsing and strict validation
internal/engine/       runner, per-provider limits, prompt rendering, pre-flight checks
internal/providers/    one Provider interface; OpenAI, Anthropic, Ollama adapters
internal/assertions/   exact-match, semantic-similarity, llm-rubric
internal/gating/       pure case and suite decision logic
internal/diffscope/    git change sets, suite selection, baseline export
internal/storage/      Store interface; SQLite and in-memory implementations
internal/cost/         token usage to estimated cost
internal/reporters/    table, JSON, exit codes, diagnostics
```

## License

MIT. See [LICENSE](LICENSE).
