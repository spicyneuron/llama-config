package config

import "testing"

func TestProcessOperationsMatchHeadersDeleteAndStop(t *testing.T) {
	envPattern := PatternField{Patterns: []string{"prod"}}
	if err := envPattern.Validate(); err != nil {
		t.Fatalf("failed to compile env pattern: %v", err)
	}
	removePattern := PatternField{Patterns: []string{".*"}}
	if err := removePattern.Validate(); err != nil {
		t.Fatalf("failed to compile remove pattern: %v", err)
	}

	// Build execution operations directly to avoid template compilation noise
	ops := []OperationExec{
		{
			MatchHeaders: map[string]PatternField{
				"X-Env": envPattern,
			},
			Merge: map[string]any{"seen": 1},
		},
		{
			MatchBody: map[string]PatternField{
				"remove_me": removePattern,
			},
			Delete: []string{"remove_me"},
			Stop:   true, // Halt before the final op
		},
		{
			// Should never run because of Stop
			Merge: map[string]any{"unreachable": true},
		},
	}

	headers := map[string]string{"X-Env": "prod"}
	body := map[string]any{
		"keep":      "x",
		"remove_me": "y",
	}

	modified, applied := processOperations("test", body, headers, 0, "", "", ops, nil)
	if !modified {
		t.Fatal("expected modifications to be applied")
	}

	if body["seen"] != 1.0 && body["seen"] != 1 { // merged as number
		t.Fatalf("expected seen=1 merge, got %v", body["seen"])
	}
	if _, exists := body["remove_me"]; exists {
		t.Fatalf("expected remove_me to be deleted, body=%v", body)
	}
	if _, exists := body["unreachable"]; exists {
		t.Fatalf("stop flag should have prevented unreachable op, body=%v", body)
	}

	if applied["seen"] != 1 {
		t.Errorf("applied merge missing, got %v", applied["seen"])
	}
	if applied["remove_me"] != "<deleted>" {
		t.Errorf("applied delete not recorded, got %v", applied["remove_me"])
	}
	if _, exists := applied["unreachable"]; exists {
		t.Errorf("stop flag should prevent recording unreachable, got %v", applied["unreachable"])
	}
}

func TestProcessResponseHeaderFilter(t *testing.T) {
	ctPattern := PatternField{Patterns: []string{"application/json"}}
	if err := ctPattern.Validate(); err != nil {
		t.Fatalf("failed to compile content-type pattern: %v", err)
	}

	compiled := &CompiledRule{
		OnResponse: []OperationExec{
			{
				MatchHeaders: map[string]PatternField{
					"Content-Type": ctPattern,
				},
				Merge: map[string]any{"tag": "processed"},
			},
		},
	}

	headers := map[string]string{"Content-Type": "application/json"}
	body := map[string]any{"message": "hi"}

	modified, applied := ProcessResponse(body, headers, compiled, 0, "", "")
	if !modified {
		t.Fatal("expected response to be modified")
	}
	if body["tag"] != "processed" {
		t.Fatalf("expected tag merge, got %v", body["tag"])
	}
	if applied["tag"] != "processed" {
		t.Fatalf("expected applied tag recorded, got %v", applied["tag"])
	}

	// Negative header match should no-op
	headers["Content-Type"] = "text/plain"
	body = map[string]any{"message": "hi"}
	modified, _ = ProcessResponse(body, headers, compiled, 0, "", "")
	if modified {
		t.Fatal("expected no modification for non-matching headers")
	}
	if _, exists := body["tag"]; exists {
		t.Fatalf("tag should not be present when headers do not match, body=%v", body)
	}

	// Sanity: ensure Matches ignores header casing
	headers = map[string]string{"Content-Type": "Application/Json"}
	body = map[string]any{"message": "hi"}
	if modified, _ := ProcessResponse(body, headers, compiled, 0, "", ""); !modified {
		t.Fatal("expected case-insensitive header match to modify response")
	}
	if body["tag"] != "processed" {
		t.Fatalf("expected tag merge on case-insensitive match, got %v", body["tag"])
	}
}
