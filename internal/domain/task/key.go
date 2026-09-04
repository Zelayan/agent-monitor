package task

import (
	"fmt"
	"strings"
)

// DefaultTenantID 定义未指定租户时的默认命名空间。
const DefaultTenantID = "default"

// NormalizeTenantID 对租户标识进行规范化，去除首尾空白，空字符串归一化为 "default"。
func NormalizeTenantID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultTenantID
	}
	return trimmed
}

// TaskKey 封装租户身份与任务 ID 的复合主键（ADR-1 领域值对象）。
// 确保不同租户即便使用相同的 Session ID，其在内存聚合根与持久化存储中也严格隔离。
type TaskKey struct {
	TenantID string `json:"tenantId"`
	TaskID   string `json:"taskId"`
}

// NewTaskKey 构造并规范化 TaskKey。
func NewTaskKey(tenantID, taskID string) TaskKey {
	return TaskKey{
		TenantID: NormalizeTenantID(tenantID),
		TaskID:   strings.TrimSpace(taskID),
	}
}

// String 返回 TaskKey 的唯一字符串标识，格式为 "tenantId:taskId"。
func (k TaskKey) String() string {
	return fmt.Sprintf("%s:%s", k.TenantID, k.TaskID)
}

// IsZero 判断 TaskKey 是否为空或未初始化任务 ID。
func (k TaskKey) IsZero() bool {
	return k.TaskID == ""
}
