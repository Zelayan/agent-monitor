package task

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// EventRecord 记录单条接收到的事件详情及产生时的状态快照（值对象）。
type EventRecord struct {
	EventID        string                 `json:"eventId"`                  // 幂等全局唯一哈希键
	Sequence       uint64                 `json:"sequence"`                 // 会话内单调递增序号 1, 2, 3...
	Timestamp      int64                  `json:"timestamp"`                // 事件产生时间戳 (Unix毫秒)
	ReceivedAt     int64                  `json:"receivedAt"`               // 服务端接收时间戳 (Unix毫秒)
	Event          string                 `json:"event"`                    // hook 事件名称
	TurnIndex      int                    `json:"turnIndex"`                // 对应所属轮次
	Detail         string                 `json:"detail"`                   // 操作详情
	Prompt         string                 `json:"prompt,omitempty"`         // 本轮 Prompt
	AIResponse     string                 `json:"aiResponse,omitempty"`     // AI 回复
	TaskStatus     string                 `json:"taskStatus"`               // 事件应用后的任务全局状态
	TaskVersion    uint64                 `json:"taskVersion"`              // 事件应用后的任务单调版本
	PayloadSummary map[string]interface{} `json:"payloadSummary,omitempty"` // 关键元数据
}

// Clone 返回 EventRecord 的只读深拷贝副本，隔离 PayloadSummary map 引用以确保并发安全。
func (rec EventRecord) Clone() EventRecord {
	cloned := rec
	if rec.PayloadSummary != nil {
		cloned.PayloadSummary = make(map[string]interface{}, len(rec.PayloadSummary))
		for k, v := range rec.PayloadSummary {
			cloned.PayloadSummary[k] = v
		}
	}
	return cloned
}

// ComputeEventFingerprint 计算事件的确定性幂等指纹哈希。
// 格式: key_id:session_id:event:timestamp:detail:turn_index:prompt
func ComputeEventFingerprint(p EventPayload) string {
	raw := fmt.Sprintf("%s:%s:%s:%d:%s:%d:%s:%s",
		strings.TrimSpace(p.KeyID),
		strings.TrimSpace(p.ID),
		strings.TrimSpace(p.Event),
		p.Timestamp,
		strings.TrimSpace(p.Detail),
		p.TurnIndex,
		strings.TrimSpace(p.Prompt),
		strings.TrimSpace(p.SubagentID),
	)
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:16]) // 32字符16进制
}

// EventLogRingBuffer 是固定容量线程安全的单会话 Event Log 内存环形缓冲区。
type EventLogRingBuffer struct {
	mu            sync.RWMutex
	capacity      int
	records       []EventRecord
	seenKeys      map[string]time.Time // key -> 首次记录时间 (带内存 TTL 防抖)
	seq           uint64
	lastCleanTime time.Time
}

// NewEventLogRingBuffer 初始化指定容量的环形事件日志缓冲区。
func NewEventLogRingBuffer(capacity int) *EventLogRingBuffer {
	if capacity <= 0 {
		capacity = 256
	}
	return &EventLogRingBuffer{
		capacity: capacity,
		records:  make([]EventRecord, 0, capacity),
		seenKeys: make(map[string]time.Time),
	}
}

// IsDuplicate 检查事件指纹是否已在最近窗口内被处理过（幂等防御）。
func (rb *EventLogRingBuffer) IsDuplicate(eventID string) bool {
	if eventID == "" {
		return false
	}
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	_, exists := rb.seenKeys[eventID]
	return exists
}

// AppendIfNew 幂等追加事件记录。如果已存在相同 eventID 则返回 false。
func (rb *EventLogRingBuffer) AppendIfNew(rec EventRecord) (bool, uint64) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// 清理超过 10 分钟的过时指纹缓存，防止内存无界增长（频率限制至少间隔 1 分钟）
	now := time.Now()
	if len(rb.seenKeys) > rb.capacity*2 && now.Sub(rb.lastCleanTime) > time.Minute {
		for k, t := range rb.seenKeys {
			if now.Sub(t) > 10*time.Minute {
				delete(rb.seenKeys, k)
			}
		}
		rb.lastCleanTime = now
	}

	if rec.EventID != "" {
		if _, exists := rb.seenKeys[rec.EventID]; exists {
			return false, 0
		}
		rb.seenKeys[rec.EventID] = now
	}

	rb.seq++
	rec.Sequence = rb.seq
	if rec.ReceivedAt == 0 {
		rec.ReceivedAt = now.UnixMilli()
	}

	if len(rb.records) >= rb.capacity {
		// 环形丢弃最旧
		rb.records = append(rb.records[1:], rec)
	} else {
		rb.records = append(rb.records, rec)
	}

	return true, rec.Sequence
}

// Snapshot 获取全部事件历史深拷贝。
func (rb *EventLogRingBuffer) Snapshot() []EventRecord {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	out := make([]EventRecord, len(rb.records))
	for i, r := range rb.records {
		out[i] = r.Clone()
	}
	return out
}

// Count 返回已记录的事件总数。
func (rb *EventLogRingBuffer) Count() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return len(rb.records)
}

// UpdateLastPostApplication 更新最近追加的一条事件记录在应用到聚合根后的任务状态与单调版本。
func (rb *EventLogRingBuffer) UpdateLastPostApplication(status string, version uint64) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if len(rb.records) > 0 {
		idx := len(rb.records) - 1
		rb.records[idx].TaskStatus = status
		rb.records[idx].TaskVersion = version
	}
}
