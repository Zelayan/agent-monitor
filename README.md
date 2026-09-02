# AGENT MONITOR

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![GitHub Release](https://img.shields.io/github/v/release/Zelayan/agent-monitor?include_prereleases&color=emerald)](https://github.com/Zelayan/agent-monitor/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/Zelayan/agent-monitor)](https://goreportcard.com/report/github.com/Zelayan/agent-monitor)

面向 AI Coding Agent（Claude Code、ZCode、Cursor Agent、Aider、Windsurf 等）的暗色工具台实时监视器。

Agent 通过 Hook 零延迟上报事件，页面基于原生 SSE 极速刷新三列任务看板（Running、Completed、Failed / Aborted）。点击卡片即可在侧边抽屉中实时追踪多轮 Prompt、AI 回复与微秒级 Hook 执行轨迹。

![Agent Monitor Dashboard Overview](docs/images/dashboard.png)

<details>
<summary><b>🔍 查看多轮执行抽屉与时间线（点击展开）</b></summary>

![Agent Monitor Drawer Timeline Detail](docs/images/drawer.png)

</details>

## 快速开始

纯 Go 静态编译，零外部运行时依赖（**无需安装 Python**）。Linux 上一键安装会把 `agent-monitor` 注册为 **systemd 用户服务**，登录后自动拉起看板。

```bash
curl -fsSL https://raw.githubusercontent.com/Zelayan/agent-monitor/main/install.sh | bash
```

浏览器打开 [http://127.0.0.1:8000/](http://127.0.0.1:8000/)。

```bash
systemctl --user status agent-monitor
journalctl --user -u agent-monitor -f
systemctl --user restart agent-monitor
```

会话数据默认在 `~/.local/share/agent-monitor/sessions`。改端口：编辑 `~/.config/systemd/user/agent-monitor.service` 里的 `PORT=`，然后：

```bash
systemctl --user daemon-reload && systemctl --user restart agent-monitor
```

SSH 登出后仍要保持运行：`loginctl enable-linger "$USER"`。不要用 `sudo` 跑安装脚本（否则服务会装到 root）。只要二进制、不要服务：`INSTALL_SYSTEMD=0 bash install.sh`。内网把 Release 的 `tar.gz` 拷进去后执行 `./install.sh ./agent-monitor_*_linux_amd64.tar.gz`（见下方离线安装）。

macOS 没有 systemd，脚本只安装到 PATH，然后执行 `agent-monitor`。

从源码试跑：

```bash
go run main.go
# PORT=9000 go run main.go
```

## 安装与分发

### 方式一：一键安装脚本（推荐，macOS / Linux）

自动识别系统与 CPU 架构，下载官方最新静态二进制并配置到 PATH。Linux 上默认启用 systemd 用户服务（见上方快速开始）。以 **root** 运行安装脚本时，会改为写入 `/etc/systemd/system/agent-monitor.service`（`multi-user.target`，数据目录 `/var/lib/agent-monitor/sessions`）。

```bash
curl -fsSL https://raw.githubusercontent.com/Zelayan/agent-monitor/main/install.sh | bash
```

### 内网 / 离线安装

发布包是纯静态二进制，安装过程不需要 Go 模块代理。把 `install.sh` 和对应平台的 `tar.gz` 一起拷进内网（之后的 GitHub Release 压缩包内也会带上安装脚本）：

```bash
# 本地压缩包，零外网
./install.sh ./agent-monitor_v1.0.0-beta.3_linux_amd64.tar.gz

# 已解压目录，或本机 make build 产出的 bin/
./install.sh ./bin

# 内网 HTTP 镜像（不访问 GitHub API，需指定版本）
VERSION=v1.0.0-beta.3 BASE_URL=https://files.corp.local/agent-monitor ./install.sh
```

Docker 离线：在外网 `docker pull` 后 `docker save`，内网 `docker load`。

### 方式二：Go 原生安装（零依赖）

若本地已安装 Go 1.22+，可直接一键安装至 `$GOPATH/bin`：

```bash
# 安装 Monitor Web 仪表盘服务
go install github.com/Zelayan/agent-monitor@latest

# 安装 Hook 上报器
go install github.com/Zelayan/agent-monitor/cmd/reporter@latest
```

### 方式三：Docker 容器运行

推送 `v*` Tag 后会构建 linux/amd64 与 linux/arm64 镜像，并发布到 GHCR（`latest` 与对应版本号）。配有 Docker Hub Secrets 时会同时推送到 Docker Hub。

```bash
# GitHub Container Registry
docker run -d --name agent-monitor \
  -p 8000:8000 \
  -v $(pwd)/data:/app/data \
  ghcr.io/zelayan/agent-monitor:latest

# 或 Docker Hub
docker run -d --name agent-monitor \
  -p 8000:8000 \
  -v $(pwd)/data:/app/data \
  zelayan/agent-monitor:latest
```

### 方式四：源码本地构建

```bash
# 本地编译 Monitor 与 Reporter 至 bin/
make build

# 跨平台一键编译全平台静态发布包至 dist/
make build-all
```

编译产物：
- `bin/agent-monitor`：Monitor Web 仪表盘服务
- `bin/agent-reporter`：面向 AI Agent 的零依赖 Hook 上报器（支持 macOS/Linux/Windows）

## ⚡ 让 AI 自动为你接入（零手工配置）

你可以直接对你正在使用的 **Claude Code / Cursor / ZCode / Aider** 说一句话：

> *"请阅读 [https://raw.githubusercontent.com/Zelayan/agent-monitor/main/docs/AGENT_INTEGRATION.md](docs/AGENT_INTEGRATION.md) 并帮我为当前项目自动安装配置 Agent Monitor 的 Hook"*

AI Agent 会自动：
1. 检查并一键安装 `agent-reporter` 命令行工具；
2. 识别当前 Agent 类型并自动生成对应的工作区 Hook 配置文件；
3. 发送测试心跳验证连通性。

---

## 手动接入各 Agent Hook

ZCode 支持在工作区 `.zcode/config.json` 或全局 `~/.zcode/cli/config.json` 中配置生命周期 Hook。

本仓库已在 `.zcode/config.json` 中预置了 Hook 配置（模板见 `configs/zcode-hooks.json`）。关键配置如下：

```json
{
  "hooks": {
    "enabled": true,
    "events": {
      "SessionStart": [
        { "hooks": [{ "type": "command", "command": "bin/agent-reporter --event SessionStart --agent ZCode" }] }
      ],
      "UserPromptSubmit": [
        { "hooks": [{ "type": "command", "command": "bin/agent-reporter --event UserPromptSubmit --agent ZCode" }] }
      ],
      "PreToolUse": [
        { "matcher": ".*", "hooks": [{ "type": "command", "command": "bin/agent-reporter --event PreToolUse --agent ZCode" }] }
      ],
      "PostToolUseFailure": [
        { "matcher": ".*", "hooks": [{ "type": "command", "command": "bin/agent-reporter --event PostToolUseFailure --agent ZCode" }] }
      ],
      "Stop": [
        { "hooks": [{ "type": "command", "command": "bin/agent-reporter --event Stop --agent ZCode" }] }
      ]
    }
  }
}
```

## 接入 Cursor Hook

把 `configs/cursor-hooks.json` 配进 Cursor 的 hooks（项目级 `.cursor/hooks.json` 或用户级 `~/.cursor/hooks.json`）。使用官方事件名：`sessionStart`、`beforeSubmitPrompt`、`preToolUse`、`postToolUseFailure`、`beforeShellExecution`、`beforeMCPExecution`、`subagentStart`、`afterAgentResponse`、`stop`、`sessionEnd`。

上报器会从 stdin 读取官方 payload。Cursor 的完成事件是 `stop`（不是 `agentCompletion`）；`aborted` / `error` 会记入 Failed 列。用户级 hooks 的工作目录是 `~/.cursor/`，命令请写成 `agent-reporter` 的绝对路径。

```bash
bin/agent-reporter --event sessionStart --agent 'Cursor Agent'
```

## 接入 Claude Code (Anthropic 官方 CLI)

Claude Code 原生支持 Hook 事件。将 `configs/claude-hooks.json` 配置写入工作区 `.claude/config.json` 或全局 `~/.claude/config.json`：

```json
{
  "hooks": {
    "enabled": true,
    "events": {
      "SessionStart": [{ "hooks": [{ "type": "command", "command": "bin/agent-reporter --event SessionStart --agent 'Claude Code'" }] }],
      "UserPromptSubmit": [{ "hooks": [{ "type": "command", "command": "bin/agent-reporter --event UserPromptSubmit --agent 'Claude Code'" }] }],
      "PreToolUse": [{ "matcher": ".*", "hooks": [{ "type": "command", "command": "bin/agent-reporter --event PreToolUse --agent 'Claude Code'" }] }],
      "PostToolUseFailure": [{ "matcher": ".*", "hooks": [{ "type": "command", "command": "bin/agent-reporter --event PostToolUseFailure --agent 'Claude Code'" }] }],
      "Stop": [{ "hooks": [{ "type": "command", "command": "bin/agent-reporter --event Stop --agent 'Claude Code'" }] }]
    }
  }
}
```

## 接入 Aider 终端 Agent

Aider 可在工作区配置文件 `.aider.conf.yml`（或全局 `~/.aider.conf.yml`）中添加通知上报：

```yaml
notifications-command: "bin/agent-reporter --event agentCompletion --agent Aider"
auto-commits: true
```

## 接入 Continue / Roo Code / Windsurf / 自定义 Agent

- **Continue**：在 `~/.continue/config.json` 中配置自定义 telemetry endpoint（见 `configs/continue-config.json`）。
- **通用 HTTP 上报**：任何 Agent / 脚本只需向 `POST http://127.0.0.1:8000/api/event` 发送 JSON，顶部过滤胶囊会**自动按实际上报的 agent 动态生成**。

手动试一条：

```bash
curl -s http://127.0.0.1:8000/api/event \
  -H 'Content-Type: application/json' \
  -d '{"id":"task-demo","agent":"Claude Code","repo":"agent-monitor:master","event":"sessionStart","title":"示例任务","detail":"会话启动"}'
```

## HTTP API

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/` | Monitor 页面（嵌入二进制） |
| `POST` | `/api/event` | Hook 上报，创建或更新任务并广播 |
| `GET` | `/api/stream` | SSE 长连接：先推快照，再推增量 |
| `GET` | `/api/tasks` | 当前全部任务 |
| `DELETE` | `/api/tasks` | 清除已完成和失败的任务（进行中保留） |

`POST /api/event` 请求体：

```json
{
  "id": "task-9481",
  "agent": "ZCode",
  "repo": "my-project:feat/auth",
  "branch": "feat/auth",
  "event": "afterFileEdit",
  "title": "给支付服务补上 JWT 鉴权",
  "timestamp": 1735689600,
  "detail": "编辑文件: src/auth/jwt.go"
}
```

`event` 会映射到 Monitor 列：

- `sessionStart` / `onStart` / `SessionStart` / `UserPromptSubmit` / `beforeSubmitPrompt` → 正在运行
- `agentCompletion` / `onComplete` / `complete` / `Stop` / `stop` / `SessionEnd` / `sessionEnd` / `afterAgentResponse` → 已完成（Cursor 的 `afterAgentResponse` 表示回复已交付，作为 `stop` 丢失时的兜底收口）
- `failed` / `error` → 异常 / 中断（Cursor `stop` 且 status 为 `aborted`/`error` 时由上报器映射而来）
- `toolUse` / `beforeShellExecution` / `toolFailure` → 操作轨迹（记录在时间线中）

同一 `id` 的多次上报会聚合成一条任务，并追加到时间线。

## 目录

```
main.go                  Monitor 服务入口（内存任务 + SSE）
static/index.html        Monitor 前端（go:embed）
cmd/reporter/            Go 原生零依赖 Hook 上报器命令行入口
internal/reporter/       上报器核心逻辑（协议放行、过滤规则、Git/Transcript解析）
llms.txt                 符合 LLM 索引规范的 Agent 入口
docs/AGENT_INTEGRATION.md 面向 AI Agent 的自动安装配置指南
scripts/reporter.py      历史 Python 上报脚本（已废弃，建议使用 cmd/reporter）
configs/                 多 Agent Hook 配置模板（ZCode, Cursor, Claude Code, Aider 等）
install.sh               全平台一键安装脚本（Linux 默认注册 systemd 用户服务）
configs/agent-monitor.service  systemd 用户服务单元
Dockerfile               轻量容器镜像定义
.github/workflows/       CI/CD 与多架构 Release 发布流水线
AGENTS.md                给 Agent 的仓库约定
Makefile                 构建与跨平台编译脚本
```

改 `static/index.html` 后必须重启 `go run main.go`，嵌入内容只在编译时打进二进制。

## 开源协议

本项目采用 [MIT License](LICENSE) 开源协议。

