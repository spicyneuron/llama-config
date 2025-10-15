package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/spicyneuron/llama-config-proxy/config"
)

func TestModifyRequestWithNilBody(t *testing.T) {
	cfg := &config.Config{
		Proxy: config.ProxyConfig{
			Listen: "localhost:8080",
			Target: "http://localhost:9000",
		},
		Rules: []config.Rule{
			{
				Methods:    newPatternField("GET"),
				Paths:      newPatternField("^/api/tags$"),
				TargetPath: "/v1/models",
				OnResponse: []config.Operation{
					{
						// Add a dummy field to pass validation
						Default: map[string]any{
							"test": "value",
						},
					},
				},
			},
		},
	}

	// Validate and compile the config
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	// Compile templates
	if err := config.CompileTemplates(cfg); err != nil {
		t.Fatalf("Template compilation failed: %v", err)
	}

	// Create GET request with nil body (typical for GET requests)
	req := httptest.NewRequest("GET", "/api/tags", nil)

	// This should not panic
	ModifyRequest(req, cfg)

	// Verify path was rewritten
	if req.URL.Path != "/v1/models" {
		t.Errorf("Expected path to be /v1/models, got %s", req.URL.Path)
	}

	// Verify body is still nil (or empty reader)
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		if len(body) > 0 {
			t.Errorf("Expected empty body, got %d bytes", len(body))
		}
	}
}

func TestModifyRequestWithEmptyBody(t *testing.T) {
	cfg := &config.Config{
		Proxy: config.ProxyConfig{
			Listen: "localhost:8080",
			Target: "http://localhost:9000",
		},
		Rules: []config.Rule{
			{
				Methods:    newPatternField("POST"),
				Paths:      newPatternField("^/api/test$"),
				TargetPath: "/v1/test",
				OnRequest: []config.Operation{
					{
						Default: map[string]any{
							"field": "value",
						},
					},
				},
			},
		},
	}

	// Validate and compile the config
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	// Compile templates
	if err := config.CompileTemplates(cfg); err != nil {
		t.Fatalf("Template compilation failed: %v", err)
	}

	// Create POST request with empty body
	req := httptest.NewRequest("POST", "/api/test", bytes.NewReader([]byte{}))

	// This should not panic
	ModifyRequest(req, cfg)

	// Verify path was rewritten
	if req.URL.Path != "/v1/test" {
		t.Errorf("Expected path to be /v1/test, got %s", req.URL.Path)
	}
}

func TestModifyRequestWithJSONBody(t *testing.T) {
	cfg := &config.Config{
		Proxy: config.ProxyConfig{
			Listen: "localhost:8080",
			Target: "http://localhost:9000",
		},
		Rules: []config.Rule{
			{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("^/api/chat$"),
				OnRequest: []config.Operation{
					{
						Default: map[string]any{
							"temperature": 0.7,
						},
					},
				},
			},
		},
	}

	// Validate and compile the config
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	// Compile templates
	if err := config.CompileTemplates(cfg); err != nil {
		t.Fatalf("Template compilation failed: %v", err)
	}

	// Create POST request with JSON body
	reqData := map[string]any{
		"model":  "llama3",
		"prompt": "Hello",
	}
	reqBody, _ := json.Marshal(reqData)
	req := httptest.NewRequest("POST", "/api/chat", bytes.NewReader(reqBody))

	// Apply modifications
	ModifyRequest(req, cfg)

	// Read and verify modified body
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("Failed to read modified body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal modified body: %v", err)
	}

	// Verify temperature was added
	if temp, ok := result["temperature"].(float64); !ok || temp != 0.7 {
		t.Errorf("Expected temperature to be 0.7, got %v", result["temperature"])
	}

	// Verify original fields are preserved
	if model, ok := result["model"].(string); !ok || model != "llama3" {
		t.Errorf("Expected model to be llama3, got %v", result["model"])
	}
}

