# Contract: CLI

## `evalci run`

```text
evalci run --baseline-dir <dir> [--config .evalci.yaml] [--evals evals] [--output evalci-result.json]
```

| Flag | Default | Meaning |
|------|---------|---------|
| `--baseline-dir` | required | Repo-shaped directory holding baseline suites and prompts (see research R1) |
| `--config` | `.evalci.yaml` | Settings file |
| `--evals` | `evals` | Directory scanned for `*.eval.yaml` (candidate) |
| `--output` | `evalci-result.json` | JSON result path |
| `--verbose` | off | Log one line per model call to stderr (suite, case, variant, run, provider, model, duration, ok/error) |

### Streams

- **stdout**: the results table and final decision line.
- **stderr**: errors and progress/diagnostics. Nothing else is written to stdout. Failure
  reasons for failed runs and ERROR cases (suite, case, variant, run, message) are always
  written to stderr, so the cause of an `!N` note or an ERROR row is visible; `--verbose`
  adds per-call lines.

### Table (stdout)

```text
SUITE    CASE      BASELINE  CANDIDATE  DROP  STATUS
support  refund-1  92.0      90.0       2.0   PASS
support  tone-2    88.0      71.0       17.0  REGRESSED
support  edge-3    80.0      60.0       20.0  UNRELIABLE

Decision: FAIL (1 regressed, 1 unreliable)
```

Non-critical cases carry a `(non-critical)` suffix on the status. Failed-run counts appear
as a `!N` note beside the averages when any run failed.

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | PASS or WARN |
| 1 | FAIL (a critical case REGRESSED) |
| 2 | Configuration error, missing credentials, no suites found, or any case ERROR when no critical case regressed |

### Pre-flight (before any model call)

Fails with exit 2, naming file and field, if: any config is invalid; a referenced prompt
file is missing; a used provider lacks credentials or configuration; an embedding provider
reports no embedding support; a prompt file has an unknown `{{name}}` placeholder; a suite has no counterpart in `--baseline-dir`; no suites match.
