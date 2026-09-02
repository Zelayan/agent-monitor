package monitor

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
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

// HandleHookEvent 处理来自 Hook 的上报事件。
func (s *MonitorService) HandleHookEvent(p task.EventPayload) (*task.Task, error) {
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
	t.ApplyEvent(p, nowMs, nowStr)

	taskJSON, err := json.Marshal(t)
	taskID := t.ID
	taskCopy := t.Clone()
	s.mu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("failed to marshal task: %w", err)
	}

	// 异步持久化：写入不可变快照字节切片，彻底消除数据竞争
	if s.repo != nil {
		go func(id string, data []byte) {
			if err := s.repo.SaveRaw(id, data); err != nil {
				log.Printf("[Application] Error saving task %s: %v", id, err)
			}
		}(taskID, taskJSON)
	}

	// 广播事件
	if s.hub != nil {
		s.hub.Broadcast(string(taskJSON))
	}

	return taskCopy, nil
}

// GetAllTasks 返回当前所有任务的独立只读深拷贝副本。
func (s *MonitorService) GetAllTasks() []*task.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*task.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if t != nil {
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
	s.mu.Lock()
	var toDelete []string

	if req.All {
		// 清空全部任务（包括 running）
		for id := range s.tasks {
			delete(s.tasks, id)
			toDelete = append(toDelete, id)
		}
	} else if len(req.IDs) > 0 {
		// 精确删除指定 ID 列表
		for _, targetID := range req.IDs {
			if _, exists := s.tasks[targetID]; exists {
				delete(s.tasks, targetID)
				toDelete = append(toDelete, targetID)
			}
		}
	} else {
		// 默认行为：只清已完成和失败任务
		for id, t := range s.tasks {
			if t.Status == "completed" || t.Status == "failed" {
				delete(s.tasks, id)
				toDelete = append(toDelete, id)
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

	// 广播 SSE 删除消息，确保所有客户端同步清理
	if s.hub != nil && len(toDelete) > 0 {
		delEvent := map[string]interface{}{
			"type":       "delete_tasks",
			"deletedIds": toDelete,
		}
		if msgJSON, err := json.Marshal(delEvent); err == nil {
			s.hub.Broadcast(string(msgJSON))
		}
	}

	return toDelete
}

// ClearFinishedTasks 清除所有已完成或失败的任务，并返回清除数量。
func (s *MonitorService) ClearFinishedTasks() int {
	deleted := s.DeleteTasks(DeleteTasksRequest{})
	return len(deleted)
}
