# Agent Monitor 开发进度与多 Agent 协作留痕看板

[English](PROGRESS.en.md) | [简体中文](PROGRESS.md)

本文档是 Agent Monitor 项目中 **Main Agent、Phase Subagent 与 PR Subagent** 之间唯一的全局持久化进度协同与状态留痕看板。所有开发阶段、Work Package（工作包）、分支生命周期、质量门禁与阻断自愈记录必须在此文档中同步留痕。

---

## 1. 全局概览与协作总则

### 1.1 当前全局状态
- **当前活跃阶段**: **Phase 4：Agent 与 Cursor 生态扩展已全面圆满收官（100% 交付合入）**
- **历史归档**: Phase 0 ~ Phase 4 全部工作包（WP-1 ~ WP-23）均已交付并经 CI 审查合入主干
- **主干最新基线**: PR #48 已合入，多架构 VSIX 打包与诊断工具完备
- **质量门禁基线**:
  - 单元测试与竞态检测: `go test -v -race ./...` (100% 通过且 0 Race)
  - 前端与扩展测试: `npm test` (20/20 全部通过)
  - 审查结论自愈: 任何阶段出现 `[BLOCK]`，Subagent 就地分析自愈并在文档登记留痕
  - 自动合并授权: 本地审查与云端 CI（Test + AI Review）全部跑绿后自动 Squash Merge 合入主干

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

## 2. 历史阶段归档 (Phase 0 ~ Phase 3)

