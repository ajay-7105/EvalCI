# Contract: `.evalci.yaml`

Unknown keys are rejected with a message naming the file and field path.

```yaml
runs_per_case: 5                    # optional, default 5, >= 1
min_drop_threshold: 5.0             # optional, default 5.0
max_spread_before_unreliable: 20.0  # optional, default 20.0

providers:                          # only providers used by suites need entries
  openai:
    api_key_env: OPENAI_API_KEY     # name of the env var, never the key itself
    max_concurrent: 4               # optional, default 4
    requests_per_minute: 60         # optional, 0 or absent = unlimited
  anthropic:
    api_key_env: ANTHROPIC_API_KEY
  ollama:
    base_url: http://localhost:11434

storage:
  driver: sqlite                    # sqlite | postgres; validated, unused this phase
```

Validation: numeric values in range per [data-model.md](../data-model.md); an `api_key_env`
that names an unset environment variable is an error only if a suite uses that provider.
