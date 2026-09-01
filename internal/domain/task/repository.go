package task

// TaskRepository 定义 Task 聚合根的持久化仓储接口（领域层规范）。
type TaskRepository interface {
	// FindAll 查询并加载所有任务
	FindAll() ([]*Task, error)
	// Save 保存或更新任务状态
	Save(task *Task) error
	// SaveRaw 保存预序列化的 JSON 任务数据
	SaveRaw(id string, data []byte) error
	// Delete 按 ID 删除任务
	Delete(id string) error
	// Close 关闭或释放底层存储资源
	Close() error
}
