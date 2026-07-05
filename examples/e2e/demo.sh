#!/usr/bin/env bash
#
# Copyright 2026 Sylvester Francis. Licensed under the Apache License, Version 2.0.
#
# End-to-end demo of the whole stack: leash governs a drover agent, offline.
#
#   drover run (openai client)  ->  leash serve  ->  fake OpenAI upstream
#
# The fake upstream returns a tool call plus a usage block every turn, so the agent
# keeps looping; leash meters each call against a price table. Three scenarios:
#
#   1. Cost budget (terminal): when the dollar budget trips, leash returns 429
#      without Retry-After; drover maps it to Stopped: cost_budget and finishes Done.
#   2. Rate limit (transient): leash returns 429 with Retry-After; drover Sleeps
#      durably and resumes, then a budget stops it. This is the transient-vs-terminal
#      distinction the governor seam draws.
#   3. Governed Gemini: drover --provider gemini routes through leash to Gemini's
#      OpenAI-compatible path (/v1beta/openai/chat/completions, not a doubled /v1),
#      proving the provider-path routing end to end.
#
# No API key and no real model: fully offline and deterministic. leash is pulled
# with "go install ...@latest", so no leash checkout is needed.
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$DIR/../.." && pwd)"
FAKE_PORT="${FAKE_PORT:-18080}"
LEASH_PORT="${LEASH_PORT:-18088}"
LEASH_PORT2="${LEASH_PORT2:-18089}"
LEASH_PORT3="${LEASH_PORT3:-18090}"
TMP="$(mktemp -d)"

cleanup() {
  for pid in "${FAKE_PID:-}" "${LEASH_PID:-}" "${LEASH_PID2:-}" "${LEASH_PID3:-}"; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
  done
  rm -rf "$TMP"
}
trap cleanup EXIT

