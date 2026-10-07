#!/usr/bin/env bash
# Demonstrates and checks diff-aware scoping and run history, offline: it builds evalci and the
# mock Ollama server, creates a demo git repository, and runs scenarios from
# specs/002-diff-scoping-storage/quickstart.md, asserting what ran and what was saved.
# Needs Go and git (git only to build the demo repository). History checks use the sqlite3
# command-line tool when it is installed.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PORT="${MOCK_PORT:-18080}"
WORK="$(mktemp -d)"
REPO="$WORK/repo"
HISTORY="$WORK/history.db"
OUT="$WORK/result.json"
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

"$ROOT/examples/scoping/setup.sh" "$REPO" "$HISTORY" "$PORT"
cd "$REPO"

FAILURES=0
STDOUT=""; STDERR=""; CODE=0
ok()   { echo "  ok   $1"; }
bad()  { echo "  FAIL $1"; FAILURES=$((FAILURES + 1)); }

# run_evalci [args...]: runs `evalci run --base main` and captures stdout, stderr, exit code.
run_evalci() {
  CODE=0
  STDOUT="$("$WORK/evalci" run --base main --output "$OUT" --verbose "$@" 2>"$WORK/stderr")" || CODE=$?
  STDERR="$(cat "$WORK/stderr")"
}
expect_contains()     { [[ "$STDOUT" == *"$1"* ]] && ok "$2" || { bad "$2 (stdout lacks: $1)"; echo "$STDOUT"; }; }
expect_not_contains() { [[ "$STDOUT" != *"$1"* ]] && ok "$2" || bad "$2 (stdout has: $1)"; }
expect_exit()         { [[ "$CODE" == "$1" ]] && ok "exit code $1" || bad "exit code $CODE, want $1"; }
# model_calls <suite>: number of per-call log lines for the suite (stderr, --verbose).
model_calls()         { grep -c "^call suite=$1 " "$WORK/stderr" || true; }
expect_calls()        { local n; n="$(model_calls "$1")"; [[ "$n" == "$2" ]] && ok "$1: $2 model call(s)" || bad "$1: $n model call(s), want $2"; }
reset_branch()        { git checkout -q feature/x && git reset -q --hard main; }
commit_all()          { git add -A && git commit -q -m "$1"; }

scenario() { echo; echo "=== $1"; }

scenario "1. a prompt change runs only the suite using it"
reset_branch
echo "GOOD " >prompts/support.txt; commit_all "tweak support prompt"
run_evalci
expect_exit 0
expect_contains "1 of 3 suites selected" "one suite selected"
expect_contains "support  RUN   prompt file changed: prompts/support.txt" "support selected for its prompt"
expect_contains "billing  SKIP  no related changes" "billing skipped"
expect_calls support 2; expect_calls billing 0; expect_calls legacy 0

scenario "2. a change touching no suite runs nothing and passes"
reset_branch
echo "changed" >README.md; commit_all "docs only"
run_evalci
expect_exit 0
expect_contains "Decision: PASS (no suites affected)" "nothing affected"
expect_calls support 0; expect_calls billing 0; expect_calls legacy 0

scenario "3. a new suite runs without a baseline and cannot fail the gate"
reset_branch
sed 's/^name: support/name: fresh/; s#prompts/support.txt#prompts/fresh.txt#' evals/support.eval.yaml >evals/fresh.eval.yaml
echo GOOD >prompts/fresh.txt; commit_all "add suite"
run_evalci
expect_exit 0
expect_contains "fresh    RUN   new suite" "new suite selected"
expect_contains "NEW" "NEW status shown"
expect_calls fresh 1

scenario "4. a deleted suite is reported and not run"
reset_branch
git rm -q evals/legacy.eval.yaml prompts/legacy.txt; commit_all "delete legacy"
run_evalci
expect_exit 0
expect_contains "legacy   REMOVED  suite removed" "removed suite reported"
expect_calls legacy 0

scenario "5. a run-affecting settings change selects every suite"
reset_branch
sed -i.bak 's/runs_per_case: 2/runs_per_case: 3/' .evalci.yaml && rm -f .evalci.yaml.bak; commit_all "more runs per case"
run_evalci
expect_exit 0
expect_contains "3 of 3 suites selected" "all suites selected"
expect_contains "settings changed" "reason shown"

