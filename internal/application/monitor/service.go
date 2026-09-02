package monitor

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/Zelayan/agent-monitor/internal/domain/task"
)

// MonitorService 负责会话用例编排、事件处理与仓储/广播联动。
type MonitorService struct {
	mu    sync.RWMutex
	tasks map[string]*task.Task
	repo  task.TaskRepository
	hub   *Hub
}

// NewMonitorService 实例化应用服务并从仓储加载已有会话数据。
func NewMonitorService(repo task.TaskRepository, hub *Hub) *MonitorService {
	s := &MonitorService{
		tasks: make(map[string]*task.Task),
		repo:  repo,
		hub:   hub,
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

	return s
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

	taskJSON, err := json.Marshal(t)
	taskID := t.ID
	taskCopy := t.Clone()
	s.mu.Unlock()

	if err != nil {
		return HookEventResult{Task: taskCopy, Action: action, Reason: reason}, fmt.Errorf("failed to marshal task: %w", err)
	}

	// 异步持久化：写入不可变快照字节切片，彻底消除数据竞争
	if s.repo != nil {
		go func(id string, data []byte) {
			if err := s.repo.SaveRaw(id, data); err != nil {
				log.Printf("[Application] Error saving task %s: %v", id, err)
			}
		}(taskID, taskJSON)
	}

	// 广播事件（向该租户空间及 Master 广播）
	if s.hub != nil {
		s.hub.BroadcastTenant(t.KeyID, string(taskJSON))
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

	taskJSON, err := json.Marshal(t)
	taskID := t.ID
	taskKeyID := t.KeyID
	taskCopy := t.Clone()
	s.mu.Unlock()

	if err == nil {
		if s.repo != nil {
			go func(id string, data []byte) {
				_ = s.repo.SaveRaw(id, data)
			}(taskID, taskJSON)
		}
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

	taskJSON, err := json.Marshal(t)
	taskID := t.ID
	taskKeyID := t.KeyID
	taskCopy := t.Clone()
	s.mu.Unlock()

	if err == nil {
		if s.repo != nil {
			go func(id string, data []byte) {
				_ = s.repo.SaveRaw(id, data)
			}(taskID, taskJSON)
		}
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
