#!/usr/bin/env bash
set -e

# ==========================================================
# Agent Monitor & Reporter 一键安装脚本
# 支持系统: macOS (Apple Silicon / Intel), Linux (x86_64 / arm64)
#
# 在线:   ./install.sh
# 离线:   在 linux-offline 解压目录里直接 ./install.sh（不访问网络）
#         或 ./install.sh ./agent-monitor_*_linux-offline.tar.gz
# 目录:   ./install.sh ./bin
# 镜像:   VERSION=v1.0.0-beta.3 BASE_URL=https://files.corp.local/am ./install.sh
# ==========================================================

REPO="Zelayan/agent-monitor"
GITHUB_URL="https://github.com/${REPO}"

usage() {
  cat <<'EOF'
Usage:
  install.sh                              Offline if binaries sit next to this script
                                          (linux-offline pack). Otherwise download GitHub.
  install.sh /path/to/package.tar.gz      Offline archive (no network)
                                          Linux: agent-monitor_*_linux-offline.tar.gz
  install.sh /path/to/dir                 Directory with agent-monitor + agent-reporter

Environment:
  VERSION             Release tag (required with BASE_URL)
  BASE_URL            Intranet prefix; fetches
                      $BASE_URL/agent-monitor_${VERSION}_${OS}_${ARCH}.tar.gz
  DOWNLOAD_URL        Full archive URL (skips GitHub API)
  INSTALL_DIR         Binary install path (default: /usr/local/bin or ~/.local/bin)
  INSTALL_SYSTEMD=0   Do not register systemd service
  FORCE_ONLINE=1      Ignore local binaries and download from GitHub / BASE_URL
EOF
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

echo "==> Detecting operating system and architecture..."

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "${OS}" in
  darwin) OS="darwin" ;;
  linux) OS="linux" ;;
  *)
    echo "Error: Unsupported operating system: ${OS}" >&2
    echo "For Windows, please download the zip release from: ${GITHUB_URL}/releases" >&2
    exit 1
    ;;
esac

ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Error: Unsupported CPU architecture: ${ARCH}" >&2
    exit 1
    ;;
esac

echo "    Platform: ${OS}/${ARCH}"

script_dir() {
  local src="${BASH_SOURCE[0]:-$0}"
  cd "$(dirname "$src")" && pwd
}

PACKAGE="${PACKAGE:-${1:-}}"
TMP_DIR=""
cleanup() {
  if [ -n "${TMP_DIR}" ]; then
    rm -rf "${TMP_DIR}"
  fi
}
trap cleanup EXIT

