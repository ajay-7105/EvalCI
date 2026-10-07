# Quickstart: MVP Eval Gate validation

Proves the feature end-to-end. Contracts: [cli.md](contracts/cli.md),
[evalci-config.schema.md](contracts/evalci-config.schema.md),
[eval-suite.schema.md](contracts/eval-suite.schema.md),
[result.schema.json](contracts/result.schema.json).

## Prerequisites

- Go 1.23+ to build; the resulting binary needs nothing else (no CGo).
- For the offline scenarios: a local Ollama server with `llama3.2:1b`
  (`ollama pull llama3.2:1b`); the fake provider covers the same paths in the test suite. For hosted providers: the API key env vars named in
  `.evalci.yaml`.

## Build and test

```bash
CGO_ENABLED=0 go build -o evalci ./cmd/evalci
go vet ./...
go test ./... -race
golangci-lint run
```

## Scenario 1: PASS (User Story 1)

```bash
cd examples/basic
../../evalci run --baseline-dir ./baseline
echo $?   # 0
```

The example ships with identical baseline and candidate prompts and `temperature: 0`, so the
result is deterministic. Expect: table with all cases PASS, `Decision: PASS`,
`evalci-result.json` written.

## Scenario 2: FAIL on regression

In `examples/basic/evals/support.eval.yaml` change `prompt:` to `prompts/support-worse.txt`
(shipped with the example) and rerun.
Expect: a case REGRESSED, `Decision: FAIL`, exit code 1.

## Scenario 3: UNRELIABLE and WARN (User Story 2)

```bash
cd examples/noisy
../../evalci run --baseline-dir ./baseline
```

The noisy example (high temperature, exact-match cases) is separate from `examples/basic` so
Scenario 1 stays a deterministic PASS. Expect UNRELIABLE, no FAIL from that case, `Decision: WARN`, exit 0. A
rubric-judged noisy variant is added with User Story 3.

## Scenario 4: Temperature 0 call count

Run the shipped `support` suite (`temperature: 0`) with `--verbose`. Expect exactly one
generation call line on stderr per case per variant.

## Scenario 5: Config errors (User Story 3)

Add an unknown key to `.evalci.yaml`, rerun. Expect exit 2 and a message naming the file and
the key, with no model calls made.

## Scenario 6: Non-critical case (User Story 5)

With the worse prompt from Scenario 2 still in place, set `critical: false` on every critical
case (`late-order`, `damaged-order`). Expect all three rows shown as REGRESSED
`(non-critical)` and `Decision: PASS (3 regressed)` with exit 0.

## Scenario 7: Machine output (User Story 4)

Validate `evalci-result.json` against [result.schema.json](contracts/result.schema.json) and
confirm its decision and exit_code match stdout and the process exit status.
