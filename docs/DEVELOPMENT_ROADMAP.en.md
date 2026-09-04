# Agent Monitor Complete Development Roadmap

[English](DEVELOPMENT_ROADMAP.en.md) | [简体中文](DEVELOPMENT_ROADMAP.md)

This document is based on the repository implementation as of 2026-09-04. It defines the complete product and engineering plan for the next stages of Agent Monitor, including requirements, architecture, migration, tests, acceptance criteria, and Pull Request boundaries.

> The roadmap does not imply that the current product lacks a usable foundation. Agent Monitor already provides an end-to-end flow from Hook reporting through the domain state machine, JSON persistence, SSE delivery, the web dashboard, and reverse control. The first objective is to make tenant isolation, real-time state, persistence, and control operations provably correct before expanding search, diagnostics, and platform capabilities.

---

## 1. Goals

This roadmap exists to:

1. Record the capabilities that already exist and avoid duplicate work.
2. Capture identified correctness, security, reliability, experience, and ecosystem gaps.
3. Define the full candidate feature set, priorities, dependencies, and expected value.
4. Split the roadmap into independently reviewable, testable, and reversible phases and PRs.
5. Define data model, API, frontend, migration, test, and acceptance requirements for each item.
6. Provide DDD-compatible directory boundaries for parallel Agent development.
7. State what should not be prioritized yet so the roadmap stays focused.

## 2. Governing Principles

All implementation work must continue to follow [`AGENTS.md`](../AGENTS.md):

- Branch from clean `origin/master` as `feat/<topic>` or `fix/<topic>`.
- Every Agent uses an isolated Git worktree or Cloud Agent; never share a checkout.
- Split work across `internal/domain`, `internal/application`, `internal/infrastructure`, `internal/reporter`, `extensions/cursor`, `static`, and `docs` boundaries.
- Commit each logical change immediately; do not leave finished work uncommitted.
- Before a PR, run `go test -v -race ./...` and `make review` or `make pre-pr`.
- Resolve every `[BLOCK]` issue before opening a PR.
- Stop after creating a PR. Never merge without explicit user authorization.
- Rebuild or restart Monitor after static frontend changes so embedded assets are verified.
- Keep Reporter zero-dependency, standard-library-only, and absolutely Fail-Safe.
- The Domain layer must not depend on Application, Infrastructure, or external services.
- Expose Tasks only through deep-cloned snapshots; asynchronous persistence must not read mutable aggregates.

---

## 3. Current Capability Baseline

### 3.1 Domain and state machine

Already implemented:

- `Task` session aggregate, Run/`Turn` entity, and `TimelineItem` value object.
- Running, Completed, and Failed session states.
- Tool failures do not escalate into session failures.
- Multi-Turn Run aggregation across 50+ turns under one Session ID.
- Automatic closure of a previous Run when its Stop is missing and the next prompt arrives.
- Cursor `afterAgentResponse` completion fallback.
- Empty-session and ghost-card filtering.
- Parent/child tasks, subagent counts, independent child tasks, and targeted guidance.
- `Task.Clone()` deep copies and aggregate versions.
- Heuristic short titles, optional LLM titles, and session goal summaries.

### 3.2 Application services

Already implemented:

- Hook ingest, state-machine application, snapshot serialization, asynchronous persistence, and SSE broadcast.
- Soft abort requests and Reporter pre-Hook deny decisions.
- Live Steer for root Agents, subagent types, subagent IDs, and independent child tasks.
- TTL cleanup for ended tasks.
- Startup recovery from the JSON Repository.
- Serialized asynchronous LLM title and `goalSummary` updates.

### 3.3 HTTP and live delivery

Already implemented:

- `POST /api/event`.
- `GET /api/stream`.
- `GET /api/tasks`, single-task reads, and single/bulk deletion.
- Abort, Kill, Steer, and Inject Context APIs.
- Bearer Token, `X-API-Key`, single-key, multi-project key, and Master Key authentication.
- Tenant-scoped task lists, SSE delivery, and control operations.
- Request-body limits, SSE heartbeat, and non-blocking broadcast.

### 3.4 Persistence

Already implemented:

- One JSON file per task.
- Temporary file, `Sync`, and atomic `Rename`.
- 32 sharded locks.
- Startup cleanup for stale temporary files.
- Corrupt files do not block loading other tasks.
- A single-consumer asynchronous write queue in the Application layer.

### 3.5 Reporter

Already implemented:

- Production integration paths for Cursor, ZCode, and Codex CLI/Desktop.
- Environment sniffing and partial compatibility for Claude Code, Aider, Windsurf, Trae, and Continue.
- snake_case, camelCase, and multiple Hook payload formats.
- Hook normalization and tool-failure protection.
- Agent-specific Fail-Safe allow responses.
- 800 ms HTTP timeout, connection reuse, circuit breaking, and offline spool replay.
- Unix cross-process spool locking.
- Default `#task` tracking and `#drop`/`#untrack` removal.
- Global config, project config, environment variable, and CLI precedence.
- Transcript extraction and pure-Go Git root/branch discovery.

### 3.6 Web dashboard

Already implemented:

