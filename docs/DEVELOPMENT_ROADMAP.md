# Agent Monitor 完整开发路线图

[English](DEVELOPMENT_ROADMAP.en.md) | [简体中文](DEVELOPMENT_ROADMAP.md)

本文档基于 2026-09-04 的仓库实现，给出 Agent Monitor 下一阶段的完整产品与工程开发计划。它既是功能路线图，也是需求、架构、迁移、测试、验收和 Pull Request 拆分指南。

> 本路线图不表示当前产品缺少基本能力。当前系统已经形成 Hook 上报、领域状态机、JSON 持久化、SSE 实时推送、Web 看板和反向控制的完整闭环。后续工作的首要目标是让多租户、实时状态、持久化和控制操作具备可证明的正确性，再扩展检索、诊断和平台化能力。

---

## 1. 文档目标

本文档用于统一以下事项：

1. 盘点当前已经具备的产品和工程能力，避免重复建设。
2. 记录已识别的正确性、安全、可靠性、体验和生态缺口。
3. 给出完整候选功能、优先级、依赖关系和预期收益。
4. 将路线图拆为可独立审查、可测试、可回滚的开发阶段和 PR。
5. 定义每项工作的数据模型、API、前端、迁移、测试和验收要求。
6. 为多 Agent 并行开发提供符合 DDD 和 GitHub Flow 的目录边界。
7. 明确暂不优先投入的方向，防止路线图失焦。

## 2. 适用原则

所有路线图实现必须继续遵守仓库根目录 [`AGENTS.md`](../AGENTS.md) 的规范：

- 从干净的 `origin/master` 创建 `feat/<topic>` 或 `fix/<topic>` 分支。
- 每个 Agent 使用独立 Git worktree 或 Cloud Agent，禁止共享 checkout。
- 按 `internal/domain`、`internal/application`、`internal/infrastructure`、`internal/reporter`、`extensions/cursor`、`static`、`docs` 拆分职责。
- 每组逻辑改动完成后立即提交，不留未提交改动。
- 提 PR 前运行 `go test -v -race ./...` 和 `make review` 或 `make pre-pr`。
- 修复所有 `[BLOCK]` 后才能创建 PR。
- 创建 PR 后停止，未经用户明确授权不得合并。
- 修改前端静态资源后必须重新编译或重启 Monitor 验证嵌入资源。
- Reporter 保持 Go 标准库零外部依赖和绝对 Fail-Safe。
- Domain 层不得反向依赖 Application、Infrastructure 或外部服务。
- 对外暴露 Task 时必须使用深拷贝快照，异步持久化不得读取可变聚合对象。

---

## 3. 当前能力基线

### 3.1 领域与状态机

当前已经实现：

- `Task` 会话聚合根、`Turn`/Run 实体和 `TimelineItem` 时间线值对象。
- Running、Completed、Failed 三类会话状态。
- 单次工具失败不升级为会话失败。
- 同一 Session ID 跨 50+ 轮的 Multi-Turn Run 聚合。
- 上一轮缺少 Stop 时由下一轮 Prompt 自动收口。
- Cursor `afterAgentResponse` 的完成兜底。
- 空会话和幽灵卡片过滤。
- 父子任务、子 Agent 数量、独立子任务和定向指引。
- `Task.Clone()` 深拷贝和版本字段。
- 启发式短标题、可选 LLM 标题与会话目标总结。

### 3.2 应用服务

当前已经实现：

- Hook 事件接收、状态机应用、快照序列化、异步落盘和 SSE 广播。
- 软中断请求与 Reporter 前置 Hook deny 决策。
- 根 Agent、指定子 Agent 类型、子 Agent ID 和独立子任务的 Live Steer。
- 已结束任务 TTL 清理。
- 服务启动时从 JSON Repository 恢复历史任务。
- LLM 标题和 `goalSummary` 的串行异步刷新。

### 3.3 HTTP 与实时流

当前已经实现：

- `POST /api/event`。
- `GET /api/stream`。
- `GET /api/tasks`、单任务读取、单个和批量删除。
- Abort、Kill、Steer 和 Inject Context API。
- Bearer Token、`X-API-Key`、单 Key、多项目 Key 和 Master Key。
- 按租户过滤任务列表、SSE 和控制操作。
- 请求体大小限制、SSE 心跳和非阻塞广播。

### 3.4 持久化

当前已经实现：

- 每任务一个 JSON 文件。
- 临时文件、`Sync`、原子 `Rename`。
- 32 路分片锁。
- 启动时清理过期临时文件。
- 损坏文件不阻断其他任务加载。
- 应用层单消费者异步写队列。

### 3.5 Reporter

当前已经实现：

- Cursor、ZCode、Codex CLI/Desktop 的正式接入链路。
- Claude Code、Aider、Windsurf、Trae、Continue 等环境嗅探和部分兼容逻辑。
- snake_case、camelCase 和多种 Hook Payload 兼容。
- Hook 名称归一化和工具失败保护。
- 不同 Agent 的 Fail-Safe 放行响应。
- 800ms HTTP 超时、连接池复用、熔断和离线 spool 补发。
- Unix 跨进程 spool 文件锁。
- 默认 `#task` 跟踪、`#drop`/`#untrack` 取消跟踪。
- 全局配置、项目配置、环境变量和 CLI 参数优先级。
- Transcript 提取和纯 Go Git 根目录/分支解析。

### 3.6 Web 看板

当前已经实现：

