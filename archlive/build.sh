#!/usr/bin/env bash
# Build the Hybrid-Edge Backup Appliance ISO using archiso.
#
# Prerequisites:
#   sudo pacman -S archiso qemu-full
#
# Usage:
#   sudo ./build.sh
#
# The output ISO will be in ./out/

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK_DIR="/tmp/archiso-tmp"
OUT_DIR="${SCRIPT_DIR}/out"

# Ensure the Go agent binary is present.
AGENT_BIN="${SCRIPT_DIR}/airootfs/usr/local/bin/backup-agent"
if [[ ! -f "${AGENT_BIN}" ]]; then
    echo "Error: backup-agent binary not found at ${AGENT_BIN}"
    echo "Build it first with: make iso-prep (from the project root)"
    exit 1
fi

# Clean previous build artifacts.
rm -rf "${WORK_DIR}"
mkdir -p "${OUT_DIR}"

echo "==> Building Hybrid-Edge Backup Appliance ISO..."
mkarchiso -v -w "${WORK_DIR}" -o "${OUT_DIR}" "${SCRIPT_DIR}"

echo "==> ISO built successfully!"
echo "    Output: ${OUT_DIR}/"
ls -lh "${OUT_DIR}"/*.iso 2>/dev/null || echo "    (no ISO found — check build output)"
