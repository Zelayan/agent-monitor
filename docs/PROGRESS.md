# Agent Monitor 开发进度与多 Agent 协作留痕看板

[English](PROGRESS.en.md) | [简体中文](PROGRESS.md)

本文档是 Agent Monitor 项目中 **Main Agent、Phase Subagent 与 PR Subagent** 之间唯一的全局持久化进度协同与状态留痕看板。所有开发阶段、Work Package（工作包）、分支生命周期、质量门禁与阻断自愈记录必须在此文档中同步留痕。

---

## 1. 全局概览与协作总则

### 1.1 当前全局状态
- **当前活跃阶段**: **Phase 3：实时可靠性与产品成熟度** (Phase 0 ~ Phase 2 已全面收官)
- **主干最新基线**: Commit `97a6c08` (PR #34 已合入)
- **质量门禁基线**:
  - 单元测试与竞态检测: `go test -v -race ./...` (必须 100% 通过且 0 Race)
  - 本地严格审查: `make review-strict` (必须 0 `[BLOCK]` 阻断项)
  - 审查结论自愈: 任何阶段出现 `[BLOCK]`，Subagent 必须就地分析、修改代码、消除阻断项并在此文档登记留痕
  - 自动合并授权: 本地审查与云端 CI（Test + AI Review）全部跑绿后，自动执行 Squash Merge 合入主干

### 1.2 多 Agent 状态流转契约 (Lifecycle State Machine)
```
[Pending] (等待派发)
    │
    ▼ Subagent 启动，初始化独立 Git Worktree 并切出 feat/ 分支
[In Progress] (编码中 / 测试中)
    │
    ├──► 审查发现 [BLOCK] 阻断项
    │         │
    │         ▼
    │    [Blocked (Review)] ──► 就地自愈修复并重新验证 0 BLOCK
    │         ▲                      │
    │         └──────────────────────┘
    │
    ▼ 本地 make pre-pr 验证通过 (测试 100% 绿灯且 0 BLOCK)
[Self-Healed / Quality Passed]
    │
    ▼ git push 并通过 gh pr create 创建 PR
[PR Created] (等待云端 CI & AI Review 运行)
    │
    ▼ 云端 CI 与 AI Review 全部通过 (0 BLOCK)
[Merged] (执行 gh pr merge --squash --delete-branch 合入 master 并归档)
```

---

## 2. 历史阶段归档 (Phase 0 ~ Phase 2)

| 阶段 | 核心目标 | 涉及 Work Package | 交付 PR | 状态 |
| :--- | :--- | :--- | :--- | :---: |
| **Phase 0** | 基线约束与测试夹具 | 租户伪造、Session ID 冲突与并发边界测试 | 内置在 Phase 1 各 PR | ✅ 已归档 |
| **Phase 1** | 可信实时链路 | WP-1: 可信鉴权身份绑定<br>WP-2: 租户复合任务键 (`TaskKey`)<br>WP-3: 有序持久化命令流与优雅停机<br>WP-4: SSE 快照对账与前端可信操作<br>WP-5: 安全 Kill 与真实进程组控制<br>WP-6: 严格 HTTP 与类型化错误 | [#24](https://github.com/Zelayan/agent-monitor/pull/24)<br>[#25](https://github.com/Zelayan/agent-monitor/pull/25)<br>[#26](https://github.com/Zelayan/agent-monitor/pull/26)<br>[#27](https://github.com/Zelayan/agent-monitor/pull/27)<br>[#28](https://github.com/Zelayan/agent-monitor/pull/28)<br>[#29](https://github.com/Zelayan/agent-monitor/pull/29) | ✅ 已归档 |
| **Phase 2** | 高频效率与可运营性 | WP-7: `filter_repos` 仓库白名单<br>WP-8: Repo/Branch Schema 独立解耦<br>WP-9: 全量深度搜索与复合状态筛选<br>WP-10 & 11: 多维排序/持久视图/深链接<br>WP-12 & 13: 探针/指标/隔离/Reporter卫生 | [#30](https://github.com/Zelayan/agent-monitor/pull/30)<br>[#31](https://github.com/Zelayan/agent-monitor/pull/31)<br>[#32](https://github.com/Zelayan/agent-monitor/pull/32)<br>[#33](https://github.com/Zelayan/agent-monitor/pull/33)<br>[#34](https://github.com/Zelayan/agent-monitor/pull/34) | ✅ 已归档 |

---

## 3. Phase 3 工作包矩阵 (当前执行阶段)

> **本阶段重点**：提供可恢复的实时流、完整无障碍体验、规模化前端性能与稳定的 PWA 离线能力。

| WP 编号 | 任务名称 | 架构目录 | 负责 Agent / 分支 | 隔离 Worktree | 当前状态 | PR 链接 | 质量门禁与审查结论 |
| :--- | :--- | :--- | :--- | :--- | :---: | :---: | :--- |
| **WP-14** | **可靠 SSE v2 协议**<br>(单调递增事件ID/环形缓冲/Last-Event-ID/resync_required) | `internal/domain`<br>`transport/http` | PR-Subagent-14<br>`feat/reliable-sse-v2` | `wt-wp14 (已清理)` | `Merged` | [#36](https://github.com/Zelayan/agent-monitor/pull/36) | [PASS] 0 BLOCK, 测试 100% 绿灯 |
| **WP-15** | **前端规模化性能**<br>(Keyed DOM Patch/局部状态列重绘/按版本缓存/分页) | `static/index.html` | PR-Subagent-15<br>`feat/frontend-dom-patch` | `../agent-monitor-worktrees/wt-wp15` | `Pending` | - | 待启动 (依赖 WP-14 协议已就绪) |
| **WP-16** | **无障碍、焦点管理与动效偏好**<br>(标准Dialog/Focus Trap/方向键导航/Reduced Motion) | `static/index.html` | PR-Subagent-16<br>`feat/a11y-focus-nav` | `wt-wp16 (已清理)` | `Merged` | [#37](https://github.com/Zelayan/agent-monitor/pull/37) | [PASS] 0 BLOCK, 测试 100% 绿灯 |
| **WP-17** | **国际化全量完备化**<br>(动态Run/事件/操作全量字典化/Intl 本地化格式) | `static/index.html` | PR-Subagent-17<br>`feat/i18n-complete` | `../agent-monitor-worktrees/wt-wp17` | `Self-Healed` | 准备提 PR | [PASS] 0 BLOCK, 测试 100% 绿灯 |
| **WP-18** | **完整 PWA 生命周期与离线快照**<br>(IndexedDB 加密快照/SW升级提示/脱机只读) | `static/index.html`<br>`static/sw.js` | PR-Subagent-18<br>`feat/pwa-offline-lifecycle` | `../agent-monitor-worktrees/wt-wp18` | `Pending` | - | 待启动 |
| **WP-19** | **浏览器级端到端测试**<br>(端到端快照对账/无障碍/重连断线覆盖) | `tests/e2e/` | PR-Subagent-19<br>`feat/e2e-browser-tests` | `../agent-monitor-worktrees/wt-wp19` | `Pending` | - | 待启动 |

---

## 4. [BLOCK] 阻断项排查与就地自愈留痕日志 (Healing Log)

> 凡是触发本地 `make review-strict`、`go test -race` 或云端 GitHub Actions 审查给出 `[BLOCK]` 的事件，必须如实记录在此表中，包含阻断原因、自愈措施和复核证据。

| 时间 (UTC) | WP 编号 | 审查阶段 (本地/CI) | `[BLOCK]` 阻断项描述 | 根因剖析 | 就地自愈与修复措施 | 复测验证凭证 | 状态 |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :---: |
| 2026-09-05 | WP-12 & 13 | 本地 Review | 无阻断项 | 遵循 DDD 边界、0 外部依赖与 Fail-Safe | 严格使用 LimitReader 与 TTL 清理 | `go test -race` 全绿，AI Review Pass | ✅ 已归档 |
| 2026-09-05 | WP-14 | CI & AI Review | 无阻断项 | 环形缓冲区线程安全与过期重同步协议 | 单调递增序号与 Last-Event-ID 重放 | `go test -race` 全绿，AI Review Pass (PR #36) | ✅ 已归档 |
| 2026-09-05 | WP-16 | CI & AI Review | 无阻断项 | ARIA 标准 Dialog、焦点还原与 Reduced Motion | CSS 动效偏好适配、Focus Trap 与卡片键盘导航 | `go test -race` 全绿，AI Review Pass (PR #37) | ✅ 已归档 |

---

## 5. 后续阶段概览 (Phase 4 & Phase 5)

- **Phase 4：多 Agent 生态与 Cursor 扩展**
  - Claude Code、Aider、Windsurf、Trae 深度适配与分级支持
  - Cursor 多根工作区 Hook 配置自动化管理
  - VSIX 跨平台二进制选择与扩展自动化测试
- **Phase 5：Agent APM 平台化**
  - 幂等 Event Log 与时序重放
  - Reporter 乱序检测与精确 Trace Span / 卡点诊断
  - 多租户配额、归档与敏感日志脱敏审计
