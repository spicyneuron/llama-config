package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"text/template"

	"github.com/spicyneuron/llama-matchmaker/config"
)

// ensure we apply all matching on_response handlers, not just the last match
func TestModifyResponseAppliesAllMatchedRules(t *testing.T) {
	rules := []config.Route{
		{
			Methods:    config.PatternField{Patterns: []string{"POST"}},
			Paths:      config.PatternField{Patterns: []string{"^/v1/chat$"}},
			OnResponse: []config.Action{{Merge: map[string]any{"first": true}}},
		},
		{
			Methods:    config.PatternField{Patterns: []string{"POST"}},
			Paths:      config.PatternField{Patterns: []string{"^/v1/chat$"}},
			OnResponse: []config.Action{{Merge: map[string]any{"second": "yes"}}},
		},
	}

	// Compile patterns and actions
	for i := range rules {
		if err := rules[i].Methods.Validate(); err != nil {
			t.Fatalf("methods validate: %v", err)
		}
		if err := rules[i].Paths.Validate(); err != nil {
			t.Fatalf("paths validate: %v", err)
		}
		rules[i].Compiled = &config.CompiledRoute{
			OnResponse:          []config.ActionExec{config.ActionExec(rules[i].OnResponse[0])},
			OnResponseTemplates: []*template.Template{nil},
		}
	}

	cfg := config.Config{Routes: rules}

	req := httptest.NewRequest("POST", "http://example.com/v1/chat", bytes.NewBufferString(`{"original":true}`))
	req.Header.Set("Content-Type", "application/json")

	ModifyRequest(req, &cfg)

	resp := &http.Response{
		Request:    req,
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"original":true}`)),
	}

	if err := ModifyResponse(resp, &cfg); err != nil {
		t.Fatalf("ModifyResponse error: %v", err)
	}

	defer resp.Body.Close()
	processed, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read processed body: %v", err)
	}

	var data map[string]any
	if err := json.Unmarshal(processed, &data); err != nil {
		t.Fatalf("unmarshal processed body: %v", err)
	}

	if data["first"] != true {
		t.Fatalf("expected first rule to apply, got %v", data["first"])
	}
	if data["second"] != "yes" {
		t.Fatalf("expected second rule to apply, got %v", data["second"])
	}
	if data["original"] != true {
		t.Fatalf("expected original field preserved, got %v", data["original"])
	}
}
