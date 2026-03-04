#!/usr/bin/env bash
set -euo pipefail

# Usage:
#   ./loop.sh
#   ./loop.sh 20
#   ./loop.sh plan
#   ./loop.sh plan 5

MODE="build"
MAX_ITERATIONS=0

if [[ "${1:-}" == "plan" ]]; then
  MODE="plan"
  MAX_ITERATIONS="${2:-0}"
elif [[ "${1:-}" =~ ^[0-9]+$ ]]; then
  MODE="build"
  MAX_ITERATIONS="$1"
fi

PROMPT_FILE="PROMPT_${MODE}.md"
ITERATION=0
AUTO_PUSH="${AUTO_PUSH:-0}"
SLEEP_SECONDS="${SLEEP_SECONDS:-1}"
MODEL="${MODEL:-opus-4.6}"

if [[ ! -f "$PROMPT_FILE" ]]; then
  echo "Missing prompt file: $PROMPT_FILE"
  exit 1
fi

if ! command -v cursor-agent >/dev/null 2>&1; then
  echo "cursor-agent not found in PATH"
  exit 1
fi

echo "========================================"
echo "LegacyLens Ralph Loop"
echo "Mode: $MODE"
echo "Prompt: $PROMPT_FILE"
if [[ "$MAX_ITERATIONS" -gt 0 ]]; then
  echo "Max iterations: $MAX_ITERATIONS"
else
  echo "Max iterations: unlimited"
fi
echo "LLM tool: cursor-agent -p --model $MODEL"
echo "========================================"

while true; do
  if [[ "$MAX_ITERATIONS" -gt 0 && "$ITERATION" -ge "$MAX_ITERATIONS" ]]; then
    echo "Reached iteration limit: $MAX_ITERATIONS"
    break
  fi

  echo ""
  echo "----- Iteration $((ITERATION + 1)) -----"

  # Headless execution, prompt provided via stdin.
  if ! cat "$PROMPT_FILE" | cursor-agent -p --model "$MODEL"; then
    echo "Iteration failed. Sleeping before retry."
    sleep "$SLEEP_SECONDS"
    continue
  fi

  if [[ "$AUTO_PUSH" == "1" ]]; then
    BRANCH="$(git branch --show-current)"
    git push -u origin "$BRANCH" || true
  fi

  ITERATION=$((ITERATION + 1))
  sleep "$SLEEP_SECONDS"
done