- Dynamic Running, Completed, and Failed columns.
- Agent filtering and task-level search.
- Run Matrix, live stopwatch, and cumulative lifetime.
- Parent/child Agent Pipeline and drawer Waterfall.
- Multi-Run navigation, Prompt, AI response, Timeline, and sanitized Markdown rendering.
- Soft abort, force kill, Live Steer, Follow-up, bulk deletion, and export.
- Desktop notifications, authentication settings, Chinese/English switching, and responsive layout.
- `requestAnimationFrame` coalescing for high-frequency SSE updates.
- PWA Manifest, Service Worker, and a static offline shell.

### 3.7 Cursor extension and releases

Already implemented:

- Editor Dashboard Webview, Activity Bar sidebar, and live status-bar timer.
- Local Daemon detection, auto-start, and restart.
- Automatic workspace `.cursor/hooks.json` configuration and path refresh.
- LLM, Server URL, API Key, and status-bar settings.
- Cross-platform Linux, macOS, and Windows binary releases.
- Linux amd64/arm64 offline bundle.
- systemd user and system service installation.
- GHCR, optional Docker Hub, and Cursor VSIX releases.

---

## 4. Overall Assessment

The next-stage problem is not a lack of pages or buttons. Four foundational guarantees are not yet fully closed:

1. **Identity and data isolation**: authentication identity and business payloads are not fully separated, and task indexes do not use tenant-composite keys.
2. **Persistence consistency**: queue-full bypass, Delete/Save races, and non-draining shutdown can regress disk state or resurrect deleted tasks.
3. **Control safety**: Kill trusts a Reporter-provided PID; remote deployments lack host/process identity verification, and PGID semantics are incomplete.
4. **Real-time eventual consistency**: SSE is best-effort; disconnects, slow consumers, and lost delete events lack authoritative reconciliation and replay.

The roadmap therefore follows this order:

1. Correctness and security first.
2. High-frequency search and operational efficiency second.
3. Agent and Cursor extension ecosystem expansion third.
4. Event Log, Trace, and data-platform capabilities last.

Suggested engineering allocation over the next two to three iterations:

- Correctness and security: 45%.
- Search and operational experience: 25%.
- Agent and extension ecosystem: 15%.
- Trace and platform work: 15%.

---

## 5. Phase Overview

### Phase 0: Baseline and protocol constraints

Goal: establish repeatable behavioral baselines before changing core identity, persistence, or SSE protocols.

Work:

- Add reproducing tests for tenant forgery, Session ID collisions, Save/Delete races, queue shutdown, and SSE reconnects.
- Record the current JSON Schema, SSE message format, and historical-data compatibility requirements.
- Add migration fixtures for missing `keyId`, legacy `repo:branch`, and legacy Run data.
- Decide whether process control is local-only or executed through a remote companion.
- Define typed application errors and the API Error Schema.

Exit criterion: every known issue has either a failing automated test or an explicit manual verification procedure.

### Phase 1: Trusted real-time foundation

Goal: close the foundations of multi-tenancy, persistence, SSE, and control safety.

Includes:

1. Trusted authentication identity binding.
2. Tenant + Task ID composite keys.
3. Ordered persistence command stream and graceful shutdown.
4. SSE snapshot reconciliation and trustworthy frontend operation handling.
5. Safe local Kill mode and real process-group control.
6. Strict HTTP methods, JSON validation, and typed errors.

### Phase 2: High-frequency productivity and operability

Goal: make large task sets easier to locate, filter, share, and operate.

Includes:

1. `filter_repos` repository allowlist.
2. Repo/Branch Schema repair.
3. Deep search and compound filters.
4. Sorting, grouping, and saved views.
5. Deep links and Follow Latest.
6. Health checks, corrupt-file quarantine, and basic metrics.
7. Reporter state TTL and response-body limits.

### Phase 3: Reliable live delivery and product maturity

Goal: provide recoverable streaming, complete accessibility, scalable rendering, and a more complete PWA.

Includes:

1. SSE v2 sequence IDs, replay, and resynchronization.
2. Incremental DOM updates, pagination, or virtualization.
3. Complete keyboard navigation, focus management, and Reduced Motion.
4. Complete internationalization and localized formatting.
5. Offline recent snapshots, install entry points, and update prompts.
6. Browser-level end-to-end tests.

### Phase 4: Agent and Cursor ecosystem

Goal: expand formally supported Agents and improve Cursor extension success rates.

Includes:

1. Production Claude Code support.
2. Feasibility validation and tiered support for Aider, Windsurf, Trae, and Continue.
3. Cursor multi-root workspace Hook management.
4. Cross-platform embedded binaries in VSIX.
5. Hook preview, diagnostics, rollback, and extension automation tests.

### Phase 5: Agent APM platform

Goal: evolve from a local live dashboard into a replayable, queryable, diagnostic Agent APM.

Includes:

1. Idempotent Event Log.
2. Reporter event sequencing and out-of-order detection.
3. Precise Trace spans, critical paths, and stall diagnostics.
4. Historical paginated search and analytics.
5. Project management, quotas, archives, audit, and redacted exports.

---

## 6. Phase 0: Baseline and Design Decisions

### 6.1 Behavioral baseline tests

Add tests for:

