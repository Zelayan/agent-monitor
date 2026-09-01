# AGENT MONITOR

面向 AI Coding Agent 的多会话实时监视器。Go 进程接收 Hook 上报，经 SSE 推送到嵌入的 Monitor 前端。

## 每次改动必须提交

- **任何代码、配置、文案改动完成后，必须立刻 `git commit`**，不得把未提交改动留在工作区。
- 一次任务若包含多组独立改动，按逻辑拆成多次 commit，不要攒到最后一次性提交。
- 提交信息用 1–2 句说明「为什么」，不要只罗列文件名。
- 不要修改 git config；不要 `--amend`、force push、hard reset，除非用户明确要求。
- 不要提交密钥、`.env`、凭证或本机私有路径。

## 架构约定

- Monitor 页面在 `static/index.html`，由 `main.go` 通过 `go:embed` 打进二进制。**改 HTML 后必须重启 `go run main.go` 才会生效。**
- 实时通道：前端用原生 `EventSource('/api/stream')`；Hook 上报 `POST /api/event`；`DELETE /api/tasks` 只清已完成/失败任务。
- Agent 过滤胶囊必须按当前任务里实际上报的 `agent` 字段动态生成，禁止写死 Cursor / Codex / Claude。
- `cmd/reporter/main.go` 编译出零依赖的 Go 原生二进制 `bin/agent-reporter`；配置在 `configs/cursor-hooks.json` 与 `configs/zcode-hooks.json`（兼容 Python 脚本 `scripts/reporter.py`）。

## 前端

- 暗色工具台风格（Linear 紧凑 / Sentry）：页面 `#09090b`，容器 `#0c0d0e`，任务卡 `#111215`，边框 `#27272a`。
- 禁止 emoji；状态与操作一律用 14px 单色描边 SVG。禁止大面积渐变与发光阴影。
- 状态色：运行中 amber，完成 emerald，失败 red。ID / 分支 / 耗时用 `font-mono tabular-nums`。
- 运行中卡片的秒表必须每秒递增；点击卡片打开右侧 400px 抽屉时间线。
