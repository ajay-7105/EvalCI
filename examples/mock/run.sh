#!/usr/bin/env bash
# Runs the real evalci binary against a mock Ollama server and asserts the decision and exit
# code of three scenarios. Needs Go; needs no model and no network.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PORT="${MOCK_PORT:-18080}"
WORK="$(mktemp -d)"
MOCK_PID=""
cleanup() {
  if [[ -n "$MOCK_PID" ]]; then
    kill "$MOCK_PID" 2>/dev/null || true
    wait "$MOCK_PID" 2>/dev/null || true
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

echo "building evalci and the mock server..."
(cd "$ROOT" && CGO_ENABLED=0 go build -o "$WORK/evalci" ./cmd/evalci && go build -o "$WORK/mockserver" ./examples/mock/mockserver)

"$WORK/mockserver" -addr "127.0.0.1:$PORT" 2>/dev/null &
MOCK_PID=$!
for _ in $(seq 1 50); do
  (exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null && break
  sleep 0.1
done

# write_suite DIR TEMPERATURE writes a repo-shaped dir with one suite of three cases.
write_suite() {
  local dir="$1" temp="$2"
  mkdir -p "$dir/evals" "$dir/prompts"
  cat >"$dir/evals/demo.eval.yaml" <<YAML
name: demo
prompt: prompts/p.txt
model: {provider: ollama, name: mock, temperature: $temp}
cases:
  - {id: case-a, input: "q1", assertions: [{type: exact-match, expected: GOOD}]}
  - {id: case-b, input: "q2", assertions: [{type: exact-match, expected: GOOD}]}
  - {id: case-flaky, input: "q3", critical: false, assertions: [{type: exact-match, expected: GOOD}]}
YAML
}

FAILURES=0
# scenario NAME TEMPERATURE BASELINE_PROMPT CANDIDATE_PROMPT WANT_DECISION WANT_EXIT
scenario() {
  local name="$1" temp="$2" base="$3" cand="$4" want="$5" want_exit="$6"
  local dir="$WORK/$name"
  mkdir -p "$dir"
  write_suite "$dir" "$temp"
  write_suite "$dir/baseline" "$temp"
  printf '%s\n' "$cand" >"$dir/prompts/p.txt"
  printf '%s\n' "$base" >"$dir/baseline/prompts/p.txt"
  cat >"$dir/.evalci.yaml" <<YAML
runs_per_case: 4
providers:
  ollama:
    base_url: http://127.0.0.1:$PORT
YAML

  echo
  echo "=== $name: expect $want (exit $want_exit)"
  local out code=0
  out="$(cd "$dir" && "$WORK/evalci" run --baseline-dir baseline 2>/dev/null)" || code=$?
  echo "$out"
  local json_decision json_exit
  json_decision="$(grep -o '"decision": "[A-Z]*"' "$dir/evalci-result.json" | head -1 | cut -d'"' -f4)"
  json_exit="$(grep -o '"exit_code": [0-9]*' "$dir/evalci-result.json" | head -1 | grep -o '[0-9]*$')"

  if [[ "$out" == *"Decision: $want"* && "$code" == "$want_exit" && "$json_decision" == "$want" && "$json_exit" == "$want_exit" ]]; then
    echo "OK   $name (stdout, JSON, and exit code agree: $want / $want_exit)"
  else
    echo "FAIL $name: stdout/JSON/exit disagree (exit=$code json=$json_decision/$json_exit)"
    FAILURES=$((FAILURES + 1))
  fi
}

scenario pass 0   GOOD    GOOD    PASS 0
scenario fail 0   GOOD    BAD     FAIL 1
scenario warn 0.8 NOISY-b NOISY-c WARN 0

echo
if [[ "$FAILURES" -eq 0 ]]; then
  echo "all mock scenarios passed"
else
  echo "$FAILURES scenario(s) failed"
  exit 1
fi