- A project key request forging another `key_id` in the payload.
- Two tenants using the same Session ID.
- Rapid versions of one task under write-queue pressure.
- Delete while an older Save is still queued.
- Service shutdown with queued writes.
- A delete during SSE disconnection followed by reconnect reconciliation.
- Slow SSE clients filling their channel.
- Kill targeting a remote host, PID reuse, start-time mismatch, and PGID mismatch.
- Repository safe-filename normalization collisions.
- Corrupt JSON, permission failures, and old-Schema recovery.

### 6.2 Architecture decisions required first

#### ADR-1: Task identity

Recommended internal value objects:

- `TenantID`: derived from trusted authentication context.
- `TaskID`: Agent session ID.
- `TaskKey`: internal composite of `TenantID + TaskID`.

HTTP URLs may continue to use Task ID, but every Service method must also receive Tenant Scope. Master operations must explicitly select a tenant and must not infer it from payload `key_id`.

#### ADR-2: Persistence ordering

Use one Persistence Command Stream:

- `SaveTask{TaskKey, Version, JSON}`.
- `DeleteTask{TaskKey, Version}`.
- Versions only increase per TaskKey.
- Delete tombstones suppress older Saves.
- Under queue pressure, coalesce stale Saves for the same task; never start a bypass goroutine that writes directly.

#### ADR-3: SSE consistency

Phase 1 implements Snapshot Reconciliation; Phase 3 adds Event Replay.

Suggested Phase 1 messages:

- `snapshot_start` with `generation` and tenant scope.
- Multiple `task_upsert` events.
- `snapshot_end` with `generation` and a task-set summary.
- Live deltas associated with the same connection generation.

Phase 3 adds:

- SSE `id:`.
- `Last-Event-ID`.
- Per-tenant ring buffers.
- `resync_required`.

#### ADR-4: Kill control model

Recommendation:

- A local deployment may directly control local processes after verification.
- A central deployment must not operate on Monitor-host PIDs. It queues control commands for a Reporter/Companion on the target host.
- Phase 1 disables Kill when host identity cannot be verified.

---

## 7. Phase 1 Work Packages

## 7.1 WP-1: Trusted authentication identity binding

### Problem

Payload `key_id` is untrusted business input. A non-Master request must not be able to use it to change task ownership.

### Implementation

- The HTTP Handler constructs an unforgeable `RequestScope` from authentication.
- Non-Master requests always use `RequestScope.TenantID` and ignore payload `key_id`.
- Master writes to a target tenant through a dedicated Header, URL parameter, or explicit Admin DTO.
- Application Services no longer infer tenant identity from ordinary Event DTOs.
- Domain `EventPayload.KeyID` becomes a compatibility field populated only by trusted callers.
- Log rejected authorization attempts without Prompt, AI response, or API Key content.

### Tests

- Project A key sends project B `key_id`; the task still belongs to project A.
- Project A cannot mutate an existing project B task.
- Master behavior without an explicit target tenant is deterministic.
- Constant-time key comparison remains intact.

### Acceptance

- Ordinary clients cannot alter tenant ownership through JSON.
- Service-layer tests prove the constraint independently of the HTTP Handler.
- API docs define the trusted identity source.

## 7.2 WP-2: Tenant-composite task key

### Problem

The in-memory Map and JSON filename primarily use Session ID, allowing tenants with the same ID to collide.

### Implementation

- Add a Domain or Application `TaskKey` value object.
- Index `MonitorService.tasks` by composite key.
- Make query, delete, Abort, Kill, Steer, and Inject Context methods accept Tenant Scope explicitly.
- Change Repository methods to use `TaskKey`.
- Recommended layout: `<dataDir>/<tenantHash>/<safeTaskID>-<taskHash>.json`.
- Preserve original `id` and `keyId` in Master responses; change frontend local indexing to a composite key.
- SSE delete messages carry composite task references, optionally including both `id` and `keyId` during compatibility rollout.

### Migration

- Legacy root JSON without `keyId` moves to the `default` tenant.
- Files with `keyId` move into that tenant directory.
- Use atomic Rename where possible; across filesystems, use copy, Sync, Rename, then delete the old file.
- Migration failure must not block startup; quarantine and expose health status.
- The first compatibility release still reads the old directory but writes only the new layout.

### Tests

- Two tenants with the same ID can run, update, delete, and recover independently.
- Master sees both tasks distinctly.
- Invalid characters and normalized-filename collisions.
- Idempotent legacy migration.

### Acceptance

- Same Session ID across tenants never shares a memory object or disk file.
- Deleting project A never affects project B.
- `go test -race ./...` remains clean.

## 7.3 WP-3: Ordered persistence command stream

### Problem

Queue-full bypass writes, separate Delete paths, and non-draining shutdown can regress versions, resurrect deleted tasks, or lose final events.

### Implementation

- Route Save and Delete through one persistence command stream.
- Commands carry `TaskKey`, `Version`, operation type, and immutable JSON.
- Track the latest committed version per TaskKey and reject stale writes.
- Under pressure, coalesce adjacent Saves per TaskKey and keep only the latest.
- Delete writes a tombstone that suppresses lower-version Saves.
- Add metrics for queue depth, coalescing, stale-version rejection, and persistence failures.
- Implement `MonitorService.Close(ctx)` to stop intake, drain the queue, and wait for workers and summarizers.
- Use `http.Server`, signal handling, and timed `Shutdown()` in `main.go`.
- Cancel SSE requests during shutdown.

