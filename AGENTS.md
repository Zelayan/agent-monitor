# AGENT MONITOR — 开发与协作规范

面向 AI Coding Agent（Claude Code、ZCode、Cursor Agent、Aider、Windsurf、Trae 等）的多会话实时监视器。
基于 Go 语言构建，前端通过 `go:embed` 内嵌，支持纳秒级 Hook 事件接收、SSE 实时状态广播与多轮会话时间线追踪。

---

## 1. 每次改动必须提交（Git 铁律）

- **改动即提交**：任何代码、配置、静态页面、文案改动完成后，**必须立刻 `git commit`**，严禁把未提交改动留在工作区。
- **逻辑拆分**：一次任务若包含多组独立改动，按逻辑拆成多次清晰的 commit，不要攒到最后一次性提交。
- **提交信息质量**：提交信息必须用 1–2 句清晰说明「为什么做了改动」以及核心收益，禁止只罗列文件名。
- **安全与私有路径**：
  - **严禁提交密钥、凭证、`.env` 文件或本机私有绝对路径**（如 `/Users/xxx/...`）。
  - 本地运行工作区 `.zcode/` 已被 `.gitignore` 忽略，禁止强制跟踪提交。
- **保护 Git 历史**：严禁修改全局 git config；严禁 `--amend`、force push、hard reset，除非用户明确要求。

---

## 2. 系统架构与分层约定 (DDD 规范)

项目采用严格的 **DDD（领域驱动设计）** 分层架构：

```
internal/
├── domain/task/          # 领域层：Task 聚合根、Turn 实体、TimelineItem 值对象、TaskRepository 接口、领域状态机
├── application/monitor/  # 应用层：MonitorService 用例编排、SSE Hub 广播管理
├── infrastructure/       # 基础设施层：
│   ├── persistence/      # 本地持久化（JSONRepository 文件原子写入）
│   └── transport/http/   # 用户接口/适配器层（HTTP Handler、SSE 流、CORS、心跳与请求体保护）
├── reporter/             # 上报器核心逻辑：协议响应、工具过滤、多 Agent 嗅探、Git/Transcript 提取
cmd/
└── reporter/main.go      # agent-reporter 独立二进制入口（0 外部依赖）
main.go                   # agent-monitor Web 服务入口（嵌入 static/index.html）
```

- **架构依赖单向性**：Domain 层为纯核心业务，严禁反向依赖 Application、Infrastructure 或外部网络库。
- **并发与数据隔离（Race-Free 铁律）**：
  - `Task` 聚合根在内存中受读写锁保护，向外部导出或进行 HTTP JSON 序列化时**必须通过 `Task.Clone()` 产生只读深拷贝副本**。
  - 异步磁盘持久化必须使用锁内预序列化的不可变 `taskJSON` 字节切片（`SaveRaw`），杜绝后台 goroutine 与实时内存对象的并发读写竞争（Data Race）。
- **静态资源嵌入**：Monitor 页面位于 `static/index.html`，由 `main.go` 通过 `go:embed` 打进二进制。**修改 HTML/CSS/JS 后必须重新编译或重启 `go run main.go` 才能生效。**

---

## 3. Hook 上报器设计规范 (`agent-reporter`)

`agent-reporter` 是所有 AI Agent 高频同步唤起的生命周期拦截器，必须遵守以下工业级准则：

1. **零外部依赖（0 Dependencies）**：
   - 必须使用 Go 官方标准库实现，严禁引入第三方库或 CGO（确保单二进制极简、跨平台秒级静态编译）。
2. **绝对 Fail-Safe（非阻塞放行铁律）**：
   - 无论上报器内部发生何种异常（JSON 损坏、Monitor 服务宕机、网络超时、文件读写失败、甚至是未知 panic），**必须向 stdout 输出合法放行协议（`{"continue":true}` 或 `{"permission":"allow"}`）并以状态码 `0` 退出**，严禁阻断用户的日常编码流程。
