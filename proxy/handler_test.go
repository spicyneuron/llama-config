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
