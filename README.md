# AGENT MONITOR

面向 AI Coding Agent（ZCode、Cursor、Codex CLI、Claude Code 等）的单页会话监视器。

Agent 通过 Hook 把会话事件打到本地进程，页面用 SSE 实时刷新：Running、Completed、Failed / Aborted 三列，点击卡片可查看完整 Prompt 与 Hook 时间线。

## 快速开始

需要 Go 1.22+ 与 Python 3（仅 Hook 上报脚本依赖标准库）。

```bash
go run main.go
```

浏览器打开 [http://127.0.0.1:8000/](http://127.0.0.1:8000/)。

默认端口 `8000`，可用环境变量覆盖：

```bash
PORT=9000 go run main.go
```

## 构建与安装

```bash
# 编译 Monitor 与 Reporter
make build

# 跨平台一键编译全平台静态二进制
make build-all
```

编译产物位于 `bin/` 目录：
- `bin/agent-monitor`：Monitor Web 仪表盘服务
- `bin/agent-reporter`：面向 AI Agent 的零依赖 Hook 上报器（支持 macOS/Linux/Windows）

## 接入 ZCode Hook

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

把 `configs/cursor-hooks.json` 配进 Cursor 的 hooks（或把其中命令复制到你的 hooks 配置）。上报器会从 stdin 读取官方 payload，并向 Monitor 上报：

```bash
bin/agent-reporter --event sessionStart --agent 'Cursor Agent'
```

*(也支持使用 `python3 scripts/reporter.py` 作为兼容方案)*

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

- `sessionStart` / `onStart` / `SessionStart` / `UserPromptSubmit` → 正在运行
- `agentCompletion` / `onComplete` / `complete` / `Stop` / `SessionEnd` → 已完成
- `stop` / `failed` / `error` → 异常 / 中断
- `toolUse` / `beforeShellExecution` / `afterFileEdit` / `toolFailure` → 操作轨迹（记录在时间线中）

同一 `id` 的多次上报会聚合成一条任务，并追加到时间线。

## 目录

```
main.go                  Monitor 服务入口（内存任务 + SSE）
static/index.html        Monitor 前端（go:embed）
cmd/reporter/            Go 原生零依赖 Hook 上报器命令行入口
internal/reporter/       上报器核心逻辑（协议放行、过滤规则、Git/Transcript解析）
scripts/reporter.py      通用 Hook 上报脚本（Python 备用兼容版本）
configs/zcode-hooks.json ZCode Hook 配置模板
configs/cursor-hooks.json Cursor Hook 配置模板
.zcode/config.json       当前仓库的本地 ZCode Hook 启用配置
AGENTS.md                给 Agent 的仓库约定
Makefile                 构建与跨平台编译脚本
```

改 `static/index.html` 后必须重启 `go run main.go`，嵌入内容只在编译时打进二进制。