### Tests

- After 100+ concurrent updates, disk contains the highest version.
- A full queue does not produce out-of-order bypass writes.
- An old Save cannot recreate a deleted file.
- Close drains within its timeout and returns an explicit error on timeout.
- A SIGTERM integration test recovers the final task state.

### Acceptance

- Repository never replaces a higher version with a lower one.
- Normal shutdown leaves memory and disk final states consistent.
- No goroutine leaks.

## 7.4 WP-4: SSE snapshot reconciliation and trustworthy frontend operations

### Problem

After reconnect, the server sends current tasks but the frontend does not remove stale local tasks. Some destructive operations mutate local state even when HTTP failed.

### Implementation

- Add `snapshot_start`, `task_upsert`, and `snapshot_end` messages.
- The frontend maintains a generation and `snapshotSeen` set for each connection.
- At `snapshot_end`, remove only stale tasks within the current tenant scope.
- Merge live updates during a snapshot by version or sequence so old snapshot data cannot overwrite newer data.
- Make `apiFetch()` throw typed errors for every non-2xx response.
- Add Pending states and duplicate-submit prevention for delete, clear, Abort, Kill, and Steer.
- Change final UI state only after server confirmation.
- Show actionable failures; authentication failures still reopen key configuration.
- Start with “remove after server confirmation”; Undo remains optional later.

### Tests

- A task deleted during disconnection disappears after reconnect.
- A new update during snapshot does not regress.
- Tenant snapshots never clear another tenant.
- DELETE 500/403/404 does not pretend success.
- Partial bulk-delete failures produce a result summary.

### Acceptance

- After reconnect, frontend tasks match the authoritative server snapshot.
- Every destructive operation trusts the server response.
- Critical operation errors are no longer swallowed by empty `catch` blocks.

## 7.5 WP-5: Safe Kill and real process-group control

### Problem

Kill trusts reported PIDs. Remote deployments may kill an unrelated process on the Monitor host, and PGID semantics are incomplete.

### Implementation

- Add `HostID`, `BootID`, `PID`, `PGID`, `ProcessStartTime`, and optional command fingerprint to Task process identity.
- Reporter gathers available values using the standard library and explicitly degrades when unavailable.
- Monitor records its own HostID/BootID and only directly kills matching-host, matching-boot, matching-start-time processes.
- On Unix, send SIGTERM to a negative PGID; optionally escalate to SIGKILL after a timeout.
- On Windows, implement Job Object/process-tree support or explicitly limit the first release to single-process termination.
- Return `requested`, `terminated`, `forced`, `rejected`, or `unknown`; never mark success unconditionally.
- Remote tasks display “soft abort only” or use a future Companion control queue.
- Gate dangerous control with an explicit environment variable and a safe default.

### Tests

- HostID mismatch rejects.
- BootID mismatch rejects.
- PID start-time mismatch rejects.
- A test parent and child process group are both terminated.
- Kill failure does not forge a killed Domain state.

### Acceptance

- Monitor never kills a local process solely because a remote payload supplied its PID.
- Every control result is auditable and matches actual execution.

## 7.6 WP-6: Strict HTTP and typed errors

### Implementation

- Every route accepts only explicit methods; others return 405 with `Allow`.
- Validate malformed JSON, empty bodies, multiple top-level values, and body limits.
- Decide unknown-field compatibility explicitly; at minimum, new Admin APIs are strict.
- Define a common error shape: `code`, `message`, `requestId`, optional `details`.
- Application errors become comparable types: NotFound, Forbidden, Conflict, InvalidArgument, and Unavailable.
- HTTP maps errors without string inspection.
- Add a configurable CORS Origin allowlist.
- Stop recommending long-lived API keys in SSE URLs; first add log redaction, then a short-lived Stream Ticket.

### Acceptance

- API behavior is stable, testable, and documented.
- Unsupported methods and malformed requests never produce misleading 200 responses.

---

## 8. Phase 2 Work Packages

## 8.1 WP-7: Enable `filter_repos`

### Implementation

- Parse `filter_repos` from global and project configs.
- Define empty, empty-array, inheritance, and override semantics.
- Initially support exact repository-name matching; optional Glob support can follow.
- Filter after Git discovery and before network/spool activity.
- Define behavior outside a Git repository.
- `agent-reporter config` displays the effective allowlist and source.

### Acceptance

- Repositories outside the allowlist generate no network request or spool entry.
- Project config overrides global config.
- Fail-Safe behavior remains unchanged.

## 8.2 WP-8: Repair Repo/Branch Schema

### Implementation

- Reporter sends separate `repo` and `branch` fields.
- `repo` contains only the repository name or normalized identifier.
- Domain and frontend remain compatible with legacy `repo:branch`.
- Recovery may migrate lazily; new writes use the new shape.
- Search, filters, exports, and cards use Repo and Branch independently.

### Acceptance

- New task branches can be filtered independently.
- Historical display does not regress.

## 8.3 WP-9: Deep search and compound filters

Search expands to:

