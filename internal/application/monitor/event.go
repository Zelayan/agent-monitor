package monitor

import "fmt"

// SSEEvent 表示符合 SSE v2 协议规范的单条广播事件。
type SSEEvent struct {
	ID    int64  // 单调递增事件序列 ID（> 0）
	Type  string // 事件类型：task_upsert, delete_tasks, snapshot_start, snapshot_end, resync_required
	KeyID string // 归属的项目/租户空间（为空表示全局事件）
	Data  string // JSON 载荷
}

// FormatSSE 将事件格式化为标准 Server-Sent Events 数据块：
// id: <seq>\n
// event: <type>\n
// data: <json>\n\n
func (e SSEEvent) FormatSSE() string {
	if e.ID > 0 {
		return fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", e.ID, e.Type, e.Data)
	}
	if e.Type != "" {
		return fmt.Sprintf("event: %s\ndata: %s\n\n", e.Type, e.Data)
	}
	return fmt.Sprintf("data: %s\n\n", e.Data)
}