- Running、Completed、Failed 动态分栏。
- Agent 筛选和任务顶层字段搜索。
- Run Matrix、实时秒表和累计生命周期。
- 父子 Agent Pipeline 和抽屉 Waterfall。
- 多轮 Run 切换、Prompt、AI 回复、Timeline 和 Markdown 安全渲染。
- 软中断、强杀、Live Steer、Follow-up、批量删除和导出。
- 桌面通知、鉴权设置、中英文切换和响应式布局。
- `requestAnimationFrame` 合并 SSE 高频渲染。
- PWA Manifest、Service Worker 和静态离线壳。

### 3.7 Cursor 扩展与发布

当前已经实现：

- 编辑器 Dashboard Webview、Activity Bar 侧栏和状态栏实时计时。
- 本地 Daemon 检测、自启和重启。
- 工作区 `.cursor/hooks.json` 自动配置和路径刷新。
- LLM、Server URL、API Key 和状态栏设置。
- Linux、macOS、Windows 跨平台二进制发布。
- Linux amd64/arm64 离线包。
- systemd 用户服务和系统服务安装。
- GHCR、可选 Docker Hub 和 Cursor VSIX Release。

---

## 4. 总体问题判断

下一阶段的主要矛盾不是“页面或按钮不够多”，而是四类基础保证尚未完全闭合：

1. **身份与数据隔离**：鉴权身份和业务 Payload 尚未彻底解耦，任务索引没有使用租户复合主键。
2. **持久化一致性**：队列满旁路、删除与 Save 竞态、关闭时未排空可能导致磁盘状态倒退或任务复活。
3. **控制安全**：Kill 信任 Reporter 上报 PID，远程部署时缺少主机和进程身份验证，PGID 语义未完整落地。
4. **实时最终一致性**：SSE 是 best-effort，断线、慢消费者和删除事件丢失后缺少权威对账与重放。

因此路线图遵循以下顺序：

1. 先保证正确性和安全。
2. 再提升高频检索和操作效率。
3. 再扩大 Agent 和 Cursor 扩展生态。
4. 最后建设 Event Log、Trace 和数据平台能力。

建议未来 2–3 个迭代的投入比例：

- 正确性与安全：45%。
- 检索和操作体验：25%。
- Agent 与扩展生态：15%。
- Trace 与平台化：15%。

---

## 5. 优先级和阶段总览

### Phase 0：建立基线与协议约束

目标：在修改核心身份、持久化和 SSE 协议前，建立可重复验证的行为基线。

工作内容：

- 为租户伪造、Session ID 冲突、Save/Delete 竞态、队列关闭和 SSE 重连补充失败复现测试。
- 记录当前 JSON Schema、SSE 消息格式和历史数据兼容要求。
- 增加迁移测试夹具，覆盖无 `keyId`、旧 `repo:branch` 和旧 Run 数据。
- 明确控制操作只支持本机还是支持远端代理执行。
- 定义类型化应用错误和 API Error Schema。

完成标准：所有已知问题均有失败测试或明确的不可自动化验证步骤。

### Phase 1：可信实时链路

目标：完成多租户、持久化、SSE 和控制安全的基础闭环。

包含：

1. 可信鉴权身份绑定。
2. 租户 + Task ID 复合主键。
3. 有序持久化命令流与优雅停机。
4. SSE 快照对账和前端操作错误处理。
5. Kill 本机安全模式和真实进程组控制。
6. 严格 HTTP 方法、JSON 校验和类型化错误。

### Phase 2：高频效率与可运营性

目标：让大量任务环境下更易定位、筛选、分享和维护。

包含：

1. `filter_repos` 仓库白名单。
2. Repo/Branch Schema 修复。
3. 深度搜索和组合筛选。
4. 排序、分组和保存视图。
5. 深链接和 Follow Latest。
6. 健康检查、损坏文件隔离和基础指标。
7. Reporter 本地状态 TTL 与响应体限制。

### Phase 3：实时可靠性和产品成熟度

目标：提供可恢复的实时流、完整无障碍、规模化前端和更稳定的 PWA。

包含：

1. SSE v2 事件序号、重放和重同步。
2. 前端增量 DOM 更新、分页或虚拟列表。
3. 完整键盘导航、焦点管理和 Reduced Motion。
4. 国际化完整化与本地化格式。
5. 离线最近快照、安装入口和版本更新提示。
6. 浏览器级端到端测试。

### Phase 4：Agent 与 Cursor 生态扩展

目标：扩大可正式支持的 Agent 类型并提升 Cursor 插件开箱成功率。

包含：

1. Claude Code 正式适配。
2. Aider、Windsurf、Trae、Continue 的可行性验证和分级支持。
3. Cursor 多根工作区 Hook 管理。
4. VSIX 跨平台内置二进制选择。
5. Hook 配置预览、诊断、撤销和扩展自动化测试。

### Phase 5：Agent APM 平台化

目标：从本地实时看板升级为可回放、可查询、可诊断的 Agent APM。

包含：

1. 幂等 Event Log。
2. Reporter 事件序号和乱序检测。
3. 精确 Trace Span、关键路径和卡点诊断。
4. 历史分页检索和统计。
5. 项目管理、配额、归档、审计和脱敏导出。

---

## 6. Phase 0：基线和设计决策

### 6.1 行为基线测试

新增测试必须覆盖：

- 项目 Key 请求在 Payload 中伪造其他 `key_id`。
- 两个租户使用相同 Session ID。
- 同一任务快速产生多个版本且写队列拥塞。
- Delete 发生在旧 Save 尚未消费时。
- 服务关闭前写队列仍有数据。
- SSE 断线期间发生删除，重连后客户端清理旧任务。
- 慢 SSE 客户端通道满时的行为。
- Kill 指向远端主机、PID 重用、进程启动时间不匹配和 PGID 不匹配。
- Repository 文件名归一化碰撞。
- 损坏 JSON、权限错误和旧 Schema 恢复。