- Task title, `rootGoal`, `goalSummary`, Repo, Branch, Agent, Tenant, and ID.
- Every Run Prompt, AI response, detail, and Timeline description.
- Subagent type, ID, and independent child-task title.

Filters:

- Status.
- Multi-select Agent types.
- Repo and Branch.
- Tenant.
- Time range.
- Run-count range.
- Contains tool failure, subagent, or unfinished Run.

Performance:

- Cache normalized search text per Task.
- Rebuild only when Task version changes.
- Preserve IME handling and debounce.
- Move to backend paginated search at larger scale.

Experience:

- Show match source such as “Run #4 AI response”.
- Provide clear-all.
- Encode filter state in the URL.

## 8.4 WP-10: Sorting, grouping, and saved views

Sorting:

- Latest event.
- Creation time.
- Current Run duration.
- Cumulative lifetime.
- Run count.
- Failures first.

Grouping:

- Repo.
- Branch.
- Agent.
- Tenant.

Saved views:

- Store filters, sorting, and grouping locally.
- Presets for Running, Recent Failures, Long Running, and selected repositories.
- Per-second timers must not continuously reorder cards; duration sort should refresh less often or manually.

## 8.5 WP-11: Deep links and Follow Latest

### Implementation

- URL Query/Hash represents `task`, `tenant`, `run`, `q`, `status`, `agent`, and view.
- Browser back/forward synchronizes drawer and filters.
- Provide “Copy current view link”.
- API Keys never enter share URLs.
- Drawer defaults to Follow Latest.
- Manual navigation to an older Run pauses following.
- A new Run displays a clickable notice.
- Missing or unauthorized tasks show explicit states.

## 8.6 WP-12: Health, corrupt-file quarantine, and metrics

### Implementation

- `/healthz`: process liveness.
- `/readyz`: Repository available, migrations complete, write queue accepting commands.
- Optional `/api/metrics` or Prometheus text metrics.
- Metrics include tasks, tenants, event throughput, write depth, coalescing, persistence failures, SSE clients, broadcast drops, and LLM failures.
- Move corrupt JSON into `quarantine/`, preserving error metadata but not sensitive content in logs.
- Health APIs expose corrupt-file counts and last error time.
- Use readable prefixes plus short hashes for safe filenames.

## 8.7 WP-13: Reporter runtime hygiene

### Implementation

- Limit server response bodies with `io.LimitReader`.
- Add TTL to tracked, dropped, aborting, prompts, and circuit-breaker files.
- Dropped TTL must be long enough for active sessions but not permanently block reused Session IDs.
- Limit prompt-history file size and Run count.
- Trigger cleanup lazily or through maintenance commands, not a full scan on every Hook.
- Implement true cross-process spool locking on Windows or use per-process spools followed by safe merging.

---

## 9. Phase 3 Work Packages

## 9.1 WP-14: Reliable SSE v2

### Protocol

- Monotonic event IDs per tenant.
- SSE `id:` and explicit `event:` types.
- Client reconnect with `Last-Event-ID`.
- Fixed-size server ring buffer.
- `resync_required` when the requested event has expired.
- Master view uses a global sequence or tenant-aware composite cursor.
- Restart semantics are explicit: persisted epoch or mandatory full resync.

### Acceptance

- Short disconnects replay without loss.
- Expired replay windows fall back to authoritative snapshots.
- Slow clients never block the Hub and drops are observable.

## 9.2 WP-15: Frontend scale and performance

### Implementation

- Keyed DOM patching by Task version.
- Refresh only affected status columns.
- Refresh the drawer only when its Task or children change.
- Cache parent-child relationships, search text, and Run summaries.
- Paginate, virtualize, or use “recent N + load more” for history.
- Reduce timers and visual work in background tabs.
- Add development metrics for render time, card count, and update batch size.

### Suggested performance acceptance

- Search remains usable with 1,000 historical tasks.
- 50 events per second do not cause sustained main-thread long tasks.
- Unchanged cards are not fully reconstructed.

## 9.3 WP-16: Keyboard, focus, and perceivable status

### Implementation

- Standard Dialog semantics for drawer and modals.
- Save original focus and move focus to title or close button on open.
- Restore focus on close.
- Focus trap and background `inert`.
- Run Listbox supports arrows, Home, End, Enter, and Escape.
- Mouse and keyboard behavior match in bulk mode.
- Toast uses `aria-live`; failed actions may use assertive.
- Aggregate high-frequency announcements.
- Support `prefers-reduced-motion`.
- Give every interactive element clear `focus-visible` styling.

## 9.4 WP-17: Complete internationalization

### Implementation

- Move dynamic Run, event, bulk, Steer, and export text into the dictionary.
- Use system language on first visit, then respect user selection.
- Use `Intl.NumberFormat`, `Intl.DateTimeFormat`, and one duration formatter.
- Generate Markdown exports in the selected language while keeping machine-readable field names stable.
- Add development-time missing-key checks.
- Keep Run, Hook, SSE, Fail-Safe, and Steer terminology consistent.

## 9.5 WP-18: Complete PWA lifecycle

### Implementation

