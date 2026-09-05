package persistence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Zelayan/agent-monitor/internal/domain/task"
)

// StripedLock 提供基于主键哈希的分片并发锁，避免不同任务写互相阻塞。
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

type tombstoneItem struct {
	version   uint64
	expiresAt time.Time
}

// JSONRepository 是基于本地 JSON 文件的 TaskRepository 实现，支持租户分目录隔离存储与版本单调递增保护。
type JSONRepository struct {
	dir            string
	mu             sync.Mutex // 全局目录扫描与迁移互斥锁
	stripe         StripedLock
	versionLock    sync.RWMutex
	lastVersion    map[string]uint64        // map[TaskKey.String()]latestVersion 已提交版本
	tombstones     map[string]tombstoneItem // map[TaskKey.String()]tombstoneItem 短生命周期墓碑 (30s TTL)
	quarantineDir  string                   // 损坏 JSON 文件的安全隔离区
	quarantineStat QuarantineStats
}

// QuarantineStats 记录隔离损坏文件的统计信息。
type QuarantineStats struct {
	Count     int       `json:"count"`
	LastError string    `json:"lastError,omitempty"`
	LastTime  time.Time `json:"lastTime,omitempty"`
}

// NewJSONRepository 初始化并返回 JSONRepository 实例。
func NewJSONRepository(dir string) (*JSONRepository, error) {
	if dir == "" {
		dir = "data/sessions"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create repository dir %s: %w", dir, err)
	}
	quarantine := filepath.Join(dir, "quarantine")
	_ = os.MkdirAll(quarantine, 0755)

	repo := &JSONRepository{
		dir:           dir,
		quarantineDir: quarantine,
		lastVersion:   make(map[string]uint64),
		tombstones:    make(map[string]tombstoneItem),
	}
	repo.CleanOrphanTmpFiles()
	return repo, nil
}

// QuarantineStats 返回隔离统计。
func (r *JSONRepository) QuarantineStats() QuarantineStats {
	r.versionLock.RLock()
	defer r.versionLock.RUnlock()
	return r.quarantineStat
}

// quarantineCorruptedFile 将损坏 JSON 移动到隔离目录，防止阻断服务启动。
func (r *JSONRepository) quarantineCorruptedFile(path string, parseErr error) {
	base := filepath.Base(path)
	target := filepath.Join(r.quarantineDir, fmt.Sprintf("%d-%s", time.Now().UnixMilli(), base))
	_ = os.Rename(path, target)

	r.versionLock.Lock()
	r.quarantineStat.Count++
	r.quarantineStat.LastError = parseErr.Error()
	r.quarantineStat.LastTime = time.Now()
	r.versionLock.Unlock()

	log.Printf("[Persistence] Warning: corrupted session file %s quarantined to %s: %v", path, target, parseErr)
}

// hashPrefix 计算输入字符串的 SHA256 哈希值前缀。
func hashPrefix(input string, length int) string {
	h := sha256.Sum256([]byte(input))
	hexStr := hex.EncodeToString(h[:])
	if length > len(hexStr) {
		return hexStr
	}
	return hexStr[:length]
}

// SafeDirName 清洗租户目录名称，支持国际化 Unicode 字符，防止路径穿越和非法控制字符。
func SafeDirName(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
	trimmed := strings.Trim(cleaned, "_ ")
	if trimmed == "" {
		trimmed = "tenant"
	}
	return trimmed
}

// SafeFilenamePrefix 清洗文件名主体，支持国际化 Unicode 字符，防止路径穿越和非法控制字符。
func SafeFilenamePrefix(id string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, id)
	trimmed := strings.Trim(cleaned, "_ ")
	if trimmed == "" {
		trimmed = "session"
	}
	return trimmed
}

// SafeFilename 保留旧格式文件名生成函数以兼容测试与旧路径解析。
func SafeFilename(id string) string {
	return SafeFilenamePrefix(id) + ".json"
}

// tenantDir 返回指定租户的存储子目录路径。
func (r *JSONRepository) tenantDir(tenantID string) string {
	tid := task.NormalizeTenantID(tenantID)
	if tid == task.DefaultTenantID {
		return filepath.Join(r.dir, "default")
	}
	safe := SafeDirName(tid)
	h := hashPrefix(tid, 8)
	return filepath.Join(r.dir, fmt.Sprintf("%s-%s", safe, h))
}

// taskKeyPath 返回根据 TaskKey 生成的绝对规范落盘路径：<dataDir>/<tenantSubdir>/<safeID>-<hash>.json。
func (r *JSONRepository) taskKeyPath(key task.TaskKey) string {
	tDir := r.tenantDir(key.TenantID)
	safeID := SafeFilenamePrefix(key.TaskID)
	h := hashPrefix(key.TaskID, 8)
	return filepath.Join(tDir, fmt.Sprintf("%s-%s.json", safeID, h))
}

// legacyTaskPath 返回旧版本扁平存放在根目录下的路径。
func (r *JSONRepository) legacyTaskPath(id string) string {
	return filepath.Join(r.dir, SafeFilename(id))
}

