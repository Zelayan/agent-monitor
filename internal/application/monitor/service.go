package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Zelayan/agent-monitor/internal/domain/task"
	"github.com/Zelayan/agent-monitor/internal/infrastructure/persistence"
)

var (
	localHostOnce sync.Once
	localHostID   string
	localBootID   string
)

func detectHostAndBootID() (string, string) {
	localHostOnce.Do(func() {
		// 1. Host ID
		for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if data, err := os.ReadFile(p); err == nil {
				id := strings.TrimSpace(string(data))
				if id != "" {
					localHostID = id
					break
				}
			}
		}
		if localHostID == "" {
			if h, err := os.Hostname(); err == nil && h != "" {
				hash := sha256.Sum256([]byte(h))
				localHostID = hex.EncodeToString(hash[:16])
			} else {
				localHostID = "local-host"
			}
		}

		// 2. Boot ID
		if data, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
			id := strings.TrimSpace(string(data))
			if id != "" {
				localBootID = id
			}
		}
		if localBootID == "" {
			if id := darwinBootID(); id != "" {
				localBootID = id
			}
		}
		if localBootID == "" {
			localBootID = "local-boot"
		}
	})
	return localHostID, localBootID
}

// ServiceMetrics 暴露 Monitor 服务的运行时统计与吞吐指标。
type ServiceMetrics struct {
	TotalEventsReceived  uint64 `json:"total_events_received"`
	PersistSuccessTotal  uint64 `json:"persist_success_total"`
	PersistErrorsTotal   uint64 `json:"persist_errors_total"`
	PersistDroppedTotal  uint64 `json:"persist_dropped_total"`
	PersistQueueLength   int    `json:"persist_queue_length"`
	PersistQueueCapacity int    `json:"persist_queue_capacity"`
	ActiveTasksCount     int    `json:"active_tasks_count"`
	TenantsCount         int    `json:"tenants_count"`
	SSEClientsCount      int    `json:"sse_clients_count"`
	SSEDroppedTotal      uint64 `json:"sse_dropped_total"`
	QuarantineFilesCount int    `json:"quarantine_files_count"`
}

// ErrPermissionDenied 表示跨租户越权访问或控制被拒绝。
var ErrPermissionDenied = errors.New("permission denied")

// ErrQuotaExceeded 表示租户并发活跃会话数量超出配额限制。
var ErrQuotaExceeded = errors.New("Tenant active task quota exceeded (429)")

// ErrHostMismatch 表示目标进程所在主机/环境与当前 Monitor 实例不一致，拒绝跨机 Kill。
var ErrHostMismatch = errors.New("host or boot mismatch: direct kill only permitted on matching local host")

const (
	// DefaultMaxActiveTasksPerTenant 默认每个租户最大并发运行中的任务数
	DefaultMaxActiveTasksPerTenant = 100

	// ActiveTaskStaleThresholdMs 活跃任务无任何更新的判定超时阈值 (2小时)，防止僵死会话永久泄漏配额
	ActiveTaskStaleThresholdMs int64 = 2 * 3600 * 1000
)

// PersistenceOp 表示持久化操作类型。
type PersistenceOp int

const (
	OpSave PersistenceOp = iota
	OpDelete
	OpAppendEventLog
)

// taskPersistenceCommand 封装带版本的统一持久化命令流。
type taskPersistenceCommand struct {
	op       PersistenceOp
	key      task.TaskKey
	version  uint64
	data     []byte
	eventRec *task.EventRecord
}

// MonitorService 负责会话用例编排、事件处理与仓储/广播联动。
type MonitorService struct {
	mu                      sync.RWMutex
	hostID                  string
	bootID                  string
	tasks                   map[task.TaskKey]*task.Task               // 以 TaskKey 复合主键索引，消除不同租户同 Session ID 覆盖
	eventRingBuffers        map[task.TaskKey]*task.EventLogRingBuffer // 会话事件幂等防抖与回放环形缓冲区
	repo                    task.TaskRepository
	hub                     *Hub
	generation              uint64                      // 全局/租户状态变更 generation，单调递增
	persistChan             chan taskPersistenceCommand // 异步串行持久化命令管道（Save 与 Delete 统一有序消费）
	stopChan                chan struct{}
	stoppedChan             chan struct{} // persistenceWorker 完成排空后关闭
	ttlDays                 int           // 自动清理天数（默认 30 天，<=0 则不清理）
	summarizer              *TitleSummarizer
	titleJobs               sync.Map                                 // map[string]*titleJobState，同一 TaskKey LLM 总结串行且可合并
	steerQueue              map[task.TaskKey][]task.SteerInstruction // map[task.TaskKey][]task.SteerInstruction 结构化上下文注入队列 (支持定向子智能体)
	maxActiveTasksPerTenant int                                      // 单租户最大并发运行中的任务配额 (<= 0 表示不限制)

	// 指标统计 (原子操作)
	eventsReceived uint64
	persistSuccess uint64
	persistErrors  uint64
	persistDropped uint64
}

// NewMonitorService 实例化应用服务并从仓储加载已有会话数据。
func NewMonitorService(repo task.TaskRepository, hub *Hub) *MonitorService {
	return NewMonitorServiceWithTTL(repo, hub, 30)
}

// NewMonitorServiceWithTTL 实例化应用服务并指定会话保留天数。
func NewMonitorServiceWithTTL(repo task.TaskRepository, hub *Hub, ttlDays int) *MonitorService {
	hostID, bootID := detectHostAndBootID()

	s := &MonitorService{
		hostID:                  hostID,
		bootID:                  bootID,
		tasks:                   make(map[task.TaskKey]*task.Task),
		eventRingBuffers:        make(map[task.TaskKey]*task.EventLogRingBuffer),
		repo:                    repo,
		hub:                     hub,
		persistChan:             make(chan taskPersistenceCommand, 5000), // 削峰缓冲
		stopChan:                make(chan struct{}),
		stoppedChan:             make(chan struct{}),
		ttlDays:                 ttlDays,
		steerQueue:              make(map[task.TaskKey][]task.SteerInstruction),
		maxActiveTasksPerTenant: DefaultMaxActiveTasksPerTenant,
	}

	if repo != nil {
		persisted, err := repo.FindAll()
		if err != nil {
			log.Printf("[Application] Warning: failed to load persisted tasks: %v", err)
		} else {
			discardedIdle := 0
			for _, t := range persisted {
				if t != nil && t.ID != "" {
					key := t.TaskKey()
					if !t.HasUserWork() {
						if err := repo.DeleteKey(key); err != nil {
							log.Printf("[Application] Warning: failed to discard idle ghost task %s: %v", key.String(), err)
						}
						discardedIdle++
						continue
					}
					if t.CloseOrphanRuns(time.Now().UnixMilli(), time.Now().Format("15:04:05")) {
						if data, err := json.Marshal(t); err == nil {
							if err := repo.SaveRawKey(key, data); err != nil {
								log.Printf("[Application] Warning: failed to persist healed task %s: %v", key.String(), err)
							}
						}
					}
					s.tasks[key] = t
				}
			}
			if discardedIdle > 0 {
				log.Printf("[Application] Discarded %d idle ghost sessions with no user work", discardedIdle)
			}
			if len(s.tasks) > 0 {
				log.Printf("[Application] Restored %d persisted tasks from repository", len(s.tasks))
			}
		}
	}

	// 启动后台单协程消费者，保证同一 Task 的持久化绝对按版本时序原子落盘
	go s.persistenceWorker()

	// 启动后台 TTL 定时巡检协程
	if s.ttlDays > 0 {
		go s.janitorWorker()
	}

	return s
}

