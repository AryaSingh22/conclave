#!/usr/bin/env bash
# Internal helper used during scaffolding: strict fmt + lint + test.
set -uo pipefail
export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"
cd "$HOME/conclave"

echo "=== gofmt ==="
fmtout=$(gofmt -l .)
if [ -n "$fmtout" ]; then
  echo "auto-formatting: $fmtout"
  gofmt -w .
fi

echo "=== make lint ==="
make lint 2>&1 | tail -40
lintrc=${PIPESTATUS[0]}

echo "=== make test ==="
make test 2>&1 | tail -40
testrc=${PIPESTATUS[0]}

echo "=== lint exit: $lintrc | test exit: $testrc ==="
[ "$lintrc" -eq 0 ] && [ "$testrc" -eq 0 ]