3. **微秒级低延迟**：
   - 使用包级单例 `http.Client` 复用 TCP 连接池；
   - 优先通过读取 `.git/HEAD` 轻量解析分支，避免高频工具调用时派生外部 `git` 子进程。
4. **多 Agent 动态嗅探**：
   - 自动根据环境变量（`CLAUDE_SESSION_ID`、`ZCODE_SESSION_ID`、`AIDER_SESSION_ID`、`CURSOR_PROJECT_DIR` 等）推断 Agent 类型，禁止在看板或核心逻辑中硬编码写死单一 Agent。

---

## 4. 领域状态机与事件流转规范

- **状态分类**：
  - **Running（运行中）**：`sessionStart`、`beforeSubmitPrompt`、`UserPromptSubmit` 等。
  - **Completed（已完成）**：`agentCompletion`、`Stop`、`stop`、`SessionEnd`、`sessionEnd`、`afterAgentResponse`（Cursor 回复已交付；`stop` 丢失时的兜底收口）等。
  - **Failed（异常中断）**：`failed`、`error`（会话级致命中断；Cursor `stop` 的 `aborted`/`error` 由上报器映射为 `failed`）。
- **工具局部失败保护**：
  - 单次工具执行失败（`toolFailure` / `PostToolUseFailure`，如 `grep` 退出码 1、测试失败）属于正常调试轨迹，**绝不能将整任务状态直接置为 `failed`**；应保持 `running`，并将错误信息记入时间线详情中。
- **多轮会话支持（Multi-Turn Runs）**：
  - 一个会话 ID 可跨越 50+ 轮连续对话。已完成的任务收到新的 `UserPromptSubmit` 时自动开启新的 Run（轮次），耗时与时间线按轮次隔离聚合。

---

## 5. 前端规范 (Linear / Sentry 紧凑暗色风格)

- **视觉规范**：
  - 背景 `#09090b`，容器 `#0c0d0e`，任务卡 `#111215`，边框 `#27272a`。
  - 状态色：运行中 `amber-400`，完成 `emerald-400`，失败 `red-400`。
  - 数据与耗时一律采用等宽字体 `font-mono tabular-nums`。
  - **禁止使用 Emoji**；所有状态图标一律采用 14px 单色描边 SVG；禁止大面积渐变与发光阴影。
- **交互与性能**：
  - 运行中卡片的秒表必须基于 `activeRunStart` 每秒递增；
  - 收到高频 SSE 广播时，必须使用 `requestAnimationFrame` 防抖合并刷新（`scheduleRender()`），杜绝布局抖动（Layout Thrashing）与 CPU 飙升；
  - 点击卡片从右侧滑出 400px 抽屉时间线，支持多轮 Run 切换（Run Matrix）与 ⌘K 全局搜索。

---

## 6. 构建、测试与发布规范

- **全量测试与竞态检测**：提交前必须运行 `go test -v -race ./...` 确保 100% 绿灯且无并发 Race。
- **编译命令**：
  - 本地构建：`make build`（输出 `bin/agent-monitor` 与 `bin/agent-reporter`）。
  - 跨平台交叉编译：`make build-all`（输出全平台二进制至 `dist/`）。
- **自动化发布**：
  - 推送 `v*` Tag（如 `git tag v1.0.0 && git push origin v1.0.0`）会自动触发 GitHub Actions：编译各平台压缩包并发布 GitHub Release；同时构建 linux/amd64 + linux/arm64 镜像并始终推送到 GHCR（`ghcr.io/zelayan/agent-monitor`）。
  - Docker Hub（`zelayan/agent-monitor`）仅在仓库 Secrets 配有 `DOCKERHUB_USERNAME`、`DOCKERHUB_TOKEN` 时一并推送。
  - 安装脚本 `install.sh` 会自动拉取最新 Release 产物；Linux 上默认注册并启动 systemd 用户服务（`INSTALL_SYSTEMD=0` 可关闭）。
