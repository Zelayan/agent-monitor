package task

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestComputeEventFingerprint_Deterministic 测试确定性事件指纹哈希生成
func TestComputeEventFingerprint_Deterministic(t *testing.T) {
	basePayload := EventPayload{
		ID:         "task-123",
		KeyID:      "tenant-a",
		Event:      "toolUse",
		Timestamp:  1700000000,
		Detail:     "Executing grep command",
		TurnIndex:  2,
		Prompt:     "Find all errors in logs",
		SubagentID: "agent-sub-1",
	}

	fp1 := ComputeEventFingerprint(basePayload)
	fp2 := ComputeEventFingerprint(basePayload)

	if fp1 == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	if len(fp1) != 32 {
		t.Fatalf("expected 32 hex characters, got %d (%s)", len(fp1), fp1)
	}
	if fp1 != fp2 {
		t.Fatalf("fingerprint should be deterministic, got %s vs %s", fp1, fp2)
	}

	// 验证字符串 TrimSpace 不影响指纹
	withSpaces := basePayload
	withSpaces.KeyID = "  tenant-a  "
	withSpaces.Detail = " Executing grep command "
	withSpaces.Prompt = "\nFind all errors in logs\t"
	if ComputeEventFingerprint(withSpaces) != fp1 {
		t.Fatal("whitespace variations should be trimmed and produce identical fingerprint")
	}

	// 验证关键字段不同会产生不同指纹
	payloads := []EventPayload{
		func() EventPayload { p := basePayload; p.ID = "task-456"; return p }(),
		func() EventPayload { p := basePayload; p.KeyID = "tenant-b"; return p }(),
		func() EventPayload { p := basePayload; p.Event = "toolResult"; return p }(),
		func() EventPayload { p := basePayload; p.Timestamp = 1700000001; return p }(),
		func() EventPayload { p := basePayload; p.Detail = "Executing cat command"; return p }(),
		func() EventPayload { p := basePayload; p.TurnIndex = 3; return p }(),
		func() EventPayload { p := basePayload; p.Prompt = "Different prompt"; return p }(),
		func() EventPayload { p := basePayload; p.SubagentID = "agent-sub-2"; return p }(),
	}

	for i, diffP := range payloads {
		diffFp := ComputeEventFingerprint(diffP)
		if diffFp == fp1 {
			t.Fatalf("variation %d should have produced a different fingerprint from %s", i, fp1)
		}
	}
}

// TestEventLogRingBuffer_AppendIfNew_Idempotency 测试环形缓冲区幂等去重
func TestEventLogRingBuffer_AppendIfNew_Idempotency(t *testing.T) {
	rb := NewEventLogRingBuffer(10)

	rec1 := EventRecord{
		EventID:   "evt-1001",
		Timestamp: time.Now().UnixMilli(),
		Event:     "sessionStart",
		Detail:    "Init session",
	}

	// 1. 初次插入：应成功且序号为 1
	isNew, seq := rb.AppendIfNew(rec1)
	if !isNew || seq != 1 {
		t.Fatalf("first append should be new with seq 1, got isNew=%v, seq=%d", isNew, seq)
	}
	if !rb.IsDuplicate("evt-1001") {
		t.Fatal("expected evt-1001 to be recognized as duplicate")
	}

	// 2. 重复插入相同 EventID：应被拒绝返回 false, 0
	isNew2, seq2 := rb.AppendIfNew(rec1)
	if isNew2 || seq2 != 0 {
		t.Fatalf("duplicate append should return false, 0, got isNew=%v, seq=%d", isNew2, seq2)
	}

	// 3. 再次插入新 EventID：应成功且序号单调递增为 2
	rec2 := EventRecord{
		EventID:   "evt-1002",
		Timestamp: time.Now().UnixMilli(),
		Event:     "toolUse",
		Detail:    "Run bash",
	}
	isNew3, seq3 := rb.AppendIfNew(rec2)
	if !isNew3 || seq3 != 2 {
		t.Fatalf("second unique append should be new with seq 2, got isNew=%v, seq=%d", isNew3, seq3)
	}

	if rb.Count() != 2 {
		t.Fatalf("expected count 2, got %d", rb.Count())
	}

	// 4. 空 EventID 测试
	recEmpty := EventRecord{
		EventID: "",
		Event:   "heartbeat",
	}
	isNewEmpty, seqEmpty := rb.AppendIfNew(recEmpty)
	if !isNewEmpty || seqEmpty != 3 {
		t.Fatalf("empty eventID should still be accepted as unique sequence, got isNew=%v, seq=%d", isNewEmpty, seqEmpty)
	}
	if rb.IsDuplicate("") {
		t.Fatal("empty eventID should not be marked duplicate")
	}
}