### 6.2 需要先确定的架构决策

#### ADR-1：任务身份

推荐使用内部值对象：

- `TenantID`：来自可信鉴权上下文。
- `TaskID`：Agent 会话 ID。
- `TaskKey`：`TenantID + TaskID` 的内部复合键。

HTTP URL 可以继续使用 Task ID，但所有 Service 方法必须同时接收 Tenant Scope。Master 全局操作必须显式选择租户，不允许依赖 Payload 内 `key_id`。

#### ADR-2：持久化顺序

推荐统一为单一 Persistence Command Stream：

- `SaveTask{TaskKey, Version, JSON}`。
- `DeleteTask{TaskKey, Version}`。
- 同一 TaskKey 只允许版本递增。
- Delete 以墓碑版本压制旧 Save。
- 队列拥塞时合并同任务的旧 Save，禁止启动旁路 goroutine 直接写。

#### ADR-3：SSE 一致性

Phase 1 先实现 Snapshot Reconciliation；Phase 3 再实现 Event Replay。

Phase 1 协议建议：

- `snapshot_start`：包含 `generation` 和租户范围。
- 多条 `task_upsert`。
- `snapshot_end`：包含 `generation` 和任务 ID 集合摘要。
- 实时增量携带同一连接的 generation。

Phase 3 增加：

- SSE `id:`。
- `Last-Event-ID`。
- 每租户环形缓冲。
- `resync_required`。

#### ADR-4：Kill 控制模型

推荐：

- 单机部署允许 Monitor 验证后直接控制本机进程。
- 中央部署不直接操作 Monitor 主机 PID，而是将控制命令排队，由目标主机 Reporter/Companion 拉取并本机执行。
- Phase 1 默认关闭无法验证主机身份的 Kill。

---

## 7. Phase 1 详细工作包

## 7.1 WP-1：可信鉴权身份绑定

### 问题

事件 Payload 中的 `key_id` 属于不可信业务输入。非 Master 请求如果能够指定或保留该字段，可能改变任务归属。

### 实现内容

- HTTP Handler 根据鉴权结果构造不可伪造的 `RequestScope`。
- 非 Master 请求无条件使用 `RequestScope.TenantID`，忽略 Payload 的 `key_id`。
- Master 请求如果需要向特定租户写入，使用独立 Header、URL 参数或显式 Admin DTO。
- Application Service 不再从普通 Event DTO 推断租户身份。
- Domain `EventPayload.KeyID` 逐步降级为兼容字段，最终由可信调用方填充。
- 记录拒绝的越权尝试，但日志不得包含 Prompt、AI 回复或 API Key。

### 测试

- 项目 A Key 携带项目 B `key_id`，任务仍归项目 A。
- 项目 A 不能修改项目 B 已存在任务。
- Master 未指定目标租户时使用明确默认语义。
- 常量时间 Key 比较行为不回退。

### 验收

- 普通客户端无法通过 JSON 修改租户归属。
- Service 层测试不依赖 HTTP Handler 也能证明租户约束。
- API 文档更新可信身份来源。

## 7.2 WP-2：租户复合任务键

### 问题

内存 Map 和 JSON 文件名只以 Session ID 为主要键，不同租户同 ID 时可能互相覆盖。

### 实现内容

- Domain 或 Application 增加 `TaskKey` 值对象。
- `MonitorService.tasks` 改为以复合键索引。
- 所有查询、删除、Abort、Kill、Steer 和 Inject Context 方法显式接收 Tenant Scope。
- Repository 接口改为按 `TaskKey` 保存、读取和删除。
- 持久化目录推荐采用：`<dataDir>/<tenantHash>/<safeTaskID>-<taskHash>.json`。
- Master 列表返回时保留原 `id` 和 `keyId`，前端本地索引改为复合键。
- SSE 删除消息使用复合任务引用，旧前端兼容期可同时发送 `id` 和 `keyId`。

### 数据迁移

- 旧根目录 JSON 无 `keyId` 时迁移到 `default` 租户。
- 有 `keyId` 时迁移到对应租户目录。
- 文件移动使用原子 Rename；跨文件系统时使用复制、Sync、Rename 和旧文件删除。
- 迁移失败不得阻止服务启动，应隔离并报告健康状态。
- 第一个兼容版本仍支持读取旧目录，但新写入只使用新布局。

### 测试

- 两租户同 ID 同时运行、更新、删除和恢复。
- Master 视图中两任务均存在且可区分。
- 文件名非法字符和归一化碰撞。
- 旧数据迁移幂等性。

### 验收

- 任意两个租户使用相同 Session ID 不共享内存对象或磁盘文件。
- 删除项目 A 任务不影响项目 B。
- `go test -race ./...` 无竞态。

## 7.3 WP-3：有序持久化命令流

### 问题

写队列满时的旁路写、Delete 与 Save 分离、服务关闭不排空可能导致版本倒退、删除复活或最后事件丢失。

### 实现内容

- Save 和 Delete 统一进入同一持久化命令流。
- 命令携带 `TaskKey`、`Version`、操作类型和不可变 JSON。
- 同 TaskKey 维护最后提交版本，拒绝旧版本落盘。
- 队列压力下按 TaskKey 合并连续 Save，只保留最新版本。
- Delete 写入墓碑状态，压制版本更低的 Save。
- 对队列长度、合并次数、丢弃旧版本次数和持久化错误增加指标。
- `MonitorService.Close(ctx)`：停止接收新命令、排空队列、等待 worker 和总结任务。
- `main.go` 使用 `http.Server`、信号处理和有超时的 `Shutdown()`。
- SSE 长连接在 Shutdown 时收到上下文取消并退出。

### 测试