- Store the most recent authorized task snapshot in IndexedDB.
- Partition caches by Tenant/Key fingerprint without storing plaintext API Keys.
- Clearly mark offline mode as read-only with last synchronization time.
- Reconcile authoritatively after connectivity returns.
- Add a `beforeinstallprompt` entry point.
- Detect waiting Service Workers and prompt for refresh.
- Cache all core static resources and fonts.
- Provide a clear-local-offline-data action.

### Security

- Limit retention time and task count by default.
- Treat Prompt, AI response, and Transcript paths as sensitive.
- Tenant switching must never reveal another tenant's offline data.

## 9.6 WP-19: Browser end-to-end tests

Use an isolated test process and random port; never start a second persistent `:8000` Daemon.

Cover:

- SSE initial snapshot, deltas, deletes, and reconnect.
- Search, filtering, sorting, and saved views.
- Card, drawer, and Run keyboard interactions.
- Multi-Turn Runs and Follow Latest.
- Chinese/English switching.
- Abort, Kill, Steer, and deletion failure feedback.
- API Key switching and tenant isolation.
- Service Worker offline shell and update flow.
- Mobile layout and basic accessibility scans.

---

## 10. Phase 4: Agent and Cursor Ecosystem

## 10.1 WP-20: Agent support maturity model

Support levels:

- **Official**: lifecycle, tool failures, Multi-Turn, Transcript, Fail-Safe, and installation docs all have automated tests.
- **Beta**: core lifecycle works, but some tools or Transcript behavior is limited.
- **Experimental**: generic HTTP/notification integration without complete state-machine guarantees.

Productionizing an Agent requires:

1. Official Hook names and payload fixtures.
2. Session ID, Turn, Prompt, tool, error, and Transcript mapping.
3. Normal completion, user abort, and fatal failure mapping.
4. Tool failures do not become session failures.
5. Valid Fail-Safe response and exit code 0.
6. Install, uninstall, conflict, and duplicate-Hook guidance.
7. At least one end-to-end recorded fixture.

Suggested order: Claude Code, Aider, Continue, then Windsurf/Trae.

## 10.2 WP-21: Cursor multi-root workspaces

### Implementation

- Enumerate every Workspace Folder instead of using only the first.
- Let users select folders or configure all.
- Show Hook state, Reporter path, and conflicts per folder.
- Preview the Diff before writing; support backup and rollback.
- Preserve non-Agent Monitor Hooks.
- Detect duplicate user-level and project-level events.

## 10.3 WP-22: Cross-platform VSIX binaries

### Implementation

- Build Monitor and Reporter for every supported release platform.
- Package as `bin/<os>-<arch>/` in the VSIX.
- Select the correct binary at runtime.
- Provide explicit installation guidance for unsupported platforms.
- Verify checksums and executable permissions.
- Do not treat a linux/amd64 binary as a universal embedded binary.

## 10.4 WP-23: Extension diagnostics and tests

### Implementation

- Daemon Output Channel logs.
- “Diagnose Agent Monitor” command checking port, version, API Key, Hooks, binaries, and SSE.
- Hook errors identify exact workspace and event.
- Daemon restart results after settings changes are visible.
- TypeScript unit tests and VS Code Extension Host tests.
- Release pipeline runs extension build, tests, and a VSIX installation smoke test.

---

## 11. Phase 5: Agent APM Platform

## 11.1 WP-24: Idempotent Event Log

### Event model

Add:

- `eventId`: globally or session-unique.
- `reporterId`: Reporter instance identity.
- `sequence`: session or Reporter monotonic sequence.
- `occurredAt`: source occurrence time.
- `receivedAt`: server receipt time.
- `schemaVersion`.

### Storage model

- Append-only Event Log stores normalized source events.
- Task JSON/database records become projections.
- Event ID idempotency.
- Detection of late, duplicate, and out-of-order events.
- Replay to rebuild Tasks.
- Retention, compaction, archive, and sensitive-field redaction.

Evaluate SQLite, BoltDB, or segmented JSONL. The choice must remain:

- Single-binary.
- Free of external services.
- Atomically committable.
- Indexable and pageable.
- Migration-friendly.

## 11.2 WP-25: Precise Trace spans

### Data model

Timeline/Span adds:

- Precise timestamps.
- `spanId` and `parentSpanId`.
- Tool name and invocation ID.
- Start, end, duration, and status.
- Input/output sizes and redacted summaries.
- Agent/Subagent identity.

### Product capabilities

- Pair before/after events.
- Real Waterfall.
- Root/subagent critical path.
- Slow tools, failed tools, and long idle gaps.
- Filtering by tool, state, duration, and Agent.
- Click a Span for safe summaries and errors.

### Security

- Do not store complete tool inputs/outputs by default.
- Redact and limit secrets, tokens, environment variables, and file content.
- Provide safe export mode.

## 11.3 WP-26: Historical search and analytics

Queries:

- Status, Agent, Repo, Branch, Tenant, and time range.
- Pagination, stable ordering, and cursors.
- Full-text Prompt, AI response, and Timeline search.
- JSON, JSONL, and redacted Markdown export.

Analytics:

- Average and percentile Run duration.
- Tool failure rate.
- Runs per session.
- Subagent utilization.
- Peak active tasks.
- Abort/Kill ratio.
- Reporter offline spool and replay behavior.

## 11.4 WP-27: Project management, quotas, and audit