// TestEventLogRingBuffer_CapacityEviction_And_Monotonicity 测试环形容量淘汰与序号单调递增
func TestEventLogRingBuffer_CapacityEviction_And_Monotonicity(t *testing.T) {
	capacity := 5
	rb := NewEventLogRingBuffer(capacity)

	// 插入 10 条唯一事件
	totalEvents := 10
	for i := 1; i <= totalEvents; i++ {
		rec := EventRecord{
			EventID:   fmt.Sprintf("evt-%04d", i),
			Timestamp: int64(1700000000 + i),
			Event:     "toolUse",
			Detail:    fmt.Sprintf("Action #%d", i),
		}
		isNew, seq := rb.AppendIfNew(rec)
		if !isNew || seq != uint64(i) {
			t.Fatalf("event %d append failed: isNew=%v, seq=%d", i, isNew, seq)
		}
	}

	// 缓冲区容量必须上限为 5
	if count := rb.Count(); count != capacity {
		t.Fatalf("expected buffer count to be %d, got %d", capacity, count)
	}

	// 验证快照中的记录属于最后 5 条 (序号 6 到 10)
	snapshot := rb.Snapshot()
	if len(snapshot) != capacity {
		t.Fatalf("expected snapshot len %d, got %d", capacity, len(snapshot))
	}

	for idx, item := range snapshot {
		expectedSeq := uint64(idx + 6)
		if item.Sequence != expectedSeq {
			t.Errorf("item[%d] sequence expected %d, got %d", idx, expectedSeq, item.Sequence)
		}
		expectedID := fmt.Sprintf("evt-%04d", expectedSeq)
		if item.EventID != expectedID {
			t.Errorf("item[%d] eventID expected %s, got %s", idx, expectedID, item.EventID)
		}
	}

	// 即使最旧事件被移出环形记录，最近指纹仍处于防抖窗口内
	if !rb.IsDuplicate("evt-0001") {
		t.Fatal("evt-0001 should still be known in seenKeys recent window")
	}
}

// TestEventLogRingBuffer_UpdateLastPostApplication 测试应用后状态与版本更新
func TestEventLogRingBuffer_UpdateLastPostApplication(t *testing.T) {
	rb := NewEventLogRingBuffer(10)
	rec := EventRecord{
		EventID: "evt-status-check",
		Event:   "toolResult",
	}
	rb.AppendIfNew(rec)

	rb.UpdateLastPostApplication("running", 42)

	snapshot := rb.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("expected 1 record, got %d", len(snapshot))
	}
	if snapshot[0].TaskStatus != "running" || snapshot[0].TaskVersion != 42 {
		t.Fatalf("expected status=running version=42, got status=%s version=%d",
			snapshot[0].TaskStatus, snapshot[0].TaskVersion)
	}
}

// TestEventLogRingBuffer_ThreadSafety 测试高并发环境下的数据竞态保护
func TestEventLogRingBuffer_ThreadSafety(t *testing.T) {
	rb := NewEventLogRingBuffer(64)
	var wg sync.WaitGroup

	goroutines := 16
	eventsPerRoutine := 100

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			for i := 0; i < eventsPerRoutine; i++ {
				// 模拟部分唯一、部分重复 (每 5 次生成一次相同 ID)
				var eventID string
				if i%5 == 0 {
					eventID = fmt.Sprintf("shared-evt-%d", i)
				} else {
					eventID = fmt.Sprintf("routine-%d-evt-%d", routineID, i)
				}

				rec := EventRecord{
					EventID:   eventID,
					Timestamp: time.Now().UnixMilli(),
					Event:     "concurrentEvent",
					Detail:    fmt.Sprintf("routine %d event %d", routineID, i),
				}

				isNew, _ := rb.AppendIfNew(rec)
				if isNew {
					rb.UpdateLastPostApplication("running", uint64(i))
				}

				// 并发读取
				_ = rb.IsDuplicate(eventID)
				_ = rb.Count()
				if i%20 == 0 {
					_ = rb.Snapshot()
				}
			}
		}(g)
	}

	wg.Wait()

	if rb.Count() > 64 {
		t.Fatalf("buffer exceeded capacity: %d", rb.Count())
	}
}