- 100+ 并发更新后磁盘版本等于最高版本。
- 队列满时不产生旁路乱序。
- Delete 后旧 Save 不会复活文件。
- Close 在超时内排空；超时时返回明确错误。
- SIGTERM 集成测试验证最后任务可恢复。

### 验收

- Repository 永不以更低版本覆盖更高版本。
- 服务正常关闭后内存最终状态与磁盘一致。
- 关闭过程没有 goroutine 泄漏。

## 7.4 WP-4：SSE 快照对账与前端可信操作

### 问题

重连时服务端只发送当前任务，前端不清理本地旧任务；前端部分删除操作即使 HTTP 失败也会修改本地状态。

### 实现内容

- 实现 `snapshot_start`、`task_upsert`、`snapshot_end` 消息。
- 前端为每次连接维护 generation 和 snapshotSeen 集合。
- `snapshot_end` 后仅删除当前租户范围内未出现的旧任务。
- 快照期间收到的增量按版本或序号合并，禁止旧快照覆盖新数据。
- `apiFetch()` 对所有非 2xx 响应抛出类型化错误。
- 删除、清空、Abort、Kill 和 Steer 增加 Pending 状态并防重复提交。
- 只有服务端确认后才改变最终 UI 状态。
- 失败时显示可操作错误；鉴权失败继续提示重新配置 Key。
- 删除可先实现“服务端确认后消失”，Undo 作为后续可选项。

### 测试

- 断线期间删除任务，重连后旧卡片消失。
- 快照过程中产生新事件，最终版本不倒退。
- 不同租户快照互不清理。
- DELETE 返回 500/403/404 时 UI 不伪装成功。
- 批量删除部分失败时显示结果摘要。

### 验收

- 重连后前端 Task 集合与服务端权威快照一致。
- 所有破坏性操作都以服务端响应为准。
- 不再存在空 `catch` 静默吞掉关键操作错误。

## 7.5 WP-5：安全 Kill 与真实进程组控制

### 问题

当前 Kill 信任上报 PID；远程部署可能误杀 Monitor 主机同号进程；PGID 尚未形成完整验证和执行语义。

### 实现内容

- Task 进程身份增加 `HostID`、`BootID`、`PID`、`PGID`、`ProcessStartTime` 和可选命令指纹。
- Reporter 仅使用标准库采集可用信息；无法采集时明确降级。
- Monitor 记录自身 HostID/BootID，仅允许直接 Kill 同主机、同 Boot 且启动时间匹配的进程。
- Unix 使用负 PGID 向进程组发送 SIGTERM，等待后可升级 SIGKILL。
- Windows 明确实现 Job Object/进程树能力，或在首版标记只支持单进程终止。
- Kill API 返回 `requested`、`terminated`、`forced`、`rejected` 或 `unknown`，不再无条件标记成功。
- 远程任务显示“仅支持软中断”或走未来 Companion 控制队列。
- 增加显式环境变量启用危险控制能力，默认采用安全值。

### 测试

- HostID 不匹配拒绝。
- BootID 不匹配拒绝。
- PID 启动时间不匹配拒绝。
- 测试进程组中的父子进程均被终止。
- Kill 失败时领域状态不伪造为 killed。

### 验收

- Monitor 不会仅凭远端 Payload PID 杀本机进程。
- 所有控制结果可审计且与实际执行结果一致。

## 7.6 WP-6：严格 HTTP 和类型化错误

### 实现内容

- 每条路由只接受明确方法，其他方法返回 405 和 `Allow`。
- JSON 解码检查语法错误、空 Body、多个顶层值和超限。
- 根据兼容策略决定是否启用 `DisallowUnknownFields()`；至少新 Admin API 必须严格。
- 定义统一错误结构：`code`、`message`、`requestId` 和可选 `details`。
- Application 定义可比较的错误类型：NotFound、Forbidden、Conflict、InvalidArgument、Unavailable。
- HTTP 层统一映射状态码，避免依赖字符串内容。
- 增加可配置 CORS Origin 白名单。
- 长期 API Key 不再推荐放入 SSE URL；先增加日志脱敏，后续增加短期 Stream Ticket。

### 验收

- API 行为稳定、可测试、文档化。
- 不存在的方法和畸形请求不会返回误导性 200。

---

## 8. Phase 2 详细工作包

## 8.1 WP-7：启用 `filter_repos`

### 实现内容

- 全局和项目配置均解析 `filter_repos`。
- 定义空值、空数组和覆盖继承语义。
- 支持仓库名称精确匹配；Glob 支持可作为后续扩展。
- 在 Git 信息解析后、事件发送前执行过滤。
- 无 Git 仓库时定义 `unknown` 或跳过行为。
- `agent-reporter config` 输出最终生效白名单和来源。

### 验收

- 不在白名单的仓库不产生网络请求或 spool。
- 项目配置可以覆盖全局设置。
- Fail-Safe 行为不受影响。

## 8.2 WP-8：Repo/Branch Schema 修复

### 实现内容

- Reporter Event DTO 分别发送 `repo` 和 `branch`。
- `repo` 只保留仓库名称或标准化仓库标识。
- Domain 和前端兼容读取旧 `repo:branch`。
- 数据恢复时可懒迁移；新写入使用新格式。
- 搜索、筛选、导出和卡片展示分别使用 Repo 与 Branch。

### 验收

- 新任务 `branch` 不为空时能独立筛选。
- 历史数据展示不回退。

## 8.3 WP-9：深度搜索和组合筛选

### 实现内容

搜索范围扩展到：

- Task 标题、`rootGoal`、`goalSummary`、Repo、Branch、Agent、Tenant 和 ID。
- 所有 Run Prompt、AI Response、Detail 和 Timeline 描述。
- 子 Agent 类型、ID 和独立子任务标题。