| 阶段 | 核心目标 | 涉及 Work Package | 交付 PR | 状态 |
| :--- | :--- | :--- | :--- | :---: |
| **Phase 0** | 基线约束与测试夹具 | 租户伪造、Session ID 冲突与并发边界测试 | 内置在 Phase 1 各 PR | ✅ 已归档 |
| **Phase 1** | 可信实时链路 | WP-1: 可信鉴权身份绑定<br>WP-2: 租户复合任务键 (`TaskKey`)<br>WP-3: 有序持久化命令流与优雅停机<br>WP-4: SSE 快照对账与前端可信操作<br>WP-5: 安全 Kill 与真实进程组控制<br>WP-6: 严格 HTTP 与类型化错误 | [#24](https://github.com/Zelayan/agent-monitor/pull/24)<br>[#25](https://github.com/Zelayan/agent-monitor/pull/25)<br>[#26](https://github.com/Zelayan/agent-monitor/pull/26)<br>[#27](https://github.com/Zelayan/agent-monitor/pull/27)<br>[#28](https://github.com/Zelayan/agent-monitor/pull/28)<br>[#29](https://github.com/Zelayan/agent-monitor/pull/29) | ✅ 已归档 |
| **Phase 2** | 高频效率与可运营性 | WP-7: `filter_repos` 仓库白名单<br>WP-8: Repo/Branch Schema 独立解耦<br>WP-9: 全量深度搜索与复合状态筛选<br>WP-10 & 11: 多维排序/持久视图/深链接<br>WP-12 & 13: 探针/指标/隔离/Reporter卫生 | [#30](https://github.com/Zelayan/agent-monitor/pull/30)<br>[#31](https://github.com/Zelayan/agent-monitor/pull/31)<br>[#32](https://github.com/Zelayan/agent-monitor/pull/32)<br>[#33](https://github.com/Zelayan/agent-monitor/pull/33)<br>[#34](https://github.com/Zelayan/agent-monitor/pull/34) | ✅ 已归档 |
| **Phase 3** | 实时可靠性与产品成熟度 | WP-14: 可靠 SSE v2 协议 (单调ID/环形缓冲/Last-Event-ID)<br>WP-15: 前端规模化性能 (Keyed DOM Patch/局部重绘/节流)<br>WP-16: 无障碍、焦点管理与动效偏好 (Dialog/Focus Trap/A11y)<br>WP-17: 国际化全量完备化 (字典插值/Intl 格式化/对称导出)<br>WP-18: 完整 PWA 生命周期与离线快照 (IndexedDB/SW更新)<br>WP-19: 浏览器级端到端测试 (E2E 集成/断线重连全覆盖) | [#36](https://github.com/Zelayan/agent-monitor/pull/36)<br>[#40](https://github.com/Zelayan/agent-monitor/pull/40)<br>[#37](https://github.com/Zelayan/agent-monitor/pull/37)<br>[#39](https://github.com/Zelayan/agent-monitor/pull/39)<br>[#42](https://github.com/Zelayan/agent-monitor/pull/42)<br>[#43](https://github.com/Zelayan/agent-monitor/pull/43) | ✅ 已归档 |
| **Phase 4** | 多 Agent 生态与 Cursor 扩展 | WP-20: Agent 成熟度模型与 Claude Code / Aider 正式支持<br>WP-21: Cursor 多根工作区 Hook 自动化管理<br>WP-22: VSIX 跨平台内置二进制与分发构建<br>WP-23: 扩展自诊断 Output Channel 与自动化测试 | [#46](https://github.com/Zelayan/agent-monitor/pull/46)<br>[#45](https://github.com/Zelayan/agent-monitor/pull/45)<br>[#47](https://github.com/Zelayan/agent-monitor/pull/47)<br>[#48](https://github.com/Zelayan/agent-monitor/pull/48) | ✅ 已归档 |

---

## 3. Phase 4 工作包矩阵 (当前执行阶段)

> **本阶段重点**：扩大可正式支持的 Agent 类型并提升 Cursor 插件开箱即用与诊断能力。

| WP 编号 | 任务名称 | 架构目录 | 负责 Agent / 分支 | 隔离 Worktree | 当前状态 | PR 链接 | 质量门禁与审查结论 |
| :--- | :--- | :--- | :--- | :--- | :---: | :---: | :--- |
| **WP-20** | **Agent 适配成熟度模型与正式化**<br>(Claude Code / Aider 正式支持、Transcript 解析、Fail-Safe 自动化测试) | `internal/reporter`<br>`internal/domain` | PR-Subagent-20<br>`feat/agent-maturity-claude-aider` | `wt-wp20` (已清理) | `Merged` | [#46](https://github.com/Zelayan/agent-monitor/pull/46) | 审查 [BLOCK] 已就地自愈，深拷贝隔离与别名覆盖验证通过，100% 绿灯 |
| **WP-21** | **Cursor 多根工作区 Hook 管理**<br>(多根工作区遍历配置、Hook 冲突检测、无损 Diff 与撤销备份) | `extensions/cursor`<br>`internal/reporter` | PR-Subagent-21<br>`feat/cursor-multi-root-hooks` | `wt-wp21` (已清理) | `Merged` | [#45](https://github.com/Zelayan/agent-monitor/pull/45) | `go test -race` 100% 绿灯，`npm test` 9/9 绿灯，AI Review [PASS]，0 BLOCK |
| **WP-22** | **VSIX 跨平台内置二进制与分发**<br>(全平台交叉编译分发、按 OS/Arch 智能选择、执行权限与校验和) | `extensions/cursor`<br>`Makefile` | PR-Subagent-22<br>`feat/vsix-cross-platform-binaries` | `wt-wp22` (已清理) | `Merged` | [#47](https://github.com/Zelayan/agent-monitor/pull/47) | `go test -race` 100% 绿灯，`npm test` 18/18 绿灯，AI Review [PASS]，全平台 VSIX 打包与权限/校验和通过 |
| **WP-23** | **扩展自诊断与自动化测试**<br>(Output Channel、自诊断健康命令、端口/版本/SSE 检测与自动化测试) | `extensions/cursor` | PR-Subagent-23<br>`feat/cursor-diagnostics-and-tests` | `wt-wp23` (已清理) | `Merged` | [#48](https://github.com/Zelayan/agent-monitor/pull/48) | `go test -race` 100% 绿灯，`npm test` 20/20 绿灯，AI Review [PASS]，0 BLOCK |

---

## 4. [BLOCK] 阻断项排查与就地自愈留痕日志 (Healing Log)

> 凡是触发本地 `make review-strict`、`go test -race` 或云端 GitHub Actions 审查给出 `[BLOCK]` 的事件，必须如实记录在此表中，包含阻断原因、自愈措施和复核证据。

| 时间 (UTC) | WP 编号 | 审查阶段 (本地/CI) | `[BLOCK]` 阻断项描述 | 根因剖析 | 就地自愈与修复措施 | 复测验证凭证 | 状态 |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :---: |
| 2026-09-05 | WP-12 & 13 | 本地 Review | 无阻断项 | 遵循 DDD 边界、0 外部依赖与 Fail-Safe | 严格使用 LimitReader 与 TTL 清理 | `go test -race` 全绿，AI Review Pass | ✅ 已归档 |
| 2026-09-05 | WP-14 | CI & AI Review | 无阻断项 | 环形缓冲区线程安全与过期重同步协议 | 单调递增序号与 Last-Event-ID 重放 | `go test -race` 全绿，AI Review Pass (PR #36) | ✅ 已归档 |
| 2026-09-05 | WP-16 | CI & AI Review | 无阻断项 | ARIA 标准 Dialog、焦点还原与 Reduced Motion | CSS 动效偏好适配、Focus Trap 与卡片键盘导航 | `go test -race` 全绿，AI Review Pass (PR #37) | ✅ 已归档 |
| 2026-09-05 | WP-17 | CI & AI Review | 无阻断项 | 动态 Run/事件字典化与 Intl 格式化 | 全量字典化与本地化导出 | `go test -race` 全绿，AI Review Pass (PR #39) | ✅ 已归档 |
| 2026-09-05 | WP-15 | CI AI Review | 批量选择状态未计入列渲染签名导致 Checkbox 勾选时 DOM 未更新 | 列签名 `runSig`/`compSig`/`failSig` 仅比对了版本和子任务，未包含 `selectedTaskIds.has(id)` | 在 `getColumnSignature` 中将 `selectedTaskIds.has(id)` 作为独立签名项纳入列变更比对 | CI AI Review 重新复核通过，`go test -race` 全绿 | ✅ 已自愈 |
| 2026-09-05 | WP-19 | 本地 E2E 验证 | 测试断言与统一错误封装结构 (error_dto) schema 及 focus-visible/trapFocus 对齐 | 测试断言未解构 error 包装层且 FocusTrap 函数名为 trapFocus | 增加 extractErrorCode 解构包装器并校准 A11y 测试断言 | `go test -race ./...` 100% 通过，0 Race，0 BLOCK | ✅ 已自愈 |
| 2026-09-05 | WP-21 | 本地验证 & 测试 | 无阻断项 | 多根工作区遍历配置、无损合并自定义钩子、路径空格转义、Diff与备份撤销、用户级重复钩子检测 | 实现完整多根工作区选择与批量配置、9组自动化单元测试全覆盖 | `go test -race ./...` 100% 通过，`npm test` 9/9 通过，AI Review [PASS]，PR #45 已合入 | ✅ 已合入 |
| 2026-09-05 | WP-20 | CI AI Review | `NormalizeAgentName` 对 `codex-desktop` 匹配缺失导致误识别为 CLI；复合规格对象 `HookTypes` 切片未做深拷贝，外部修改存在数据竞争隐患 | `strings.Contains` 别名优先级漏洞及引用类型浅拷贝 | 1. 扩展别名覆盖 `codex-desktop`/`codex_desktop`/`codex.app`；2. 为 `AgentMaturitySpec` 引入 `Clone()` 完全深拷贝切片；3. 增加并发与隔离性测试断言 | CI AI Review 重新复核给出 [PASS]，`go test -race` 100% 绿灯，PR #46 已合入 | ✅ 已自愈合入 |
| 2026-09-05 | WP-22 | CI & AI Review | 无阻断项 | 跨平台二进制交叉编译与动态解析优先级 | 多层级降级、0o755 权限修复与 SHA-256 校验清单 | `go test -race` 全绿，`npm test` 18/18 全绿，AI Review [PASS]，PR #47 已合入 | ✅ 已合入 |
| 2026-09-05 | WP-23 | CI & AI Review | 无阻断项 | 全链路自诊断工具套件与 OutputChannel 结构化分级日志 | 二进制校验、探针探测、SSE流协议协商与工作区钩子检测 | `go test -race` 全绿，`npm test` 20/20 全绿，AI Review [PASS]，PR #48 已合入 | ✅ 已合入 |

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
