package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"agent-monitor/internal/domain/task"
)

// JSONRepository 是基于本地 JSON 文件的 TaskRepository 实现。
type JSONRepository struct {
	dir string
	mu  sync.Mutex // 保护文件读写并发
}

// NewJSONRepository 初始化并返回 JSONRepository 实例。
func NewJSONRepository(dir string) (*JSONRepository, error) {
	if dir == "" {
		dir = "data/sessions"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create repository dir %s: %w", dir, err)
	}
	return &JSONRepository{
		dir: dir,
	}, nil
}

// SafeFilename 对 session ID 进行字符过滤，防止路径穿越或非法文件名。
func SafeFilename(id string) string {
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

// taskPath 返回任务对应的文件路径。
func (r *JSONRepository) taskPath(id string) string {
	return filepath.Join(r.dir, SafeFilename(id))
}

// FindAll 遍历并反序列化所有持久化的 Task 记录。
func (r *JSONRepository) FindAll() ([]*task.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read repository dir failed: %w", err)
	}

	var tasks []*task.Task
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		filePath := filepath.Join(r.dir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var t task.Task
		if err := json.Unmarshal(data, &t); err != nil {
			continue
		}
		tasks = append(tasks, &t)
	}

	return tasks, nil
}

// Save 使用原子写入（临时文件 + 重命名）保存任务。
func (r *JSONRepository) Save(t *task.Task) error {
	if t == nil || t.ID == "" {
		return fmt.Errorf("invalid task to save")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal task %s: %w", t.ID, err)
	}

	targetPath := r.taskPath(t.ID)
	tmpPath := targetPath + fmt.Sprintf(".tmp-%d", os.Getpid())

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write tmp file for task %s: %w", t.ID, err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to commit task file %s: %w", targetPath, err)
	}

	return nil
}

// Delete 删除指定 ID 的任务文件。
func (r *JSONRepository) Delete(id string) error {
	if id == "" {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	targetPath := r.taskPath(id)
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete task file %s: %w", targetPath, err)
	}
	return nil
}

// Close 释放资源。
func (r *JSONRepository) Close() error {
	return nil
}
