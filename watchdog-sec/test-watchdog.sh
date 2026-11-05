#!/bin/bash
# systemd WatchdogSec — Quick Test (fixed)
# NOTE: do NOT use `set -e` — Test 2 is *expected* to fail (SIGKILL).
set -uo pipefail

echo "=== systemd WatchdogSec — Quick Test ==="

echo "[1/3] Building..."
go build -o go-agent . || { echo "✗ Build failed"; exit 1; }
echo "  ✓ Built"

echo ""
echo "[2/3] Test 1: Normal run (watchdog active, no hang, exits 0)"
systemd-run --wait --unit=ai-watchdog-test \
  --property=Type=notify \
  --property=WatchdogSec=10s \
  --property=NotifyAccess=main \
  ./go-agent -ping-interval=2s -normal-exit=3s
rc=$?
echo "  exit code: $rc (expected 0)"
if [ "$rc" -eq 0 ]; then
  echo "  ✓ PASS"
else
  echo "  ✗ FAIL — journal:"
  journalctl -u ai-watchdog-test --no-pager -n 30
  exit 1
fi

echo ""
echo "[3/3] Test 2: Hang simulation (hangs after 2s, watchdog kills at ~10s)"
# No `timeout` wrapper and no Ctrl-C: let systemd-run --wait return
# the real exit code (137 = 128+SIGKILL) on its own.
systemd-run --wait --unit=ai-watchdog-hang \
  --property=Type=notify \
  --property=WatchdogSec=10s \
  --property=NotifyAccess=main \
  ./go-agent -ping-interval=2s -hang-after=2s
rc=$?
echo "  exit code: $rc (expected non-zero, ideally 137)"
if [ "$rc" -ne 0 ]; then
  echo "  ✓ PASS — watchdog killed hung agent"
else
  echo "  ✗ FAIL — hung agent was NOT killed"
  exit 1
fi

echo ""
echo "Journal for hang test:"
journalctl -u ai-watchdog-hang --no-pager -n 40

echo ""
echo "All tests passed!"