// CleanOrphanTmpFiles 扫描目录及子目录，清除由于历史非正常关闭遗留的 *.tmp 临时孤儿文件，并清理过期墓碑。
func (r *JSONRepository) CleanOrphanTmpFiles() {
	now := time.Now()
	_ = filepath.Walk(r.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".tmp") {
			if now.Sub(info.ModTime()) > 30*time.Minute {
				_ = os.Remove(path)
			}
		}
		return nil
	})

	r.cleanExpiredTombstones(now)
}

// cleanExpiredTombstones 清理内存中已过期的墓碑记录，防止长时间运行内存线性泄漏。
func (r *JSONRepository) cleanExpiredTombstones(now time.Time) {
	r.versionLock.Lock()
	defer r.versionLock.Unlock()
	for k, tomb := range r.tombstones {
		if now.After(tomb.expiresAt) {
			delete(r.tombstones, k)
		}
	}
}

// migrateLegacyFiles 扫描根目录旧文件并平滑迁移到租户子目录。
func (r *JSONRepository) migrateLegacyFiles() {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		oldPath := filepath.Join(r.dir, entry.Name())
		data, err := os.ReadFile(oldPath)
		if err != nil {
			continue
		}

		var t task.Task
		if err := json.Unmarshal(data, &t); err != nil || t.ID == "" {
			continue
		}

		newPath := r.taskKeyPath(t.TaskKey())
		if oldPath == newPath {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
			log.Printf("[Persistence] Warning: failed to create tenant dir for migration: %v", err)
			continue
		}

		if err := os.Rename(oldPath, newPath); err != nil {
			// 跨文件系统时复制并删除旧文件
			if writeErr := os.WriteFile(newPath, data, 0644); writeErr == nil {
				_ = os.Remove(oldPath)
			}
		}
	}
}

// FindAll 遍历并反序列化所有持久化的 Task 记录，并在首次启动时执行历史数据平滑迁移。
func (r *JSONRepository) FindAll() ([]*task.Task, error) {
	r.mu.Lock()
	r.migrateLegacyFiles()
	r.mu.Unlock()

	var tasks []*task.Task
	loadedVersions := make(map[string]uint64)
	err := filepath.Walk(r.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// 跳过 quarantine 隔离目录
		if info.IsDir() && info.Name() == "quarantine" {
			return filepath.SkipDir
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") || strings.HasSuffix(info.Name(), ".tmp") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		var t task.Task
		if err := json.Unmarshal(data, &t); err != nil {
			// 自动隔离损坏文件，避免阻塞启动与正常数据恢复
			r.quarantineCorruptedFile(path, err)
			return nil
		}
		if t.ID != "" {
			tasks = append(tasks, &t)
			if t.Version > 0 {
				kStr := t.TaskKey().String()
				if cur, ok := loadedVersions[kStr]; !ok || t.Version > cur {
					loadedVersions[kStr] = t.Version
				}
			}
		}
		return nil
	})

	if len(loadedVersions) > 0 {
		r.versionLock.Lock()
		for k, v := range loadedVersions {
			if cur, ok := r.lastVersion[k]; !ok || v > cur {
				r.lastVersion[k] = v
			}
		}
		r.versionLock.Unlock()
	}

	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read repository dir failed: %w", err)
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

	return r.SaveRawKey(t.TaskKey(), data)
}

// SaveRaw 将预序列化的 JSON 数据写入默认租户（兼容旧接口）。
func (r *JSONRepository) SaveRaw(id string, data []byte) error {
	key := task.NewTaskKey(task.DefaultTenantID, id)
	return r.SaveRawKey(key, data)
}

// SaveRawKey 根据 TaskKey 将预序列化的不可变 JSON 字节切片原子写入租户子目录。
func (r *JSONRepository) SaveRawKey(key task.TaskKey, data []byte) error {
	return r.SaveRawKeyVersioned(key, 0, data)
}

