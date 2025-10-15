package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Test helper functions

// newPatternField creates a PatternField for testing
func newPatternField(patterns ...string) PatternField {
	pf := PatternField{Patterns: patterns}
	pf.Validate() // Pre-compile regexes
	return pf
}

// parseConfig parses a YAML config string directly without file I/O
func parseConfig(t *testing.T, yamlContent string) (*Config, error) {
	t.Helper()

	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlContent), &cfg); err != nil {
		return nil, err
	}

	// Apply defaults
	if cfg.Proxy.Timeout == 0 {
		cfg.Proxy.Timeout = 60 * time.Second
	}

	// Validate
	if err := Validate(&cfg); err != nil {
		return nil, err
	}

	// Compile templates
	if err := CompileTemplates(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// mustParseConfig parses config and fails test on error
func mustParseConfig(t *testing.T, yamlContent string) *Config {
	t.Helper()
	cfg, err := parseConfig(t, yamlContent)
	if err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}
	return cfg
}

func TestLoad(t *testing.T) {
	// Create a temporary config file for testing
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yml")

	configContent := `
proxy:
  listen: "localhost:8081"
  target: "http://localhost:8080"
  timeout: 30s

rules:
  - methods: POST
    paths: /v1/chat
    on_request:
      - merge:
          temperature: 0.7
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath, CliOverrides{})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify basic fields
	if cfg.Proxy.Listen != "localhost:8081" {
		t.Errorf("Listen = %v, want localhost:8081", cfg.Proxy.Listen)
	}
	if cfg.Proxy.Target != "http://localhost:8080" {
		t.Errorf("Target = %v, want http://localhost:8080", cfg.Proxy.Target)
	}
	if cfg.Proxy.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Proxy.Timeout)
	}

	// Verify rules loaded and compiled
	if len(cfg.Rules) != 1 {
		t.Errorf("len(Rules) = %d, want 1", len(cfg.Rules))
	}
	if cfg.Rules[0].OpRule == nil {
		t.Error("Rule templates not compiled")
	}
}

func TestLoadWithOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yml")

	configContent := `
proxy:
  listen: "localhost:8081"
  target: "http://localhost:8080"

rules:
  - methods: POST
    paths: /v1/chat
    on_request:
      - merge:
          temperature: 0.7
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	overrides := CliOverrides{
		Listen:  "0.0.0.0:9000",
		Target:  "http://backend:5000",
		Timeout: 60 * time.Second,
		Debug:   true,
	}

	cfg, err := Load(configPath, overrides)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify overrides were applied
	if cfg.Proxy.Listen != "0.0.0.0:9000" {
		t.Errorf("Listen = %v, want 0.0.0.0:9000", cfg.Proxy.Listen)
	}
	if cfg.Proxy.Target != "http://backend:5000" {
		t.Errorf("Target = %v, want http://backend:5000", cfg.Proxy.Target)
	}
	if cfg.Proxy.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s", cfg.Proxy.Timeout)
	}
	if !cfg.Proxy.Debug {
		t.Error("Debug should be true")
	}
}

func TestLoadDefaultTimeout(t *testing.T) {
	configContent := `
proxy:
  listen: "localhost:8081"
  target: "http://localhost:8080"

rules:
  - methods: POST
    paths: /v1/chat
    on_request:
      - merge:
          temperature: 0.7
`
	cfg := mustParseConfig(t, configContent)

	// Should default to 60 seconds
	if cfg.Proxy.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s (default)", cfg.Proxy.Timeout)
	}
}

func TestLoadInvalidFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yml", CliOverrides{})
	if err == nil {
		t.Error("Load() should fail for nonexistent file")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	invalidYAML := `
proxy:
  listen: invalid yaml content
  - this is not valid
`
	_, err := parseConfig(t, invalidYAML)
	if err == nil {
		t.Error("parseConfig() should fail for invalid YAML")
	}
}

func TestLoadValidationFailure(t *testing.T) {
	// Missing required field (listen)
	configContent := `
proxy:
  target: "http://localhost:8080"

rules:
  - methods: POST
    paths: /v1/chat
    on_request:
      - merge:
          temperature: 0.7
`
	_, err := parseConfig(t, configContent)
	if err == nil {
		t.Error("parseConfig() should fail validation")
	}
	if !strings.Contains(err.Error(), "listen is required") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestLoadWithTemplates(t *testing.T) {
	configContent := `
proxy:
  listen: "localhost:8081"
  target: "http://localhost:8080"

rules:
  - methods: POST
    paths: /api/chat
    on_request:
      - template: |
          {
            "model": "{{ .model }}",
            "temperature": 0.7
          }
`
	cfg := mustParseConfig(t, configContent)

	// Verify template was compiled
	if len(cfg.Rules) != 1 {
		t.Fatalf("len(Rules) = %d, want 1", len(cfg.Rules))
	}

	rule := cfg.Rules[0]
	if rule.OpRule == nil {
		t.Fatal("OpRule should not be nil")
	}

	if len(rule.OpRule.OnRequestTemplates) != 1 {
		t.Errorf("len(OnRequestTemplates) = %d, want 1", len(rule.OpRule.OnRequestTemplates))
	}

	if rule.OpRule.OnRequestTemplates[0] == nil {
		t.Error("Compiled template should not be nil")
	}
}

func TestLoadInvalidTemplate(t *testing.T) {
	configContent := `
proxy:
  listen: "localhost:8081"
  target: "http://localhost:8080"

rules:
  - methods: POST
    paths: /api/chat
    on_request:
      - template: |
          {{ invalid template syntax
`
	_, err := parseConfig(t, configContent)
	if err == nil {
		t.Error("parseConfig() should fail with invalid template")
	}
	// Template compilation errors contain "rule X request operation Y"
	if !strings.Contains(err.Error(), "rule 0 request operation 0") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestPatternFieldUnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    PatternField
		wantErr bool
	}{
		{
			name: "single string",
			yaml: "test: POST",
			want: newPatternField("POST"),
		},
		{
			name: "array of strings",
			yaml: "test:\n  - POST\n  - GET",
			want: newPatternField("POST", "GET"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result struct {
				Test PatternField `yaml:"test"`
			}

			err := yaml.Unmarshal([]byte(tt.yaml), &result)

			if err != nil {
				t.Errorf("UnmarshalYAML() error = %v", err)
				return
			}

			if result.Test.Len() != tt.want.Len() {
				t.Errorf("len = %d, want %d", result.Test.Len(), tt.want.Len())
				return
			}
			for i, pattern := range result.Test.Patterns {
				if pattern != tt.want.Patterns[i] {
					t.Errorf("item %d = %v, want %v", i, pattern, tt.want.Patterns[i])
				}
			}
		})
	}
}
