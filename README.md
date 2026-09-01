# Agent Kanban Hub

面向 AI Coding Agent（Cursor、Codex CLI、Claude Code 等）的多会话实时任务看板。

Agent 通过 Hook 把会话事件打到本地 Hub，看板用 SSE 实时刷新：正在运行、已完成、异常/中断三列，点击卡片可查看完整 Prompt 与 Hook 时间线。

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

## 接入 Cursor Hook

把 `configs/cursor-hooks.json` 配进 Cursor 的 hooks（或把其中命令复制到你的 hooks 配置）。脚本会从 stdin 读取官方 payload，并向 Hub 上报：

```bash
python3 scripts/reporter.py --event sessionStart --agent 'Cursor Agent'
```

其他 Agent（Codex / Claude 等）只要向 `/api/event` POST 同一份 JSON 即可，过滤胶囊会按实际上报的 `agent` 名称动态出现。

手动试一条：

```bash
curl -s http://127.0.0.1:8000/api/event \
  -H 'Content-Type: application/json' \
  -d '{"id":"task-demo","agent":"Cursor Agent","repo":"agent-kanban:main","event":"sessionStart","title":"示例任务","detail":"会话启动"}'
```

## HTTP API

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/` | 看板页面（嵌入二进制） |
| `POST` | `/api/event` | Hook 上报，创建或更新任务并广播 |
| `GET` | `/api/stream` | SSE 长连接：先推快照，再推增量 |
| `GET` | `/api/tasks` | 当前全部任务 |
| `DELETE` | `/api/tasks` | 清除已完成和失败的任务（进行中保留） |

`POST /api/event` 请求体：

```json
{
  "id": "task-9481",
  "agent": "Cursor Agent",
  "repo": "my-project:feat/auth",
  "branch": "feat/auth",
  "event": "afterFileEdit",
  "title": "给支付服务补上 JWT 鉴权",
  "timestamp": 1735689600,
  "detail": "编辑文件: src/auth/jwt.go"
}
```

`event` 会映射到看板列：

- `sessionStart` / `onStart` → 正在运行
- `agentCompletion` / `onComplete` / `complete` → 已完成
- `stop` / `failed` / `error` → 异常 / 中断

同一 `id` 的多次上报会聚合成一条任务，并追加到时间线。

## 目录

```
main.go                  Hub 服务（内存任务 + SSE）
static/index.html        看板前端（go:embed）
scripts/reporter.py      Cursor Hook 上报
configs/cursor-hooks.json
AGENTS.md                给 Agent 的仓库约定
```

改 `static/index.html` 后必须重启 `go run main.go`，嵌入内容只在编译时打进二进制。
