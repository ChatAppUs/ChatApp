#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

python3 tests/parity_check.py
python3 scripts/validate-feature-registry.py
python3 -m py_compile tests/*.py scripts/*.py services/ml/*.py

if command -v go >/dev/null 2>&1; then
  for service in services/mesh services/api services/sfu; do
    (cd "$service" && go test -count=1 ./... && go vet ./...)
  done
else
  echo "SKIP: Go toolchain is not installed; run Go tests and vet in CI or install Go 1.25+." >&2
fi

if command -v npm >/dev/null 2>&1; then
  for app in apps/web apps/admin; do
    (cd "$app" && npm ci --no-audit --no-fund && npm run typecheck && npm run build)
  done
else
  echo "SKIP: Node/npm is not installed; run frontend checks in CI or install Node 22+." >&2
fi

if command -v g++ >/dev/null 2>&1; then
  for service in counters media realtime sfu-forwarder transcode; do
    g++ -std=c++17 -O2 -Wall -Wextra -Werror -pthread \
      -o "/tmp/chatapp-$service" "services/$service/main.cpp"
  done
else
  echo "SKIP: g++ is not installed; run C++17 data-plane compilation in CI or install g++." >&2
fi

echo "Local environment-independent validation completed."