// SetTitleSummarizer 注入可选的会话标题 LLM 总结器（未配置则保持 nil，完全不发网）。
func (s *MonitorService) SetTitleSummarizer(sum *TitleSummarizer) {
	s.summarizer = sum
}

// SetTenantQuota 设置单租户最大并发运行中的任务配额 (<= 0 表示不限制)
func (s *MonitorService) SetTenantQuota(maxActive int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxActiveTasksPerTenant = maxActive
}

// WithTenantQuota 链式配置单租户最大并发运行中的任务配额
func (s *MonitorService) WithTenantQuota(maxActive int) *MonitorService {
	s.SetTenantQuota(maxActive)
	return s
}

// persistenceWorker 顺序消费持久化命令管道，支持拥塞合并与有序落盘，并在收到 stopChan 后排空管道。
func (s *MonitorService) persistenceWorker() {
	defer close(s.stoppedChan)

	for {
		select {
		case <-s.stopChan:
			// 优雅停机：排空管道内所有已排队的命令
			for {
				select {
				case cmd := <-s.persistChan:
					s.executePersistenceCommand(cmd)
				default:
					return
				}
			}
		case cmd := <-s.persistChan:
			s.executePersistenceCommand(cmd)
		}
	}
}

// executePersistenceCommand 执行单条持久化命令。
func (s *MonitorService) executePersistenceCommand(cmd taskPersistenceCommand) {
	if s.repo == nil || cmd.key.IsZero() {
		return
	}
	switch cmd.op {
	case OpSave:
		if len(cmd.data) > 0 {
			if err := s.repo.SaveRawKeyVersioned(cmd.key, cmd.version, cmd.data); err != nil {
				atomic.AddUint64(&s.persistErrors, 1)
				log.Printf("[Application] Error persisting task %s (v%d): %v", cmd.key.String(), cmd.version, err)
			} else {
				atomic.AddUint64(&s.persistSuccess, 1)
			}
		}
	case OpDelete:
		if err := s.repo.DeleteKeyVersioned(cmd.key, cmd.version); err != nil {
			atomic.AddUint64(&s.persistErrors, 1)
			log.Printf("[Application] Error deleting task %s (v%d): %v", cmd.key.String(), cmd.version, err)
		} else {
			atomic.AddUint64(&s.persistSuccess, 1)
		}
	case OpAppendEventLog:
		if cmd.eventRec != nil && s.repo != nil {
			if er, ok := s.repo.(task.EventLogRepository); ok {
				if err := er.AppendEventLog(cmd.key, *cmd.eventRec); err != nil {
					atomic.AddUint64(&s.persistErrors, 1)
					log.Printf("[Application] Error appending event log for %s: %v", cmd.key.String(), err)
				} else {
					atomic.AddUint64(&s.persistSuccess, 1)
				}
			}
		}
	}
}

// enqueuePersist 尝试将 Save 命令推入管道。若管道满，通过短超时等待缓冲释放，超时后记录告警，严格维持单 Worker 串行有序消费。
func (s *MonitorService) enqueuePersist(key task.TaskKey, version uint64, data []byte) {
	if s.repo == nil || key.IsZero() || len(data) == 0 {
		return
	}
	cmd := taskPersistenceCommand{
		op:      OpSave,
		key:     key,
		version: version,
		data:    data,
	}

	select {
	case s.persistChan <- cmd:
		return
	default:
		// 管道满：短超时等待缓冲释放，杜绝反模式从 FIFO 管道头部误弹其他任务的命令
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		select {
		case s.persistChan <- cmd:
		case <-ctx.Done():
			atomic.AddUint64(&s.persistDropped, 1)
			log.Printf("[Application] Warning: persistence queue saturated, dropped save command for %s v%d", key.String(), version)
		}
	}
}

// enqueueEventLog 尝试将追加 EventLog 命令推入持久化管道。
func (s *MonitorService) enqueueEventLog(key task.TaskKey, rec task.EventRecord) {
	if s.repo == nil || key.IsZero() {
		return
	}
	if _, ok := s.repo.(task.EventLogRepository); !ok {
		return
	}
	cmd := taskPersistenceCommand{
		op:       OpAppendEventLog,
		key:      key,
		eventRec: &rec,
	}

	select {
	case s.persistChan <- cmd:
		return
	default:
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		select {
		case s.persistChan <- cmd:
		case <-ctx.Done():
			atomic.AddUint64(&s.persistDropped, 1)
			log.Printf("[Application] Warning: persistence queue saturated, dropped event log for %s", key.String())
		}
	}
}

// getOrCreateEventRingBufferLocked 在持有 s.mu 时获取或初始化指定 TaskKey 的事件环形缓冲区。
func (s *MonitorService) getOrCreateEventRingBufferLocked(key task.TaskKey) *task.EventLogRingBuffer {
	rb, ok := s.eventRingBuffers[key]
	if !ok || rb == nil {
		rb = task.NewEventLogRingBuffer(256)
		s.eventRingBuffers[key] = rb
	}
	return rb
}

// enqueueDelete 尝试将 Delete 命令推入管道。若管道满，通过短超时等待推入。
func (s *MonitorService) enqueueDelete(key task.TaskKey, version uint64) {
	if s.repo == nil || key.IsZero() {
		return
	}
	cmd := taskPersistenceCommand{
		op:      OpDelete,
		key:     key,
		version: version,
	}

	select {
	case s.persistChan <- cmd:
		return
	default:
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		select {
		case s.persistChan <- cmd:
		case <-ctx.Done():
			atomic.AddUint64(&s.persistDropped, 1)
			log.Printf("[Application] Warning: persistence queue saturated, dropped delete command for %s v%d", key.String(), version)
		}
	}
}

// janitorWorker 定时巡检清理已完成且超期的任务。
func (s *MonitorService) janitorWorker() {
	ticker := time.NewTicker(2 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.cleanExpiredTasks()
		}
	}
}

