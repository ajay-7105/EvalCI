# Contract: `.evalci.yaml` additions

Extends `specs/001-mvp-eval-gate/contracts/evalci-config.schema.md`. Unknown keys are still
rejected with the file and field named.

```yaml
currency: USD                         # optional label, default USD; informational only

prices:                               # optional; key is "<provider>/<model>"
  openai/gpt-4o-mini:
    input_per_million: 0.15           # >= 0, in `currency` per million input tokens
    output_per_million: 0.60          # >= 0
  anthropic/claude-haiku-4-5-20251001:
    input_per_million: 1.00
    output_per_million: 5.00

storage:                              # optional; absent means nothing is saved
  driver: sqlite                      # sqlite | postgres (postgres: rejected as unavailable)
  path: .evalci/evalci.db             # optional, default shown; parent directory is created
```

Rules:
- A price key must be `provider/model` with `provider` in `openai | anthropic | ollama`.
- Models without a price are allowed; their cost is reported as unknown, never zero.
- `storage.driver: postgres` stops the run before any model call: `storage backend "postgres"
  is not available in this version`.
- No price list ships with the binary.
- Changing only `prices`, `currency`, or `storage` does not select suites for a scoped run.
