#!/usr/bin/env bash
# Builds a demo git repository for diff-aware scoping.
#
#   examples/scoping/setup.sh <dir> <history-db-path> [mock-port]
#
# The repository has three suites (support, billing, legacy), each with its own prompt, on
# branch main, plus a checked-out branch feature/x. It needs the `git` command-line tool only
# to build the demo; evalci itself never calls git.
set -euo pipefail

DIR="${1:?usage: setup.sh <dir> <history-db-path> [mock-port]}"
HISTORY="${2:?usage: setup.sh <dir> <history-db-path> [mock-port]}"
PORT="${3:-18080}"

mkdir -p "$DIR"
cd "$DIR"
git init -q -b main
git config user.name evalci-demo
git config user.email demo@example.com

cat >.evalci.yaml <<YAML
runs_per_case: 2

providers:
  ollama:
    base_url: http://127.0.0.1:$PORT

# Prices are optional. A model without one has unknown (never zero) cost.
prices:
  ollama/mock:
    input_per_million: 1.00
    output_per_million: 2.00

storage:
  driver: sqlite
  path: $HISTORY
YAML

mkdir -p evals prompts
for name in support billing legacy; do
  cat >"evals/$name.eval.yaml" <<YAML
name: $name
prompt: prompts/$name.txt
model: {provider: ollama, name: mock, temperature: 0}
cases:
  - {id: case-1, input: "question", assertions: [{type: exact-match, expected: GOOD}]}
YAML
  echo GOOD >"prompts/$name.txt"
done
echo "demo repository" >README.md

git add -A
git commit -q -m "base"
git checkout -q -b feature/x