筛选维度：

- 状态。
- 多 Agent 类型多选。
- Repo 和 Branch。
- 租户。
- 时间范围。
- Run 数量区间。
- 是否包含失败工具、子 Agent 或未完成 Run。

性能要求：

- 为每个 Task 缓存标准化搜索文本。
- 仅 Task 版本变化时重建索引。
- 输入保持 IME 保护和 debounce。
- 大数据量后转为后端分页搜索。

体验要求：

- 显示命中来源，例如“Run #4 AI 回复”。
- 支持一键清空全部筛选。
- 筛选状态可写入 URL。

## 8.4 WP-10：排序、分组和保存视图

### 实现内容

排序：

- 最近事件。
- 创建时间。
- 当前 Run 耗时。
- 累计生命周期。
- Run 数量。
- 失败优先。

分组：

- Repo。
- Branch。
- Agent。
- Tenant。

保存视图：

- 使用 Local Storage 保存筛选、排序和分组。
- 提供“运行中”“最近失败”“长时间运行”“指定仓库”等预设。
- 每秒计时更新不得造成持续重排；按耗时排序可使用固定刷新频率或手动刷新。

## 8.5 WP-11：深链接与 Follow Latest

### 实现内容

- URL Query/Hash 表达 `task`、`tenant`、`run`、`q`、`status`、`agent` 和视图。
- 浏览器前进/后退同步抽屉和筛选状态。
- 提供“复制当前视图链接”。
- API Key 永不写入分享 URL。
- 抽屉默认 Follow Latest。
- 用户主动查看旧 Run 后暂停自动跟随。
- 新 Run 到达时显示可点击提示。
- 任务不存在或无权限时显示明确状态。

## 8.6 WP-12：健康检查、损坏文件隔离和指标

### 实现内容

- `/healthz`：进程存活。
- `/readyz`：Repository 可用、迁移完成、写队列可接受命令。
- `/api/metrics` 或 Prometheus 文本指标，可通过配置启用。
- 指标包括任务数、租户数、事件吞吐、写队列长度、合并次数、持久化错误、SSE 客户端、广播丢弃和 LLM 失败。
- 损坏 JSON 移入 `quarantine/`，保留错误元数据但不记录敏感正文。
- 健康 API 展示损坏文件数量和最后错误时间。
- `SafeFilename` 使用可读前缀加原始 ID 短哈希，避免归一化碰撞。

## 8.7 WP-13：Reporter 运行卫生

### 实现内容

- 对服务端响应体使用 `io.LimitReader`。
- tracked、dropped、aborting、prompts 和 circuit breaker 状态文件增加 TTL。
- dropped TTL 必须足够长，但不能永久阻止未来复用的 Session ID。
- 限制 Prompt 历史文件大小和最大轮数。
- 清理由低概率惰性触发或显式维护命令执行，避免每次 Hook 全目录扫描。
- Windows spool 使用真正的跨进程锁；无法实现时采用每进程 spool 再安全归并。

---

## 9. Phase 3 详细工作包

## 9.1 WP-14：可靠 SSE v2

### 协议能力

- 每租户单调递增事件 ID。
- SSE 输出 `id:` 和明确 `event:` 类型。
- 客户端使用 `Last-Event-ID` 重连。
- 服务端维护固定容量环形缓冲。
- 缓冲覆盖后返回 `resync_required`，客户端执行完整快照。
- Master 视图使用全局序号或带租户的复合游标。
- 服务重启后的游标语义必须明确：持久化 epoch 或要求全量重同步。

### 验收

- 短暂断线可无损重放。
- 超出缓冲窗口时自动回到权威快照。
- 慢客户端不会阻塞 Hub，且丢包可观测。

## 9.2 WP-15：前端规模化性能

### 实现内容

- 按 Task 版本进行 keyed DOM patch。
- 仅刷新受影响的状态列。
- 抽屉只在选中 Task 或其子任务变化时刷新。
- 缓存父子关系、搜索文本和 Run 摘要。
- 历史任务支持分页、虚拟列表或“最近 N 条 + 加载更多”。
- 后台标签页降低计时器和视觉刷新频率。
- 增加渲染耗时、卡片数量和更新批次开发指标。

### 性能验收建议

- 1,000 个历史任务时搜索输入仍保持可用。
- 每秒 50 个事件时不出现持续主线程长任务。
- 未变化卡片不重新构造完整 HTML。

## 9.3 WP-16：键盘、焦点和可感知状态

### 实现内容

- 抽屉与 Modal 使用标准 Dialog 语义。
- 打开时保存原焦点并聚焦标题或关闭按钮。
- 关闭后恢复焦点。
- 实现焦点陷阱和背景 `inert`。
- Run Listbox 支持方向键、Home、End、Enter 和 Escape。
- 批量模式下鼠标与键盘操作一致。
- Toast 使用 `aria-live`，失败操作可使用 assertive。
- 高频状态播报聚合，避免屏幕阅读器噪音。
- 支持 `prefers-reduced-motion`。
- 所有交互元素有清晰 `focus-visible`。

## 9.4 WP-17：国际化完整化

### 实现内容

- 将动态 Run、事件、批量操作、Steer 提示和导出文案全部放入字典。
- 首次访问使用系统语言，之后遵循用户选择。
- 使用 `Intl.NumberFormat`、`Intl.DateTimeFormat` 和统一时长格式。
- Markdown 导出按当前语言生成，同时保持机器可解析字段稳定。
- 增加开发期缺失翻译 Key 检查。
- 专有术语 Run、Hook、SSE、Fail-Safe、Steer 保持统一。

## 9.5 WP-18：完整 PWA 生命周期

