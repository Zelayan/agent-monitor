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

纯 Go 静态编译，零外部运行时依赖（**无需安装 Python**）。

```bash
go run main.go
```

浏览器打开 [http://127.0.0.1:8000/](http://127.0.0.1:8000/)。

默认端口 `8000`，可用环境变量覆盖：

```bash
PORT=9000 go run main.go
```

## 安装与分发

### 方式一：一键安装脚本（推荐，macOS / Linux）

自动识别系统与 CPU 架构，下载官方最新静态二进制并配置到 PATH：

```bash
curl -fsSL https://raw.githubusercontent.com/Zelayan/agent-monitor/main/install.sh | bash
```

### 方式二：Go 原生安装（零依赖）

若本地已安装 Go 1.22+，可直接一键安装至 `$GOPATH/bin`：

```bash
# 安装 Monitor Web 仪表盘服务
go install github.com/Zelayan/agent-monitor@latest

# 安装 Hook 上报器
go install github.com/Zelayan/agent-monitor/cmd/reporter@latest
```

### 方式三：Docker 容器运行

```bash
# 启动持久化监控容器
docker run -d --name agent-monitor \
  -p 8000:8000 \
  -v $(pwd)/data:/app/data \
  ghcr.io/zelayan/agent-monitor:latest
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
install.sh               全平台一键安装脚本
Dockerfile               轻量容器镜像定义
.github/workflows/       CI/CD 与多架构 Release 发布流水线
AGENTS.md                给 Agent 的仓库约定
Makefile                 构建与跨平台编译脚本
```

改 `static/index.html` 后必须重启 `go run main.go`，嵌入内容只在编译时打进二进制。

## 开源协议

本项目采用 [MIT License](LICENSE) 开源协议。

