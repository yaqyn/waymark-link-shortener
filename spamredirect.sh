#!/usr/bin/env bash
# Development load helper. Create a fresh link through the API; never overwrite data files.
set -euo pipefail
iterations=${1:-200}
if ! [[ "$iterations" =~ ^[0-9]+$ ]] || (( iterations < 1 || iterations > 5000 )); then
  echo "Usage: $0 [requests: 1–5000]" >&2
  exit 1
fi
base_url=${WAYMARK_BASE_URL:-http://localhost:8899}
short_code=$(curl --fail-with-body -sS \
  -u "${WAYMARK_TEST_USERNAME:-frodo}:${WAYMARK_TEST_PASSWORD:-ofTheNineFingers}" \
  --data-urlencode 'url=https://example.com' "$base_url/api/shorten")
for ((i = 1; i <= iterations; i++)); do
  curl --fail -sS "$base_url/$short_code" > /dev/null
  if (( i % 100 == 0 )); then echo "Completed $i requests"; fi
done
