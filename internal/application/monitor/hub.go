package monitor

import "sync"

// ClientSubscription 记录客户端订阅的租户空间与权限模式。
type ClientSubscription struct {
	Ch       chan string
	KeyID    string // 订阅的项目/租户空间
	IsMaster bool   // 是否拥有全局 Master 视图
}

// HubMessage 封装广播消息与其所属的项目空间。
type HubMessage struct {
	KeyID string
	Data  string
}

// Hub 负责 SSE 客户端连接生命周期管理与事件广播（支持多项目多租户隔离）。
type Hub struct {
	mu        sync.RWMutex
	clients   map[chan string]ClientSubscription
	addClient chan ClientSubscription
	rmClient  chan chan string
	broadcast chan HubMessage
}

// NewHub 创建一个广播中心。
func NewHub() *Hub {
	return &Hub{
		clients:   make(map[chan string]ClientSubscription),
		addClient: make(chan ClientSubscription),
		rmClient:  make(chan chan string),
		broadcast: make(chan HubMessage, 100),
	}
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
			h.mu.RLock()
			for ch, sub := range h.clients {
				// 租户空间隔离：
				// 1. Master 客户端接收全量项目流
				// 2. 消息未指定空间（全局系统消息）推给所有客户端
				// 3. 普通租户客户端仅接收与其 KeyID 匹配的消息
				if sub.IsMaster || sub.KeyID == "*" || msg.KeyID == "" || sub.KeyID == msg.KeyID {
					select {
					case ch <- msg.Data: // 非阻塞发送，避免慢客户端阻塞广播
					default:
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
	ch := make(chan string, 20)
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

// Broadcast 向所有客户端广播全局消息。
func (h *Hub) Broadcast(msg string) {
	h.BroadcastTenant("", msg)
}

// BroadcastTenant 向指定租户/Key空间客户端广播消息（同时抄送 Master 客户端）。
func (h *Hub) BroadcastTenant(keyID string, msg string) {
	select {
	case h.broadcast <- HubMessage{KeyID: keyID, Data: msg}:
	default:
	}
}