locate_payload() {
  local root="$1"
  local d candidates=()
  candidates+=("${root}")
  candidates+=("${root}/linux-${ARCH}")
  candidates+=("${root}/linux_${ARCH}")
  candidates+=("${root}/linux/${ARCH}")
  for d in "${root}"/*/; do
    [ -d "${d}" ] || continue
    candidates+=("${d%/}")
    candidates+=("${d%/}/linux-${ARCH}")
    candidates+=("${d%/}/linux_${ARCH}")
    candidates+=("${d%/}/linux/${ARCH}")
  done
  for d in "${candidates[@]}"; do
    if [ -f "${d}/agent-monitor" ] && [ -f "${d}/agent-reporter" ]; then
      printf '%s\n' "${d}"
      return 0
    fi
  done
  return 1
}

guess_version_from_name() {
  local name="$1"
  if [[ "${name}" =~ agent-monitor_(v[^_]+) ]]; then
    printf '%s\n' "${BASH_REMATCH[1]}"
  fi
}

EXTRACTED_DIR=""

if [ -z "${PACKAGE}" ] && [ "${FORCE_ONLINE:-0}" != "1" ]; then
  for _root in "$(script_dir)" "$(pwd)"; do
    if EXTRACTED_DIR="$(locate_payload "${_root}")"; then
      PACKAGE="${_root}"
      echo "==> Using bundled binaries in ${EXTRACTED_DIR} (offline)"
      if [ -z "${VERSION:-}" ]; then
        VERSION="$(guess_version_from_name "$(basename "${_root}")" || true)"
      fi
      break
    fi
  done
  unset _root
fi

if [ -n "${EXTRACTED_DIR}" ]; then
  :
elif [ -n "${PACKAGE}" ]; then
  if [ ! -e "${PACKAGE}" ]; then
    echo "Error: package not found: ${PACKAGE}" >&2
    exit 1
  fi
  if [ -d "${PACKAGE}" ]; then
    echo "==> Using local directory ${PACKAGE} (offline)"
    EXTRACTED_DIR="$(locate_payload "${PACKAGE}")" || {
      echo "Error: ${PACKAGE} must contain agent-monitor and agent-reporter binaries." >&2
      exit 1
    }
  else
    echo "==> Using local archive ${PACKAGE} (offline)"
    TMP_DIR="$(mktemp -d)"
    case "${PACKAGE}" in
      *.tar.gz|*.tgz)
        tar -xzf "${PACKAGE}" -C "${TMP_DIR}"
        ;;
      *.zip)
        if command -v unzip >/dev/null 2>&1; then
          unzip -q "${PACKAGE}" -d "${TMP_DIR}"
        else
          echo "Error: unzip is required for .zip packages." >&2
          exit 1
        fi
        ;;
      *)
        echo "Error: unsupported package (use .tar.gz, .tgz, .zip, or a directory)." >&2
        exit 1
        ;;
    esac
    EXTRACTED_DIR="$(locate_payload "${TMP_DIR}")" || {
      echo "Error: archive does not contain agent-monitor and agent-reporter." >&2
      exit 1
    }
    if [ -z "${VERSION:-}" ]; then
      VERSION="$(guess_version_from_name "$(basename "${PACKAGE}")" || true)"
    fi
  fi
else
  ARCHIVE_VERSION="${VERSION:-}"
  if [ -z "${DOWNLOAD_URL:-}" ] && [ -z "${BASE_URL:-}" ] && [ -z "${ARCHIVE_VERSION}" ]; then
    echo "==> Fetching latest release tag from GitHub..."
    ARCHIVE_VERSION=$(curl -sL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "${ARCHIVE_VERSION}" ]; then
      echo "Error: could not resolve latest release. For intranet use a local package or set VERSION and BASE_URL." >&2
      exit 1
    fi
    echo "    Latest version: ${ARCHIVE_VERSION}"
  fi

  if [ -z "${ARCHIVE_VERSION}" ]; then
    ARCHIVE_VERSION="${VERSION}"
  fi
  if [ -z "${ARCHIVE_VERSION}" ] && [ -z "${DOWNLOAD_URL:-}" ]; then
    echo "Error: VERSION is required when using BASE_URL." >&2
    exit 1
  fi
  VERSION="${ARCHIVE_VERSION}"
  ARCHIVE="agent-monitor_${VERSION}_${OS}_${ARCH}.tar.gz"

  if [ -z "${DOWNLOAD_URL:-}" ]; then
    if [ -n "${BASE_URL:-}" ]; then
      DOWNLOAD_URL="${BASE_URL%/}/${ARCHIVE}"
    else
      DOWNLOAD_URL="${GITHUB_URL}/releases/download/${VERSION}/${ARCHIVE}"
    fi
  fi

  TMP_DIR="$(mktemp -d)"
  echo "==> Downloading ${DOWNLOAD_URL}..."
  if ! curl -sSL -f "${DOWNLOAD_URL}" -o "${TMP_DIR}/${ARCHIVE}"; then
    echo "Error: Failed to download release asset." >&2
    echo "Intranet: copy the tar.gz in and run:  ./install.sh ./$(basename "${ARCHIVE}")" >&2
    exit 1
  fi
  tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "${TMP_DIR}"
  EXTRACTED_DIR="$(locate_payload "${TMP_DIR}")" || {
    echo "Error: downloaded archive does not contain expected binaries." >&2
    exit 1
  }
fi

if [ -n "${VERSION:-}" ]; then
  echo "    Version: ${VERSION}"
fi

if [ -z "${INSTALL_DIR:-}" ]; then
  INSTALL_DIR="/usr/local/bin"
  USE_SUDO=false
  if [ ! -w "${INSTALL_DIR}" ]; then
    if command -v sudo >/dev/null 2>&1 && [ -t 0 ]; then
      USE_SUDO=true
    else
      INSTALL_DIR="${HOME}/.local/bin"
      mkdir -p "${INSTALL_DIR}"
    fi
  fi
else
  USE_SUDO=false
  mkdir -p "${INSTALL_DIR}"
  if [ ! -w "${INSTALL_DIR}" ] && command -v sudo >/dev/null 2>&1 && [ -t 0 ]; then
    USE_SUDO=true
  fi
fi

echo "==> Installing binaries to ${INSTALL_DIR}..."
if [ "${USE_SUDO}" = true ]; then
  echo "    (Root permissions required to write to ${INSTALL_DIR})"
  sudo install -m 755 "${EXTRACTED_DIR}/agent-monitor" "${INSTALL_DIR}/agent-monitor"
  sudo install -m 755 "${EXTRACTED_DIR}/agent-reporter" "${INSTALL_DIR}/agent-reporter"
else
  install -m 755 "${EXTRACTED_DIR}/agent-monitor" "${INSTALL_DIR}/agent-monitor"
  install -m 755 "${EXTRACTED_DIR}/agent-reporter" "${INSTALL_DIR}/agent-reporter"
fi

install_linux_systemd() {
  if [ "${OS}" != "linux" ]; then
    return 0
  fi
  if [ "${INSTALL_SYSTEMD:-1}" = "0" ]; then
    echo "==> Skipping systemd (INSTALL_SYSTEMD=0). Start with: ${INSTALL_DIR}/agent-monitor"
    return 0
  fi
  if ! command -v systemctl >/dev/null 2>&1; then
    echo "==> systemd not found. Start with: ${INSTALL_DIR}/agent-monitor"
    return 0
  fi

  local exec_path="${INSTALL_DIR}/agent-monitor"
  if [ "$(id -u)" -eq 0 ]; then
    echo "==> Installing systemd system service..."
    mkdir -p /var/lib/agent-monitor/sessions
    cat > /etc/systemd/system/agent-monitor.service <<EOF
[Unit]
Description=Agent Monitor dashboard
Documentation=https://github.com/Zelayan/agent-monitor
After=network-online.target

[Service]
Type=simple
ExecStartPre=/bin/mkdir -p /var/lib/agent-monitor/sessions
ExecStart=${exec_path}
WorkingDirectory=/var/lib/agent-monitor
Environment=PORT=8000
Environment=DATA_DIR=/var/lib/agent-monitor/sessions
Restart=on-failure
RestartSec=2
TimeoutStopSec=10
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF
    if systemctl daemon-reload && systemctl enable --now agent-monitor.service; then
      echo "    enabled: systemctl status agent-monitor"
    else
      echo "Notice: failed to enable system service. Start with: ${exec_path}"
    fi
    return 0
  fi

  echo "==> Installing systemd user service..."
  if [ -z "${XDG_RUNTIME_DIR:-}" ]; then
    export XDG_RUNTIME_DIR="/run/user/$(id -u)"
  fi
  mkdir -p "${HOME}/.config/systemd/user" "${HOME}/.local/share/agent-monitor/sessions"
  cat > "${HOME}/.config/systemd/user/agent-monitor.service" <<EOF
[Unit]
Description=Agent Monitor dashboard
Documentation=https://github.com/Zelayan/agent-monitor
After=network-online.target

[Service]
Type=simple
ExecStartPre=/bin/mkdir -p %h/.local/share/agent-monitor/sessions
ExecStart=${exec_path}
WorkingDirectory=%h/.local/share/agent-monitor
Environment=PORT=8000
Environment=DATA_DIR=%h/.local/share/agent-monitor/sessions
Restart=on-failure
RestartSec=2
TimeoutStopSec=10
NoNewPrivileges=true

[Install]
WantedBy=default.target
EOF
  if systemctl --user daemon-reload && systemctl --user enable --now agent-monitor.service; then
    echo "    enabled: systemctl --user status agent-monitor"
  else
    echo "Notice: failed to enable user service (need a logged-in systemd session)."
    echo "  Start with: ${exec_path}"
    echo "  Or: systemctl --user enable --now agent-monitor"
  fi
}

install_linux_systemd

echo "=========================================================="
echo "✓ Agent Monitor and Reporter installed successfully!"
echo "  - ${INSTALL_DIR}/agent-monitor"
echo "  - ${INSTALL_DIR}/agent-reporter"
echo ""
echo "Usage:"
echo "  1. Dashboard:  http://127.0.0.1:8000"
if [ "${OS}" = "linux" ] && [ "${INSTALL_SYSTEMD:-1}" != "0" ]; then
  echo "  2. Service:    systemctl --user status agent-monitor"
  echo "  3. Logs:       journalctl --user -u agent-monitor -f"
else
  echo "  2. Start:      agent-monitor"
fi
echo "  Configure Hooks: See configs/ in repo"
echo "=========================================================="

if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
  echo "Notice: ${INSTALL_DIR} is not in your PATH."
  echo "Add it by running: export PATH=\"${INSTALL_DIR}:\$PATH\""
fi
