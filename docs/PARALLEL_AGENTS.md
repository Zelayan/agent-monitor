# 多 Agent 并发分支开发

[English](PARALLEL_AGENTS.en.md) | [简体中文](PARALLEL_AGENTS.md)

本仓库托管在 GitHub（[Zelayan/agent-monitor](https://github.com/Zelayan/agent-monitor)）。多个 Cursor Agent 可以同时开发，但必须各自使用独立 Git worktree 或 Cloud Agent，以及独立的 `feat/` / `fix/` 分支。不要让多个 Agent 同时写同一份 checkout。

Cursor 创建 worktree 时会读取 [`.cursor/worktrees.json`](../.cursor/worktrees.json)：执行 `go mod download`，并在主目录存在本机 `.cursor/hooks.json` 时复制过来（该文件已被 gitignore，禁止提交）。

---

## 1. 主干保护（GitHub Flow）

- `master` 为默认可发布分支，禁止直推。
- 功能走 `feat/<topic>`，修复走 `fix/<topic>`，例如 `feat/reporter-timeout`、`fix/sse-heartbeat`。
- 从干净的 `origin/master` 拉分支，不要把未提交的脏工作区带进 worktree。
- 每个 Agent 一条短命分支、一个 Pull Request；合入默认 Squash merge。
- **提 PR 前本地自查自愈**：必须先运行 `go test -v -race ./...` 和本地 AI 审查（`make review` 或 `make pre-pr`），发现 `[BLOCK]` 级别问题必须本地解决后方可提 PR。
- **严禁自动合并 PR**：Agent 在 `gh pr create` 并输出 PR 链接后必须立刻停止，合并必须由用户审阅确认后执行，禁止未经明确许可调用 `gh pr merge`。
- PR 必须通过 `.github/workflows/ci.yml` 的 `go test -race ./...`。CI 按 `github.ref` 隔离，多个 PR 可并行跑绿。
- 先合无冲突的 PR；文件重叠的后合者 rebase `master` 后再过 CI。

## 2. 启动路径

### 本地短任务（Worktree）

1. 在 Agents Window 把 Agent 放进 Worktree，或在聊天中使用 `/worktree`。
2. 确认当前 checkout 不是主工作区，且已切到独立 `feat/` / `fix/` 分支。
3. 完成后在该 worktree 内提交，然后：

```bash
git push -u origin HEAD
gh pr create
```

4. 推荐用 GitHub PR 合入主干（与主干保护一致）。需要在主目录验证时再用 `/apply-worktree`。用完后 `/delete-worktree`。

同一任务对比多模型实现时用 `/best-of-n`（每个候选一份 worktree）。可拆成互不依赖的块时用 `/multitask`，并仍为每个子任务保留独立分支。

### 长任务 / 不占本机（Cloud Agent）

使用 `/in-cloud` 或 Cloud Agent。Cursor 需已连接本 GitHub 仓库。云端在独立 VM 与分支上工作，完成后提 PR。本机继续用主 checkout 做别的事。

## 3. 如何按 DDD 拆任务

尽量按目录一人一块，减少撞文件：

| 适合并行 | 路径 |
| :--- | :--- |
| 领域状态机 | `internal/domain/` |
| 上报器协议 | `internal/reporter/` |
| HTTP / SSE | `internal/infrastructure/transport/` |
| Cursor 扩展 | `extensions/cursor/` |
| 文档 | `docs/` |

不要并行拆开的情况：

- 同一段状态机或同一份 `static/index.html`
- 会同时改 `static/index.html` 与 `main.go`（`go:embed`）的任务
- 「先改 API 再改调用方」这种有顺序依赖的活

扩展任务才在对应 worktree 里执行 `npm install`（或 `make extension`）。Go 任务不要默认装 Node 依赖。

## 4. 本机资源

- 全机只跑一份 `agent-monitor`（默认 `http://127.0.0.1:8000/`）。Worktree 里不要再起第二个守护进程。
- `.cursor/hooks.json` 含本机绝对路径，已被 `.gitignore` 忽略。Worktree setup 会从主目录复制；不要 `git add` 它。
- 提交前在该 worktree 运行 `go test -v -race ./...`。改了 `extensions/cursor` 时再跑扩展构建。
