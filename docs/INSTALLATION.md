# 安装与部署指南 (Installation & Deployment)

本文档提供 **AGENT MONITOR** 在不同操作系统、网络环境（公网、局域网私有云、完全离线环境）下的完整安装与运维管理方案。

---

## 目录 (Table of Contents)

1. [一键安装脚本（推荐）](#1-一键安装脚本推荐)
2. [systemd 守护进程与服务管理](#2-systemd-守护进程与服务管理)
3. [内网与离线环境部署](#3-内网与离线环境部署)
4. [Docker 容器化运行](#4-docker-容器化运行)
5. [Go 原生工具链安装](#5-go-原生工具链安装)
6. [从源码本地构建](#6-从源码本地构建)
7. [局域网远程 IP 部署与 PWA / HTTPS 指南](#7-局域网远程-ip-部署与-pwa--https-指南)
8. [卸载与清理](#8-卸载与清理)

---

## 1. 安装 Cursor 专属扩展（推荐，开箱即用）

如果你使用 **Cursor IDE**，最推荐直接安装官方专属扩展 `agent-monitor-cursor`：
- **功能特性**：
  - 自动检测并拉起本地 `agent-monitor` 守护进程，无需手动启动服务；
  - 提供一键注入工作区 `.cursor/hooks.json` 命令，自动绑定绝对路径，杜绝 PATH 缺失；
  - 底部状态栏秒表计时，实时显示 Agent 运行与耗时；
  - 在 Cursor 内部直接分栏或在左侧活动栏查看 Agent Monitor 看板。

### 安装方法
1. 从 Releases 下载最新的 `agent-monitor-cursor-*.vsix`；
2. 在 Cursor 中按 `⌘⇧P`，执行 `Extensions: Install from VSIX...` 并选择该文件；
3. 或在终端运行：
   ```bash
   cursor --install-extension agent-monitor-cursor-1.0.0.vsix
   ```

---

## 2. 一键安装脚本（系统级 CLI / 独立服务）

通过官方安装脚本，全自动识别系统架构并拉取最新 Release，零外部运行时依赖：

```bash
curl -fsSL https://raw.githubusercontent.com/Zelayan/agent-monitor/main/install.sh | bash
```

- **Linux**：默认以 systemd **用户服务 (`systemctl --user`)** 安装，开机自启且无需 root 提权；若在 root 用户或无 systemd 环境下运行，则自动退化为标准二进制并写入系统 PATH。
- **macOS**：自动将 `agent-monitor` 和 `agent-reporter` 软链接或安装至 `~/.local/bin`（或 `/usr/local/bin`），并在终端提示直接运行命令。
- **Windows (WSL2)**：与 Linux 完全一致，支持直接作为用户守护进程拉起。

---

## 2. systemd 守护进程与服务管理

在 Linux 上，安装脚本会默认配置好 systemd 服务，支持开机自启、故障自动重启与日志捕获。

### 用户服务管理 (非 Root，默认模式)
```bash
# 查看服务状态
systemctl --user status agent-monitor

# 重启服务（更新二进制或配置后）
systemctl --user restart agent-monitor

# 查看实时日志
journalctl --user -u agent-monitor -f

# 允许用户退出登录后服务依然在后台常驻 (推荐服务器环境配置)
loginctl enable-linger "$USER"
```

### 端口与持久化目录自定义
用户服务配置文件位于 `~/.config/systemd/user/agent-monitor.service`，可通过环境变量重写：
```ini
[Service]
Environment="PORT=8000"
Environment="DATA_DIR=%h/.local/share/agent-monitor/sessions"
# Optional: LLM session titles (OpenAI-compatible, e.g. Ollama / vLLM)
# Environment="AGENT_MONITOR_LLM_BASE_URL=http://127.0.0.1:11434/v1"
# Environment="AGENT_MONITOR_LLM_MODEL=qwen2.5:7b"
# Environment="AGENT_MONITOR_LLM_API_KEY="
```
修改后执行重载：
```bash
systemctl --user daemon-reload
systemctl --user restart agent-monitor
```

### 可选：用 LLM 总结会话容器标题

默认关闭。未配置时看板使用本地清洗短标题（Prompt 首行），**不会发任何模型请求**，并保持离线可用。

配置 OpenAI 兼容的 Chat Completions 后，Monitor 在每一轮 `completed` / `failed` 收口后异步生成不超过约 24 字的会话总标题；失败或超时静默忽略，绝不阻断 Hook 上报。

**Cursor 用户优先在扩展设置里填写**（Settings 搜索 `Agent Monitor`）：`llmBaseUrl` / `llmModel` / `llmApiKey`。插件拉起守护进程时会注入 `AGENT_MONITOR_LLM_*`，不必手写 systemd 环境变量。由插件启动的 daemon 在改设置后会自动重启；若本机已有 systemd 等外部进程，需执行命令 **Agent Monitor: Restart Backend Daemon**，或先停掉外部服务再让插件拉起。

也可以直接导出环境变量（CLI / systemd / Docker）：

```bash
export AGENT_MONITOR_LLM_BASE_URL="http://127.0.0.1:11434/v1"   # 或 https://api.openai.com/v1
export AGENT_MONITOR_LLM_MODEL="qwen2.5:7b"                      # 或 gpt-4o-mini 等
export AGENT_MONITOR_LLM_API_KEY=""                              # 本地 Ollama 可留空
```

`agent-reporter` 热路径不会调用 LLM。首轮 Prompt 原文仍保存在 `rootGoal`，抽屉 GOAL 区可查看完整内容。

### 系统级服务 (Root 模式)
在多用户服务器环境中，如果你以 `root` 或 `sudo bash install.sh` 运行，服务将安装为全局系统服务：
- 服务单元文件：`/etc/systemd/system/agent-monitor.service`
- 数据保存目录：`/var/lib/agent-monitor`
- 管理命令：`sudo systemctl status/restart/stop agent-monitor`

---

## 3. 内网与离线环境部署

针对不能访问 GitHub 或处于隔离内网的开发环境，提供以下三种离线安装方案：

### 方案 A：使用离线归档包（推荐）
每个版本 Release 均提供免拉取的离线整合包 `agent-monitor_<version>_linux-offline.tar.gz`（内含安装脚本、全架构静态二进制及预设服务单元）：

1. 在有网环境下载离线包并拷贝至内网机；
2. 解压并直接执行内置安装脚本：
   ```bash
   tar -xzf agent-monitor_1.0.0-beta.8_linux-offline.tar.gz
   cd agent-monitor_1.0.0-beta.8_linux-offline
   ./install.sh
   ```
   脚本将优先使用包内自带的二进制完成安装，**全程 0 网络请求**。

### 方案 B：指定本地二进制目录安装
如果你已预先下载或编译好 `agent-monitor` 和 `agent-reporter`：
```bash
./install.sh /path/to/my-bins
```

### 方案 C：指向内网私有 HTTP 镜像源
如果企业内部搭建了文件镜像服务器（如 Nexus、MinIO 或内部 Nginx）：
```bash
VERSION=v1.0.0-beta.8 \
BASE_URL="http://internal-mirror.corp/agent-monitor" \
./install.sh
```

---

## 4. Docker 容器化运行

你可以将 Monitor 仪表盘运行在 Docker 容器中，并通过数据卷持久化所有历史任务数据：

```bash
# 从 GitHub Container Registry (GHCR) 运行
docker run -d \
  --name agent-monitor \
  --restart unless-stopped \
  -p 8000:8000 \
  -v $(pwd)/data/sessions:/app/data/sessions \
  ghcr.io/zelayan/agent-monitor:latest

# 或使用 Docker Hub 镜像
docker run -d \
  --name agent-monitor \
  --restart unless-stopped \
  -p 8000:8000 \
  -v $(pwd)/data/sessions:/app/data/sessions \
  zelayan/agent-monitor:latest
```

> **注意**：容器运行模式仅承载 Web 看板和 API 接收；宿主机上的各 Coding Agent（Claude Code、Cursor 等）仍需在本地安装 `agent-reporter` 拦截器，并将上报目标指向 `http://127.0.0.1:8000`。

---

## 5. Go 原生工具链安装

如果本地已有 Go 1.21+ 环境，可以直接通过 `go install` 一键下载并编译进 `$GOPATH/bin`：

```bash
# 1. 安装 Monitor Web 仪表盘主服务
go install github.com/Zelayan/agent-monitor@latest

# 2. 安装 Hook 上报器拦截器
go install github.com/Zelayan/agent-monitor/cmd/reporter@latest

# 运行服务
agent-monitor
```

---

## 6. 从源码本地构建

AGENT MONITOR 纯标准库实现，0 外部第三方库，秒级构建：

```bash
# 克隆仓库
git clone https://github.com/Zelayan/agent-monitor.git
cd agent-monitor

# 本地构建（生成 bin/agent-monitor 与 bin/agent-reporter）
make build

# 跨平台交叉编译（输出各平台全量产物至 dist/ 目录）
make build-all
```

构建后产物：
- `bin/agent-monitor`：Web 仪表盘服务（已内嵌所有离线前端页面、样式与 PWA 资源）。
- `bin/agent-reporter`：Hook 命令行拦截器。

---

## 7. 局域网远程 IP 部署与 PWA / HTTPS 指南

如果你将服务部署在另一台局域网主机（如 `192.168.x.x`）或私有云服务器上，并希望在客户端浏览器上正常使用 **PWA 独立桌面 App 安装**，由于跨 IP 访问受到浏览器安全上下文限制，需要配置 HTTPS 或启用浏览器本地白名单。

详细配置步骤（含 Chrome 免证书白名单、Caddy 自动证书、Nginx 自签名反代配置）请参阅：
👉 **[📖 远端 IP 部署与 PWA / HTTPS 指南 (REMOTE_DEPLOYMENT.md)](REMOTE_DEPLOYMENT.md)**

---

## 8. 卸载与清理

如果需要完全移除服务与相关二进制文件：

```bash
# 停止并注销 systemd 用户服务
systemctl --user stop agent-monitor 2>/dev/null || true
systemctl --user disable agent-monitor 2>/dev/null || true
rm -f ~/.config/systemd/user/agent-monitor.service
systemctl --user daemon-reload 2>/dev/null || true

# 移除二进制程序
rm -f ~/.local/bin/agent-monitor ~/.local/bin/agent-reporter
rm -f /usr/local/bin/agent-monitor /usr/local/bin/agent-reporter

# （可选）清理持久化历史数据
rm -rf ~/.local/share/agent-monitor
```
