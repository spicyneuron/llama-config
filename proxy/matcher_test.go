package proxy

import (
	"net/http/httptest"
	"testing"

	"github.com/spicyneuron/llama-config-proxy/config"
)

func TestFindMatchingRules(t *testing.T) {
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("/v1/chat/completions"),
			},
			{
				Methods: newPatternField("GET", "POST"),
				Paths:   newPatternField("/api/.*"),
			},
			{
				Methods: newPatternField(".*"), // Match all
				Paths:   newPatternField("/.*"),
			},
		},
	}

	tests := []struct {
		name        string
		method      string
		path        string
		wantIndices []int // indices of rules that should match
	}{
		{
			name:        "exact match first and third rules",
			method:      "POST",
			path:        "/v1/chat/completions",
			wantIndices: []int{0, 2}, // First rule matches exactly, third matches all
		},
		{
			name:        "regex match second and third rules",
			method:      "GET",
			path:        "/api/models",
			wantIndices: []int{1, 2}, // Second rule matches /api/.*, third matches all
		},
		{
			name:        "POST matches second and third rules",
			method:      "POST",
			path:        "/api/generate",
			wantIndices: []int{1, 2}, // Second rule matches, third matches all
		},
		{
			name:        "only third rule matches",
			method:      "DELETE",
			path:        "/other/endpoint",
			wantIndices: []int{2}, // Only the catch-all rule
		},
		{
			name:        "case insensitive method",
			method:      "post",
			path:        "/v1/chat/completions",
			wantIndices: []int{0, 2}, // Same as first test
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rules := FindMatchingRules(req, cfg)

			if len(rules) != len(tt.wantIndices) {
				t.Fatalf("FindMatchingRules() returned %d rules, want %d", len(rules), len(tt.wantIndices))
			}

			// Verify each matched rule
			for i, wantIndex := range tt.wantIndices {
				if rules[i] != &cfg.Rules[wantIndex] {
					t.Errorf("Rule %d: got rule index %d, want %d", i, getRuleIndex(rules[i], cfg), wantIndex)
				}
			}
		})
	}
}

func TestFindMatchingRulesStacking(t *testing.T) {
	// Test that multiple matching rules are all returned in order
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("/api/.*"),
			},
			{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("/api/chat"),
			},
			{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("/api/.*"),
			},
		},
	}

	req := httptest.NewRequest("POST", "/api/chat", nil)
	rules := FindMatchingRules(req, cfg)

	// All three rules should match /api/chat
	if len(rules) != 3 {
		t.Fatalf("Expected 3 matching rules, got %d", len(rules))
	}

	// Verify order: should be rules 0, 1, 2
	if rules[0] != &cfg.Rules[0] {
		t.Error("First matched rule should be rule 0")
	}
	if rules[1] != &cfg.Rules[1] {
		t.Error("Second matched rule should be rule 1")
	}
	if rules[2] != &cfg.Rules[2] {
		t.Error("Third matched rule should be rule 2")
	}
}

// Helper to get rule index
func getRuleIndex(rule *config.Rule, cfg *config.Config) int {
	for i := range cfg.Rules {
		if rule == &cfg.Rules[i] {
			return i
		}
	}
	return -1
}
