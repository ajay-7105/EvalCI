# examples/providers/ollama

Shows prompt rendering with `{{input}}` and an `llm-rubric` assertion, offline with Ollama.
The prompt file contains `{{input}}`, so the case input replaces it and the whole text is sent
as one user message (without the placeholder, the file would be the system message and the
input the user message). `\{{` writes a literal `{{`; any other `{{name}}` is rejected before
any model call.

```bash
cd examples/providers/ollama
../../../evalci run --baseline-dir ./baseline
```

Requires a local Ollama server with `llama3.2:1b`. A 1B model is a noisy judge, so expect the
case to be reported UNRELIABLE (decision WARN) on many runs; that is the gate declining to
fail on a weak signal. Use a stronger judge for a stable result.

`semantic-similarity` needs an embedding-capable model. Pull one (for example
`ollama pull nomic-embed-text`) and add an assertion with
`embedding: { provider: ollama, model: nomic-embed-text }`. A server or model without
embeddings makes each run fail with the server's message and the case ERROR (exit 2).