### 实现内容

- IndexedDB 保存最近一次已授权任务快照。
- 缓存按 Tenant/Key 指纹隔离，不保存明文 API Key。
- 离线时明确显示只读和最后同步时间。
- 恢复在线后执行权威对账。
- 支持 `beforeinstallprompt` 安装入口。
- 检测等待中的新 Service Worker 并提示刷新升级。
- 缓存完整核心静态资源和字体。
- 提供清除本地离线数据入口。

### 安全要求

- 默认限制缓存保留时间和任务数量。
- Prompt、AI 回复和 Transcript 路径视为敏感数据。
- 多租户切换不得展示上一租户离线数据。

## 9.6 WP-19：浏览器端到端测试

推荐使用独立测试进程和随机端口，不启动第二个常驻 `:8000` Daemon。

覆盖：

- SSE 初始快照、增量、删除和重连。
- 搜索、筛选、排序和保存视图。
- 卡片、抽屉和 Run 键盘操作。
- 多轮 Run 和 Follow Latest。
- 中英文切换。
- Abort、Kill、Steer 和删除失败反馈。
- API Key 切换和租户隔离。
- Service Worker 静态离线壳和版本更新。
- 移动端布局和基础无障碍扫描。

---

## 10. Phase 4：Agent 与 Cursor 生态

## 10.1 WP-20：Agent 适配成熟度模型

将 Agent 支持分为：

- **Official**：生命周期、工具失败、多轮、Transcript、Fail-Safe 和安装文档均有自动测试。
- **Beta**：核心生命周期可用，但部分工具或 Transcript 能力有限。
- **Experimental**：仅提供通用 HTTP/通知适配，不承诺完整状态机语义。

每个 Agent 正式化必须完成：

1. 官方 Hook 名称和 Payload 样本。
2. Session ID、Turn、Prompt、工具、错误和 Transcript 映射。
3. 正常结束、用户中断和致命失败映射。
4. 工具失败不升级为会话失败。
5. Fail-Safe 合法响应和退出码 0。
6. 安装、卸载、冲突和重复 Hook 说明。
7. 至少一组端到端录制夹具。

建议顺序：Claude Code → Aider → Continue → Windsurf/Trae。

## 10.2 WP-21：Cursor 多根工作区

### 实现内容

- 枚举所有 Workspace Folder，而不是只处理第一个目录。
- 用户选择需要配置的目录或批量配置。
- 显示每个目录 Hook 状态、Reporter 路径和冲突。
- 配置前展示 Diff，支持备份和撤销。
- 不覆盖非 Agent Monitor Hook。
- 检测用户级和项目级重复事件。

## 10.3 WP-22：VSIX 跨平台二进制

### 实现内容

- Release 构建所有支持平台的 Monitor/Reporter。
- VSIX 按 `bin/<os>-<arch>/` 打包。
- 扩展运行时选择对应二进制。
- 不支持平台给出明确安装指引。
- 校验二进制 checksum 和可执行权限。
- 避免把仅 linux/amd64 的二进制当作通用内置包。

## 10.4 WP-23：扩展诊断和测试

### 实现内容

- Daemon 日志 Output Channel。
- “诊断 Agent Monitor”命令，检查端口、版本、API Key、Hook、二进制和 SSE。
- Hook 配置错误定位到具体工作区和事件。
- Daemon 设置变更后的重启结果可见。
- 增加 TypeScript 单元测试和 VS Code Extension Host 测试。
- Release 前执行扩展构建、测试和 VSIX 安装烟测。

---

## 11. Phase 5：Agent APM 平台化

## 11.1 WP-24：幂等 Event Log

### 数据模型

每个 Reporter 事件增加：

- `eventId`：全局或会话内唯一。
- `reporterId`：Reporter 实例标识。
- `sequence`：会话或 Reporter 单调序号。
- `occurredAt`：事件发生时间。
- `receivedAt`：服务端接收时间。
- `schemaVersion`。

### 存储模型

- Append-only Event Log 保存原始标准化事件。
- Task JSON/数据库记录作为投影快照。
- Event ID 幂等去重。
- 检测迟到、重复和乱序事件。
- 支持从事件回放重建 Task。
- 定义日志保留、压缩、归档和敏感字段脱敏。

存储技术可评估 SQLite、BoltDB 或分段 JSONL。选择必须满足：

- 单二进制。
- 无外部服务。
- 可原子提交。
- 支持索引和分页。
- 迁移可控。

## 11.2 WP-25：精确 Trace Span

### 数据模型

Timeline/Span 增加：

- 精确时间戳。
- `spanId`、`parentSpanId`。
- 工具名和调用 ID。
- 开始、结束、耗时和状态。
- 输入输出大小和脱敏摘要。
- Agent/Subagent 身份。

### 产品能力

- before/after 事件配对。
- 真实 Waterfall。
- 根 Agent 与子 Agent 关键路径。
- 慢工具、失败工具和长空白区间。
- 按工具、状态、耗时和 Agent 过滤。
- 点击 Span 查看安全摘要和错误。

### 安全

- 默认不保存完整工具输入输出。
- 对密钥、Token、环境变量和文件正文做长度限制与脱敏。
- 导出时提供安全模式。

## 11.3 WP-26：历史检索与统计

查询能力：

- 状态、Agent、Repo、Branch、Tenant、时间范围。
- 分页、稳定排序和游标。
- Prompt、AI 回复和 Timeline 全文检索。
- JSON、JSONL 和脱敏 Markdown 导出。

统计能力：

- Run 耗时平均值和百分位数。
- 工具失败率。
- 每会话 Run 数量。
- 子 Agent 使用率。
- 活跃任务峰值。
- Abort/Kill 比例。
- Reporter 离线 spool 和补发情况。

