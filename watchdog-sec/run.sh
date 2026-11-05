#!/bin/bash
set -euo pipefail

# ──────────────────────────────────────────────
# systemd WatchdogSec experiment — run script
# ──────────────────────────────────────────────

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== systemd WatchdogSec — AI Agent Liveness Experiment ===${NC}"
echo ""

# ── 1. Build ───────────────────────────────────
echo -e "${YELLOW}[1/4] Building go-agent...${NC}"
go build -o go-agent . || {
    echo -e "${RED}Build failed. Run 'go mod download' first.${NC}"
    exit 1
}
echo "  ✓ Binary built: ./go-agent"
echo ""

# ── 2. Check systemd availability ───────────────
echo -e "${YELLOW}[2/4] Checking systemd...${NC}"
if ! command -v systemctl &>/dev/null; then
    echo -e "${RED}systemctl not found. This experiment requires systemd.${NC}"
    exit 1
fi
echo "  ✓ systemd found"
echo ""

# ── 3. Install unit file ────────────────────────
echo -e "${YELLOW}[3/4] Installing systemd unit...${NC}"
if [ "$(id -u)" -ne 0 ]; then
    echo -e "${RED}This script must be run as root (sudo) to install the systemd unit.${NC}"
    echo "  Run: sudo ./run.sh"
    exit 1
fi

cp ai-agent.service /etc/systemd/system/ai-agent.service
systemctl daemon-reload
echo "  ✓ Unit installed: /etc/systemd/system/ai-agent.service"
echo ""

# ── 4. Run experiments ──────────────────────────
echo -e "${YELLOW}[4/4] Running experiments...${NC}"
echo ""
echo "Choose an experiment:"
echo "  1) Normal run (no hang, watchdog active)"
echo "  2) Simulate hang (agent hangs after 10s, killed at 30s)"
echo "  3) Direct run (no systemd, for quick testing)"
echo "  4) systemd-run quick test"
echo ""
read -p "Enter choice [1-4]: " choice

case $choice in
    1)
        echo -e "${GREEN}Starting normal run...${NC}"
        systemctl start ai-agent
        echo "Watch with: journalctl -u ai-agent -f"
        echo "Stop with:  systemctl stop ai-agent"
        ;;
    2)
        echo -e "${GREEN}Starting hang simulation...${NC}"
        systemctl stop ai-agent 2>/dev/null || true
        systemctl start ai-agent -- -hang-after=10s
        echo "Watch with: journalctl -u ai-agent -f"
        echo ""
        echo -e "${YELLOW}Expected sequence:${NC}"
        echo "  T+0s:   Agent starts, pings every 5s"
        echo "  T+10s:  Agent prints 'SIMULATING HANG' and loops"
        echo "  T+30s:  systemd sends SIGKILL (WatchdogSec=30s)"
        echo "  T+35s:  systemd restarts (RestartSec=5s)"
        echo "  T+35s+: New agent starts fresh"
        ;;
    3)
        echo -e "${GREEN}Running directly (no systemd)...${NC}"
        echo "  No NOTIFY_SOCKET — watchdog inactive"
        echo "  Press Ctrl+C to stop"
        echo ""
        ./go-agent "$@"
        ;;
    4)
        echo -e "${GREEN}Running via systemd-run...${NC}"
        echo "  WatchdogSec=30s, no persistent unit"
        echo ""
        systemd-run --scope -p WatchdogSec=30s ./go-agent "$@"
        ;;
    *)
        echo -e "${RED}Invalid choice${NC}"
        exit 1
        ;;
esac

echo ""
echo -e "${GREEN}Done.${NC}"