func (s *MonitorService) cleanExpiredTasks() {
	if s.ttlDays <= 0 {
		return
	}
	cutoffMs := time.Now().AddDate(0, 0, -s.ttlDays).UnixMilli()
	type delTarget struct {
		key task.TaskKey
		ver uint64
	}
	var toDelete []delTarget

	s.mu.Lock()
	for k, t := range s.tasks {
		if t != nil && (t.Status == "completed" || t.Status == "failed") {
			endTime := t.EndTime
			if endTime == 0 {
				endTime = t.StartTime
			}
			if endTime > 0 && endTime < cutoffMs {
				delete(s.tasks, k)
				delete(s.steerQueue, k)
				delete(s.eventRingBuffers, k)
				toDelete = append(toDelete, delTarget{key: k, ver: t.Version})
			}
		}
	}
	s.mu.Unlock()

	if s.repo != nil && len(toDelete) > 0 {
		for _, item := range toDelete {
			s.enqueueDelete(item.key, item.ver)
		}
		log.Printf("[Application] Janitor cleaned up %d expired tasks (older than %d days)", len(toDelete), s.ttlDays)
	}
}

// Close 停止后台 worker，等待持久化队列排空（默认 5 秒超时）。
func (s *MonitorService) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.CloseWithContext(ctx)
}

