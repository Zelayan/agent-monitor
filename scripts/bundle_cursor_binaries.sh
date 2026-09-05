#!/usr/bin/env bash
# ==============================================================================
# Cross-Platform Binary Bundling Script for Cursor / VS Code Extension
# Compiles agent-monitor and agent-reporter for supported platform/arch targets:
#   - darwin-arm64
#   - darwin-x64
#   - linux-x64
#   - linux-arm64
#   - win32-x64
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CURSOR_BIN_DIR="${REPO_ROOT}/extensions/cursor/bin"

LDFLAGS="-s -w"
CGO_ENABLED=0
export CGO_ENABLED

TARGET_FILTER="${1:-all}"

# Map target names to GOOS, GOARCH, and executable suffix
TARGETS=(
  "darwin-arm64:darwin:arm64:"
  "darwin-x64:darwin:amd64:"
  "linux-x64:linux:amd64:"
  "linux-arm64:linux:arm64:"
  "win32-x64:windows:amd64:.exe"
)

echo "==> Preparing Cursor extension binary bundle in ${CURSOR_BIN_DIR}..."
mkdir -p "${CURSOR_BIN_DIR}"

cd "${REPO_ROOT}"

for entry in "${TARGETS[@]}"; do
  IFS=":" read -r target_name goos goarch ext <<< "${entry}"

  if [ "${TARGET_FILTER}" != "all" ] && [ "${TARGET_FILTER}" != "${target_name}" ]; then
    continue
  fi

  out_dir="${CURSOR_BIN_DIR}/${target_name}"
  mkdir -p "${out_dir}"

  monitor_bin="${out_dir}/agent-monitor${ext}"
  reporter_bin="${out_dir}/agent-reporter${ext}"

  echo "==> Compiling target: ${target_name} (GOOS=${goos} GOARCH=${goarch})..."

  GOOS="${goos}" GOARCH="${goarch}" go build -ldflags="${LDFLAGS}" -o "${monitor_bin}" .
  GOOS="${goos}" GOARCH="${goarch}" go build -ldflags="${LDFLAGS}" -o "${reporter_bin}" ./cmd/reporter

  if [ "${goos}" != "windows" ]; then
    chmod +x "${monitor_bin}" "${reporter_bin}"
  fi

  echo "    ✓ ${target_name}/agent-monitor${ext}"
  echo "    ✓ ${target_name}/agent-reporter${ext}"
done

echo "==> Generating checksums.json for bundled binaries..."
python3 - <<EOF
import os
import json
import hashlib

bin_dir = "${CURSOR_BIN_DIR}"
checksums = {}

if os.path.isdir(bin_dir):
    for entry in sorted(os.listdir(bin_dir)):
        sub_dir = os.path.join(bin_dir, entry)
        if os.path.isdir(sub_dir) and not entry.startswith('.'):
            target_map = {}
            for fname in sorted(os.listdir(sub_dir)):
                fpath = os.path.join(sub_dir, fname)
                if os.path.isfile(fpath):
                    h = hashlib.sha256()
                    with open(fpath, "rb") as f:
                        while chunk := f.read(65536):
                            h.update(chunk)
                    target_map[fname] = h.hexdigest()
            if target_map:
                checksums[entry] = target_map

checksums_path = os.path.join(bin_dir, "checksums.json")
with open(checksums_path, "w", encoding="utf-8") as f:
    json.dump(checksums, f, indent=2)

print(f"    ✓ Checksums recorded for {len(checksums)} target platform(s) at {checksums_path}")
EOF

echo "==> Cursor extension binary bundling completed successfully!"