wait_port() { # wait_port PORT: succeed once something is listening on it
  for _ in $(seq 1 100); do
    (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null && { exec 3>&- 3<&-; return 0; }
    sleep 0.2
  done
  return 1
}

echo "==> building binaries (drover and the fake upstream from this repo; leash from @latest)"
go -C "$ROOT" build -o "$TMP/fakeupstream" ./examples/e2e/fakeupstream
go -C "$ROOT" build -o "$TMP/drover" ./cmd/drover
GOBIN="$TMP" go install github.com/sylvester-francis/leash/cmd/leash@latest

echo "==> starting the fake OpenAI upstream on 127.0.0.1:$FAKE_PORT"
FAKE_UPSTREAM_ADDR="127.0.0.1:$FAKE_PORT" "$TMP/fakeupstream" >"$TMP/fake.log" 2>&1 &
FAKE_PID=$!
disown "$FAKE_PID" 2>/dev/null || true
wait_port "$FAKE_PORT" || { echo "fake upstream did not come up"; cat "$TMP/fake.log"; exit 1; }

# ---- Scenario 1: a dollar budget stops the agent (terminal) -------------------
echo
echo "### Scenario 1: a \$0.10 budget stops the agent (cost_budget, terminal)"
"$TMP/leash" serve \
  --listen "127.0.0.1:$LEASH_PORT" --upstream "http://127.0.0.1:$FAKE_PORT" \
  --prices "$DIR/prices.json" --max-cost 0.10 --max-calls 0 --insecure \
  --db "$TMP/leash1.db" --log-format text >"$TMP/leash1.log" 2>&1 &
LEASH_PID=$!
disown "$LEASH_PID" 2>/dev/null || true
wait_port "$LEASH_PORT" || { echo "leash did not come up"; cat "$TMP/leash1.log"; exit 1; }

"$TMP/drover" run --provider openai --model demo-model \
  --leash-url "http://127.0.0.1:$LEASH_PORT" --db "$TMP/drover1.db" --run cost-run \
  --goal "keep working until the governor stops you" 2>&1 | tee "$TMP/cost.out"

echo
echo "  leash's stop line:"
grep -i "stopped run" "$TMP/leash1.log" | tail -1 | sed 's/^/    /' || true

if grep -q "stopped after" "$TMP/cost.out" && grep -q "cost_budget" "$TMP/cost.out"; then
  echo "  PASS: leash metered the loop and stopped the run on its budget; drover finished Done."
else
  echo "  FAIL: expected drover to finish Done with Stopped: cost_budget."
  cat "$TMP/cost.out"; tail -n 20 "$TMP/leash1.log"; exit 1
fi

# ---- Scenario 2: a rate limit makes it Sleep and resume (transient) -----------
echo
echo "### Scenario 2: a rate limit throttles the agent (durable Sleep, then resume)"
"$TMP/leash" serve \
  --listen "127.0.0.1:$LEASH_PORT2" --upstream "http://127.0.0.1:$FAKE_PORT" \
  --prices "$DIR/prices.json" --rate 3000/1s --max-cost 0.10 --max-calls 0 --insecure \
  --db "$TMP/leash2.db" --log-format text >"$TMP/leash2.log" 2>&1 &
LEASH_PID2=$!
disown "$LEASH_PID2" 2>/dev/null || true
wait_port "$LEASH_PORT2" || { echo "leash (rate) did not come up"; cat "$TMP/leash2.log"; exit 1; }

"$TMP/drover" run --provider openai --model demo-model \
  --leash-url "http://127.0.0.1:$LEASH_PORT2" --db "$TMP/drover2.db" --run rate-run \
  --goal "keep working until the governor stops you" 2>&1 | tee "$TMP/rate.out"

if grep -q "sleep:" "$TMP/rate.out" && grep -q "stopped after" "$TMP/rate.out"; then
  echo "  PASS: the rate limit returned 429 with Retry-After; drover Slept durably (a sleep step) and"
  echo "        resumed, then the cost budget stopped it. Transient throttle, terminal stop."
else
  echo "  FAIL: expected a durable sleep then a clean stop under the rate limit."
  cat "$TMP/rate.out"; tail -n 20 "$TMP/leash2.log"; exit 1
fi

# ---- Scenario 3: governed Gemini routes to the Gemini OpenAI-compatible path -----
echo
echo "### Scenario 3: governed Gemini through leash (provider-path routing)"
"$TMP/leash" serve \
  --listen "127.0.0.1:$LEASH_PORT3" --upstream "http://127.0.0.1:$FAKE_PORT/v1beta/openai" \
  --prices "$DIR/prices.json" --max-cost 0.06 --max-calls 0 --insecure \
  --db "$TMP/leash3.db" --log-format text >"$TMP/leash3.log" 2>&1 &
LEASH_PID3=$!
disown "$LEASH_PID3" 2>/dev/null || true
wait_port "$LEASH_PORT3" || { echo "leash (gemini) did not come up"; cat "$TMP/leash3.log"; exit 1; }

"$TMP/drover" run --provider gemini --model gemini-demo \
  --leash-url "http://127.0.0.1:$LEASH_PORT3" --db "$TMP/drover3.db" --run gemini-run \
  --goal "keep working until the governor stops you" 2>&1 | tee "$TMP/gemini.out"

echo
echo "  the upstream saw Gemini's OpenAI-compatible path (a doubled /v1 would mean misrouting):"
grep -m1 "POST /v1beta/openai/chat/completions" "$TMP/fake.log" | sed 's/^/    /' || true

if grep -q "stopped after" "$TMP/gemini.out" && grep -q "cost_budget" "$TMP/gemini.out" \
   && grep -q "POST /v1beta/openai/chat/completions" "$TMP/fake.log"; then
  echo "  PASS: drover --provider gemini routed through leash to /v1beta/openai/chat/completions,"
  echo "        leash metered it as OpenAI-compatible, and the budget stopped the run."
else
  echo "  FAIL: governed Gemini did not route to the Gemini path or stop as expected."
  cat "$TMP/gemini.out"; tail -n 20 "$TMP/leash3.log"; exit 1
fi

echo
echo "ALL SCENARIOS PASSED: leash + drover, end to end, offline."