// CloseWithContext 优雅停止应用服务并排空待落盘数据。
func (s *MonitorService) CloseWithContext(ctx context.Context) error {
	select {
	case <-s.stopChan:
	default:
		close(s.stopChan)
	}

	select {
	case <-s.stoppedChan:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// HookEventResult 封装事件处理后的 Task 实体快照及向 Reporter 下发的控制指令。
type HookEventResult struct {
	Task              *task.Task
	Action            string // "allow" | "deny" | "abort"
	Reason            string
	AdditionalContext string // 动态塞入 Agent 上下文 (postToolUse 等)
	AgentMessage      string // 随工具审查一同注入给模型的指导/提醒
}

func isPreActionHook(event string) bool {
	switch event {
	case "beforeShellExecution", "beforeMCPExecution", "preToolUse", "PreToolUse", "PermissionRequest", "subagentStart", "beforeSubmitPrompt", "UserPromptSubmit":
		return true
	default:
		return false
	}
}

// HandleHookEvent 处理来自 Hook 的上报事件（默认单机或非多租户模式）。
func (s *MonitorService) HandleHookEvent(p task.EventPayload) (HookEventResult, error) {
	return s.HandleHookEventTenant(p, p.KeyID, p.KeyID == "" || p.KeyID == "master")
}

// HandleHookEventTenant 处理带租户身份校验的 Hook 上报事件。
// 若非 Master 且尝试修改属于其他租户的既有任务，返回 permission denied 错误。
func (s *MonitorService) HandleHookEventTenant(p task.EventPayload, tenantKeyID string, isMaster bool) (HookEventResult, error) {
	atomic.AddUint64(&s.eventsReceived, 1)

	if !isMaster && tenantKeyID != "" {
		p.KeyID = tenantKeyID
	}

	if p.Timestamp == 0 {
		p.Timestamp = time.Now().Unix()
	}

	if p.EventID == "" {
		p.EventID = task.ComputeEventFingerprint(p)
	}

	nowMs := p.Timestamp * 1000
	nowStr := time.Unix(p.Timestamp, 0).Format("15:04:05")

	summary := make(map[string]interface{})
	if p.Agent != "" {
		summary["agent"] = p.Agent
	}
	if p.Repo != "" {
		summary["repo"] = p.Repo
	}
	if p.Branch != "" {
		summary["branch"] = p.Branch
	}
	if p.SubagentID != "" {
		summary["subagentId"] = p.SubagentID
	}
	if p.SubagentType != "" {
		summary["subagentType"] = p.SubagentType
	}

	rec := task.EventRecord{
		EventID:        p.EventID,
		Timestamp:      p.Timestamp,
		ReceivedAt:     nowMs,
		Event:          p.Event,
		TurnIndex:      p.TurnIndex,
		Detail:         p.Detail,
		Prompt:         p.Prompt,
		AIResponse:     p.AIResponse,
		PayloadSummary: summary,
	}

	targetKey := task.NewTaskKey(p.KeyID, p.ID)

	s.mu.Lock()
	t, exists := s.tasks[targetKey]
	if exists && t != nil {
		// 校验租户访问权限：非 Master 且任务已被其他租户认领时禁止越权修改
		if !isMaster && t.KeyID != "" && t.KeyID != tenantKeyID {
			s.mu.Unlock()
			return HookEventResult{Action: "deny"}, fmt.Errorf("%w: task %s belongs to tenant %s, cannot be modified by tenant %s", ErrPermissionDenied, t.ID, t.KeyID, tenantKeyID)
		}

		if !t.HasUserWork() && task.IsTerminalHook(p.Event) {
			delete(s.tasks, targetKey)
			delete(s.eventRingBuffers, targetKey)
			ver := t.Version
			s.mu.Unlock()
			s.forgetTask(targetKey, ver)
			return HookEventResult{Action: "allow"}, nil
		}
	} else if !exists && !isMaster {
		// 当前租户下不存在该任务。检查是否有其他租户已拥有同名既有任务：
		// 若存在且当前请求并非合法开启全新会话的事件（如 sessionStart / UserPromptSubmit），说明企图跨租户篡改既有任务
		for otherKey, otherTask := range s.tasks {
			if otherTask != nil && otherTask.ID == p.ID && otherKey.TenantID != targetKey.TenantID {
				if p.Event != "sessionStart" && p.Event != "UserPromptSubmit" {
					s.mu.Unlock()
					return HookEventResult{Action: "deny"}, fmt.Errorf("%w: task %s belongs to tenant %s, cannot be modified by tenant %s", ErrPermissionDenied, p.ID, otherKey.TenantID, tenantKeyID)
				}
			}
		}
	}
	if !exists {
		if task.IsVacuousLifecycle(p) {
			s.mu.Unlock()
			return HookEventResult{Action: "allow"}, nil
		}

		// 检查租户并发活跃运行中任务配额 (O(1) key 字段访问 + 僵死超时过滤防泄漏)
		normTenant := task.NormalizeTenantID(p.KeyID)
		if s.maxActiveTasksPerTenant > 0 {
			activeCount := 0
			for k, existing := range s.tasks {
				if existing != nil && k.TenantID == normTenant && existing.Status == "running" {
					lastActive := existing.LastTimelineTimestamp
					if lastActive == 0 {
						lastActive = existing.ActiveRunStart
					}
					if lastActive == 0 {
						lastActive = existing.StartTime
					}
					if lastActive > 0 && (nowMs-lastActive) > ActiveTaskStaleThresholdMs {
						continue // 超过超时阈值未活跃的会话视为孤儿僵尸任务，不阻塞新会话接入
					}
					activeCount++
				}
			}
			if activeCount >= s.maxActiveTasksPerTenant {
				s.mu.Unlock()
				return HookEventResult{
					Action: "deny",
					Reason: "Tenant active task quota exceeded (429)",
				}, ErrQuotaExceeded
			}
		}

		t = task.NewTask(p, nowMs)
		targetKey = t.TaskKey()
		s.tasks[targetKey] = t
	}

	// 幂等去重检查与事件流水准备
	rb := s.getOrCreateEventRingBufferLocked(targetKey)

	isNew, seq := rb.AppendIfNew(rec)
	if !isNew {
		// 命中幂等指纹：防抖降级，返回既有状态副本，避免重复应用导致 Turn 或 Timeline 膨胀
		action := "allow"
		reason := ""
		agentMsg := ""
		if t.IsAbortRequested() && isPreActionHook(p.Event) {
			action = "deny"
			reason = t.AbortReason
			if reason == "" {
				reason = "Session aborted from Agent Monitor Dashboard"
			}
			agentMsg = "CRITICAL: The user has intentionally aborted this session from the control panel. Do not invoke any more tools. Acknowledge and stop immediately."
		}
		taskCopy := t.Clone()
		s.mu.Unlock()
		return HookEventResult{
			Task:         taskCopy,
			Action:       action,
			Reason:       reason,
			AgentMessage: agentMsg,
		}, nil
	}

	action := "allow"
	reason := ""
	additionalCtx := ""
	agentMsg := ""

	// 控制反转：如果当前会话已被请求中断，且当前 Hook 为前置拦截点，下发 deny 拒绝工具执行
	if t.IsAbortRequested() && isPreActionHook(p.Event) {
		action = "deny"
		reason = t.AbortReason
		if reason == "" {
			reason = "Session aborted from Agent Monitor Dashboard"
		}
		t.RecordActionDenial(fmt.Sprintf("拦截动作: %s (%s)", p.Detail, reason), nowMs, nowStr)
		agentMsg = "CRITICAL: The user has intentionally aborted this session from the control panel. Do not invoke any more tools. Acknowledge and stop immediately."
	} else {
		t.ApplyEvent(p, nowMs, nowStr)
	}

	// 动态上下文注入：检查当前会话是否有排队的提示词需要注入给 Agent（支持精准定向子智能体）
	if instructions, ok := s.steerQueue[targetKey]; ok && len(instructions) > 0 {
		var consumedTexts []string
		var remaining []task.SteerInstruction
		var targetTypeMatched string

		for _, inst := range instructions {
			matched := false
			// 1. 若指定了具体的子智能体类型 (如 Explore / judge)
			if inst.TargetSubagentType != "" {
				if strings.EqualFold(p.SubagentType, inst.TargetSubagentType) || (p.Event == "subagentStart" && strings.EqualFold(p.SubagentType, inst.TargetSubagentType)) {
					matched = true
					targetTypeMatched = inst.TargetSubagentType
				}
			} else if inst.TargetSubagentID != "" {
				// 2. 若指定了具体的子智能体 ID (如 agent_explore_01)
				if p.SubagentID == inst.TargetSubagentID {
					matched = true
					targetTypeMatched = inst.TargetSubagentType
				}
			} else {
				// 3. 未指定目标（全局广播）：匹配任意当前动作
				matched = true
			}

			if matched {
				consumedTexts = append(consumedTexts, inst.Message)
			} else {
				remaining = append(remaining, inst)
			}
		}

		if len(consumedTexts) > 0 {
			additionalCtx = strings.Join(consumedTexts, "\n\n")
			if len(remaining) > 0 {
				s.steerQueue[targetKey] = remaining
			} else {
				delete(s.steerQueue, targetKey)
			}
			t.RecordContextInjected(additionalCtx, targetTypeMatched, nowStr)
		}
	}

	s.generation++
	t.Generation = s.generation
	taskCopy := t.Clone()
	taskKeyID := t.KeyID
	taskVersion := t.Version

	// 更新环形缓冲区中该条事件记录的应用后状态与版本
	rb.UpdateLastPostApplication(taskCopy.Status, taskCopy.Version)
	rec.Sequence = seq
	rec.TaskStatus = taskCopy.Status
	rec.TaskVersion = taskCopy.Version

	s.mu.Unlock() // 锁范围最小化

	taskJSON, err := json.Marshal(taskCopy)
	if err != nil {
		return HookEventResult{Task: taskCopy, Action: action, Reason: reason, AdditionalContext: additionalCtx, AgentMessage: agentMsg}, fmt.Errorf("failed to marshal task: %w", err)
	}

	// 异步持久化：写入串行管道
	s.enqueuePersist(targetKey, taskVersion, taskJSON)
	s.enqueueEventLog(targetKey, rec)

	// 广播事件（向该租户空间及 Master 广播）
	if s.hub != nil {
		s.hub.BroadcastTenant(taskKeyID, string(taskJSON))
	}

	if shouldSummarizeSessionTitle(p.Event, action, taskCopy) {
		s.scheduleTitleSummary(targetKey)
	}

	return HookEventResult{
		Task:              taskCopy,
		Action:            action,
		Reason:            reason,
		AdditionalContext: additionalCtx,
		AgentMessage:      agentMsg,
	}, nil
}

// findTaskLocked 必须在持有 s.mu 时调用，根据 ID 与租户权限查找匹配任务。
func (s *MonitorService) findTaskLocked(id string, keyID string, isMaster bool) (task.TaskKey, *task.Task, bool) {
	if !isMaster || (keyID != "" && keyID != "*" && keyID != "master") {
		// 租户空间严格按复合键检索
		targetKey := task.NewTaskKey(keyID, id)
		if t, ok := s.tasks[targetKey]; ok && t != nil && t.BelongsTo(keyID, isMaster) {
			return targetKey, t, true
		}
		return task.TaskKey{}, nil, false
	}

	// Master 模式且未指定租户：全局遍历所有同 ID 任务，按生命周期与时间确定性择优
	// 策略：优先选择正在运行的任务；状态相同时选择最新启动时间；启动时间相同时优先 default 租户或字典序
	var bestKey task.TaskKey
	var bestTask *task.Task

	for k, t := range s.tasks {
		if t != nil && t.ID == id {
			if bestTask == nil {
				bestKey = k
				bestTask = t
				continue
			}

			bestIsRunning := bestTask.Status == "running"
			curIsRunning := t.Status == "running"

			if !bestIsRunning && curIsRunning {
				bestKey = k
				bestTask = t
			} else if bestIsRunning == curIsRunning {
				if t.StartTime > bestTask.StartTime {
					bestKey = k
					bestTask = t
				} else if t.StartTime == bestTask.StartTime {
					if k.TenantID == task.DefaultTenantID && bestKey.TenantID != task.DefaultTenantID {
						bestKey = k
						bestTask = t
					} else if bestKey.TenantID != task.DefaultTenantID && k.TenantID < bestKey.TenantID {
						bestKey = k
						bestTask = t
					}
				}
			}
		}
	}
	if bestTask != nil {
		return bestKey, bestTask, true
	}
	return task.TaskKey{}, nil, false
}

// AbortTask 标记指定会话为中断请求状态，并向该租户客户端广播状态变更。
func (s *MonitorService) AbortTask(id string, reason string) (*task.Task, error) {
	return s.AbortTaskTenant(id, reason, "", true)
}

// AbortTaskTenant 在指定租户权限下标记会话为中断请求状态。
func (s *MonitorService) AbortTaskTenant(id string, reason string, keyID string, isMaster bool) (*task.Task, error) {
	s.mu.Lock()
	targetKey, t, exists := s.findTaskLocked(id, keyID, isMaster)
	if !exists {
		s.mu.Unlock()
		return nil, fmt.Errorf("task not found: %s", id)
	}

	nowMs := time.Now().UnixMilli()
	nowStr := time.Now().Format("15:04:05")

	if reason == "" {
		reason = "用户从 Web 看板中断了会话"
	}
	t.RequestAbort(reason, nowMs, nowStr)

	s.generation++
	t.Generation = s.generation
	taskCopy := t.Clone()
	taskKeyID := t.KeyID
	taskVersion := t.Version
	s.mu.Unlock()

	taskJSON, err := json.Marshal(taskCopy)
	if err == nil {
		s.enqueuePersist(targetKey, taskVersion, taskJSON)
		if s.hub != nil {
			s.hub.BroadcastTenant(taskKeyID, string(taskJSON))
		}
	}

	return taskCopy, nil
}

// InjectSteerTargetedTenant 向指定会话或其派生的特定子智能体注入结构化指导指令。
func (s *MonitorService) InjectSteerTargetedTenant(id string, inst task.SteerInstruction, keyID string, isMaster bool) (*task.Task, error) {
	s.mu.Lock()
	targetKey, t, exists := s.findTaskLocked(id, keyID, isMaster)
	if !exists {
		s.mu.Unlock()
		return nil, fmt.Errorf("task not found: %s", id)
	}

	text := strings.TrimSpace(inst.Message)
	if text == "" {
		s.mu.Unlock()
		return nil, fmt.Errorf("context text cannot be empty")
	}
	inst.Message = text
	if inst.CreatedAt == 0 {
		inst.CreatedAt = time.Now().Unix()
	}

	queueKey := targetKey
	if inst.TargetChildID != "" && inst.TargetChildID != id {
		childKey, child, childExists := s.findTaskLocked(inst.TargetChildID, keyID, isMaster)
		if childExists && child.ParentID == id {
			queueKey = childKey
		}
	}

	s.steerQueue[queueKey] = append(s.steerQueue[queueKey], inst)

	taskCopy := t.Clone()
	s.mu.Unlock()
	return taskCopy, nil
}

// InjectContextTenant 向指定会话注入动态上下文/提示词，将在下一次 Hook 交互时返回给 Agent。
func (s *MonitorService) InjectContextTenant(id string, contextText string, keyID string, isMaster bool) (*task.Task, error) {
	return s.InjectSteerTargetedTenant(id, task.SteerInstruction{
		Message: contextText,
	}, keyID, isMaster)
}

// InjectContext 向指定会话注入动态上下文。
func (s *MonitorService) InjectContext(id string, contextText string) (*task.Task, error) {
	return s.InjectContextTenant(id, contextText, "", true)
}

// GetTask 返回指定 ID 任务的只读深拷贝副本。
func (s *MonitorService) GetTask(id string) *task.Task {
	return s.GetTaskTenant(id, "", true)
}

// GetTaskTenant 根据 ID 及 KeyID 空间返回匹配的任务只读深拷贝副本。
func (s *MonitorService) GetTaskTenant(id string, keyID string, isMaster bool) *task.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, t, exists := s.findTaskLocked(id, keyID, isMaster)
	if exists && t != nil {
		return t.Clone()
	}
	return nil
}

// GetTaskEventReplay 查询指定会话的事件回放流水（Master 默认空间）。
func (s *MonitorService) GetTaskEventReplay(id string) ([]task.EventRecord, error) {
	return s.GetTaskEventReplayTenant(id, "", true)
}

// GetTaskEventReplayTenant 查询指定会话的时序回放事件流水日志。
// 优先检索仓储持久化记录或环形缓冲区快照，支持时间有序回放。
func (s *MonitorService) GetTaskEventReplayTenant(id string, tenantKeyID string, isMaster bool) ([]task.EventRecord, error) {
	s.mu.RLock()
	targetKey, _, exists := s.findTaskLocked(id, tenantKeyID, isMaster)
	if !exists {
		// 检查是否属于其他租户以区分 403 Forbidden 与 404 Not Found
		if !isMaster && tenantKeyID != "" {
			for otherKey, otherTask := range s.tasks {
				if otherTask != nil && otherTask.ID == id && otherKey.TenantID != tenantKeyID {
					s.mu.RUnlock()
					return nil, fmt.Errorf("%w: task %s belongs to tenant %s", ErrPermissionDenied, id, otherKey.TenantID)
				}
			}
		}
		s.mu.RUnlock()
		return nil, fmt.Errorf("task not found: %s", id)
	}

	var rbSnapshot []task.EventRecord
	if rb, ok := s.eventRingBuffers[targetKey]; ok && rb != nil {
		rbSnapshot = rb.Snapshot()
	}
	s.mu.RUnlock()

	var records []task.EventRecord
	if er, ok := s.repo.(task.EventLogRepository); ok {
		persisted, err := er.ReadEventLogs(targetKey)
		if err == nil && len(persisted) > 0 {
			records = append(records, persisted...)
		}
	}

	if len(records) == 0 {
		records = rbSnapshot
	} else if len(rbSnapshot) > 0 {
		// 合并内存中尚未刷盘的最新事件
		seen := make(map[string]struct{}, len(records))
		for _, r := range records {
			if r.EventID != "" {
				seen[r.EventID] = struct{}{}
			}
		}
		for _, r := range rbSnapshot {
			if r.EventID != "" {
				if _, ok := seen[r.EventID]; !ok {
					seen[r.EventID] = struct{}{}
					records = append(records, r)
				}
			}
		}
	}

	if records == nil {
		records = []task.EventRecord{}
	}

	// 确保按会话内序号及时间戳严格单调递增排序
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Sequence != records[j].Sequence {
			return records[i].Sequence < records[j].Sequence
		}
		return records[i].Timestamp < records[j].Timestamp
	})

	return records, nil
}