Possible later capabilities:

- Project metadata and key rotation.
- Project retention and task quotas.
- Master audit log.
- Actor, time, target task, and result records.
- Archive, restore, and compliant deletion.
- Sensitive-field policies and redacted exports.

This work must not precede Phase 1 tenant isolation and typed authorization.

---

## 12. Documentation and Implementation Mismatches

Implementation work should also correct these known differences:

1. README/integration docs mark Claude Code and Aider as Pending while Reporter already has sniffing and partial compatibility; describe them through the maturity model.
2. `filter_repos` exists in configuration structures/output but is not fully loaded and enforced.
3. Domain has a separate `Branch` field while Reporter primarily concatenates Repo and Branch.
4. README HTTP API summaries do not cover all task-control endpoints.
5. Current PWA “offline” primarily means a static shell, not offline task data; avoid misleading claims.
6. Cursor extension docs must state the platform scope of embedded VSIX binaries and external Daemon behavior.
7. Installation document section numbering contains duplication and should be corrected in a documentation cleanup PR.
8. Kill product language must distinguish soft abort, single-process termination, and process-group termination.

Public docs remain structurally mirrored in Chinese and English; `llms.txt` remains fully English.

---

## 13. Test Strategy

### 13.1 Unit tests

- Domain: state machine, TaskKey, versions, tombstones, and Span pairing.
- Application: tenant constraints, command stream, Close, control decisions, and typed errors.
- Persistence: migration, stale-version rejection, quarantine, and filename hashing.
- Reporter: config precedence, filters, event sequence, TTL, response limits, and platform differences.
- HTTP: methods, JSON, authentication, error mapping, Snapshot, and Stream Ticket.

### 13.2 Race and load tests

Continue to run:

```bash
go test -v -race ./...
```

Add:

- High-concurrency events on one task.
- Same ID across tenants.
- Many SSE clients subscribing and disconnecting.
- Write-queue pressure and shutdown.
- TTL cleanup concurrent with live writes.

### 13.3 Integration tests

- Temporary-directory Repository.
- Random-port HTTP Server.
- Real SSE clients.
- Process-group Kill tests on supported platforms only.
- Legacy data-directory migration.
- Reporter spool offline and recovery.

### 13.4 Browser tests

Introduce in Phase 3 for core user paths and accessibility. Tests use random ports and never conflict with the single local `:8000` Daemon.

### 13.5 Release tests

- All Go cross-compilation targets.
- Linux offline-bundle install.
- systemd user/system services.
- Docker amd64/arm64.
- VSIX platform startup and Hook configuration.
- Upgrade preserving old data and config.
- Checksum verification.

---

## 14. Observability Targets

Every new mechanism must be observable:

- Hook accepted, rejected, and parse-failure counts.
- Task count and active Runs per tenant.
- Write depth, coalescing, tombstone rejection, and errors.
- SSE clients, replay, snapshots, drops, and resync.
- Reporter spool size, replay count, circuit state, and drops.
- Abort/Kill/Steer requests and outcomes.
- LLM request duration, failure, and throttling.
- Migration and quarantine counts.

Logging requirements:

- Structured or stable key-value format.
- Never log API Keys, complete Prompts, complete AI responses, or sensitive tool parameters.
- Every HTTP request may be correlated through `requestId`.
- Control operations have audit records.

---

## 15. Security and Privacy Requirements

All phases follow these rules:

1. API Keys never enter logs, share URLs, exports, or task body persistence.
2. Replace SSE Query Tokens over time with short-lived Tickets or secure same-origin sessions.
3. CORS defaults to a configurable Origin allowlist.
4. Transcript, Prompt, AI response, and tool parameters are sensitive.
5. Offline browser caches are tenant-partitioned and bounded.
6. Kill verifies host and process identity.
7. Master operations and tenant operations use separate permission paths.
8. Exports support redacted mode.
9. Never commit `.env`, credentials, or machine-local absolute paths.
10. Repository quarantine and error logs do not expose task contents.

---

## 16. Compatibility and Migration Strategy

### 16.1 Schema versions

Add independent versions for persisted Tasks, Reporter Events, and SSE protocol. Rules:

- A new service reads at least the previous stable version.
- During the compatibility window, a new Reporter can still send foundational fields to an old service.
- The frontend recognizes legacy Task messages and new typed messages during rollout.
- Avoid irreversible in-place overwrite; preserve recoverable backups or old files before migration.

### 16.2 Staged migration

1. Add read compatibility and tests.
2. Enable the new write format.
3. After one stable release, remove legacy writes.
4. Remove legacy reads only in a Major Version.

### 16.3 Rollback

Every migration documents:

- Whether the binary can be rolled back.
- Whether new data is readable by the old version.
- Whether the data directory needs backup.
- How new fields, tenant directories, and tombstones behave after rollback.

---

## 17. PR and Parallel-Agent Plan

Each PR starts from the latest `origin/master`. Dependent PRs rebase and rerun quality gates after predecessors merge.

### Group 1: Phase 0/1

1. `test/trusted-realtime-baseline`
   - Failure reproductions, fixtures, and ADRs only.
2. `fix/trusted-tenant-context`
   - HTTP/Application identity binding.
