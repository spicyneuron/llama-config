package proxy

import (
	"net/http/httptest"
	"testing"

	"github.com/spicyneuron/llama-config-proxy/config"
)

func TestFindMatchingRule(t *testing.T) {
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
		name      string
		method    string
		path      string
		wantIndex int // -1 for no match
	}{
		{
			name:      "exact match first rule",
			method:    "POST",
			path:      "/v1/chat/completions",
			wantIndex: 0,
		},
		{
			name:      "regex match second rule",
			method:    "GET",
			path:      "/api/models",
			wantIndex: 1,
		},
		{
			name:      "POST also matches second rule",
			method:    "POST",
			path:      "/api/generate",
			wantIndex: 1, // But first rule doesn't match, so second rule wins
		},
		{
			name:      "fallback to third rule",
			method:    "DELETE",
			path:      "/other/endpoint",
			wantIndex: 2,
		},
		{
			name:      "case insensitive method",
			method:    "post",
			path:      "/v1/chat/completions",
			wantIndex: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rule := FindMatchingRule(req, cfg)

			if tt.wantIndex == -1 {
				if rule != nil {
					t.Errorf("FindMatchingRule() = %v, want nil", rule)
				}
			} else {
				if rule == nil {
					t.Fatal("FindMatchingRule() = nil, want non-nil")
				}

				// Find which rule was matched
				var gotIndex int = -1
				for i := range cfg.Rules {
					if rule == &cfg.Rules[i] {
						gotIndex = i
						break
					}
				}

				if gotIndex != tt.wantIndex {
					t.Errorf("FindMatchingRule() matched rule %d, want %d", gotIndex, tt.wantIndex)
				}
			}
		})
	}
}

func TestFindMatchingRuleFirstMatch(t *testing.T) {
	// Test that first matching rule wins
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
		},
	}

	req := httptest.NewRequest("POST", "/api/chat", nil)
	rule := FindMatchingRule(req, cfg)

	if rule != &cfg.Rules[0] {
		t.Error("Should match first rule, not second")
	}
}
