package task

// TaskRepository 定义 Task 聚合根的持久化仓储接口（领域层规范）。
type TaskRepository interface {
	// FindAll 查询并加载所有任务
	FindAll() ([]*Task, error)
	// Save 保存或更新任务状态
	Save(task *Task) error
	// SaveRaw 保存预序列化的 JSON 任务数据（兼容旧调用，默认租户）
	SaveRaw(id string, data []byte) error
	// SaveRawKey 根据 TaskKey 保存预序列化的 JSON 任务数据（按租户隔离落盘）
	SaveRawKey(key TaskKey, data []byte) error
	// SaveRawKeyVersioned 保存预序列化的 JSON 任务数据，并校验版本单调递增
	SaveRawKeyVersioned(key TaskKey, version uint64, data []byte) error
	// Delete 按 ID 删除任务（兼容旧调用，默认租户）
	Delete(id string) error
	// DeleteKey 根据 TaskKey 删除对应租户下的任务文件
	DeleteKey(key TaskKey) error
	// DeleteKeyVersioned 根据 TaskKey 写入删除墓碑/执行删除并记录删除版本
	DeleteKeyVersioned(key TaskKey, version uint64) error
	// Close 关闭或释放底层存储资源
	Close() error
}

// EventLogRepository 定义事件流水账的持久化仓储接口（领域层规范）。
type EventLogRepository interface {
	// AppendEventLog 追加单条事件记录至持久化介质
	AppendEventLog(key TaskKey, rec EventRecord) error
	// ReadEventLogs 读取指定任务的历史事件流水记录
	ReadEventLogs(key TaskKey) ([]EventRecord, error)
}