// GetTaskTraceSpansTenant 查询指定会话的 TraceSpan 追踪跨度列表与活跃跨度。
func (s *MonitorService) GetTaskTraceSpansTenant(id string, tenantKeyID string, isMaster bool, nowMs int64) ([]task.TraceSpan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, targetTask, exists := s.findTaskLocked(id, tenantKeyID, isMaster)
	if !exists {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	allSpans := make([]task.TraceSpan, len(targetTask.TraceSpans))
	copy(allSpans, targetTask.TraceSpans)
	activeSpans := targetTask.GetActiveTraceSpans(nowMs)
	for _, active := range activeSpans {
		found := false
		for i, existing := range allSpans {
			if existing.SpanID == active.SpanID {
				allSpans[i] = active
				found = true
				break
			}
		}
		if !found {
			allSpans = append(allSpans, active)
		}
	}
	return allSpans, nil
}

// DetectTaskAnomaliesTenant 执行指定会话的工具卡死与超时异常诊断（只读读锁）。
func (s *MonitorService) DetectTaskAnomaliesTenant(id string, tenantKeyID string, isMaster bool, nowMs int64) ([]task.AnomalyInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, targetTask, exists := s.findTaskLocked(id, tenantKeyID, isMaster)
	if !exists {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	return targetTask.DetectAnomalies(nowMs), nil
}

// KillTask 强制杀死指定会话关联的本地进程组，并将任务标记为终止终态。
func (s *MonitorService) KillTask(id string) (*task.Task, error) {
	return s.KillTaskTenant(id, "", true)
}

// KillTaskTenant 在指定租户权限下强制杀死会话关联的本地进程组（验证 HostID/BootID 与负 PGID 控制）。
func (s *MonitorService) KillTaskTenant(id string, keyID string, isMaster bool) (*task.Task, error) {
	s.mu.Lock()
	targetKey, t, exists := s.findTaskLocked(id, keyID, isMaster)
	if !exists {
		s.mu.Unlock()
		return nil, fmt.Errorf("task not found: %s", id)
	}

	// 1. 安全校验：如果任务上报了 HostID 或 BootID，且与 Monitor 运行宿主不匹配，严禁调用本机 kill 杀同号进程
	if (t.HostID != "" && t.HostID != s.hostID) || (t.BootID != "" && t.BootID != s.bootID) {
		s.mu.Unlock()
		return nil, fmt.Errorf("%w: task host (%s/%s) does not match monitor host (%s/%s)", ErrHostMismatch, t.HostID, t.BootID, s.hostID, s.bootID)
	}

	pid := t.PID
	pgid := t.PGID
	// 释放全局读写锁后再调用 OS 进程组终止系统调用，避免阻塞并发 HTTP 查询与事件流
	s.mu.Unlock()

	// 2. 真实进程组级终止：跨平台隔离实现（非持锁阶段）
	_ = terminateProcessGroup(pid, pgid)

	// 重新加锁更新领域状态
	s.mu.Lock()
	// 再次确认任务是否依然存在
	targetKey, t, exists = s.findTaskLocked(id, keyID, isMaster)
	if !exists || t == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("task disappeared during kill: %s", id)
	}

	nowMs := time.Now().UnixMilli()
	nowStr := time.Now().Format("15:04:05")

	reason := "用户强制终止了会话进程组 (SIGTERM/SIGKILL)"
	t.MarkKilled(reason, nowMs, nowStr)

	s.generation++
	t.Generation = s.generation
	taskCopy := t.Clone()
	taskKeyID := t.KeyID
	taskVersion := t.Version
	s.mu.Unlock()

	taskJSON, err := json.Marshal(taskCopy)
	if err == nil {
		s.enqueuePersist(targetKey, taskVersion, taskJSON)
		if s.hub != nil {
			s.hub.BroadcastTenant(taskKeyID, string(taskJSON))
		}
	}

	return taskCopy, nil
}

// Metrics 返回当前服务运行时关键指标快照。
func (s *MonitorService) Metrics() ServiceMetrics {
	s.mu.RLock()
	tasksCount := len(s.tasks)
	tenantsSet := make(map[string]struct{})
	for k := range s.tasks {
		tenantsSet[k.TenantID] = struct{}{}
	}
	tenantsCount := len(tenantsSet)
	s.mu.RUnlock()

	var sseCount int
	var sseDropped uint64
	if s.hub != nil {
		sseCount = s.hub.ClientCount()
		sseDropped = s.hub.DroppedEvents()
	}

	var quarantineCount int
	if jsonRepo, ok := s.repo.(*persistence.JSONRepository); ok && jsonRepo != nil {
		quarantineCount = jsonRepo.QuarantineStats().Count
	}

	return ServiceMetrics{
		TotalEventsReceived:  atomic.LoadUint64(&s.eventsReceived),
		PersistSuccessTotal:  atomic.LoadUint64(&s.persistSuccess),
		PersistErrorsTotal:   atomic.LoadUint64(&s.persistErrors),
		PersistDroppedTotal:  atomic.LoadUint64(&s.persistDropped),
		PersistQueueLength:   len(s.persistChan),
		PersistQueueCapacity: cap(s.persistChan),
		ActiveTasksCount:     tasksCount,
		TenantsCount:         tenantsCount,
		SSEClientsCount:      sseCount,
		SSEDroppedTotal:      sseDropped,
		QuarantineFilesCount: quarantineCount,
	}
}

// IsReady 检查服务核心依赖与写管道是否处于就绪可用状态。
func (s *MonitorService) IsReady() bool {
	select {
	case <-s.stopChan:
		return false
	default:
	}
	return s.repo != nil
}

// GetAllTasks 返回当前所有任务的独立只读深拷贝副本。
func (s *MonitorService) GetAllTasks() []*task.Task {
	return s.GetAllTasksTenant("", true)
}

// GetAllTasksTenant 返回属于指定 KeyID/租户空间任务的独立只读深拷贝副本。
func (s *MonitorService) GetAllTasksTenant(keyID string, isMaster bool) []*task.Task {
	tasks, _ := s.GetSnapshotWithGeneration(keyID, isMaster)
	return tasks
}

// GetSnapshotWithGeneration 返回快照与对应的单调递增 generation。
func (s *MonitorService) GetSnapshotWithGeneration(keyID string, isMaster bool) ([]*task.Task, uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*task.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if t != nil && t.BelongsTo(keyID, isMaster) {
			list = append(list, t.Clone())
		}
	}
	return list, s.generation
}

