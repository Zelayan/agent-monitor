package monitor

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Zelayan/agent-monitor/internal/domain/task"
)

// taskWriteRequest 封装按 Task 串行持久化的请求。
type taskWriteRequest struct {
	id   string
	data []byte
}

// MonitorService 负责会话用例编排、事件处理与仓储/广播联动。
type MonitorService struct {
	mu         sync.RWMutex
	tasks      map[string]*task.Task
	repo       task.TaskRepository
	hub        *Hub
	writeChan  chan taskWriteRequest // 异步串行持久化管道，消除无节制 goroutine 与磁盘乱序
	stopChan   chan struct{}
	ttlDays    int // 自动清理天数（默认 30 天，<=0 则不清理）
	summarizer *TitleSummarizer
	titleJobs  sync.Map // map[string]*titleJobState，同一会话 LLM 总结串行且可合并
}

// NewMonitorService 实例化应用服务并从仓储加载已有会话数据。
func NewMonitorService(repo task.TaskRepository, hub *Hub) *MonitorService {
	return NewMonitorServiceWithTTL(repo, hub, 30)
}

// NewMonitorServiceWithTTL 实例化应用服务并指定会话保留天数。
func NewMonitorServiceWithTTL(repo task.TaskRepository, hub *Hub, ttlDays int) *MonitorService {
	s := &MonitorService{
		tasks:     make(map[string]*task.Task),
		repo:      repo,
		hub:       hub,
		writeChan: make(chan taskWriteRequest, 5000), // 削峰缓冲
		stopChan:  make(chan struct{}),
		ttlDays:   ttlDays,
	}

	if repo != nil {
		persisted, err := repo.FindAll()
		if err != nil {
			log.Printf("[Application] Warning: failed to load persisted tasks: %v", err)
		} else {
			for _, t := range persisted {
				if t != nil && t.ID != "" {
					if t.CloseOrphanRuns(time.Now().UnixMilli(), time.Now().Format("15:04:05")) {
						if data, err := json.Marshal(t); err == nil {
							if err := repo.SaveRaw(t.ID, data); err != nil {
								log.Printf("[Application] Warning: failed to persist healed task %s: %v", t.ID, err)
							}
						}
					}
					s.tasks[t.ID] = t
				}
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

// persistenceWorker 顺序消费写入管道，彻底消除并发 goroutine 调度乱序导致的磁盘倒流
func (s *MonitorService) persistenceWorker() {
	for {
		select {
		case <-s.stopChan:
			return
		case req := <-s.writeChan:
			if s.repo != nil && req.id != "" && len(req.data) > 0 {
				if err := s.repo.SaveRaw(req.id, req.data); err != nil {
					log.Printf("[Application] Error persisting task %s: %v", req.id, err)
				}
			}
		}
	}
}

// enqueuePersist 尝试将序列化数据推入写入队列
func (s *MonitorService) enqueuePersist(id string, data []byte) {
	if s.repo == nil || id == "" || len(data) == 0 {
		return
	}
	select {
	case s.writeChan <- taskWriteRequest{id: id, data: data}:
	default:
		// 若队列暴涨触发极端背压，直接在独立 goroutine 写入
		go func(taskID string, taskData []byte) {
			_ = s.repo.SaveRaw(taskID, taskData)
		}(id, data)
	}
}

// janitorWorker 定时巡检清理已完成且超期的任务
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
	var toDelete []string

	s.mu.Lock()
	for id, t := range s.tasks {
		if t != nil && (t.Status == "completed" || t.Status == "failed") {
			endTime := t.EndTime
			if endTime == 0 {
				endTime = t.StartTime
			}
			if endTime > 0 && endTime < cutoffMs {
				delete(s.tasks, id)
				toDelete = append(toDelete, id)
			}
		}
	}
	s.mu.Unlock()

	if s.repo != nil && len(toDelete) > 0 {
		for _, id := range toDelete {
			_ = s.repo.Delete(id)
		}
		log.Printf("[Application] Janitor cleaned up %d expired tasks (older than %d days)", len(toDelete), s.ttlDays)
	}
}

// Close 停止后台 worker
func (s *MonitorService) Close() {
	select {
	case <-s.stopChan:
	default:
		close(s.stopChan)
	}
}

// HookEventResult 封装事件处理后的 Task 实体快照及向 Reporter 下发的控制指令。
type HookEventResult struct {
	Task   *task.Task
	Action string // "allow" | "deny" | "abort"
	Reason string
}

func isPreActionHook(event string) bool {
	switch event {
	case "beforeShellExecution", "beforeMCPExecution", "preToolUse", "PreToolUse", "PermissionRequest", "subagentStart", "beforeSubmitPrompt", "UserPromptSubmit":
		return true
	default:
		return false
	}
}

// HandleHookEvent 处理来自 Hook 的上报事件，并根据当前控制状态返回决策指令。
func (s *MonitorService) HandleHookEvent(p task.EventPayload) (HookEventResult, error) {
	if p.Timestamp == 0 {
		p.Timestamp = time.Now().Unix()
	}

	nowMs := p.Timestamp * 1000
	nowStr := time.Unix(p.Timestamp, 0).Format("15:04:05")

	s.mu.Lock()
	t, exists := s.tasks[p.ID]
	if !exists {
		t = task.NewTask(p, nowMs)
		s.tasks[t.ID] = t
	}

	action := "allow"
	reason := ""

	// 控制反转：如果当前会话已被请求中断，且当前 Hook 为前置拦截点，立即下发 deny 并标记为终态
	if t.IsAbortRequested() && isPreActionHook(p.Event) {
		action = "deny"
		reason = t.AbortReason
		if reason == "" {
			reason = "Session aborted from Agent Monitor Dashboard"
		}
		t.MarkAborted(reason, nowMs, nowStr)
	} else {
		t.ApplyEvent(p, nowMs, nowStr)
	}

	taskID := t.ID
	taskKeyID := t.KeyID
	taskCopy := t.Clone()
	s.mu.Unlock() // 【锁范围最小化：立即释放锁，杜绝持锁执行 CPU 密集序列化】

	taskJSON, err := json.Marshal(taskCopy)
	if err != nil {
		return HookEventResult{Task: taskCopy, Action: action, Reason: reason}, fmt.Errorf("failed to marshal task: %w", err)
	}

	// 异步持久化：写入串行队列，消除 goroutine 激增与磁盘乱序倒流
	s.enqueuePersist(taskID, taskJSON)

	// 广播事件（向该租户空间及 Master 广播）
	if s.hub != nil {
		s.hub.BroadcastTenant(taskKeyID, string(taskJSON))
	}

	if shouldSummarizeSessionTitle(p.Event, action, taskCopy) {
		s.scheduleTitleSummary(taskCopy.ID)
	}

	return HookEventResult{
		Task:   taskCopy,
		Action: action,
		Reason: reason,
	}, nil
}

// AbortTask 标记指定会话为中断请求状态，并向该租户客户端广播状态变更。
func (s *MonitorService) AbortTask(id string, reason string) (*task.Task, error) {
	return s.AbortTaskTenant(id, reason, "", true)
}

// AbortTaskTenant 在指定租户权限下标记会话为中断请求状态。
func (s *MonitorService) AbortTaskTenant(id string, reason string, keyID string, isMaster bool) (*task.Task, error) {
	s.mu.Lock()
	t, exists := s.tasks[id]
	if !exists {
		s.mu.Unlock()
		return nil, fmt.Errorf("task not found: %s", id)
	}
	if !t.BelongsTo(keyID, isMaster) {
		s.mu.Unlock()
		return nil, fmt.Errorf("permission denied for task: %s", id)
	}

	nowMs := time.Now().UnixMilli()
	nowStr := time.Now().Format("15:04:05")

	if reason == "" {
		reason = "用户从 Web 看板中断了会话"
	}
	t.RequestAbort(reason, nowMs, nowStr)

	taskID := t.ID
	taskKeyID := t.KeyID
	taskCopy := t.Clone()
	s.mu.Unlock() // 释放锁

	taskJSON, err := json.Marshal(taskCopy)
	if err == nil {
		s.enqueuePersist(taskID, taskJSON)
		if s.hub != nil {
			s.hub.BroadcastTenant(taskKeyID, string(taskJSON))
		}
	}

	return taskCopy, nil
}

// GetTask 返回指定 ID 任务的只读深拷贝副本。
func (s *MonitorService) GetTask(id string) *task.Task {
	return s.GetTaskTenant(id, "", true)
}

// GetTaskTenant 根据 ID 及 KeyID 空间返回匹配的任务只读深拷贝副本。
func (s *MonitorService) GetTaskTenant(id string, keyID string, isMaster bool) *task.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.tasks[id]; ok && t != nil && t.BelongsTo(keyID, isMaster) {
		return t.Clone()
	}
	return nil
}

// KillTask 强制杀死指定会话关联的本地进程组，并将任务标记为终止终态。
func (s *MonitorService) KillTask(id string) (*task.Task, error) {
	return s.KillTaskTenant(id, "", true)
}

// KillTaskTenant 在指定租户权限下强制杀死会话关联的本地进程组。
func (s *MonitorService) KillTaskTenant(id string, keyID string, isMaster bool) (*task.Task, error) {
	s.mu.Lock()
	t, exists := s.tasks[id]
	if !exists {
		s.mu.Unlock()
		return nil, fmt.Errorf("task not found: %s", id)
	}
	if !t.BelongsTo(keyID, isMaster) {
		s.mu.Unlock()
		return nil, fmt.Errorf("permission denied for task: %s", id)
	}

	pid := t.PID
	nowMs := time.Now().UnixMilli()
	nowStr := time.Now().Format("15:04:05")

	// 尝试向本地操作系统进程发送中断信号
	if pid > 0 {
		proc, err := os.FindProcess(pid)
		if err == nil && proc != nil {
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				_ = proc.Kill()
			}
		}
	}

	reason := "用户强制终止了会话进程 (SIGTERM/SIGKILL)"
	t.MarkKilled(reason, nowMs, nowStr)

	taskID := t.ID
	taskKeyID := t.KeyID
	taskCopy := t.Clone()
	s.mu.Unlock() // 释放锁

	taskJSON, err := json.Marshal(taskCopy)
	if err == nil {
		s.enqueuePersist(taskID, taskJSON)
		if s.hub != nil {
			s.hub.BroadcastTenant(taskKeyID, string(taskJSON))
		}
	}

	return taskCopy, nil
}

// GetAllTasks 返回当前所有任务的独立只读深拷贝副本。
func (s *MonitorService) GetAllTasks() []*task.Task {
	return s.GetAllTasksTenant("", true)
}

// GetAllTasksTenant 返回属于指定 KeyID/租户空间任务的独立只读深拷贝副本。
func (s *MonitorService) GetAllTasksTenant(keyID string, isMaster bool) []*task.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*task.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if t != nil && t.BelongsTo(keyID, isMaster) {
			list = append(list, t.Clone())
		}
	}
	return list
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
	var toDelete []string

	if req.All {
		// 清空当前空间全部任务（包括 running）
		for id, t := range s.tasks {
			if t != nil && t.BelongsTo(keyID, isMaster) {
				delete(s.tasks, id)
				toDelete = append(toDelete, id)
			}
		}
	} else if len(req.IDs) > 0 {
		// 精确删除指定 ID 列表
		for _, targetID := range req.IDs {
			if t, exists := s.tasks[targetID]; exists && t.BelongsTo(keyID, isMaster) {
				delete(s.tasks, targetID)
				toDelete = append(toDelete, targetID)
			}
		}
	} else {
		// 默认行为：只清已完成和失败任务
		for id, t := range s.tasks {
			if t != nil && t.BelongsTo(keyID, isMaster) {
				if t.Status == "completed" || t.Status == "failed" {
					delete(s.tasks, id)
					toDelete = append(toDelete, id)
				}
			}
		}
	}
	s.mu.Unlock()

	// 异步持久化清理
	if s.repo != nil && len(toDelete) > 0 {
		go func(ids []string) {
			for _, id := range ids {
				if err := s.repo.Delete(id); err != nil {
					log.Printf("[Application] Error deleting task file %s: %v", id, err)
				}
			}
		}(toDelete)
	}

	// 广播 SSE 删除消息给该空间客户端
	if s.hub != nil && len(toDelete) > 0 {
		delEvent := map[string]interface{}{
			"type":       "delete_tasks",
			"deletedIds": toDelete,
		}
		if msgJSON, err := json.Marshal(delEvent); err == nil {
			s.hub.BroadcastTenant(keyID, string(msgJSON))
		}
	}

	return toDelete
}

