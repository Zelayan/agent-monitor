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
	// Delete 按 ID 删除任务（兼容旧调用，默认租户）
	Delete(id string) error
	// DeleteKey 根据 TaskKey 删除对应租户下的任务文件
	DeleteKey(key TaskKey) error
	// Close 关闭或释放底层存储资源
	Close() error
}
