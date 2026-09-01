package monitor

import "sync"

// Hub 负责 SSE 客户端连接生命周期管理与事件广播。
type Hub struct {
	mu        sync.RWMutex
	clients   map[chan string]bool
	addClient chan chan string
	rmClient  chan chan string
	broadcast chan string
}

// NewHub 创建一个广播中心。
func NewHub() *Hub {
	return &Hub{
		clients:   make(map[chan string]bool),
		addClient: make(chan chan string),
		rmClient:  make(chan chan string),
		broadcast: make(chan string, 100),
	}
}

// Run 启动广播中心事件循环，在独立 goroutine 中运行。
func (h *Hub) Run() {
	for {
		select {
		case ch := <-h.addClient:
			h.mu.Lock()
			h.clients[ch] = true
			h.mu.Unlock()
		case ch := <-h.rmClient:
			h.mu.Lock()
			delete(h.clients, ch)
			close(ch)
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.mu.RLock()
			for ch := range h.clients {
				select {
				case ch <- msg: // 非阻塞发送，避免慢客户端阻塞广播
				default:
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Subscribe 注册一个新 SSE 客户端通道。
func (h *Hub) Subscribe() chan string {
	ch := make(chan string, 20)
	h.addClient <- ch
	return ch
}

// Unsubscribe 注销 SSE 客户端通道。
func (h *Hub) Unsubscribe(ch chan string) {
	h.rmClient <- ch
}

// Broadcast 向所有连接的客户端广播消息（非阻塞，防止队列积压阻塞调用者）。
func (h *Hub) Broadcast(msg string) {
	select {
	case h.broadcast <- msg:
	default:
	}
}