## 11.4 WP-27：项目管理、配额和审计

后续可增加：

- 项目元数据和 Key 轮换。
- 项目级保留周期和任务配额。
- Master 审计日志。
- 操作人、时间、目标任务和结果记录。
- 归档、恢复和合法删除。
- 敏感字段策略和脱敏导出。

此工作不应先于 Phase 1 的租户隔离和类型化权限模型。

---

## 12. 文档与实现不一致项

路线图实施过程中应同步修复以下已识别差异：

1. README/集成文档将 Claude Code、Aider 等标为 Pending，但 Reporter 已存在嗅探和部分兼容逻辑；应按成熟度模型准确描述。
2. `filter_repos` 出现在配置结构或输出中，但尚未完整加载和执行。
3. Domain 有独立 `Branch` 字段，但 Reporter 当前主要将 Repo 与 Branch 拼接。
4. README 的 HTTP API 概览未覆盖全部任务控制 API。
5. PWA 的“离线”主要指静态壳离线，不代表任务数据离线可用，应避免误导。
6. Cursor 扩展文档需要明确 VSIX 内置二进制的平台范围和外部 Daemon 行为。
7. 安装文档章节编号存在重复，应在后续文档整理 PR 中修复。
8. 对 Kill 的产品描述必须区分软中断、单进程终止和进程组终止。

文档修改仍需保持中英文结构对称，`llms.txt` 保持全英文。

---

## 13. 测试战略

### 13.1 单元测试

- Domain：状态机、TaskKey、版本、墓碑、Span 配对。
- Application：租户约束、命令流、Close、控制决策和错误类型。
- Persistence：迁移、版本拒绝、损坏隔离和文件名哈希。
- Reporter：配置优先级、过滤、事件序号、TTL、响应限制和平台差异。
- HTTP：方法、JSON、鉴权、错误映射、Snapshot 和 Stream Ticket。

### 13.2 竞态与压力测试

必须继续执行：

```bash
go test -v -race ./...
```

增加：

- 同任务高并发事件。
- 多租户同 ID。
- 大量 SSE 客户端订阅和断开。
- 写队列拥塞和关闭。
- TTL 清理与实时写入并发。

### 13.3 集成测试

- 临时目录 Repository。
- 随机端口 HTTP Server。
- 真实 SSE 客户端。
- 子进程组 Kill 测试，仅在支持平台执行。
- 旧版本数据目录迁移。
- Reporter spool 断网和恢复。

### 13.4 浏览器测试

在 Phase 3 引入，覆盖主要用户路径和无障碍行为。测试进程使用随机端口，不得与本机唯一 `:8000` Daemon 冲突。

### 13.5 发布测试

- 所有 Go 平台交叉编译。
- Linux 离线包安装。
- systemd 用户/系统服务。
- Docker amd64/arm64。
- VSIX 对应平台启动和 Hook 配置。
- 升级保留旧数据和配置。
- checksum 验证。

---

## 14. 可观测性目标

新增能力自身必须可观测：

- Hook 接收成功、拒绝和解析失败计数。
- 每租户 Task 数和活跃 Run 数。
- 写队列长度、合并、墓碑拒绝和错误。
- SSE 客户端、重放、快照、丢弃和重同步。
- Reporter spool 大小、补发数量、熔断状态和丢弃。
- Abort/Kill/Steer 请求与结果。
- LLM 请求耗时、失败和限流。
- 数据迁移和 quarantine 数量。

日志要求：

- 结构化或至少稳定键值格式。
- 不记录 API Key、完整 Prompt、完整 AI 回复或敏感工具参数。
- 每个 HTTP 请求可关联 `requestId`。
- 控制操作有审计记录。

---

## 15. 安全和隐私要求

所有阶段统一遵守：

1. API Key 不进入日志、分享 URL、导出或持久化任务正文。
2. SSE Query Token 逐步替换为短期 Ticket 或同源安全会话。
3. CORS 默认收紧为可配置 Origin。
4. Transcript、Prompt、AI 回复和工具参数均视为敏感数据。
5. 离线浏览器缓存按租户隔离并有保留上限。
6. Kill 必须验证主机和进程身份。
7. Master 操作与普通租户操作使用不同权限路径。
8. 导出支持脱敏模式。
9. 不提交 `.env`、凭证或本机绝对路径。
10. Repository quarantine 和错误日志不得泄露任务正文。

---

## 16. 兼容与迁移策略

### 16.1 Schema 版本

为持久化 Task、Reporter Event 和 SSE 协议分别增加版本号。版本变更遵循：

- 新服务至少读取上一个稳定版本。
- 新 Reporter 在兼容窗口内可向旧服务发送基础字段。
- 前端在协议切换期间识别旧 Task 消息和新类型消息。
- 不进行不可逆原地覆盖，迁移前保留备份或可恢复旧文件。

### 16.2 分阶段迁移

1. 先增加读取兼容和测试。
2. 再启用新写格式。
3. 运行一个稳定版本后移除旧写入。
4. 最后在 Major Version 中移除旧读取。

### 16.3 回滚

每项迁移必须说明：

- 是否可以回滚二进制。
- 新数据是否仍可被旧版本读取。
- 是否需要备份目录。
- 回滚时如何处理新字段、租户目录和墓碑。

---

## 17. PR 和多 Agent 拆分计划

以下为推荐 PR 顺序。每个 PR 从最新 `origin/master` 创建，前序合并后，依赖 PR 必须 rebase 并重新通过质量门禁。

### 第一组：Phase 0/1

1. `test/trusted-realtime-baseline`
   - 仅增加失败复现、夹具和 ADR。
