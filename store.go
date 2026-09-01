package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Store 定义 Monitor 会话数据的持久化存储接口，便于后续切换介质（SQLite / Postgres / KV / JSON 等）。
type Store interface {
	// LoadAll 加载所有持久化的任务
	LoadAll() ([]*Task, error)
	// SaveTask 保存或更新单个任务
	SaveTask(task *Task) error
	// DeleteTask 按 ID 删除持久化的任务
	DeleteTask(id string) error
	// Close 释放或关闭底层存储资源
	Close() error
}

// JSONStore 是基于本地文件系统 JSON 文件的 Store 实现。
// 每个会话保存为独立的 session_id.json 文件。
type JSONStore struct {
	dir string
	mu  sync.Mutex // 保护文件读写冲突
}

// NewJSONStore 初始化本地 JSON 存储目录。
func NewJSONStore(dir string) (*JSONStore, error) {
	if dir == "" {
		dir = "data/sessions"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create store dir %s: %w", dir, err)
	}
	return &JSONStore{
		dir: dir,
	}, nil
}

// safeFilename 对 session ID 进行字符过滤，防止路径穿越或非法文件名。
func safeFilename(id string) string {
	cleaned := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, id)
	if cleaned == "" {
		cleaned = "unknown"
	}
	return cleaned + ".json"
}

// taskPath 返回任务对应的绝对/相对文件路径。
func (s *JSONStore) taskPath(id string) string {
	return filepath.Join(s.dir, safeFilename(id))
}

// LoadAll 遍历存储目录并读取反序列化所有 JSON 文件。
func (s *JSONStore) LoadAll() ([]*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read store dir failed: %w", err)
	}

	var tasks []*Task
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		filePath := filepath.Join(s.dir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var task Task
		if err := json.Unmarshal(data, &task); err != nil {
			continue
		}
		tasks = append(tasks, &task)
	}

	return tasks, nil
}

// SaveTask 使用原子写入（临时文件 + 重命名）保存任务，防止并发半写或断电损坏。
func (s *JSONStore) SaveTask(task *Task) error {
	if task == nil || task.ID == "" {
		return fmt.Errorf("invalid task to save")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal task %s: %w", task.ID, err)
	}

	targetPath := s.taskPath(task.ID)
	tmpPath := targetPath + fmt.Sprintf(".tmp-%d", os.Getpid())

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write tmp file for task %s: %w", task.ID, err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to commit task file %s: %w", targetPath, err)
	}

	return nil
}

// DeleteTask 删除对应的持久化文件。
func (s *JSONStore) DeleteTask(id string) error {
	if id == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	targetPath := s.taskPath(id)
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete task file %s: %w", targetPath, err)
	}
	return nil
}

// Close 清理释放资源。
func (s *JSONStore) Close() error {
	return nil
}
