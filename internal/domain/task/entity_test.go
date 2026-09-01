package task

import (
	"testing"
)

func TestTask_LifecycleAndDomainRules(t *testing.T) {
	// 1. Initial creation with placeholder
	p1 := EventPayload{
		ID:        "sess-domain-1",
		Agent:     "ZCode",
		Repo:      "my/repo",
		Branch:    "master",
		Event:     "SessionStart",
		Timestamp: 1000,
	}

	task := NewTask(p1, 1000000)
	if task.Title != "ZCode 任务" || task.RootGoal != "ZCode 任务" {
		t.Fatalf("expected placeholder title, got title=%q rootGoal=%q", task.Title, task.RootGoal)
	}
	if len(task.Runs) != 1 || task.Runs[0].Status != "running" {
		t.Fatalf("expected 1 running turn")
	}

	// 2. Real prompt submitted in turn 1
	p2 := EventPayload{
		ID:        "sess-domain-1",
		Agent:     "ZCode",
		Event:     "UserPromptSubmit",
		Prompt:    "#task 按照 DDD 重构代码结构\n补充说明：分成 domain/app/infra",
		Timestamp: 1005,
		Detail:    "Prompt arrived",
	}
	task.ApplyEvent(p2, 1005000, "10:00:05")

	expectedTitle := "按照 DDD 重构代码结构"
	if task.Title != expectedTitle {
		t.Errorf("expected Title=%q, got %q", expectedTitle, task.Title)
	}
	if task.RootGoal != p2.Prompt {
		t.Errorf("expected RootGoal=%q, got %q", p2.Prompt, task.RootGoal)
	}

		// 3. Complete turn 1 with AIResponse
		p3 := EventPayload{
			ID:         "sess-domain-1",
			Agent:      "ZCode",
			Event:      "agentCompletion",
			Timestamp:  1010,
			Detail:     "Finished turn 1",
			AIResponse: "重构已完成，划分为了领域层、应用层与基础设施层。",
		}
		task.ApplyEvent(p3, 1010000, "10:00:10")
		if task.Status != "completed" || task.Runs[0].Status != "completed" {
			t.Fatalf("expected completed status")
		}
		if task.Runs[0].AIResponse != p3.AIResponse {
			t.Errorf("expected AIResponse %q, got %q", p3.AIResponse, task.Runs[0].AIResponse)
		}

		// Test timeline deduplication
		initialTimelineLen := len(task.Runs[0].Timeline)
		duplicateEvent := EventPayload{
			ID:        "sess-domain-1",
			Agent:     "ZCode",
			Event:     "agentCompletion",
			Timestamp: 1010,
			Detail:    "Finished turn 1",
		}
		task.ApplyEvent(duplicateEvent, 1010000, "10:00:10")
		if len(task.Runs[0].Timeline) != initialTimelineLen {
			t.Errorf("expected timeline deduplication, len was %d, now %d", initialTimelineLen, len(task.Runs[0].Timeline))
		}

		// 4. Start turn 2
		p4 := EventPayload{
			ID:        "sess-domain-1",
			Agent:     "ZCode",
			Event:     "UserPromptSubmit",
			Prompt:    "继续执行单元测试",
			Timestamp: 1020,
		}
		task.ApplyEvent(p4, 1020000, "10:00:20")
		if len(task.Runs) != 2 || task.TotalRuns != 2 {
			t.Fatalf("expected 2 runs, got %d", len(task.Runs))
		}
		if task.Status != "running" || task.Runs[1].Status != "running" {
			t.Fatalf("expected turn 2 running")
		}
		if task.Runs[1].Title != "继续执行单元测试" {
			t.Errorf("expected run 2 title '继续执行单元测试', got %q", task.Runs[1].Title)
		}

		// 5. Test transient tool failure (should NOT terminate task)
		pFail := EventPayload{
			ID:        "sess-domain-1",
			Event:     "PostToolUseFailure",
			Detail:    "grep exit code 1",
			Timestamp: 1025,
		}
		task.ApplyEvent(pFail, 1025000, "10:00:25")
		if task.Status != "running" || task.Runs[1].Status != "running" {
			t.Fatalf("transient tool failure should keep task in running state, got task.Status=%s", task.Status)
		}

		// 6. Test Task Clone deep copy
		clone := task.Clone()
		if clone.ID != task.ID || len(clone.Runs) != len(task.Runs) {
			t.Fatalf("clone failed to copy properties")
		}
		clone.Runs[0].Title = "Modified in clone"
		if task.Runs[0].Title == "Modified in clone" {
			t.Fatalf("clone was not a deep copy")
		}
	}

func TestService_Helpers(t *testing.T) {
	if !IsPlaceholderTitle("未命名") || !IsPlaceholderTitle("ZCode 任务") || !IsPlaceholderTitle("CLI Task 123") {
		t.Errorf("placeholder detection failed")
	}
	if IsPlaceholderTitle("实现支付模块") {
		t.Errorf("false positive on placeholder detection")
	}
	if FormatDuration(65) != "01m 05s" {
		t.Errorf("FormatDuration error: got %s", FormatDuration(65))
	}
	cleaned := CleanPromptTitle("#task [board]  修复 Bug \n第二行")
	if cleaned != "修复 Bug" {
		t.Errorf("CleanPromptTitle failed: %q", cleaned)
	}
}
