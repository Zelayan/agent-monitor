package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Zelayan/agent-monitor/internal/domain/task"
)

// StripedLock 提供基于 Task ID 哈希的分片并发锁，避免不同会话文件写互相阻塞。
type StripedLock struct {
	locks [32]sync.Mutex
}

func (sl *StripedLock) getLock(key string) *sync.Mutex {
	var hash uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= 16777619
	}
	return &sl.locks[hash%32]
}

// JSONRepository 是基于本地 JSON 文件的 TaskRepository 实现。
type JSONRepository struct {
	dir    string
	mu     sync.Mutex // 全局目录级操作互斥
	stripe StripedLock
}

// NewJSONRepository 初始化并返回 JSONRepository 实例。
func NewJSONRepository(dir string) (*JSONRepository, error) {
	if dir == "" {
		dir = "data/sessions"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create repository dir %s: %w", dir, err)
	}
	repo := &JSONRepository{
		dir: dir,
	}
	repo.CleanOrphanTmpFiles()
	return repo, nil
}

// CleanOrphanTmpFiles 扫描目录并清除由于历史非正常关闭遗留的 *.tmp 临时孤儿文件。
func (r *JSONRepository) CleanOrphanTmpFiles() {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, entry := range entries {
		if !entry.IsDir() && strings.Contains(entry.Name(), ".tmp") {
			info, err := entry.Info()
			if err == nil && now.Sub(info.ModTime()) > 30*time.Minute {
				_ = os.Remove(filepath.Join(r.dir, entry.Name()))
			}
		}
	}
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
	entries, err := os.ReadDir(r.dir)
	r.mu.Unlock()

	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read repository dir failed: %w", err)
	}

	var tasks []*task.Task
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || strings.Contains(entry.Name(), ".tmp") {
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

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal task %s: %w", t.ID, err)
	}

	return r.SaveRaw(t.ID, data)
}

// SaveRaw 将预序列化的不可变 JSON 字节切片原子写入文件（使用内核临时文件 + Sync + Rename 保证绝对原子性）。
func (r *JSONRepository) SaveRaw(id string, data []byte) error {
	if id == "" || len(data) == 0 {
		return fmt.Errorf("invalid id or data to save")
	}

	// 使用分片锁隔离不同会话文件的写并发
	lock := r.stripe.getLock(id)
	lock.Lock()
	defer lock.Unlock()

	targetPath := r.taskPath(id)
	tmpFile, err := os.CreateTemp(r.dir, SafeFilename(id)+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create tmp file for task %s: %w", id, err)
	}
	tmpName := tmpFile.Name()

	var success bool
	defer func() {
		if !success {
			_ = tmpFile.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write tmp file for task %s: %w", id, err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync tmp file for task %s: %w", id, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close tmp file for task %s: %w", id, err)
	}

	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("failed to commit task file %s: %w", targetPath, err)
	}

	success = true
	return nil
}

// Delete 删除指定 ID 的任务文件。
func (r *JSONRepository) Delete(id string) error {
	if id == "" {
		return nil
	}

	lock := r.stripe.getLock(id)
	lock.Lock()
	defer lock.Unlock()

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
