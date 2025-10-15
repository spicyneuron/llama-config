package proxy

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/spicyneuron/llama-config-proxy/config"
)

func mustParseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return u
}

func TestModifyStreamingResponse_OpenAIFormat(t *testing.T) {
	// Load config with streaming transformation
	cfg, err := config.Load([]string{"../examples/ollama.yml"}, config.CliOverrides{})
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Create mock streaming response (OpenAI SSE format)
	sseData := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1694268190,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1694268190,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1694268190,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader(sseData)),
		Request: &http.Request{
			Method: "POST",
			URL:    mustParseURL("/api/chat"),
		},
	}

	// Find matching rule
	rule := FindMatchingRule(resp.Request, cfg)
	if rule == nil {
		t.Fatal("No matching rule found")
	}

	// Apply streaming transformation
	err = ModifyStreamingResponse(resp, rule)
	if err != nil {
		t.Fatalf("ModifyStreamingResponse failed: %v", err)
	}

	// Read transformed response
	scanner := bufio.NewScanner(resp.Body)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	resp.Body.Close()

	if err := scanner.Err(); err != nil {
		t.Fatalf("Scanner error: %v", err)
	}

	// Verify transformation
	// First chunk should be transformed to Ollama format with SSE prefix
	if !strings.HasPrefix(lines[0], "data: ") {
		t.Errorf("Expected SSE format, got: %s", lines[0])
	}

	// Check if first chunk contains Ollama fields
	firstChunk := strings.TrimPrefix(lines[0], "data: ")
	if !strings.Contains(firstChunk, `"done":false`) {
		t.Errorf("Expected done:false in first chunk, got: %s", firstChunk)
	}
	if !strings.Contains(firstChunk, `"message"`) {
		t.Errorf("Expected message field in chunk, got: %s", firstChunk)
	}

	// Check [DONE] marker is preserved
	foundDone := false
	for _, line := range lines {
		if strings.Contains(line, "[DONE]") {
			foundDone = true
			break
		}
	}
	if !foundDone {
		t.Error("Expected [DONE] marker to be preserved")
	}
}

func TestModifyStreamingResponse_OllamaFormat(t *testing.T) {
	// Create a simple config with transformation
	cfg := &config.Config{
		Proxy: config.ProxyConfig{
			Listen: "localhost:8080",
			Target: "http://localhost:9000",
		},
		Rules: []config.Rule{
			{
				Methods:    config.PatternField{Patterns: []string{"POST"}},
				Paths:      config.PatternField{Patterns: []string{"^/test$"}},
				TargetPath: "/v1/test",
				OnResponse: []config.Operation{
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

	// Find matching rule
	rule := FindMatchingRule(resp.Request, cfg)
	if rule == nil {
		t.Fatal("No matching rule found")
	}

	// Apply streaming transformation
	err := ModifyStreamingResponse(resp, rule)
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

func TestModifyStreamingResponse_PassthroughNonJSON(t *testing.T) {
	cfg := &config.Config{
		Proxy: config.ProxyConfig{
			Listen: "localhost:8080",
			Target: "http://localhost:9000",
		},
		Rules: []config.Rule{
			{
				Methods: config.PatternField{Patterns: []string{"GET"}},
				Paths:   config.PatternField{Patterns: []string{"^/stream$"}},
				OnResponse: []config.Operation{
					{
						Merge: map[string]any{"test": "dummy"},
					},
				},
			},
		},
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

	rule := FindMatchingRule(resp.Request, cfg)
	if rule == nil {
		t.Fatal("No matching rule found")
	}

	// Apply streaming transformation (should pass through)
	err := ModifyStreamingResponse(resp, rule)
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
		Proxy: config.ProxyConfig{
			Listen: "localhost:8080",
			Target: "http://localhost:9000",
		},
		Rules: []config.Rule{
			{
				Methods: config.PatternField{Patterns: []string{"POST"}},
				Paths:   config.PatternField{Patterns: []string{"^/api/chat$"}},
				OnResponse: []config.Operation{
					{
						Merge: map[string]any{"test": "value"},
					},
				},
			},
		},
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

			// Find and store matching rule in context
			rule := FindMatchingRule(req, cfg)
			if rule == nil {
				t.Fatal("No matching rule")
			}

			// Store rule in request context (mimicking what ModifyRequest does)
			ctx := context.WithValue(req.Context(), ruleContextKey, rule)
			*req = *req.WithContext(ctx)

			// Call ModifyResponse which should route correctly
			err := ModifyResponse(resp, cfg)
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
