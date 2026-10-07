# examples/mock

A repeatable end-to-end check that needs no model, no network, and no API keys. `run.sh`
builds the real `evalci` binary and a tiny fake Ollama server (`mockserver/`), then runs three
scenarios and asserts that the printed decision, the JSON result, and the process exit code
all agree.

```bash
examples/mock/run.sh          # needs Go on PATH; set MOCK_PORT to change port 18080
```

| Scenario | Mock behavior | Expected decision | Exit |
|----------|---------------|-------------------|------|
| pass | candidate and baseline prompts give the same answer | PASS | 0 |
| fail | candidate prompt gives a wrong answer (100 -> 0) | FAIL | 1 |
| warn | answers alternate GOOD/BAD, so scores swing 0/100 | WARN | 0 |

The mock replies with the system prompt (so the prompt file decides the answer), or alternates
`GOOD`/`BAD` when the prompt starts with `NOISY`. The `case-flaky` case is `critical: false`, so
in the `fail` scenario it shows `REGRESSED (non-critical)` while the other two cases cause the
FAIL. The script exits non-zero if any scenario disagrees, so it can run in CI.
