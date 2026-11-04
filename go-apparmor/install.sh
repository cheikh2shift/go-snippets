#!/bin/bash
# install.sh — Build, install, and load the AppArmor profile for the
# Go AppArmor experiment.
#
# Usage: sudo ./install.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY_NAME="go-apparmor"
PROFILE_NAME="go-apparmor"
TARGET_BIN="/usr/local/bin/${BINARY_NAME}"
PROFILE_DST="/etc/apparmor.d/${PROFILE_NAME}"

echo "=== Building Go binary ==="
cd "${SCRIPT_DIR}"
go build -o "${BINARY_NAME}" main.go
echo "  Built: ${BINARY_NAME}"

echo "=== Installing binary to ${TARGET_BIN} ==="
install -m 0755 "${BINARY_NAME}" "${TARGET_BIN}"

echo "=== Generating and installing AppArmor profile to ${PROFILE_DST} ==="
cat << 'EOF' > "${PROFILE_DST}"
# AppArmor profile for the Go AppArmor experiment binary.
# This profile is intentionally restrictive: it allows only what the
# experiment needs to demonstrate confinement, and denies everything else.
# Denials are logged to the kernel audit log so you can observe them.

#include <tunables/global>

profile go-apparmor /usr/local/bin/go-apparmor {
    # --- Include base abstractions for libc, dynamic loader, etc. ---
    #include <abstractions/base>

    # --- Filesystem: read-only access to a curated set of paths ---
    # Allow reading /etc/hostname (demonstrates allowed read)
    /etc/hostname r,

    # Allow reading /proc/self/status (capability probe)
    /proc/self/status r,

    # Allow writing to /tmp only (demonstrates allowed write)
    /tmp/** rw,

    # Deny everything else under /etc (covers /etc/shadow, /etc/passwd, etc.)
    deny /etc/** r,

    # --- Network: deny all outbound network ---
    deny network,

    # --- Capabilities: deny all capabilities ---
    deny capability,

    # --- Process execution: deny exec of arbitrary binaries ---
    deny /bin/ls x,
    deny /usr/bin/** x,
    deny /bin/sh x,
    deny /bin/bash x,
    deny /bin/** x,

    # --- Signals: deny sending signals to arbitrary processes ---
    deny signal,

    # --- Ptrace: deny ptrace ---
    deny ptrace,

    # --- Mount: deny mounts ---
    deny mount,
    deny umount,
}
EOF
chmod 0644 "${PROFILE_DST}"

echo "=== Reloading AppArmor parser ==="
apparmor_parser -W "${PROFILE_DST}"

echo "=== Reloading AppArmor service ==="
if systemctl is-active --quiet apparmor 2>/dev/null; then
    systemctl reload apparmor
else
    # Fallback: use apparmor_parser to load
    apparmor_parser -r "${PROFILE_DST}"
fi

echo ""
echo "=== Done ==="
echo "Profile loaded. Run with:"
echo "  sudo aa-exec -p ${PROFILE_NAME} -- ${TARGET_BIN}"
echo ""
echo "To observe denials in real time:"
echo "  sudo journalctl -kf --grep=apparmor"