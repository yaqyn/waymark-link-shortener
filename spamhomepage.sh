#!/usr/bin/env bash

set -euo pipefail
for i in {1..5000}; do
  curl -sS "http://localhost:8899" > /dev/null
  if (( i % 100 == 0 )); then
    echo "Completed $i requests"
  fi
done
