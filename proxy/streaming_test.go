package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/spicyneuron/llama-matchmaker/config"
)

func mustParseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return u
}

func TestModifyStreamingResponse_OllamaFormat(t *testing.T) {
	// Create a simple config with transformation
	cfg := &config.Config{
		Proxies: []config.ProxyConfig{{
			Listen: "localhost:8080",
			Target: "http://localhost:9000",
			Routes: []config.Route{
				{
					Methods:    config.PatternField{Patterns: []string{"POST"}},
					Paths:      config.PatternField{Patterns: []string{"^/test$"}},
					TargetPath: "/v1/test",
					OnResponse: []config.Action{
						{
							MatchBody: map[string]config.PatternField{
								"role": {Patterns: []string{".*"}},
							},
							Merge: map[string]any{
								"transformed": true,
							},
						},
					},
				},
			},
		}},
	}

	if err := config.Validate(cfg); err != nil {
		t.Fatalf("Failed to validate config: %v", err)
	}

	if err := config.CompileTemplates(cfg); err != nil {
		t.Fatalf("Failed to compile templates: %v", err)
	}

	// Create mock streaming response (Ollama raw JSON format)
	jsonData := `{"role":"assistant","content":"Hello"}
{"role":"assistant","content":" world"}
{"role":"assistant","done":true}
`

	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader(jsonData)),
		Request: &http.Request{
			Method: "POST",
			URL:    mustParseURL("/test"),
		},
	}

	// Find matching rules
	rules := FindMatchingRoutes(resp.Request, cfg.Proxies[0].Routes)
	if len(rules) == 0 {
		t.Fatal("No matching rules found")
	}
	rule := rules[0]

	// Apply streaming transformation
	err := ModifyStreamingResponse(resp, []*config.Route{rule}, []int{0})
	if err != nil {
		t.Fatalf("ModifyStreamingResponse failed: %v", err)
	}

	// Read transformed response
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}

	// Verify transformation - should add "transformed":true to each line
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	for i, line := range lines {
		if !strings.Contains(line, `"transformed":true`) {
			t.Errorf("Line %d missing transformed field: %s", i, line)
		}
		// Should NOT have SSE prefix for Ollama format
		if strings.HasPrefix(line, "data: ") {
			t.Errorf("Line %d should not have SSE prefix for Ollama format: %s", i, line)
		}
	}
}

func TestModifyStreamingResponse_PassThroughNonJSONAndDone(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader("data: notjson\n\ndata: [DONE]\n")),
		Request: &http.Request{
			Method: "GET",
			URL:    mustParseURL("/sse"),
		},
	}

	if err := ModifyStreamingResponse(resp, nil, nil); err != nil {
		t.Fatalf("ModifyStreamingResponse failed: %v", err)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}
	rawLines := strings.Split(string(body), "\n")
	var lines []string
	for _, l := range rawLines {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 non-empty lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "data: notjson" {
		t.Fatalf("expected non-JSON line to pass through, got %q", lines[0])
	}
	if lines[1] != "data: [DONE]" {
		t.Fatalf("expected [DONE] marker to be preserved, got %q", lines[1])
	}
}

func TestModifyStreamingResponse_PassthroughNonJSON(t *testing.T) {
	cfg := &config.Config{
		Proxies: []config.ProxyConfig{{
			Listen: "localhost:8080",
			Target: "http://localhost:9000",
			Routes: []config.Route{
				{
					Methods: config.PatternField{Patterns: []string{"GET"}},
					Paths:   config.PatternField{Patterns: []string{"^/stream$"}},
					OnResponse: []config.Action{
						{
							Merge: map[string]any{"test": "dummy"},
						},
					},
				},
			},
		}},
	}

	if err := config.Validate(cfg); err != nil {
		t.Fatalf("Failed to validate config: %v", err)
	}

	if err := config.CompileTemplates(cfg); err != nil {
		t.Fatalf("Failed to compile templates: %v", err)
	}

	// Non-JSON streaming data
	streamData := `event: ping
data: keep-alive

: comment line
`

	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader(streamData)),
		Request: &http.Request{
			Method: "GET",
			URL:    mustParseURL("/stream"),
		},
	}

	rules := FindMatchingRoutes(resp.Request, cfg.Proxies[0].Routes)
	if len(rules) == 0 {
		t.Fatal("No matching rules found")
	}
	rule := rules[0]

	// Apply streaming transformation (should pass through)
	err := ModifyStreamingResponse(resp, []*config.Route{rule}, []int{0})
	if err != nil {
		t.Fatalf("ModifyStreamingResponse failed: %v", err)
	}

	// Read response
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}

	// Should be unchanged
	if string(body) != streamData {
		t.Errorf("Non-JSON data was modified.\nExpected:\n%s\nGot:\n%s", streamData, string(body))
	}
}

func TestModifyResponse_RoutesToStreaming(t *testing.T) {
	cfg := &config.Config{
		Proxies: []config.ProxyConfig{{
			Listen: "localhost:8080",
			Target: "http://localhost:9000",
			Routes: []config.Route{
				{
					Methods: config.PatternField{Patterns: []string{"POST"}},
					Paths:   config.PatternField{Patterns: []string{"^/api/chat$"}},
					OnResponse: []config.Action{
						{
							Merge: map[string]any{"test": "value"},
						},
					},
				},
			},
		}},
	}

	if err := config.Validate(cfg); err != nil {
		t.Fatalf("Failed to compile config: %v", err)
	}

	if err := config.CompileTemplates(cfg); err != nil {
		t.Fatalf("Failed to compile templates: %v", err)
	}

	tests := []struct {
		name        string
		contentType string
		body        string
		expectSSE   bool
	}{
		{
			name:        "SSE content type routes to streaming",
			contentType: "text/event-stream",
			body:        `data: {"test":"input"}` + "\n",
			expectSSE:   true,
		},
		{
			name:        "JSON content type routes to non-streaming",
			contentType: "application/json",
			body:        `{"test":"input"}`,
			expectSSE:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{
				Method: "POST",
				URL:    mustParseURL("/api/chat"),
			}

			resp := &http.Response{
				StatusCode: 200,
				Header: http.Header{
					"Content-Type": []string{tt.contentType},
				},
				Body:    io.NopCloser(strings.NewReader(tt.body)),
				Request: req,
			}

			// Find and store matching rules in context
			rules := FindMatchingRoutes(req, cfg.Proxies[0].Routes)
			if len(rules) == 0 {
				t.Fatal("No matching rules")
			}
			rule := rules[0]

			// Store rule in request context (mimicking what ModifyRequest does)
			ctx := context.WithValue(req.Context(), routeContextKey, rule)
			*req = *req.WithContext(ctx)

			// Call ModifyResponse which should route correctly
			err := ModifyResponse(resp, cfg.Proxies[0].Routes)
			if err != nil {
				t.Fatalf("ModifyResponse failed: %v", err)
			}

			// Read result
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if tt.expectSSE {
				// Streaming should preserve format
				if !bytes.Contains(body, []byte("data: ")) {
					t.Error("Expected SSE format to be preserved")
				}
			} else {
				// Non-streaming should have merged value
				if !bytes.Contains(body, []byte(`"test":"value"`)) {
					t.Errorf("Expected merged value in response, got: %s", string(body))
				}
			}
		})
	}
}