// DeleteTasksRequest 定义删除任务的参数载荷。
type DeleteTasksRequest struct {
	IDs []string `json:"ids,omitempty"`
	All bool     `json:"all,omitempty"`
}

// DeleteTasks 根据模式删除任务（指定 ids、全清、或清空已完成/失败），并通过 SSE 广播删除事件，返回被删除的 ID 列表。
func (s *MonitorService) DeleteTasks(req DeleteTasksRequest) []string {
	return s.DeleteTasksTenant(req, "", true)
}

// DeleteTasksTenant 在指定租户权限下删除属于该空间的任务。
func (s *MonitorService) DeleteTasksTenant(req DeleteTasksRequest, keyID string, isMaster bool) []string {
	s.mu.Lock()
	type delTarget struct {
		key task.TaskKey
		ver uint64
	}
	var toDelete []delTarget
	var toDeleteIDs []string

	if req.All {
		// 清空当前空间全部任务（包括 running）
		for k, t := range s.tasks {
			if t != nil && t.BelongsTo(keyID, isMaster) {
				delete(s.tasks, k)
				delete(s.steerQueue, k)
				delete(s.eventRingBuffers, k)
				toDelete = append(toDelete, delTarget{key: k, ver: t.Version})
				toDeleteIDs = append(toDeleteIDs, t.ID)
			}
		}
	} else if len(req.IDs) > 0 {
		// 精确删除指定 ID 列表
		for _, targetID := range req.IDs {
			k, t, exists := s.findTaskLocked(targetID, keyID, isMaster)
			if exists && t != nil {
				delete(s.tasks, k)
				delete(s.steerQueue, k)
				delete(s.eventRingBuffers, k)
				toDelete = append(toDelete, delTarget{key: k, ver: t.Version})
				toDeleteIDs = append(toDeleteIDs, t.ID)
			}
		}
	} else {
		// 默认行为：只清已完成和失败任务
		for k, t := range s.tasks {
			if t != nil && t.BelongsTo(keyID, isMaster) {
				if t.Status == "completed" || t.Status == "failed" {
					delete(s.tasks, k)
					delete(s.steerQueue, k)
					delete(s.eventRingBuffers, k)
					toDelete = append(toDelete, delTarget{key: k, ver: t.Version})
					toDeleteIDs = append(toDeleteIDs, t.ID)
				}
			}
		}
	}
	s.generation++
	gen := s.generation
	s.mu.Unlock()

	// 统一写入有序持久化命令流，写入墓碑压制旧 Save，杜绝删除后复活
	if s.repo != nil && len(toDelete) > 0 {
		for _, item := range toDelete {
			s.enqueueDelete(item.key, item.ver)
		}
	}

	// 广播 SSE 删除消息给该空间客户端（同时携带 deletedIds、deletedKeys 和 generation）
	if s.hub != nil && len(toDeleteIDs) > 0 {
		var toDeleteKeyStrs []string
		for _, item := range toDelete {
			toDeleteKeyStrs = append(toDeleteKeyStrs, item.key.String())
		}
		delEvent := map[string]interface{}{
			"type":        "delete_tasks",
			"deletedIds":  toDeleteIDs,
			"deletedKeys": toDeleteKeyStrs,
			"generation":  gen,
		}
		if msgJSON, err := json.Marshal(delEvent); err == nil {
			s.hub.BroadcastEvent("delete_tasks", keyID, string(msgJSON))
		}
	}

	return toDeleteIDs
}

