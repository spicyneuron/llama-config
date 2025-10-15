package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/spicyneuron/llama-config-proxy/config"
	"github.com/spicyneuron/llama-config-proxy/proxy"
)

// loadTestFixture reads a JSON fixture file from testdata/fixtures
func loadTestFixture(t *testing.T, filename string) map[string]any {
	t.Helper()

	path := filepath.Join("testdata", "fixtures", filename)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read fixture %s: %v", filename, err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to parse fixture %s: %v", filename, err)
	}

	return result
}

// loadTestConfig loads a config file from examples directory
func loadTestConfig(t *testing.T, filename string) *config.Config {
	t.Helper()

	path := filepath.Join("examples", filename)
	cfg, err := config.Load([]string{path}, config.CliOverrides{})
	if err != nil {
		t.Fatalf("Failed to load config %s: %v", filename, err)
	}

	return cfg
}

func TestOllamaChatRequestToOpenAI(t *testing.T) {
	// Load Ollama chat request fixture
	ollamaRequest := loadTestFixture(t, "ollama-chat-request.json")

	// Load the complete Ollama transformation config
	cfg := loadTestConfig(t, "ollama.yml")

	// Create a test request
	bodyBytes, _ := json.Marshal(ollamaRequest)
	req := httptest.NewRequest("POST", "/api/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Apply the transformation
	proxy.ModifyRequest(req, cfg)

	// Read the modified request body
	modifiedBody, _ := io.ReadAll(req.Body)
	var result map[string]any
	if err := json.Unmarshal(modifiedBody, &result); err != nil {
		t.Fatalf("Failed to parse modified request: %v", err)
	}

	// Verify transformations
	t.Run("path rewrite", func(t *testing.T) {
		if req.URL.Path != "/v1/chat/completions" {
			t.Errorf("Expected path /v1/chat/completions, got %s", req.URL.Path)
		}
	})

	t.Run("model preserved", func(t *testing.T) {
		if result["model"] != "llama3.2" {
			t.Errorf("Expected model llama3.2, got %v", result["model"])
		}
	})

	t.Run("messages preserved", func(t *testing.T) {
		messages, ok := result["messages"].([]any)
		if !ok || len(messages) == 0 {
			t.Errorf("Expected messages array, got %v", result["messages"])
		}
	})

	t.Run("options flattened", func(t *testing.T) {
		// Check that options.temperature became top-level temperature
		if result["temperature"] != 0.7 {
			t.Errorf("Expected temperature 0.7, got %v", result["temperature"])
		}

		if result["top_p"] != 0.9 {
			t.Errorf("Expected top_p 0.9, got %v", result["top_p"])
		}

		// options should not exist in output
		if _, exists := result["options"]; exists {
			t.Error("Expected options field to be removed")
		}
	})

	t.Run("stream preserved", func(t *testing.T) {
		if result["stream"] != false {
			t.Errorf("Expected stream false, got %v", result["stream"])
		}
	})
}

func TestOllamaChatRequestWithFormatToOpenAI(t *testing.T) {
	// Load Ollama structured output request fixture
	ollamaRequest := loadTestFixture(t, "ollama-chat-request-structured.json")

	cfg := loadTestConfig(t, "ollama.yml")

	bodyBytes, _ := json.Marshal(ollamaRequest)
	req := httptest.NewRequest("POST", "/api/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	proxy.ModifyRequest(req, cfg)

	modifiedBody, _ := io.ReadAll(req.Body)
	var result map[string]any
	json.Unmarshal(modifiedBody, &result)

	t.Run("format mapped to response_format", func(t *testing.T) {
		responseFormat, ok := result["response_format"].(map[string]any)
		if !ok {
			t.Fatalf("Expected response_format to be a map, got %T", result["response_format"])
		}

		if responseFormat["type"] != "json_object" {
			t.Errorf("Expected response_format.type to be json_object, got %v", responseFormat["type"])
		}

		// format field should not exist
		if _, exists := result["format"]; exists {
			t.Error("Expected format field to be removed")
		}
	})
}

func TestOllamaChatRequestWithToolsToOpenAI(t *testing.T) {
	// Load Ollama tools request fixture
	ollamaRequest := loadTestFixture(t, "ollama-chat-request-tools.json")

	cfg := loadTestConfig(t, "ollama.yml")

	bodyBytes, _ := json.Marshal(ollamaRequest)
	req := httptest.NewRequest("POST", "/api/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	proxy.ModifyRequest(req, cfg)

	modifiedBody, _ := io.ReadAll(req.Body)
	var result map[string]any
	json.Unmarshal(modifiedBody, &result)

	t.Run("tools preserved", func(t *testing.T) {
		tools, ok := result["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Fatalf("Expected tools array, got %v", result["tools"])
		}

		tool := tools[0].(map[string]any)
		if tool["type"] != "function" {
			t.Errorf("Expected tool type function, got %v", tool["type"])
		}
	})
}

func TestOpenAIResponseToOllamaChat(t *testing.T) {
	// Load OpenAI response fixture
	openaiResponse := loadTestFixture(t, "openai-chat-response.json")

	cfg := loadTestConfig(t, "ollama.yml")

	// Create a simple request to trigger the response transformation
	req := httptest.NewRequest("POST", "/api/chat", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")

	// Apply request transformation to get the right path
	proxy.ModifyRequest(req, cfg)

	// Create a matching rule in context
	matchingRule := &cfg.Rules[0]
	ctx := req.Context()
	ctx = context.WithValue(ctx, "matched_rule", matchingRule)
	req = req.WithContext(ctx)

	// Create a response
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(mustMarshalJSON(openaiResponse))),
		Request:    req,
	}

	// Apply response transformation
	if err := proxy.ModifyResponse(resp, cfg); err != nil {
		t.Fatalf("ModifyResponse failed: %v", err)
	}

	// Read transformed response
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to parse transformed response: %v", err)
	}

	t.Run("message unwrapped", func(t *testing.T) {
		message, ok := result["message"].(map[string]any)
		if !ok {
			t.Fatalf("Expected message to be a map, got %T", result["message"])
		}

		if message["role"] != "assistant" {
			t.Errorf("Expected message.role to be assistant, got %v", message["role"])
		}

		content, ok := message["content"].(string)
		if !ok || content == "" {
			t.Error("Expected message.content to be a non-empty string")
		}

		// choices should not exist
		if _, exists := result["choices"]; exists {
			t.Error("Expected choices field to be removed")
		}
	})

	t.Run("ollama metadata added", func(t *testing.T) {
		if result["done"] != true {
			t.Errorf("Expected done to be true, got %v", result["done"])
		}

		if result["done_reason"] != "stop" {
			t.Errorf("Expected done_reason to be stop, got %v", result["done_reason"])
		}

		if _, exists := result["created_at"]; !exists {
			t.Error("Expected created_at field")
		}
	})

	t.Run("usage tokens mapped", func(t *testing.T) {
		if result["prompt_eval_count"] != float64(26) {
			t.Errorf("Expected prompt_eval_count 26, got %v", result["prompt_eval_count"])
		}

		if result["eval_count"] != float64(48) {
			t.Errorf("Expected eval_count 48, got %v", result["eval_count"])
		}

		// usage should not exist
		if _, exists := result["usage"]; exists {
			t.Error("Expected usage field to be removed")
		}
	})

	t.Run("model preserved", func(t *testing.T) {
		if result["model"] == nil {
			t.Error("Expected model field to be preserved")
		}
	})
}

func TestOpenAIModelsToOllamaTags(t *testing.T) {
	// Load OpenAI models response fixture
	openaiModels := loadTestFixture(t, "openai-models-response.json")

	cfg := loadTestConfig(t, "ollama.yml")

	// Create a request
	req := httptest.NewRequest("GET", "/api/tags", nil)

	// Apply request transformation (path rewrite)
	proxy.ModifyRequest(req, cfg)

	t.Run("path rewrite", func(t *testing.T) {
		if req.URL.Path != "/v1/models" {
			t.Errorf("Expected path /v1/models, got %s", req.URL.Path)
		}
	})

	// Create a matching rule in context for response
	matchingRule := &cfg.Rules[1] // Models rule is second
	ctx := req.Context()
	ctx = context.WithValue(ctx, "matched_rule", matchingRule)
	req = req.WithContext(ctx)

	// Create response with OpenAI models
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(mustMarshalJSON(openaiModels))),
		Request:    req,
	}

	// Apply response transformation
	if err := proxy.ModifyResponse(resp, cfg); err != nil {
		t.Fatalf("ModifyResponse failed: %v", err)
	}

	// Read transformed response
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to parse transformed response: %v", err)
	}

	t.Run("models array created", func(t *testing.T) {
		models, ok := result["models"].([]any)
		if !ok {
			t.Fatalf("Expected models to be an array, got %T", result["models"])
		}

		if len(models) != 3 {
			t.Errorf("Expected 3 models, got %d", len(models))
		}

		// object field should not exist
		if _, exists := result["object"]; exists {
			t.Error("Expected object field to be removed")
		}

		// data field should not exist
		if _, exists := result["data"]; exists {
			t.Error("Expected data field to be removed")
		}
	})

	t.Run("model names mapped from ids", func(t *testing.T) {
		models := result["models"].([]any)
		firstModel := models[0].(map[string]any)

		if firstModel["name"] != "gpt-4" {
			t.Errorf("Expected first model name to be gpt-4, got %v", firstModel["name"])
		}

		// Verify Ollama model structure
		if _, exists := firstModel["modified_at"]; !exists {
			t.Error("Expected modified_at field in model")
		}

		if _, exists := firstModel["details"]; !exists {
			t.Error("Expected details field in model")
		}

		// Verify details structure matches Ollama format
		details, ok := firstModel["details"].(map[string]any)
		if !ok {
			t.Fatalf("Expected details to be a map, got %T", firstModel["details"])
		}

		expectedFields := []string{"format", "family", "families", "parameter_size", "quantization_level"}
		for _, field := range expectedFields {
			if _, exists := details[field]; !exists {
				t.Errorf("Expected details.%s field to exist", field)
			}
		}
	})
}

// Helper function for tests
func mustMarshalJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
