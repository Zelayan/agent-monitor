package monitor

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"agent-monitor/internal/domain/task"
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
	s.mu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("failed to marshal task: %w", err)
	}

	// 异步持久化
	if s.repo != nil {
		go func(target *task.Task) {
			if err := s.repo.Save(target); err != nil {
				log.Printf("[Application] Error saving task %s: %v", target.ID, err)
			}
		}(t)
	}

	// 广播事件
	if s.hub != nil {
		s.hub.Broadcast(string(taskJSON))
	}

	return t, nil
}

// GetAllTasks 返回当前所有任务列表。
func (s *MonitorService) GetAllTasks() []*task.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*task.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		list = append(list, t)
	}
	return list
}

// ClearFinishedTasks 清除所有已完成或失败的任务，并返回清除数量。
func (s *MonitorService) ClearFinishedTasks() int {
	s.mu.Lock()
	var toDelete []string
	for id, t := range s.tasks {
		if t.Status == "completed" || t.Status == "failed" {
			delete(s.tasks, id)
			toDelete = append(toDelete, id)
		}
	}
	s.mu.Unlock()

	if s.repo != nil && len(toDelete) > 0 {
		go func(ids []string) {
			for _, id := range ids {
				if err := s.repo.Delete(id); err != nil {
					log.Printf("[Application] Error deleting task file %s: %v", id, err)
				}
			}
		}(toDelete)
	}

	return len(toDelete)
}