// forgetTask 删除从未产生真实工作的空会话，避免看板留下「打开即关」的占位卡片。
func (s *MonitorService) forgetTask(key task.TaskKey, version uint64) {
	if key.IsZero() {
		return
	}
	delete(s.steerQueue, key)
	if s.repo != nil {
		s.enqueueDelete(key, version)
	}
	s.generation++
	gen := s.generation
	if s.hub != nil {
		delEvent := map[string]interface{}{
			"type":        "delete_tasks",
			"deletedIds":  []string{key.TaskID},
			"deletedKeys": []string{key.String()},
			"generation":  gen,
		}
		if msgJSON, err := json.Marshal(delEvent); err == nil {
			s.hub.BroadcastEvent("delete_tasks", key.TenantID, string(msgJSON))
		}
	}
}

// ClearFinishedTasks 清除所有已完成或失败的任务，并返回清除数量。
func (s *MonitorService) ClearFinishedTasks() int {
	deleted := s.DeleteTasks(DeleteTasksRequest{})
	return len(deleted)
}

// ArchiveTask 将指定 ID 的任务执行冷归档（默认租户/Master）。
func (s *MonitorService) ArchiveTask(id string) (string, error) {
	return s.ArchiveTaskTenant(id, "", true)
}

// ArchiveTaskTenant 按租户权限校验并将已完成/失败的任务打包为 gzip tar 归档并清理活跃存储。
func (s *MonitorService) ArchiveTaskTenant(id string, tenantKeyID string, isMaster bool) (string, error) {
	s.mu.Lock()
	targetKey, t, exists := s.findTaskLocked(id, tenantKeyID, isMaster)
	var taskVer uint64
	var latestSnapshot []byte
	if exists {
		if !isMaster && t.KeyID != "" && t.KeyID != tenantKeyID {
			s.mu.Unlock()
			return "", fmt.Errorf("%w: task %s belongs to tenant %s, cannot be archived by %s", ErrPermissionDenied, id, t.KeyID, tenantKeyID)
		}
		if t.Status == "running" {
			s.mu.Unlock()
			return "", fmt.Errorf("task %s is currently running; only completed or failed tasks can be archived", id)
		}
		taskVer = t.Version
		latestSnapshot, _ = json.MarshalIndent(t, "", "  ")
	} else {
		if !isMaster {
			normTenant := task.NormalizeTenantID(tenantKeyID)
			for otherKey, otherTask := range s.tasks {
				if otherTask != nil && otherTask.ID == id && otherKey.TenantID != normTenant {
					s.mu.Unlock()
					return "", fmt.Errorf("%w: task %s belongs to tenant %s, cannot be archived by %s", ErrPermissionDenied, id, otherKey.TenantID, tenantKeyID)
				}
			}
		}
		targetKey = task.NewTaskKey(tenantKeyID, id)
	}
	s.mu.Unlock()

	if s.repo == nil {
		return "", fmt.Errorf("repository is nil")
	}

	// 1. 同步预刷盘：若内存中存在最新状态，立即同步刷盘以保证打包数据绝对新鲜，消除与 persistChan 的时序竞态
	if len(latestSnapshot) > 0 {
		_ = s.repo.SaveRawKey(targetKey, latestSnapshot)
	}

	// 2. 执行打包并删除 raw 磁盘文件
	archivePath, err := s.repo.ArchiveTask(targetKey)
	if err != nil {
		return "", err
	}

	// 3. 写入墓碑：压制并作废 persistChan 中可能正在排队写入的旧版本 Save 命令，杜绝已归档任务被滞后写“死而复生”
	_ = s.repo.DeleteKeyVersioned(targetKey, taskVer+1)

	s.mu.Lock()
	delete(s.tasks, targetKey)
	delete(s.eventRingBuffers, targetKey)
	delete(s.steerQueue, targetKey)
	s.generation++
	gen := s.generation
	s.mu.Unlock()

	if s.hub != nil {
		delEvent := map[string]interface{}{
			"type":        "delete_tasks",
			"deletedIds":  []string{targetKey.TaskID},
			"deletedKeys": []string{targetKey.String()},
			"generation":  gen,
			"archived":    true,
		}
		if msgJSON, err := json.Marshal(delEvent); err == nil {
			s.hub.BroadcastEvent("delete_tasks", targetKey.TenantID, string(msgJSON))
		}
	}

	return archivePath, nil
}