// ClearFinishedTasks 清除所有已完成或失败的任务，并返回清除数量。
func (s *MonitorService) ClearFinishedTasks() int {
	deleted := s.DeleteTasks(DeleteTasksRequest{})
	return len(deleted)
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

func (s *MonitorService) scheduleTitleSummary(id string) {
	if s == nil || s.summarizer == nil || !s.summarizer.Enabled() || id == "" {
		return
	}
	stateI, _ := s.titleJobs.LoadOrStore(id, &titleJobState{})
	state := stateI.(*titleJobState)
	state.mu.Lock()
	if state.running {
		state.pending = true
		state.mu.Unlock()
		return
	}
	state.running = true
	state.mu.Unlock()
	go s.runTitleSummary(id)
}

func (s *MonitorService) runTitleSummary(id string) {
	for {
		select {
		case <-s.stopChan:
			return
		default:
		}

		s.mu.RLock()
		var snap *task.Task
		if t, ok := s.tasks[id]; ok && t != nil {
			snap = t.Clone()
		}
		s.mu.RUnlock()

		if snap != nil && s.summarizer != nil && s.summarizer.Enabled() {
			title, err := s.summarizer.Summarize(snap)
			if err == nil && strings.TrimSpace(title) != "" {
				s.applySummarizedTitle(id, title)
			} else if err != nil {
				log.Printf("[Application] Title summary skipped for %s: %v", id, err)
			}
		}

		stateI, ok := s.titleJobs.Load(id)
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

func (s *MonitorService) applySummarizedTitle(id, title string) {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if !ok || t == nil {
		s.mu.Unlock()
		return
	}
	if !t.ApplyDisplayTitle(title) {
		s.mu.Unlock()
		return
	}
	taskID := t.ID
	taskKeyID := t.KeyID
	taskCopy := t.Clone()
	s.mu.Unlock()

	taskJSON, err := json.Marshal(taskCopy)
	if err != nil {
		return
	}
	s.enqueuePersist(taskID, taskJSON)
	if s.hub != nil {
		s.hub.BroadcastTenant(taskKeyID, string(taskJSON))
	}
}