scenario "6. changing only a price selects nothing"
reset_branch
sed -i.bak 's/input_per_million: 1.00/input_per_million: 9.00/' .evalci.yaml && rm -f .evalci.yaml.bak; commit_all "price change"
run_evalci
expect_exit 0
expect_contains "no suites affected" "price-only change selects nothing"
expect_calls support 0

scenario "7. --full runs every suite"
reset_branch
echo "changed" >README.md; commit_all "docs only"
run_evalci --full
expect_exit 0
expect_contains "full run requested; 3 of 3 suites selected" "everything selected"
expect_calls support 2; expect_calls billing 2; expect_calls legacy 2

scenario "8. a regression fails the gate (baseline comes from the base branch)"
reset_branch
echo BAD >prompts/support.txt; commit_all "break support"
run_evalci
expect_exit 1
expect_contains "Decision: FAIL" "regression fails"
expect_contains "REGRESSED" "REGRESSED shown"

scenario "9. refusals happen before any model call"
reset_branch
CODE=0; "$WORK/evalci" run --base does-not-exist --output "$OUT" >/dev/null 2>"$WORK/stderr" || CODE=$?
[[ "$CODE" == 2 ]] && ok "unresolvable base: exit 2" || bad "unresolvable base: exit $CODE"
CODE=0; "$WORK/evalci" run --base main --baseline-dir x --output "$OUT" >/dev/null 2>"$WORK/stderr" || CODE=$?
[[ "$CODE" == 2 ]] && ok "both baseline sources: exit 2" || bad "both baseline sources: exit $CODE"

scenario "10. history is saved (needs the sqlite3 command-line tool)"
if command -v sqlite3 >/dev/null 2>&1; then
  RUNS="$(sqlite3 "$HISTORY" 'SELECT COUNT(*) FROM runs')"
  [[ "$RUNS" -ge 8 ]] && ok "one record per finished run ($RUNS saved)" || bad "only $RUNS records saved"
  FAILS="$(sqlite3 "$HISTORY" "SELECT COUNT(*) FROM runs WHERE decision='FAIL' AND exit_code=1")"
  [[ "$FAILS" == 1 ]] && ok "the FAIL run is recorded with exit code 1" || bad "FAIL rows: $FAILS"
  PRICED="$(sqlite3 "$HISTORY" "SELECT COUNT(*) FROM runs WHERE total_cost_micros > 0")"
  [[ "$PRICED" -ge 1 ]] && ok "cost is estimated from the configured price" || bad "no priced run found"
  LEAK="$(sqlite3 "$HISTORY" "SELECT COUNT(*) FROM pragma_table_info('run_cases') WHERE name IN ('response','prompt','input')")"
  [[ "$LEAK" == 0 ]] && ok "no column holds responses, prompts, or inputs" || bad "content column found"
  echo "    latest runs:"; sqlite3 -header -column "$HISTORY" "SELECT substr(id,1,24) AS id, decision, exit_code, mode, total_input_tokens AS in_tok FROM runs ORDER BY started_at DESC LIMIT 3" | sed 's/^/      /'
else
  echo "  skipped (sqlite3 not installed)"
fi

scenario "11. unknown cost is shown as unknown, never zero"
reset_branch
sed -i.bak '/^prices:/,/^$/d' .evalci.yaml && rm -f .evalci.yaml.bak; commit_all "drop the price"
run_evalci --full
expect_contains "est. unknown (cost unknown for 1 model)" "unknown cost flagged"
if command -v sqlite3 >/dev/null 2>&1; then
  NULLS="$(sqlite3 "$HISTORY" "SELECT COUNT(*) FROM runs WHERE total_cost_micros IS NULL AND cost_partial = 1")"
  [[ "$NULLS" -ge 1 ]] && ok "stored cost is NULL (not 0)" || bad "no NULL-cost record"
fi

echo
if [[ "$FAILURES" -eq 0 ]]; then
  echo "all scoping scenarios passed"
else
  echo "$FAILURES check(s) failed"
  exit 1
fi