func TestModifyRequestGETWithPathRewrite(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		path          string
		targetPath    string
		expectedPath  string
		expectedPanic bool
	}{
		{
			name:          "GET request with path rewrite",
			method:        "GET",
			path:          "/api/tags",
			targetPath:    "/v1/models",
			expectedPath:  "/v1/models",
			expectedPanic: false,
		},
		{
			name:          "GET request to models",
			method:        "GET",
			path:          "/api/models",
			targetPath:    "/v1/models",
			expectedPath:  "/v1/models",
			expectedPanic: false,
		},
		{
			name:          "DELETE request with nil body",
			method:        "DELETE",
			path:          "/api/resource/123",
			targetPath:    "/v1/resource/123",
			expectedPath:  "/v1/resource/123",
			expectedPanic: false,
		},
		{
			name:          "HEAD request with nil body",
			method:        "HEAD",
			path:          "/api/status",
			targetPath:    "/v1/status",
			expectedPath:  "/v1/status",
			expectedPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Proxy: config.ProxyConfig{
					Listen: "localhost:8080",
					Target: "http://localhost:9000",
				},
				Rules: []config.Rule{
					{
						Methods:    newPatternField(tt.method),
						Paths:      newPatternField("^" + tt.path + "$"),
						TargetPath: tt.targetPath,
						OnResponse: []config.Operation{
							{
								// Add a dummy field to pass validation (not used for requests)
								Default: map[string]any{
									"test": "value",
								},
							},
						},
					},
				},
			}

			// Validate and compile the config
			if err := config.Validate(cfg); err != nil {
				t.Fatalf("Config validation failed: %v", err)
			}

			// Compile templates
			if err := config.CompileTemplates(cfg); err != nil {
				t.Fatalf("Template compilation failed: %v", err)
			}

			// Create request with nil body
			req := httptest.NewRequest(tt.method, tt.path, nil)

			// Wrap in recover to catch panics
			defer func() {
				if r := recover(); r != nil {
					if !tt.expectedPanic {
						t.Errorf("Unexpected panic: %v", r)
					}
				} else if tt.expectedPanic {
					t.Error("Expected panic but didn't get one")
				}
			}()

			// This should not panic
			ModifyRequest(req, cfg)

			// Verify path was rewritten
			if req.URL.Path != tt.expectedPath {
				t.Errorf("Expected path to be %s, got %s", tt.expectedPath, req.URL.Path)
			}
		})
	}
}

func TestModifyRequestNonJSONBody(t *testing.T) {
	cfg := &config.Config{
		Proxy: config.ProxyConfig{
			Listen: "localhost:8080",
			Target: "http://localhost:9000",
		},
		Rules: []config.Rule{
			{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("^/api/upload$"),
				OnRequest: []config.Operation{
					{
						Default: map[string]any{
							"field": "value",
						},
					},
				},
			},
		},
	}

	// Validate and compile the config
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	// Compile templates
	if err := config.CompileTemplates(cfg); err != nil {
		t.Fatalf("Template compilation failed: %v", err)
	}

	// Create POST request with non-JSON body (e.g., plain text)
	req := httptest.NewRequest("POST", "/api/upload", bytes.NewReader([]byte("not json")))

	// This should not panic, should just skip processing
	ModifyRequest(req, cfg)

	// Read body and verify it's unchanged
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}

	if string(body) != "not json" {
		t.Errorf("Expected body to be unchanged, got %s", string(body))
	}
}

func TestModifyRequestNoMatchingRule(t *testing.T) {
	cfg := &config.Config{
		Proxy: config.ProxyConfig{
			Listen: "localhost:8080",
			Target: "http://localhost:9000",
		},
		Rules: []config.Rule{
			{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("^/api/chat$"),
				OnRequest: []config.Operation{
					{
						Default: map[string]any{
							"test": "value",
						},
					},
				},
			},
		},
	}

	// Validate and compile the config
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	// Compile templates
	if err := config.CompileTemplates(cfg); err != nil {
		t.Fatalf("Template compilation failed: %v", err)
	}

	// Create GET request that doesn't match any rule
	req := httptest.NewRequest("GET", "/api/other", nil)
	originalPath := req.URL.Path

	// This should not panic
	ModifyRequest(req, cfg)

	// Verify path is unchanged
	if req.URL.Path != originalPath {
		t.Errorf("Expected path to remain %s, got %s", originalPath, req.URL.Path)
	}
}

