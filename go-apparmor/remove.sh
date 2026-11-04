#!/bin/bash
# remove.sh — Unload and remove the AppArmor profile and experiment binary.
#
# Usage: sudo ./remove.sh

set -euo pipefail

PROFILE_NAME="go-apparmor"
PROFILE_DST="/etc/apparmor.d/${PROFILE_NAME}"
TARGET_BIN="/usr/local/bin/go-apparmor"

echo "=== Unloading AppArmor profile from kernel ==="
if aa-status --enabled 2>/dev/null && grep -q "${PROFILE_NAME}" /sys/kernel/security/apparmor/profiles 2>/dev/null; then
    # Unload the profile using apparmor_parser (-R for remove)
    apparmor_parser -R "${PROFILE_DST}" 2>/dev/null || true
    echo "  Unloaded: ${PROFILE_NAME}"
else
    echo "  Profile was not actively loaded."
fi

echo "=== Removing AppArmor profile file ==="
if [ -f "${PROFILE_DST}" ]; then
    rm -f "${PROFILE_DST}"
    echo "  Removed: ${PROFILE_DST}"
else
    echo "  Profile file not found at ${PROFILE_DST}"
fi

echo "=== Removing installed binary ==="
if [ -f "${TARGET_BIN}" ]; then
    rm -f "${TARGET_BIN}"
    echo "  Removed: ${TARGET_BIN}"
else
    echo "  Binary not found at ${TARGET_BIN}"
fi

echo "=== Reloading AppArmor service ==="
if systemctl is-active --quiet apparmor 2>/dev/null; then
    systemctl reload apparmor
    echo "  AppArmor service reloaded."
fi

echo ""
echo "=== Cleanup Complete ==="