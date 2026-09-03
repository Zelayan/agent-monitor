# Parallel Agents on GitHub Branches

[English](PARALLEL_AGENTS.en.md) | [简体中文](PARALLEL_AGENTS.md)

This repository is hosted on GitHub ([Zelayan/agent-monitor](https://github.com/Zelayan/agent-monitor)). Multiple Cursor Agents can work at the same time if each uses an isolated Git worktree or a Cloud Agent, plus its own `feat/` / `fix/` branch. Do not let multiple Agents write the same checkout.

When Cursor creates a worktree it reads [`.cursor/worktrees.json`](../.cursor/worktrees.json): it runs `go mod download` and copies a machine-local `.cursor/hooks.json` from the main checkout when that file exists (it is gitignored and must not be committed).

---

## 1. Trunk protection (GitHub Flow)

- `master` is the default releasable branch. Do not push it directly.
- Features use `feat/<topic>`, fixes use `fix/<topic>`, for example `feat/reporter-timeout`, `fix/sse-heartbeat`.
- Branch from a clean `origin/master`. Do not copy an unclean working tree into a worktree.
- One Agent, one short-lived branch, one Pull Request. Default merge is Squash merge.
- **Local AI Review Before PR**: Always run `go test -v -race ./...` and local AI review (`make review` or `make pre-pr`). Resolve all `[BLOCK]` issues before opening a PR.
- **Never auto-merge PRs**: Agent must stop after `gh pr create` and outputting the PR link. Merging requires explicit user review and confirmation (`gh pr merge` is forbidden without explicit authorization).
- Every PR must pass `.github/workflows/ci.yml` (`go test -race ./...`). CI is isolated per `github.ref`, so multiple PRs can go green in parallel.
- Merge conflict-free PRs first. Overlapping PRs rebase onto `master` and re-run CI.

## 2. How to start

### Local short tasks (Worktree)

1. Move the Agent into a Worktree from the Agents Window, or use `/worktree` in chat.
2. Confirm the checkout is not the main workspace and is on its own `feat/` / `fix/` branch.
3. Commit inside that worktree, run `make pre-pr` (unit tests + local AI Review) to ensure no blocking issues, then:

```bash
git push -u origin HEAD
gh pr create
```

4. Prefer a GitHub PR into trunk (matches trunk protection). Use `/apply-worktree` only when you need to verify in the main checkout. Clean up with `/delete-worktree`.

Use `/best-of-n` to compare models (one worktree per candidate). Use `/multitask` when the work splits into independent chunks, still keeping a separate branch per subtask.

### Long tasks / keep the machine free (Cloud Agent)

Use `/in-cloud` or a Cloud Agent. Cursor must be connected to this GitHub repository. The cloud run uses its own VM and branch, then opens a PR. Keep using the main checkout locally for other work.

## 3. How to split work (DDD)

Assign one directory per Agent when possible:

| Good parallel split | Path |
| :--- | :--- |
| Domain state machine | `internal/domain/` |
| Reporter protocol | `internal/reporter/` |
| HTTP / SSE | `internal/infrastructure/transport/` |
| Cursor extension | `extensions/cursor/` |
| Docs | `docs/` |

Do not split these in parallel:

- The same state machine or the same `static/index.html`
- A change that touches both `static/index.html` and `main.go` (`go:embed`)
- Sequentially dependent work such as “change the API, then the callers”

Install Node dependencies (`npm install` or `make extension`) only in worktrees that touch `extensions/cursor`. Go-only tasks should not install npm packages by default.

## 4. Local resources

- Run a single `agent-monitor` on the machine (default `http://127.0.0.1:8000/`). Do not start a second daemon from a worktree.
- `.cursor/hooks.json` contains machine-local absolute paths and is gitignored. Worktree setup copies it from the main checkout; never `git add` it.
- Before commit, run `go test -v -race ./...` in that worktree. Rebuild the extension only when `extensions/cursor` changed.