// SaveRawKeyVersioned 保存预序列化的 JSON 任务数据，并严格检查单调递增版本号与墓碑压制。
func (r *JSONRepository) SaveRawKeyVersioned(key task.TaskKey, version uint64, data []byte) error {
	if key.IsZero() || len(data) == 0 {
		return fmt.Errorf("invalid task key or data to save")
	}

	keyStr := key.String()
	now := time.Now()

	// 1. 版本与墓碑检查：如果版本号大于 0，拒绝低于等于未过期墓碑版本或已落盘版本的旧请求
	if version > 0 {
		r.versionLock.RLock()
		if tomb, hasTomb := r.tombstones[keyStr]; hasTomb && now.Before(tomb.expiresAt) && version <= tomb.version {
			r.versionLock.RUnlock()
			return nil // 旧版本已被活跃墓碑压制，幂等忽略，杜绝删除后短时间内被滞后写复活
		}
		if lastVer, hasVer := r.lastVersion[keyStr]; hasVer && version <= lastVer {
			r.versionLock.RUnlock()
			return nil // 已存在更高或相同版本，幂等忽略，杜绝乱序覆写
		}
		r.versionLock.RUnlock()
	}

	lock := r.stripe.getLock(keyStr)
	lock.Lock()
	defer lock.Unlock()

	// 双重校验版本：重新获取当前最新时间，保证极端并发排队后墓碑过期判定新鲜准确
	if version > 0 {
		now = time.Now()
		r.versionLock.RLock()
		if tomb, hasTomb := r.tombstones[keyStr]; hasTomb && now.Before(tomb.expiresAt) && version <= tomb.version {
			r.versionLock.RUnlock()
			return nil
		}
		if lastVer, hasVer := r.lastVersion[keyStr]; hasVer && version <= lastVer {
			r.versionLock.RUnlock()
			return nil
		}
		r.versionLock.RUnlock()
	}

	targetPath := r.taskKeyPath(key)
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create tenant directory %s: %w", targetDir, err)
	}

	tmpFile, err := os.CreateTemp(targetDir, SafeFilenamePrefix(key.TaskID)+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create tmp file for task %s: %w", keyStr, err)
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
		return fmt.Errorf("failed to write tmp file for task %s: %w", keyStr, err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync tmp file for task %s: %w", keyStr, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close tmp file for task %s: %w", keyStr, err)
	}

	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("failed to commit task file %s: %w", targetPath, err)
	}

	success = true

	// 更新落盘记录的版本号并清理墓碑
	if version > 0 {
		r.versionLock.Lock()
		if version > r.lastVersion[keyStr] {
			r.lastVersion[keyStr] = version
		}
		delete(r.tombstones, keyStr)
		r.versionLock.Unlock()
	}

	return nil
}

// Delete 按 ID 删除默认租户或旧格式文件（兼容旧接口）。
func (r *JSONRepository) Delete(id string) error {
	key := task.NewTaskKey(task.DefaultTenantID, id)
	return r.DeleteKey(key)
}

// DeleteKey 删除指定 TaskKey 对应的任务文件及可能遗留的旧文件。
func (r *JSONRepository) DeleteKey(key task.TaskKey) error {
	return r.DeleteKeyVersioned(key, 0)
}

// DeleteKeyVersioned 根据 TaskKey 写入删除墓碑（TTL 30 秒）并删除对应文件，清除旧版本记录，防滞后旧写复活。
func (r *JSONRepository) DeleteKeyVersioned(key task.TaskKey, version uint64) error {
	if key.IsZero() {
		return nil
	}

	keyStr := key.String()

	r.versionLock.Lock()
	if version > 0 {
		r.tombstones[keyStr] = tombstoneItem{
			version:   version,
			expiresAt: time.Now().Add(30 * time.Second),
		}
	}
	delete(r.lastVersion, keyStr)
	r.versionLock.Unlock()

	lock := r.stripe.getLock(keyStr)
	lock.Lock()
	defer lock.Unlock()

	targetPath := r.taskKeyPath(key)
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete task file %s: %w", targetPath, err)
	}

	// 清理可能存在的历史旧格式文件
	legacyPath := r.legacyTaskPath(key.TaskID)
	_ = os.Remove(legacyPath)

	return nil
}

// eventLogPath 返回根据 TaskKey 生成的事件流水账规范落盘路径：<dataDir>/<tenantSubdir>/<safeID>-<hash>.events.jsonl。
func (r *JSONRepository) eventLogPath(key task.TaskKey) string {
	tDir := r.tenantDir(key.TenantID)
	safeID := SafeFilenamePrefix(key.TaskID)
	h := hashPrefix(key.TaskID, 8)
	return filepath.Join(tDir, fmt.Sprintf("%s-%s.events.jsonl", safeID, h))
}

// AppendEventLog 追加一条事件记录至指定 TaskKey 的事件流水账文件 (events.jsonl)。
func (r *JSONRepository) AppendEventLog(key task.TaskKey, rec task.EventRecord) error {
	if key.IsZero() {
		return fmt.Errorf("invalid task key")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	eventLogFile := r.eventLogPath(key)
	dir := filepath.Dir(eventLogFile)
	_ = os.MkdirAll(dir, 0755)

	f, err := os.OpenFile(eventLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open event log file %s: %w", eventLogFile, err)
	}
	defer f.Close()

	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("failed to marshal event record: %w", err)
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write event record: %w", err)
	}
	return nil
}

// ReadEventLogs 读取指定 TaskKey 的所有事件流水历史。
func (r *JSONRepository) ReadEventLogs(key task.TaskKey) ([]task.EventRecord, error) {
	if key.IsZero() {
		return nil, fmt.Errorf("invalid task key")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	eventLogFile := r.eventLogPath(key)
	data, err := os.ReadFile(eventLogFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read event log file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var records []task.EventRecord
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec task.EventRecord
		if err := json.Unmarshal([]byte(line), &rec); err == nil {
			records = append(records, rec)
		}
	}
	return records, nil
}

// Close 释放存储资源。
func (r *JSONRepository) Close() error {
	return nil
}
