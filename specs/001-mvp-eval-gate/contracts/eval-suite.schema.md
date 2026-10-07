# Contract: `evals/*.eval.yaml`

Unknown keys are rejected with a message naming the file and field path.

```yaml
name: support-replies               # unique across suites
prompt: prompts/support.txt         # system message by default; if it contains {{input}}, see below
model:
  provider: anthropic               # openai | anthropic | ollama (no default)
  name: claude-haiku-4-5-20251001
  temperature: 0.7                  # required; 0 => one run per variant
  max_tokens: 512                   # optional

cases:
  - id: refund-1                    # unique within suite
    input: "I want my money back for order 123"
    critical: true                  # optional, default true; false = non-blocking
    assertions:
      - type: exact-match
        expected: "REFUND"
        case_sensitive: false       # optional, default true
      - type: semantic-similarity
        expected: "We will refund your order."
        embedding: { provider: openai, model: text-embedding-3-small }
      - type: llm-rubric
        rubric: "Polite, acknowledges the refund request, no promises beyond policy."
        judge: { provider: anthropic, model: claude-haiku-4-5-20251001 }
```

Prompt rendering (same for baseline and candidate):
- No placeholder in the prompt file: file contents are the system message, the case `input`
  is the user message.
- Prompt file contains `{{input}}`: the case input replaces it and the whole text is sent as
  one user message.
- `\{{` renders a literal `{{`. Any other `{{name}}` is a validation error naming the file
  and line, raised before any model call.

Rules: at least one case; at least one assertion per case; `embedding.provider` must be a
provider that supports embeddings (asked of the adapter, never hard-coded); every provider referenced (model, embedding, judge) must exist in
`.evalci.yaml`. Scoring per assertion is defined in [research.md](../research.md) R2-R4.
