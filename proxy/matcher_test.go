package proxy

import (
	"net/http/httptest"
	"testing"

	"github.com/spicyneuron/llama-matchmaker/config"
)

func TestFindMatchingRoutes(t *testing.T) {
	routes := []config.Route{
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
	}

	tests := []struct {
		name        string
		method      string
		path        string
		wantIndices []int // indices of routes that should match
	}{
		{
			name:        "exact match first and third routes",
			method:      "POST",
			path:        "/v1/chat/completions",
			wantIndices: []int{0, 2}, // First route matches exactly, third matches all
		},
		{
			name:        "regex match second and third routes",
			method:      "GET",
			path:        "/api/models",
			wantIndices: []int{1, 2}, // Second route matches /api/.*, third matches all
		},
		{
			name:        "POST matches second and third routes",
			method:      "POST",
			path:        "/api/generate",
			wantIndices: []int{1, 2}, // Second route matches, third matches all
		},
		{
			name:        "only third route matches",
			method:      "DELETE",
			path:        "/other/endpoint",
			wantIndices: []int{2}, // Only the catch-all route
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
			rules := FindMatchingRoutes(req, routes)

			if len(rules) != len(tt.wantIndices) {
				t.Fatalf("FindMatchingRoutes() returned %d routes, want %d", len(rules), len(tt.wantIndices))
			}

			// Verify each matched route
			for i, wantIndex := range tt.wantIndices {
				if rules[i] != &routes[wantIndex] {
					t.Errorf("Route %d: got route index %d, want %d", i, getRouteIndex(rules[i], routes), wantIndex)
				}
			}
		})
	}
}

func TestFindMatchingRoutesStacking(t *testing.T) {
	// Test that multiple matching routes are all returned in order
	routes := []config.Route{
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
	}

	req := httptest.NewRequest("POST", "/api/chat", nil)
	rules := FindMatchingRoutes(req, routes)

	// All three routes should match /api/chat
	if len(rules) != 3 {
		t.Fatalf("Expected 3 matching routes, got %d", len(rules))
	}

	// Verify order: should be routes 0, 1, 2
	if rules[0] != &routes[0] {
		t.Error("First matched route should be route 0")
	}
	if rules[1] != &routes[1] {
		t.Error("Second matched route should be route 1")
	}
	if rules[2] != &routes[2] {
		t.Error("Third matched route should be route 2")
	}
}

// Helper to get route index
func getRouteIndex(rule *config.Route, routes []config.Route) int {
	for i := range routes {
		if rule == &routes[i] {
			return i
		}
	}
	return -1
}
