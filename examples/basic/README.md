# examples/basic

Offline example: extracts order numbers from support messages with a local Ollama model and
gates a prompt change. Everything is deterministic (`temperature: 0`, one run per variant).

## Prerequisites

- A local Ollama server with `llama3.2:1b` (`ollama pull llama3.2:1b`).
- The binary: `CGO_ENABLED=0 go build -o evalci ./cmd/evalci` from the repository root. It is
  a single self-contained binary (no CGo, no runtime dependencies).

## Run (PASS)

```bash
cd examples/basic
../../evalci run --baseline-dir ./baseline
echo $?   # 0
```

`baseline/` mirrors the repo layout (`evals/` and `prompts/`) and holds the baseline version
of the prompt and model. Cases and assertions always come from the candidate in `evals/`, so
both sides are scored on identical inputs. The baseline and candidate prompts start out
identical, so the gate passes. A JSON result is written to `evalci-result.json`.

## See a FAIL

In `evals/support.eval.yaml` change `prompt: prompts/support.txt` to
`prompt: prompts/support-worse.txt` and run again. The candidate now answers `BANANA`, the
critical cases REGRESSED, the decision is `FAIL`, and the exit status is 1. The
`where-is-it` case is marked `critical: false`: it shows as REGRESSED `(non-critical)` and does
not by itself fail the run.

## Useful flags

- `--verbose` logs one stderr line per model call.
- `--output path.json` writes the JSON result elsewhere.
- Exit codes: 0 = PASS or WARN, 1 = FAIL, 2 = configuration error or an unevaluable case.
