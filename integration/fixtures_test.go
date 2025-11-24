package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestFixturesValidJSON verifies all JSON fixtures are valid
func TestFixturesValidJSON(t *testing.T) {
	fixturesDir := filepath.Join("testdata", "fixtures")

	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatalf("Failed to read fixtures directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(fixturesDir, entry.Name()))
			if err != nil {
				t.Fatalf("Failed to read fixture: %v", err)
			}

			var result any
			if err := json.Unmarshal(data, &result); err != nil {
				t.Errorf("Invalid JSON in %s: %v", entry.Name(), err)
			}
		})
	}
}

// TestOpenAIChatFixtures validates OpenAI chat request/response structure
func TestOpenAIChatFixtures(t *testing.T) {
	tests := []struct {
		name       string
		file       string
		hasModel   bool
		hasChoices bool
		hasUsage   bool
	}{
		{
			name:     "openai chat request",
			file:     "fixtures/openai-chat-request.json",
			hasModel: true,
		},
		{
			name:       "openai chat response",
			file:       "fixtures/openai-chat-response.json",
			hasModel:   true,
			hasChoices: true,
			hasUsage:   true,
		},
		{
			name:       "openai streaming chunk",
			file:       "fixtures/openai-chat-streaming-chunk.json",
			hasModel:   true,
			hasChoices: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join("testdata", tt.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("Failed to read fixture %s: %v", path, err)
			}

			var result map[string]any
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatalf("Failed to parse JSON: %v", err)
			}

			if tt.hasModel {
				if _, ok := result["model"].(string); !ok {
					t.Error("Missing or invalid 'model' field")
				}
			}

			if tt.hasChoices {
				if choices, ok := result["choices"].([]any); !ok {
					t.Error("Missing or invalid 'choices' field")
				} else if len(choices) > 0 {
					choice := choices[0].(map[string]any)
					// Check for either 'message' (non-streaming) or 'delta' (streaming)
					if _, hasMessage := choice["message"]; !hasMessage {
						if _, hasDelta := choice["delta"]; !hasDelta {
							t.Error("Choice missing both 'message' and 'delta' fields")
						}
					}
				}
			}

			if tt.hasUsage {
				if usage, ok := result["usage"].(map[string]any); !ok {
					t.Error("Missing or invalid 'usage' field")
				} else {
					if _, ok := usage["prompt_tokens"]; !ok {
						t.Error("Missing 'prompt_tokens' in usage")
					}
					if _, ok := usage["completion_tokens"]; !ok {
						t.Error("Missing 'completion_tokens' in usage")
					}
				}
			}
		})
	}
}

// TestModelListFixtures validates model list response structures
func TestModelListFixtures(t *testing.T) {
	t.Run("openai models", func(t *testing.T) {
		path := filepath.Join("testdata", "fixtures", "openai-models-response.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Failed to read fixture %s: %v", path, err)
		}

		var result map[string]any
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}

		data_field, ok := result["data"].([]any)
		if !ok {
			t.Fatal("Missing or invalid 'data' field")
		}

		if len(data_field) == 0 {
			t.Error("Expected at least one model")
		}

		for i, m := range data_field {
			model := m.(map[string]any)
			if _, ok := model["id"].(string); !ok {
				t.Errorf("Model %d missing 'id' field", i)
			}
		}
	})
}
