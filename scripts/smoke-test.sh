#!/usr/bin/env bash
set -euo pipefail

URL="${1:-http://127.0.0.1:8080}"

curl -fsS "$URL/healthz" >/dev/null
curl -fsS "$URL/api/random" | grep -q .