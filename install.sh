#!/usr/bin/env bash
set -e

# ==========================================================
# Agent Monitor & Reporter 一键安装脚本
# 支持系统: macOS (Apple Silicon / Intel), Linux (x86_64 / arm64)
# ==========================================================

REPO="Zelayan/agent-monitor"
GITHUB_URL="https://github.com/${REPO}"

echo "==> Detecting operating system and architecture..."

# 1. 检测操作系统
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

# 2. 检测 CPU 架构
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

# 3. 获取版本号（默认获取最新 Release）
if [ -z "${VERSION}" ]; then
  echo "==> Fetching latest release tag from GitHub..."
  VERSION=$(curl -sL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
  if [ -z "${VERSION}" ]; then
    VERSION="v1.0.0"
    echo "    Warning: Failed to fetch latest tag, defaulting to ${VERSION}"
  else
    echo "    Latest version: ${VERSION}"
  fi
fi

# 4. 下载发布包
ARCHIVE="agent-monitor_${VERSION}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="${GITHUB_URL}/releases/download/${VERSION}/${ARCHIVE}"
TMP_DIR=$(mktemp -d)
trap 'rm -rf "${TMP_DIR}"' EXIT

echo "==> Downloading ${DOWNLOAD_URL}..."
if ! curl -sSL -f "${DOWNLOAD_URL}" -o "${TMP_DIR}/${ARCHIVE}"; then
  echo "Error: Failed to download release asset." >&2
  echo "Please check if the release exists at: ${GITHUB_URL}/releases/tag/${VERSION}" >&2
  exit 1
fi

echo "==> Extracting archive..."
tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "${TMP_DIR}"

EXTRACTED_DIR="${TMP_DIR}/agent-monitor_${VERSION}_${OS}_${ARCH}"
if [ ! -d "${EXTRACTED_DIR}" ]; then
  # 兼容平铺解压情况
  EXTRACTED_DIR="${TMP_DIR}"
fi

# 5. 确定安装路径
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

echo "==> Installing binaries to ${INSTALL_DIR}..."
if [ "${USE_SUDO}" = true ]; then
  echo "    (Root permissions required to write to /usr/local/bin)"
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
