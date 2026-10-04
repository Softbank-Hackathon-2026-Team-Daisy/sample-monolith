#!/bin/sh
# Post-deploy smoke test for HelloCalc. Requires only sh and curl.
#
#   BASE_URL=http://localhost:8080 ./scripts/smoke-test.sh
#
# Environment:
#   BASE_URL          target (default http://localhost:8080)
#   WAIT_SECONDS      how long to wait for /readyz before testing (default 10)
#   TIMEOUT           per-request timeout in seconds (default 5)
#   EXPECTED_VERSION  if set, /version must report this version
#   EXPECTED_COMMIT   if set, /version must report this commit
#
# Exits 0 when every check passes, 1 otherwise.
set -u

BASE_URL=${BASE_URL:-http://localhost:8080}
BASE_URL=${BASE_URL%/}
WAIT_SECONDS=${WAIT_SECONDS:-10}
TIMEOUT=${TIMEOUT:-5}
EXPECTED_VERSION=${EXPECTED_VERSION:-}
EXPECTED_COMMIT=${EXPECTED_COMMIT:-}

if ! command -v curl >/dev/null 2>&1; then
  echo "smoke-test: curl is required" >&2
  exit 1
fi

body_file=$(mktemp)
header_file=$(mktemp)
trap 'rm -f "$body_file" "$header_file"' EXIT
trap 'exit 1' INT TERM

total=0
failed=0

# request METHOD PATH [JSON_BODY] -> sets $status; body in $body_file.
request() {
  if [ $# -ge 3 ]; then
    status=$(curl -sS --max-time "$TIMEOUT" -o "$body_file" -D "$header_file" -w '%{http_code}' \
      -X "$1" -H 'Content-Type: application/json' -H "X-Request-ID: $request_id" \
      --data "$3" "$BASE_URL$2") || status=000
  else
    status=$(curl -sS --max-time "$TIMEOUT" -o "$body_file" -D "$header_file" -w '%{http_code}' \
      -X "$1" -H "X-Request-ID: $request_id" "$BASE_URL$2") || status=000
  fi
}

pass() {
  total=$((total + 1))
  printf 'PASS  %s\n' "$1"
}

fail() {
  total=$((total + 1))
  failed=$((failed + 1))
  printf 'FAIL  %s: %s\n' "$1" "$2"
  printf '      body: %s\n' "$(head -c 300 "$body_file" | tr '\n' ' ')"
}

# check NAME METHOD PATH WANT_STATUS BODY_REGEX [JSON_BODY]
check() {
  name=$1 method=$2 path=$3 want=$4 pattern=$5
  shift 5
  request "$method" "$path" "$@"
  if [ "$status" != "$want" ]; then
    fail "$name" "HTTP $status, want $want"
  elif ! grep -Eq -- "$pattern" "$body_file"; then
    fail "$name" "body does not match /$pattern/"
  else
    pass "$name"
  fi
}

request_id="smoke-$$-$(date +%s)"
echo "HelloCalc smoke test against $BASE_URL"

# Wait for readiness so the script can run right after a deploy.
elapsed=0
while :; do
  request GET /readyz
  [ "$status" = 200 ] && break
  if [ "$elapsed" -ge "$WAIT_SECONDS" ]; then
    echo "      /readyz not ready after ${WAIT_SECONDS}s (last status $status)"
    break
  fi
  sleep 1
  elapsed=$((elapsed + 1))
done

check "GET / serves the UI"             GET  /        200 '<title>HelloCalc</title>'
check "GET /health"                     GET  /health  200 '"status": *"ok"'
# /healthz is not checked: Cloud Run reserves it and answers 404 before the request reaches the app
check "GET /readyz"                     GET  /readyz  200 '"status": *"ready"'
check "GET /version"                    GET  /version 200 '"name": *"HelloCalc"'
printf '      %s\n' "$(tr -d '\n' <"$body_file")"

if [ -n "$EXPECTED_VERSION" ]; then
  check "version is $EXPECTED_VERSION"  GET  /version 200 "\"version\": *\"$EXPECTED_VERSION\""
fi
if [ -n "$EXPECTED_COMMIT" ]; then
  check "commit is $EXPECTED_COMMIT"    GET  /version 200 "\"commit\": *\"$EXPECTED_COMMIT\""
fi

check "POST /api/calculate 12.5 * 4"    POST /api/calculate 200 '"result": *50[,}]' '{"left":12.5,"operator":"*","right":4}'
check "POST /api/calculate 10 / 4"      POST /api/calculate 200 '"result": *2\.5[,}]' '{"left":10,"operator":"/","right":4}'
check "POST /api/calculate -7 * 3"      POST /api/calculate 200 '"result": *-21[,}]' '{"left":-7,"operator":"*","right":3}'
check "POST /api/calculate 1 / 0"       POST /api/calculate 422 '"error": *"division by zero"' '{"left":1,"operator":"/","right":0}'
check "POST /api/calculate bad JSON"    POST /api/calculate 400 '"error"' '{"left":'

# The last response must echo our X-Request-ID.
if grep -iq "^x-request-id: *$request_id" "$header_file"; then
  pass "X-Request-ID is propagated"
else
  fail "X-Request-ID is propagated" "response did not echo $request_id"
fi

if [ "$failed" -ne 0 ]; then
  echo "FAILED: $failed of $total checks failed"
  exit 1
fi
echo "OK: all $total checks passed"