2. `fix/trusted-tenant-context`
   - HTTP 与 Application 身份绑定。
3. `feat/tenant-task-key`
   - Domain、Service 和 Repository 复合键及迁移。
4. `fix/ordered-persistence`
   - Save/Delete 命令流、墓碑和 Close。
5. `feat/sse-snapshot-reconcile`
   - HTTP/SSE 协议和前端权威对账。
6. `fix/frontend-operation-errors`
   - 前端非 2xx、Pending 和失败反馈。
7. `fix/safe-process-control`
   - Reporter 进程身份、Domain 字段和安全 Kill。
8. `fix/strict-http-errors`
   - 方法、JSON、错误类型和 CORS。

这些 PR 涉及相同核心文件时不应并行修改同一 checkout。可并行的是：

- Domain TaskKey 设计与 HTTP 错误类型设计。
- Persistence 命令流与前端操作错误处理。
- Reporter 进程身份采集与 SSE 前端协议，但合并前需协调 Schema。

### 第二组：Phase 2

1. `feat/reporter-repo-filter`
2. `fix/repo-branch-schema`
3. `feat/dashboard-deep-search`
4. `feat/dashboard-saved-views`
5. `feat/dashboard-deep-links`
6. `feat/health-and-metrics`
7. `fix/reporter-state-hygiene`

### 第三组：Phase 3

1. `feat/sse-replay-v2`
2. `feat/dashboard-incremental-render`
3. `feat/dashboard-accessibility`
4. `feat/dashboard-i18n-complete`
5. `feat/pwa-offline-snapshot`
6. `test/dashboard-e2e`

### 第四组：Phase 4

按 Agent 和目录独立拆 PR；Cursor 扩展单独 worktree，只有扩展任务安装 npm 依赖。

### 第五组：Phase 5

Event Log、Trace 和查询平台必须先单独完成 ADR/原型，不应在一个 PR 中同时替换持久化、SSE 和前端。

---

## 18. 每个功能 PR 的完成定义

每个 PR 必须满足：

- 有明确问题陈述和非目标。
- 有兼容与迁移说明。
- 领域、应用、基础设施边界符合 DDD。
- 新并发路径有 Race 测试。
- Reporter 路径保持 Fail-Safe 和响应上限。
- 中英文用户文档同步更新。
- API 或 Schema 变化更新 `llms.txt`。
- 运行 `go test -v -race ./...`。
- 运行 `make review` 或 `make pre-pr`。
- 修复所有 `[BLOCK]`。
- 每个逻辑改动有清晰 Commit。
- PR 描述包含验证步骤和回滚说明。
- 不自动合并。

---

## 19. 发布里程碑建议

版本号仅为建议，实际按 Release 决策调整。

### v1.x Reliability

范围：Phase 0 和 Phase 1。

发布标准：

- 多租户同 ID 完全隔离。
- 持久化无倒退、无删除复活、可优雅关闭。
- SSE 重连后与服务端一致。
- Kill 不会误杀无法验证的进程。
- API 错误稳定。

### v1.x Productivity

范围：Phase 2。

发布标准：

- Repo 过滤和 Branch 数据可用。
- 深度搜索、组合筛选、保存视图和深链接稳定。
- 健康检查和基本指标可用于运维。

### v1.x Experience

范围：Phase 3。

发布标准：

- SSE 支持重放或自动重同步。
- 大任务量下前端仍流畅。
- 键盘、Reduced Motion 和国际化完整。
- 浏览器端到端测试进入 CI。

### v2.0 Agent APM

范围：Phase 4 和 Phase 5 的稳定部分。

发布标准：

- 多 Agent 适配成熟度清晰。
- Event Log 可回放。
- Trace 使用真实 Span。
- 历史查询和统计可分页。
- Schema 和迁移策略稳定。

---

## 20. 暂不优先投入的方向

以下方向有价值，但不应先于可信实时链路：

- 复杂账号注册和组织后台。
- 公有云同步服务。
- 插件市场。
- 大量主题和装饰性图表。
- 复杂 RBAC 权限编辑器。
- 默认保存完整工具输入输出。
- 在缺少 Event Log 前构建重型分析报表。

原因：当前产品的差异化是本地、轻量、实时和可控。过早扩大共享面或数据面会放大租户、安全和一致性风险。

---

## 21. 长期产品定位

Agent Monitor 最有潜力形成壁垒的三个方向是：

1. **跨 Agent 标准化事件协议**：把 Cursor、ZCode、Codex、Claude Code 等不同 Hook 统一为稳定领域语言。
2. **可恢复的实时状态与 Event Log**：实时看板断线可恢复，历史状态可审计和回放。
3. **真实 Trace、关键路径和卡点诊断**：从“看到 Agent 在做什么”升级为“解释为什么慢、失败在哪里、哪个子 Agent 阻塞”。

当这三项完成后，产品将从实时 Kanban/飞行仪表盘演进为本地优先、零外部服务依赖的 AI Coding Agent APM。

---

## 22. 推荐立即启动的第一个实施包

建议第一个实施包命名为 **Trusted Realtime Foundation**，范围严格限制为：

1. 可信租户身份绑定。
2. 两租户同 Session ID 的复现测试和复合键设计。
3. SSE Snapshot Reconciliation。
4. 前端所有破坏性操作以服务端非 2xx 为失败。
5. SSE、删除和租户隔离测试。

不在首个实施包中同时加入：

- Event Log。
- 全文搜索。
- UI 大规模重构。
- 新 Agent 适配。
- 离线任务缓存。

这样可以在范围可控的前提下，同时消除最重要的安全和产品可信度问题，并为后续持久化命令流、SSE v2 和平台化能力建立稳定协议基础。
