package task

import (
	"testing"
)

func TestAgentMaturityCatalog(t *testing.T) {
	catalog := GetMaturityCatalog()
	if len(catalog) < 5 {
		t.Fatalf("expected at least 5 agents in catalog, got %d", len(catalog))
	}

	// 验证 Claude Code 和 Aider 均为 Official 成熟度
	claudeSpec := GetAgentSpec("Claude Code")
	if claudeSpec.Tier != MaturityOfficial {
		t.Errorf("expected Claude Code to be MaturityOfficial, got %v", claudeSpec.Tier)
	}
	if !claudeSpec.HasLifecycle || !claudeSpec.HasToolFailure || !claudeSpec.HasMultiTurn || !claudeSpec.HasTranscript {
		t.Errorf("Claude Code must support lifecycle, tool failure, multi-turn and transcript: %+v", claudeSpec)
	}

	aiderSpec := GetAgentSpec("Aider")
	if aiderSpec.Tier != MaturityOfficial {
		t.Errorf("expected Aider to be MaturityOfficial, got %v", aiderSpec.Tier)
	}
	if !aiderSpec.HasLifecycle || !aiderSpec.HasToolFailure || !aiderSpec.HasMultiTurn || !aiderSpec.HasTranscript {
		t.Errorf("Aider must support lifecycle, tool failure, multi-turn and transcript: %+v", aiderSpec)
	}
}

func TestNormalizeAgentName(t *testing.T) {
	cases := []struct {
		input    string
		expected string
		tier     MaturityTier
	}{
		{"claude", "Claude Code", MaturityOfficial},
		{"CLAUDE CODE", "Claude Code", MaturityOfficial},
		{"aider-cli", "Aider", MaturityOfficial},
		{"Aider", "Aider", MaturityOfficial},
		{"cursor-agent", "Cursor Agent", MaturityOfficial},
		{"zcode_cli", "ZCode", MaturityOfficial},
		{"codex", "Codex CLI", MaturityOfficial},
		{"codex desktop", "Codex Desktop", MaturityOfficial},
		{"continue.dev", "Continue", MaturityBeta},
		{"windsurf-cascade", "Windsurf", MaturityBeta},
		{"trae_ide", "Trae", MaturityBeta},
		{"unknown-agent", "unknown-agent", MaturityExperimental},
	}

	for _, tc := range cases {
		normalized := NormalizeAgentName(tc.input)
		if normalized != tc.expected {
			t.Errorf("NormalizeAgentName(%q) = %q, expected %q", tc.input, normalized, tc.expected)
		}
		tier := ResolveAgentMaturity(tc.input)
		if tier != tc.tier {
			t.Errorf("ResolveAgentMaturity(%q) = %v, expected %v", tc.input, tier, tc.tier)
		}
	}
}

func TestListAgentsByTier(t *testing.T) {
	officials := ListAgentsByTier(MaturityOfficial)
	if len(officials) < 5 {
		t.Errorf("expected at least 5 Official agents (Cursor, ZCode, Codex CLI, Codex Desktop, Claude Code, Aider), got %d", len(officials))
	}

	betas := ListAgentsByTier(MaturityBeta)
	if len(betas) < 3 {
		t.Errorf("expected at least 3 Beta agents (Continue, Windsurf, Trae), got %d", len(betas))
	}

	experimentals := ListAgentsByTier(MaturityExperimental)
	if len(experimentals) < 1 {
		t.Errorf("expected at least 1 Experimental agent, got %d", len(experimentals))
	}
}
