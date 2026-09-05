package task

import (
	"strings"
	"testing"
)

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "no secrets",
			input:    "Hello world, running tests on main branch",
			expected: "Hello world, running tests on main branch",
		},
		{
			name:     "OpenAI key",
			input:    "Using key sk-proj-1234567890abcdef1234567890 for API calls",
			expected: "Using key [REDACTED_SECRET] for API calls",
		},
		{
			name:     "GitHub personal access token",
			input:    "git clone https://ghp_123456789012345678901234567890123456@github.com/org/repo.git",
			expected: "git clone https://[REDACTED_SECRET]@github.com/org/repo.git",
		},
		{
			name:     "GitHub fine-grained PAT",
			input:    "PAT: github_pat_11AABBCCDDEEFFGGHHIIJJ_12345678901234567890",
			expected: "PAT: [REDACTED_SECRET]",
		},
		{
			name:     "Slack bot token",
			input:    "SLACK_BOT_TOKEN=xoxb-123456789012-abcdef123456",
			expected: "SLACK_BOT_TOKEN=[REDACTED_SECRET]",
		},
		{
			name:     "Authorization Bearer header",
			input:    "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.secret",
			expected: "Authorization: Bearer [REDACTED_SECRET]",
		},
		{
			name:     "password assignment with quotes",
			input:    `db_password = "SuperSecretPassword123!"`,
			expected: `db_password = "[REDACTED_SECRET]"`,
		},
		{
			name:     "api_key assignment with colon",
			input:    `api_key: secret_api_val_999`,
			expected: `api_key: [REDACTED_SECRET]`,
		},
		{
			name:     "token assignment with single quotes",
			input:    `TOKEN='my-auth-token-456'`,
			expected: `TOKEN='[REDACTED_SECRET]'`,
		},
		{
			name:     "macOS home path normalization",
			input:    "Read file at /Users/developer/code/project/config.yaml",
			expected: "Read file at /Users/***USER***/code/project/config.yaml",
		},
		{
			name:     "Linux home path normalization",
			input:    "Log written to /home/ubuntu/app/server.log",
			expected: "Log written to /home/***USER***/app/server.log",
		},
		{
			name:     "Combined multiple secrets and paths",
			input:    "curl -H 'Authorization: Bearer my-secret-token' https://api.openai.com -d '{\"key\": \"sk-abcdef12345678901234567890\"}' > /Users/alice/out.json",
			expected: "curl -H 'Authorization: Bearer [REDACTED_SECRET]' https://api.openai.com -d '{\"key\": \"[REDACTED_SECRET]\"}' > /Users/***USER***/out.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeString(tc.input)
			if got != tc.expected {
				t.Fatalf("SanitizeString(%q)\n got:  %q\n want: %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestTask_Sanitize_DeepCopySafety(t *testing.T) {
	orig := &Task{
		ID:          "sess-test",
		RootGoal:    "Run script with api_key: sk-proj-1234567890abcdef1234567890 in /Users/bob/repo",
		GoalSummary: "Using password: secret123",
		Title:       "Task with Bearer my-auth-token-123",
		Prompt:      "Inspect /home/charlie/data.csv",
		Detail:      "Executing with ghp_123456789012345678901234567890123456",
		AbortReason: "Aborted due to bad key sk-111122223333444455556666",
		Runs: []Turn{
			{
				Index:      1,
				Prompt:     "Turn 1 prompt with xoxb-1234567890-abcdefg",
				Title:      "Turn 1 token='secret-token'",
				AIResponse: "Here is your key: sk-abcdef12345678901234567890",
				Detail:     "Turn 1 detail /Users/bob/test.go",
				Timeline: []TimelineItem{
					{
						Desc: "Ran command with password: mypassword",
					},
				},
				TraceSpans: []TraceSpan{
					{
						SpanID:     "span-1",
						Detail:     "Call curl -H 'Authorization: Bearer secret-span-token'",
						AnomalyMsg: "Anomaly with secret: 987654321",
					},
				},
			},
		},
		Timeline: []TimelineItem{
			{
				Desc: "Top timeline with sk-1234567890123456789012345",
			},
		},
		TraceSpans: []TraceSpan{
			{
				SpanID: "top-span-1",
				Detail: "Top span detail with ghp_111122223333444455556666777788889999",
			},
		},
		ActiveSpans: map[string]TraceSpan{
			"active-1": {
				SpanID: "active-1",
				Detail: "Active span with Bearer active-token-123",
			},
		},
	}

	sanitized := orig.Sanitize()

	// 1. Check that original object was NOT mutated
	if !strings.Contains(orig.RootGoal, "sk-proj-1234567890abcdef1234567890") {
		t.Fatalf("Original RootGoal was mutated!")
	}
	if !strings.Contains(orig.Runs[0].Timeline[0].Desc, "mypassword") {
		t.Fatalf("Original Timeline item was mutated!")
	}
	if !strings.Contains(orig.Runs[0].TraceSpans[0].Detail, "secret-span-token") {
		t.Fatalf("Original TraceSpan was mutated!")
	}

	// 2. Check that sanitized object has all secrets redacted
	if strings.Contains(sanitized.RootGoal, "sk-proj") || strings.Contains(sanitized.RootGoal, "/bob/") {
		t.Fatalf("Sanitized RootGoal still contains secret or username: %s", sanitized.RootGoal)
	}
	if !strings.Contains(sanitized.RootGoal, RedactedSecret) || !strings.Contains(sanitized.RootGoal, "***USER***") {
		t.Fatalf("Sanitized RootGoal missing redacted markers: %s", sanitized.RootGoal)
	}

	if strings.Contains(sanitized.GoalSummary, "secret123") {
		t.Fatalf("Sanitized GoalSummary leaked password: %s", sanitized.GoalSummary)
	}

	if strings.Contains(sanitized.Title, "my-auth-token-123") {
		t.Fatalf("Sanitized Title leaked token: %s", sanitized.Title)
	}

	if strings.Contains(sanitized.Detail, "ghp_") {
		t.Fatalf("Sanitized Detail leaked ghp token: %s", sanitized.Detail)
	}

	if strings.Contains(sanitized.Runs[0].Prompt, "xoxb-") {
		t.Fatalf("Sanitized Run prompt leaked slack token: %s", sanitized.Runs[0].Prompt)
	}

	if strings.Contains(sanitized.Runs[0].Timeline[0].Desc, "mypassword") {
		t.Fatalf("Sanitized Run timeline leaked password: %s", sanitized.Runs[0].Timeline[0].Desc)
	}

	if strings.Contains(sanitized.Runs[0].TraceSpans[0].Detail, "secret-span-token") {
		t.Fatalf("Sanitized Run span leaked token: %s", sanitized.Runs[0].TraceSpans[0].Detail)
	}

	if strings.Contains(sanitized.Timeline[0].Desc, "sk-1234567890") {
		t.Fatalf("Sanitized top timeline leaked sk key: %s", sanitized.Timeline[0].Desc)
	}

	if strings.Contains(sanitized.TraceSpans[0].Detail, "ghp_") {
		t.Fatalf("Sanitized top trace span leaked ghp token: %s", sanitized.TraceSpans[0].Detail)
	}

	if strings.Contains(sanitized.ActiveSpans["active-1"].Detail, "active-token-123") {
		t.Fatalf("Sanitized active span leaked token: %s", sanitized.ActiveSpans["active-1"].Detail)
	}

	// 3. Test nil task
	var nilTask *Task
	if nilTask.Sanitize() != nil {
		t.Fatalf("nilTask.Sanitize() must return nil")
	}
}
