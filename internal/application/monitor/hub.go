package monitor

import (
	"sync"
	"sync/atomic"
)

// ClientSubscription 记录客户端订阅的租户空间与权限模式。
type ClientSubscription struct {
	Ch       chan string
	KeyID    string // 订阅的项目/租户空间
	IsMaster bool   // 是否拥有全局 Master 视图
}

// HubMessage 封装广播消息与其所属的项目空间与元数据。
type HubMessage struct {
	ID    int64  // 单调递增事件序列 ID
	Type  string // 事件类型 (如 task_upsert, delete_tasks)
	KeyID string // 所属租户空间
	Data  string // JSON 载荷
}

// Hub 负责 SSE 客户端连接生命周期管理、单调自增 ID 分配、环形缓冲区缓存与事件广播（支持多项目多租户隔离）。
type Hub struct {
	mu            sync.RWMutex
	clients       map[chan string]ClientSubscription
	addClient     chan ClientSubscription
	rmClient      chan chan string
	broadcast     chan HubMessage
	seqCounter    int64
	ringBuffer    *RingBuffer
	ringCap       int
	droppedEvents uint64
}

// NewHub 创建一个广播中心，默认环形缓冲容量为 512。
func NewHub() *Hub {
	return NewHubWithCapacity(512)
}

// NewHubWithCapacity 创建指定环形缓冲区容量的广播中心。
func NewHubWithCapacity(capacity int) *Hub {
	if capacity <= 0 {
		capacity = 512
	}
	return &Hub{
		clients:    make(map[chan string]ClientSubscription),
		addClient:  make(chan ClientSubscription),
		rmClient:   make(chan chan string),
		broadcast:  make(chan HubMessage, 200),
		ringBuffer: NewRingBuffer(capacity),
		ringCap:    capacity,
	}
}

// NextSeqID 分配下一个全局单调递增事件 ID。
func (h *Hub) NextSeqID() int64 {
	return atomic.AddInt64(&h.seqCounter, 1)
}

// CurrentSeqID 返回当前最新的事件 ID。
func (h *Hub) CurrentSeqID() int64 {
	return atomic.LoadInt64(&h.seqCounter)
}

// DroppedEvents 返回因客户端通道满而被非阻塞丢弃的消息总数。
func (h *Hub) DroppedEvents() uint64 {
	return atomic.LoadUint64(&h.droppedEvents)
}

// RingBuffer 获取底层的环形缓冲区。
func (h *Hub) RingBuffer() *RingBuffer {
	return h.ringBuffer
}

// Run 启动广播中心事件循环，在独立 goroutine 中运行。
func (h *Hub) Run() {
	for {
		select {
		case sub := <-h.addClient:
			h.mu.Lock()
			h.clients[sub.Ch] = sub
			h.mu.Unlock()
		case ch := <-h.rmClient:
			h.mu.Lock()
			delete(h.clients, ch)
			close(ch)
			h.mu.Unlock()
		case msg := <-h.broadcast:
			event := SSEEvent{
				ID:    msg.ID,
				Type:  msg.Type,
				KeyID: msg.KeyID,
				Data:  msg.Data,
			}

			// 格式化为 SSE 协议数据帧
			sseChunk := event.FormatSSE()

			h.mu.RLock()
			for ch, sub := range h.clients {
				// 租户空间隔离：
				// 1. Master 客户端接收全量项目流
				// 2. 消息未指定空间（全局系统消息）推给所有客户端
				// 3. 普通租户客户端仅接收与其 KeyID 匹配的消息
				if sub.IsMaster || sub.KeyID == "*" || msg.KeyID == "" || sub.KeyID == msg.KeyID {
					select {
					case ch <- sseChunk: // 非阻塞发送，避免慢客户端阻塞广播
					default:
						atomic.AddUint64(&h.droppedEvents, 1)
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Subscribe 注册一个默认客户端通道（具有全局视角，兼容旧调用）。
func (h *Hub) Subscribe() chan string {
	return h.SubscribeTenant("", true)
}

// SubscribeTenant 注册一个绑定特定 KeyID 空间的 SSE 客户端通道。
func (h *Hub) SubscribeTenant(keyID string, isMaster bool) chan string {
	ch := make(chan string, 50)
	h.addClient <- ClientSubscription{
		Ch:       ch,
		KeyID:    keyID,
		IsMaster: isMaster,
	}
	return ch
}

// ClientCount 返回当前连接中的活跃 SSE 客户端数。
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Unsubscribe 注销 SSE 客户端通道。
func (h *Hub) Unsubscribe(ch chan string) {
	h.rmClient <- ch
}

// Broadcast 向所有客户端广播全局消息（默认使用 task_upsert 类型并自动生成单调 ID）。
func (h *Hub) Broadcast(msg string) {
	h.BroadcastTenant("", msg)
}

// BroadcastTenant 向指定租户/Key空间客户端广播消息（使用 task_upsert 类型并自动分配单调 ID）。
func (h *Hub) BroadcastTenant(keyID string, msg string) {
	h.BroadcastEvent("task_upsert", keyID, msg)
}

// BroadcastEvent 向指定租户广播特定类型事件，自动分配全局单调递增 Sequence ID 并同步入缓冲。
func (h *Hub) BroadcastEvent(eventType string, keyID string, msg string) {
	if eventType == "" {
		eventType = "task_upsert"
	}
	seqID := h.NextSeqID()
	event := SSEEvent{
		ID:    seqID,
		Type:  eventType,
		KeyID: keyID,
		Data:  msg,
	}
	// 同步写入环形缓冲区，确保递增 Sequence ID 与环形缓冲区存储严格同步，消除时序洞
	h.ringBuffer.Add(event)

	select {
	case h.broadcast <- HubMessage{
		ID:    seqID,
		Type:  eventType,
		KeyID: keyID,
		Data:  msg,
	}:
	default:
		atomic.AddUint64(&h.droppedEvents, 1)
	}
}

// ReplayMissedEvents 为重连客户端拉取错过的事件流。
// 如果 canReplay 为 true，返回当前租户匹配的待重放事件切片。
// 如果 canReplay 为 false，表示错过事件已超出环形缓冲范围，客户端必须做 resync。
func (h *Hub) ReplayMissedEvents(sinceID int64, keyID string, isMaster bool) ([]SSEEvent, bool) {
	allMissed, ok := h.ringBuffer.EventsSince(sinceID)
	if !ok {
		return nil, false
	}

	// 针对当前租户过滤事件
	var filtered []SSEEvent
	for _, ev := range allMissed {
		if isMaster || keyID == "*" || ev.KeyID == "" || ev.KeyID == keyID {
			filtered = append(filtered, ev)
		}
	}
	return filtered, true
}