// ArchiveCompletedTasks 批量将截止时间前的已完结任务执行冷归档（Master/全量）。
func (s *MonitorService) ArchiveCompletedTasks(beforeTime time.Time) ([]string, error) {
	return s.ArchiveCompletedTasksTenant("*", true, beforeTime)
}

// ArchiveCompletedTasksTenant 批量将指定租户在截止时间前的已完结任务执行冷归档。
func (s *MonitorService) ArchiveCompletedTasksTenant(tenantKeyID string, isMaster bool, beforeTime time.Time) ([]string, error) {
	targetTenant := tenantKeyID
	if !isMaster {
		if targetTenant == "" || targetTenant == "*" {
			targetTenant = "default"
		}
	}

	if s.repo == nil {
		return nil, fmt.Errorf("repository is nil")
	}

	archivedResults, err := s.repo.ArchiveCompletedTasks(targetTenant, beforeTime)
	if err != nil {
		return nil, err
	}

	var archivedPaths []string
	if len(archivedResults) > 0 {
		var evictedIDs []string
		var evictedKeys []string
		s.mu.Lock()
		for _, item := range archivedResults {
			archivedPaths = append(archivedPaths, item.ArchivePath)
			delete(s.tasks, item.Key)
			delete(s.eventRingBuffers, item.Key)
			delete(s.steerQueue, item.Key)
			evictedIDs = append(evictedIDs, item.Key.TaskID)
			evictedKeys = append(evictedKeys, item.Key.String())
		}
		s.generation++
		gen := s.generation
		s.mu.Unlock()

		if s.hub != nil && len(evictedIDs) > 0 {
			delEvent := map[string]interface{}{
				"type":        "delete_tasks",
				"deletedIds":  evictedIDs,
				"deletedKeys": evictedKeys,
				"generation":  gen,
				"archived":    true,
			}
			if msgJSON, err := json.Marshal(delEvent); err == nil {
				s.hub.BroadcastEvent("delete_tasks", targetTenant, string(msgJSON))
			}
		}
	}

	return archivedPaths, nil
}

func shouldSummarizeSessionTitle(event, action string, t *task.Task) bool {
	if t == nil || len(t.Runs) == 0 {
		return false
	}
	st := t.Runs[len(t.Runs)-1].Status
	if st != "completed" && st != "failed" {
		return false
	}
	return isTurnSettleEvent(event) || action == "deny"
}

func (s *MonitorService) scheduleTitleSummary(key task.TaskKey) {
	if s == nil || s.summarizer == nil || !s.summarizer.Enabled() || key.IsZero() {
		return
	}
	keyStr := key.String()
	stateI, _ := s.titleJobs.LoadOrStore(keyStr, &titleJobState{})
	state := stateI.(*titleJobState)
	state.mu.Lock()
	if state.running {
		state.pending = true
		state.mu.Unlock()
		return
	}
	state.running = true
	state.mu.Unlock()
	go s.runTitleSummary(key)
}

func (s *MonitorService) runTitleSummary(key task.TaskKey) {
	keyStr := key.String()
	for {
		select {
		case <-s.stopChan:
			return
		default:
		}

		s.mu.RLock()
		var snap *task.Task
		if t, ok := s.tasks[key]; ok && t != nil {
			snap = t.Clone()
		}
		s.mu.RUnlock()

		if snap != nil && s.summarizer != nil && s.summarizer.Enabled() {
			title, err := s.summarizer.Summarize(snap)
			if err == nil && strings.TrimSpace(title) != "" {
				s.applySummarizedTitle(key, title)
			} else if err != nil {
				log.Printf("[Application] Title summary skipped for %s: %v", keyStr, err)
			}

			s.mu.RLock()
			if t, ok := s.tasks[key]; ok && t != nil {
				snap = t.Clone()
			}
			s.mu.RUnlock()
			if snap != nil && shouldRefreshGoal(snap, s.summarizer.GoalEveryN(), snap.LastHook) {
				goal, err := s.summarizer.SummarizeGoal(snap)
				if err == nil && strings.TrimSpace(goal) != "" {
					s.applySummarizedGoal(key, goal, settledRunCount(snap))
				} else if err != nil {
					log.Printf("[Application] Goal summary skipped for %s: %v", keyStr, err)
				}
			}
		}

		stateI, ok := s.titleJobs.Load(keyStr)
		if !ok {
			return
		}
		state := stateI.(*titleJobState)
		state.mu.Lock()
		if state.pending {
			state.pending = false
			state.mu.Unlock()
			continue
		}
		state.running = false
		state.mu.Unlock()
		return
	}
}

func (s *MonitorService) applySummarizedTitle(key task.TaskKey, title string) {
	s.mu.Lock()
	t, ok := s.tasks[key]
	if !ok || t == nil {
		s.mu.Unlock()
		return
	}
	if !t.ApplyDisplayTitle(title) {
		s.mu.Unlock()
		return
	}
	s.generation++
	t.Generation = s.generation
	taskKeyID := t.KeyID
	taskVersion := t.Version
	taskCopy := t.Clone()
	s.mu.Unlock()

	taskJSON, err := json.Marshal(taskCopy)
	if err != nil {
		return
	}
	s.enqueuePersist(key, taskVersion, taskJSON)
	if s.hub != nil {
		s.hub.BroadcastTenant(taskKeyID, string(taskJSON))
	}
}

func (s *MonitorService) applySummarizedGoal(key task.TaskKey, goal string, atRun int) {
	s.mu.Lock()
	t, ok := s.tasks[key]
	if !ok || t == nil {
		s.mu.Unlock()
		return
	}
	if !t.ApplyGoalSummary(goal, atRun) {
		s.mu.Unlock()
		return
	}
	s.generation++
	t.Generation = s.generation
	taskKeyID := t.KeyID
	taskVersion := t.Version
	taskCopy := t.Clone()
	s.mu.Unlock()

	taskJSON, err := json.Marshal(taskCopy)
	if err != nil {
		return
	}
	s.enqueuePersist(key, taskVersion, taskJSON)
	if s.hub != nil {
		s.hub.BroadcastTenant(taskKeyID, string(taskJSON))
	}
}
