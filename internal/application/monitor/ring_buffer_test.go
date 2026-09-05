package monitor

import (
	"fmt"
	"sync"
	"testing"
)

func TestRingBuffer_Basic(t *testing.T) {
	rb := NewRingBuffer(5)

	// 空 buffer 查 sinceID = 0
	events, ok := rb.EventsSince(0)
	if !ok || len(events) != 0 {
		t.Fatalf("expected empty events and true for since=0 on empty buffer, got ok=%v len=%d", ok, len(events))
	}

	// 空 buffer 查 sinceID = 10
	events, ok = rb.EventsSince(10)
	if ok || events != nil {
		t.Fatalf("expected ok=false for since=10 on empty buffer, got ok=%v", ok)
	}

	// 添加 3 个事件
	for i := int64(1); i <= 3; i++ {
		rb.Add(SSEEvent{
			ID:   i,
			Type: "task_upsert",
			Data: fmt.Sprintf(`{"id":"task-%d"}`, i),
		})
	}

	first, last, count := rb.Range()
	if first != 1 || last != 3 || count != 3 {
		t.Fatalf("expected range [1, 3] count 3, got [%d, %d] count %d", first, last, count)
	}

	// 查 sinceID = 0 (获取全量 1, 2, 3)
	events, ok = rb.EventsSince(0)
	if !ok || len(events) != 3 {
		t.Fatalf("expected 3 events for since=0, got ok=%v len=%d", ok, len(events))
	}
	if events[0].ID != 1 || events[1].ID != 2 || events[2].ID != 3 {
		t.Fatalf("unexpected event IDs: %+v", events)
	}

	// 查 sinceID = 1 (获取 2, 3)
	events, ok = rb.EventsSince(1)
	if !ok || len(events) != 2 {
		t.Fatalf("expected 2 events for since=1, got ok=%v len=%d", ok, len(events))
	}
	if events[0].ID != 2 || events[1].ID != 3 {
		t.Fatalf("unexpected event IDs: %+v", events)
	}

	// 查 sinceID = 3 (无新事件)
	events, ok = rb.EventsSince(3)
	if !ok || len(events) != 0 {
		t.Fatalf("expected 0 events for since=3, got ok=%v len=%d", ok, len(events))
	}
}

func TestRingBuffer_OverflowAndResync(t *testing.T) {
	capacity := 4
	rb := NewRingBuffer(capacity)

	// 写入 10 个事件 (1..10)
	for i := int64(1); i <= 10; i++ {
		rb.Add(SSEEvent{
			ID:   i,
			Type: "task_upsert",
			Data: fmt.Sprintf(`{"id":"task-%d"}`, i),
		})
	}

	first, last, count := rb.Range()
	if count != 4 {
		t.Fatalf("expected count 4, got %d", count)
	}
	if first != 7 || last != 10 {
		t.Fatalf("expected range [7, 10], got [%d, %d]", first, last)
	}

	// sinceID = 6 是可以衔接的最早 ID (firstID - 1)
	events, ok := rb.EventsSince(6)
	if !ok || len(events) != 4 {
		t.Fatalf("expected 4 events for since=6, got ok=%v len=%d", ok, len(events))
	}
	if events[0].ID != 7 || events[3].ID != 10 {
		t.Fatalf("unexpected event sequence: %+v", events)
	}

	// sinceID = 5 已经过时被覆盖，无法重放，必须触发 resync
	events, ok = rb.EventsSince(5)
	if ok || events != nil {
		t.Fatalf("expected ok=false for expired sinceID=5, got ok=%v", ok)
	}

	// sinceID = 10 已经是最新
	events, ok = rb.EventsSince(10)
	if !ok || len(events) != 0 {
		t.Fatalf("expected ok=true and empty slice for sinceID=10, got ok=%v len=%d", ok, len(events))
	}
}

func TestRingBuffer_Concurrent(t *testing.T) {
	rb := NewRingBuffer(128)
	var wg sync.WaitGroup

	// 并发写
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := int64(1); i <= 100; i++ {
				id := int64(workerID*1000) + i
				rb.Add(SSEEvent{
					ID:   id,
					Type: "task_upsert",
					Data: `{"status":"ok"}`,
				})
			}
		}(w)
	}

	// 并发读
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, _ = rb.EventsSince(50)
				_, _, _ = rb.Range()
			}
		}()
	}

	wg.Wait()
}