func TestModifyRequestStackingRules(t *testing.T) {
	cfg := &config.Config{
		Proxy: config.ProxyConfig{
			Listen: "localhost:8080",
			Target: "http://localhost:9000",
		},
		Rules: []config.Rule{
			// Rule 1: Adds temperature
			{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("^/api/chat$"),
				OnRequest: []config.Operation{
					{
						Default: map[string]any{
							"temperature": 0.7,
						},
					},
				},
			},
			// Rule 2: Adds max_tokens (should also match and apply)
			{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("^/api/chat$"),
				OnRequest: []config.Operation{
					{
						Default: map[string]any{
							"max_tokens": 100,
						},
					},
				},
			},
			// Rule 3: Adds stream flag (should also match and apply)
			{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("^/api/chat$"),
				OnRequest: []config.Operation{
					{
						Default: map[string]any{
							"stream": false,
						},
					},
				},
			},
		},
	}

	// Validate and compile the config
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	// Compile templates
	if err := config.CompileTemplates(cfg); err != nil {
		t.Fatalf("Template compilation failed: %v", err)
	}

	// Create POST request with JSON body
	reqData := map[string]any{
		"model":  "llama3",
		"prompt": "Hello",
	}
	reqBody, _ := json.Marshal(reqData)
	req := httptest.NewRequest("POST", "/api/chat", bytes.NewReader(reqBody))

	// Apply modifications
	ModifyRequest(req, cfg)

	// Read and verify modified body
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("Failed to read modified body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal modified body: %v", err)
	}

	// Verify all three rules applied their defaults
	if temp, ok := result["temperature"].(float64); !ok || temp != 0.7 {
		t.Errorf("Expected temperature to be 0.7, got %v", result["temperature"])
	}

	if maxTokens, ok := result["max_tokens"].(float64); !ok || maxTokens != 100 {
		t.Errorf("Expected max_tokens to be 100, got %v", result["max_tokens"])
	}

	if stream, ok := result["stream"].(bool); !ok || stream != false {
		t.Errorf("Expected stream to be false, got %v", result["stream"])
	}

	// Verify original fields are preserved
	if model, ok := result["model"].(string); !ok || model != "llama3" {
		t.Errorf("Expected model to be llama3, got %v", result["model"])
	}

	if prompt, ok := result["prompt"].(string); !ok || prompt != "Hello" {
		t.Errorf("Expected prompt to be Hello, got %v", result["prompt"])
	}
}

func TestModifyRequestStackingWithConditionalMatch(t *testing.T) {
	cfg := &config.Config{
		Proxy: config.ProxyConfig{
			Listen: "localhost:8080",
			Target: "http://localhost:9000",
		},
		Rules: []config.Rule{
			// Rule 1: Adds type field
			{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("^/api/chat$"),
				OnRequest: []config.Operation{
					{
						Merge: map[string]any{
							"type": "completion",
						},
					},
				},
			},
			// Rule 2: Only applies if type is "completion" (set by rule 1)
			{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("^/api/chat$"),
				OnRequest: []config.Operation{
					{
						MatchBody: map[string]config.PatternField{
							"type": newPatternField("^completion$"),
						},
						Merge: map[string]any{
							"completion_config": map[string]any{
								"enabled": true,
							},
						},
					},
				},
			},
		},
	}

	// Validate and compile the config
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	// Compile templates
	if err := config.CompileTemplates(cfg); err != nil {
		t.Fatalf("Template compilation failed: %v", err)
	}

	// Create POST request with JSON body (no type field)
	reqData := map[string]any{
		"model":  "llama3",
		"prompt": "Hello",
	}
	reqBody, _ := json.Marshal(reqData)
	req := httptest.NewRequest("POST", "/api/chat", bytes.NewReader(reqBody))

	// Apply modifications
	ModifyRequest(req, cfg)

	// Read and verify modified body
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("Failed to read modified body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal modified body: %v", err)
	}

	// Verify rule 1 added the type
	if typeVal, ok := result["type"].(string); !ok || typeVal != "completion" {
		t.Errorf("Expected type to be 'completion', got %v", result["type"])
	}

	// Verify rule 2 added completion_config (because rule 1 set type=completion)
	completionConfig, ok := result["completion_config"].(map[string]any)
	if !ok {
		t.Fatalf("Expected completion_config to be present, got %v", result["completion_config"])
	}

	if enabled, ok := completionConfig["enabled"].(bool); !ok || !enabled {
		t.Errorf("Expected completion_config.enabled to be true, got %v", completionConfig["enabled"])
	}
}
