# examples/providers/hosted

OpenAI and Anthropic suites, including cross-provider judging and embeddings. Not run in
automated tests: it needs real API keys and makes paid calls.

```bash
export ANTHROPIC_API_KEY=...
export OPENAI_API_KEY=...
cd examples/providers/hosted
../../../evalci run --baseline-dir ./baseline
```

Notes:

- There is no default provider or model. Every model, embedding, and judge is named
  explicitly, and each provider used must be configured in `.evalci.yaml`.
- Pre-flight fails before any model call if a used provider has no key, or if an embedding
  provider cannot do embeddings (Anthropic cannot, which is why the embedding model here is
  OpenAI's).
- `requests_per_minute` and `max_concurrent` pace calls per provider, including judge and
  embedding calls.
- Worst-case calls per case: 2 variants x `runs_per_case` generations, plus one judge call per
  `llm-rubric` and one embedding call per `semantic-similarity` for each generated output (the
  expected text is embedded once).
