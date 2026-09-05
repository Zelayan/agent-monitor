package monitor

import "sync"

// RingBuffer 是线程安全的固定容量环形缓冲区，保留最近的 N 个 SSEEvent。
type RingBuffer struct {
	mu       sync.RWMutex
	capacity int
	events   []SSEEvent
	head     int   // 下一个写入位置
	count    int   // 当前保存的事件数
	firstID  int64 // 缓冲区中保留的最早事件 ID（0 表示尚无事件）
	lastID   int64 // 缓冲区中保留的最新事件 ID（0 表示尚无事件）
}

// NewRingBuffer 创建一个指定容量的环形缓冲区。默认容量建议 256 或 512。
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 256
	}
	return &RingBuffer{
		capacity: capacity,
		events:   make([]SSEEvent, capacity),
	}
}

// Add 追加一个新事件进入环形缓冲区。
func (rb *RingBuffer) Add(e SSEEvent) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.events[rb.head] = e
	rb.head = (rb.head + 1) % rb.capacity
	if rb.count < rb.capacity {
		rb.count++
	}

	rb.lastID = e.ID
	// 计算当前最早存留的事件 ID
	oldestIdx := (rb.head - rb.count + rb.capacity) % rb.capacity
	rb.firstID = rb.events[oldestIdx].ID
}

// EventsSince 获取所有事件 ID 大于 sinceID 的事件列表。
// 返回 (events, canReplay)。
// 如果 sinceID 等于 lastID，返回空切片和 true（无新事件，无需 resync）。
// 如果 sinceID 小于 firstID - 1，说明客户端错过的事件已被环形缓冲区覆盖，返回 nil, false。
// 如果 sinceID 能够被衔接（sinceID >= firstID - 1 且 sinceID <= lastID），返回错过的切片和 true。
func (rb *RingBuffer) EventsSince(sinceID int64) ([]SSEEvent, bool) {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.count == 0 {
		// 环形缓冲尚无任何事件
		if sinceID <= 0 {
			return nil, true
		}
		// 客户端带有 sinceID 但服务端空缓冲，说明服务端重启或 epoch 变更，必须全量对账
		return nil, false
	}

	// 客户端已是最新
	if sinceID >= rb.lastID {
		return []SSEEvent{}, true
	}

	// 客户端要求从 sinceID 开始，如果 sinceID 比最早能衔接的 ID 还老，无法重放
	// 最早能无缝重放的 sinceID 为 firstID - 1
	if sinceID < rb.firstID-1 {
		return nil, false
	}

	// 顺序收集大于 sinceID 的所有事件
	result := make([]SSEEvent, 0, rb.count)
	startIdx := (rb.head - rb.count + rb.capacity) % rb.capacity
	for i := 0; i < rb.count; i++ {
		idx := (startIdx + i) % rb.capacity
		ev := rb.events[idx]
		if ev.ID > sinceID {
			result = append(result, ev)
		}
	}

	return result, true
}

// Range 返回缓冲区内最早与最新的事件 ID。
func (rb *RingBuffer) Range() (firstID int64, lastID int64, count int) {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.firstID, rb.lastID, rb.count
}

// Clear 清空缓冲区。
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.head = 0
	rb.count = 0
	rb.firstID = 0
	rb.lastID = 0
	for i := range rb.events {
		rb.events[i] = SSEEvent{}
	}
}