3. `feat/tenant-task-key`
   - Domain, Service, Repository composite key and migration.
4. `fix/ordered-persistence`
   - Save/Delete stream, tombstones, and Close.
5. `feat/sse-snapshot-reconcile`
   - HTTP/SSE protocol and frontend reconciliation.
6. `fix/frontend-operation-errors`
   - Frontend non-2xx, Pending, and failures.
7. `fix/safe-process-control`
   - Reporter process identity, Domain fields, safe Kill.
8. `fix/strict-http-errors`
   - Methods, JSON, typed errors, and CORS.

Do not modify overlapping core files concurrently in the same checkout. Potential parallel pairs:

- Domain TaskKey design and HTTP error design.
- Persistence command stream and frontend operation errors.
- Reporter process identity and SSE frontend protocol, with Schema coordination before merge.

### Group 2: Phase 2

1. `feat/reporter-repo-filter`
2. `fix/repo-branch-schema`
3. `feat/dashboard-deep-search`
4. `feat/dashboard-saved-views`
5. `feat/dashboard-deep-links`
6. `feat/health-and-metrics`
7. `fix/reporter-state-hygiene`

### Group 3: Phase 3

1. `feat/sse-replay-v2`
2. `feat/dashboard-incremental-render`
3. `feat/dashboard-accessibility`
4. `feat/dashboard-i18n-complete`
5. `feat/pwa-offline-snapshot`
6. `test/dashboard-e2e`

### Group 4: Phase 4

Split by Agent and directory. Cursor extension work uses a dedicated worktree and is the only work that installs npm dependencies.

### Group 5: Phase 5

Event Log, Trace, and query-platform work starts with separate ADRs/prototypes. Never replace persistence, SSE, and frontend together in one PR.

---

## 18. Definition of Done for Every Feature PR

Every PR must include:

- Clear problem statement and non-goals.
- Compatibility and migration notes.
- DDD-compliant Domain/Application/Infrastructure boundaries.
- Race tests for new concurrent paths.
- Reporter Fail-Safe behavior and response limits.
- Synchronized Chinese and English public docs.
- `llms.txt` updates for API or Schema changes.
- `go test -v -race ./...`.
- `make review` or `make pre-pr`.
- No unresolved `[BLOCK]`.
- Clear commits for each logical change.
- Verification and rollback steps in the PR description.
- No automatic merge.

---

## 19. Suggested Release Milestones

Version numbers are suggestions and remain subject to release decisions.

### v1.x Reliability

Scope: Phase 0 and Phase 1.

Release criteria:

- Same Session ID is fully isolated across tenants.
- Persistence never regresses, resurrects deletes, or loses normal-shutdown writes.
- SSE reconnects converge to server state.
- Kill never targets unverifiable processes.
- API errors are stable.

### v1.x Productivity

Scope: Phase 2.

Release criteria:

- Repo filtering and Branch data work.
- Deep search, compound filters, saved views, and deep links are stable.
- Health and basic metrics support operations.

### v1.x Experience

Scope: Phase 3.

Release criteria:

- SSE replays or automatically resynchronizes.
- Frontend remains responsive with large task counts.
- Keyboard, Reduced Motion, and I18N are complete.
- Browser end-to-end tests run in CI.

### v2.0 Agent APM

Scope: stable portions of Phase 4 and Phase 5.

Release criteria:

- Agent support maturity is explicit.
- Event Log can replay.
- Trace uses real spans.
- Historical queries and analytics paginate.
- Schema and migration policy are stable.

---

## 20. Not Yet a Priority

Valuable, but not before the trusted real-time foundation:

- Complex account registration and organization administration.
- Public cloud synchronization.
- Plugin marketplace.
- Large theme systems and decorative charts.
- Complex RBAC editors.
- Default storage of complete tool inputs/outputs.
- Heavy analytics before an Event Log exists.

Reason: the product differentiates through local-first, lightweight, real-time, and controllable operation. Expanding sharing or data surfaces too early amplifies tenant, security, and consistency risk.

---

## 21. Long-Term Product Position

The three strongest defensible directions are:

1. **Cross-Agent standardized event protocol**: unify Cursor, ZCode, Codex, Claude Code, and other Hooks into a stable domain language.
2. **Recoverable live state and Event Log**: reconnectable dashboards plus auditable, replayable history.
3. **Real Trace, critical path, and stall diagnostics**: move from “what is the Agent doing?” to “why is it slow, where did it fail, and which subagent is blocking?”

With these capabilities, Agent Monitor evolves from a real-time Kanban/flight dashboard into a local-first AI Coding Agent APM with no external service dependency.

---

## 22. Recommended First Implementation Package

Start with **Trusted Realtime Foundation**, strictly scoped to:

1. Trusted tenant identity binding.
2. A regression test and composite-key design for identical Session IDs across tenants.
3. SSE Snapshot Reconciliation.
4. Treat every non-2xx destructive frontend operation as failure.
5. SSE, delete, and tenant-isolation tests.

Do not combine the first package with:

- Event Log.
- Full-text search.
- A large UI rewrite.
- New Agent integrations.
- Offline task caching.

This scope removes the most important security and product-trust risks while creating a stable protocol base for the ordered persistence stream, SSE v2, and later platform work.
