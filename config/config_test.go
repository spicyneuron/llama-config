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

	cfg, err := Load([]string{configPath}, CliOverrides{})
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

	cfg, err := Load([]string{configPath}, overrides)
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
	_, err := Load([]string{"/nonexistent/config.yml"}, CliOverrides{})
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

func TestTemplateExecutionTracking(t *testing.T) {
	// Test that template execution properly tracks what was applied
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
            "temperature": 0.8,
            "max_tokens": 100
          }
`
	cfg := mustParseConfig(t, configContent)

	// Simulate processing a request with the template
	data := map[string]any{
		"model":    "llama3",
		"messages": []any{map[string]string{"role": "user", "content": "test"}},
	}
	headers := make(map[string]string)

	modified, appliedValues := ProcessRequest(data, headers, cfg.Rules[0].OpRule)

	if !modified {
		t.Error("Expected template to be applied")
	}

	// The appliedValues should contain the actual template output, not just "<applied>"
	if len(appliedValues) == 0 {
		t.Error("Expected appliedValues to be populated")
	}

	// Check that appliedValues contains the actual fields from the template
	if _, ok := appliedValues["model"]; !ok {
		t.Error("Expected appliedValues to contain 'model' field")
	}

	if _, ok := appliedValues["temperature"]; !ok {
		t.Error("Expected appliedValues to contain 'temperature' field")
	}

	if _, ok := appliedValues["max_tokens"]; !ok {
		t.Error("Expected appliedValues to contain 'max_tokens' field")
	}

	// The old buggy behavior would have been:
	// appliedValues = {"template": "<applied>"}
	// So let's explicitly check it's NOT that
	if val, ok := appliedValues["template"]; ok && val == "<applied>" {
		t.Error("appliedValues should not contain the marker '<applied>', but actual template output")
	}

	// Verify the actual data was also modified correctly
	if model, ok := data["model"].(string); !ok || model != "llama3" {
		t.Errorf("Expected model to be 'llama3', got %v", data["model"])
	}

	if temp, ok := data["temperature"].(float64); !ok || temp != 0.8 {
		t.Errorf("Expected temperature to be 0.8, got %v", data["temperature"])
	}

	if tokens, ok := data["max_tokens"].(float64); !ok || tokens != 100 {
		t.Errorf("Expected max_tokens to be 100, got %v", data["max_tokens"])
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

func TestLoadMultipleConfigs(t *testing.T) {
	tmpDir := t.TempDir()

	baseConfig := `
proxy:
  listen: "localhost:8080"
  target: "http://localhost:3000"

rules:
  - methods: GET
    paths: /health
    on_request:
      - merge:
          from: "base-config"
`
	baseConfigPath := filepath.Join(tmpDir, "base.yml")
	if err := os.WriteFile(baseConfigPath, []byte(baseConfig), 0644); err != nil {
		t.Fatalf("Failed to write base config: %v", err)
	}

	rulesConfig := `
rules:
  - methods: POST
    paths: /api/.*
    on_request:
      - merge:
          from: "rules-config"
`
	rulesConfigPath := filepath.Join(tmpDir, "rules.yml")
	if err := os.WriteFile(rulesConfigPath, []byte(rulesConfig), 0644); err != nil {
		t.Fatalf("Failed to write rules config: %v", err)
	}

	cfg, err := Load([]string{baseConfigPath, rulesConfigPath}, CliOverrides{})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Proxy.Listen != "localhost:8080" {
		t.Errorf("Listen = %v, want localhost:8080", cfg.Proxy.Listen)
	}

	if len(cfg.Rules) != 2 {
		t.Fatalf("len(Rules) = %d, want 2", len(cfg.Rules))
	}

	if cfg.Rules[0].Methods.Patterns[0] != "GET" {
		t.Errorf("Rules[0].Methods = %v, want GET", cfg.Rules[0].Methods.Patterns[0])
	}

	if cfg.Rules[1].Methods.Patterns[0] != "POST" {
		t.Errorf("Rules[1].Methods = %v, want POST", cfg.Rules[1].Methods.Patterns[0])
	}
}

func TestLoadProxyMerge(t *testing.T) {
	tmpDir := t.TempDir()

	config1 := `
proxy:
  listen: "localhost:8080"

rules:
  - methods: GET
    paths: /health
    on_request:
      - merge:
          from: "config1"
`
	config1Path := filepath.Join(tmpDir, "config1.yml")
	if err := os.WriteFile(config1Path, []byte(config1), 0644); err != nil {
		t.Fatalf("Failed to write config1: %v", err)
	}

	config2 := `
proxy:
  target: "http://localhost:3000"
  debug: true

rules:
  - methods: POST
    paths: /data
    on_request:
      - merge:
          from: "config2"
`
	config2Path := filepath.Join(tmpDir, "config2.yml")
	if err := os.WriteFile(config2Path, []byte(config2), 0644); err != nil {
		t.Fatalf("Failed to write config2: %v", err)
	}

	cfg, err := Load([]string{config1Path, config2Path}, CliOverrides{})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Proxy.Listen != "localhost:8080" {
		t.Errorf("Listen = %v, want localhost:8080 (from config1, not overridden)", cfg.Proxy.Listen)
	}

	if cfg.Proxy.Target != "http://localhost:3000" {
		t.Errorf("Target = %v, want http://localhost:3000 (from config2)", cfg.Proxy.Target)
	}

	if !cfg.Proxy.Debug {
		t.Error("Debug should be true (from config2)")
	}

	if len(cfg.Rules) != 2 {
		t.Fatalf("len(Rules) = %d, want 2", len(cfg.Rules))
	}
}

func TestLoadProxyOverride(t *testing.T) {
	tmpDir := t.TempDir()

	base := `
proxy:
  listen: "localhost:8080"
  target: "http://localhost:3000"
  timeout: 30s

rules:
  - methods: GET
    paths: /health
    on_request:
      - merge:
          from: "base"
`
	basePath := filepath.Join(tmpDir, "base.yml")
	if err := os.WriteFile(basePath, []byte(base), 0644); err != nil {
		t.Fatalf("Failed to write base: %v", err)
	}

	override := `
proxy:
  listen: "localhost:9000"
  debug: true
`
	overridePath := filepath.Join(tmpDir, "override.yml")
	if err := os.WriteFile(overridePath, []byte(override), 0644); err != nil {
		t.Fatalf("Failed to write override: %v", err)
	}

	cfg, err := Load([]string{basePath, overridePath}, CliOverrides{})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Proxy.Listen != "localhost:9000" {
		t.Errorf("Listen = %v, want localhost:9000 (overridden by later config)", cfg.Proxy.Listen)
	}

	if cfg.Proxy.Target != "http://localhost:3000" {
		t.Errorf("Target = %v, want http://localhost:3000 (from base, not overridden)", cfg.Proxy.Target)
	}

	if cfg.Proxy.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s (from base, not overridden)", cfg.Proxy.Timeout)
	}

	if !cfg.Proxy.Debug {
		t.Error("Debug should be true (from override)")
	}
}

func TestLoadThreeConfigs(t *testing.T) {
	tmpDir := t.TempDir()

	configs := []struct {
		name    string
		content string
		method  string
	}{
		{"config1.yml", `
rules:
  - methods: GET
    paths: /health
    on_request:
      - merge:
          from: "config1"
`, "GET"},
		{"config2.yml", `
rules:
  - methods: POST
    paths: /data
    on_request:
      - merge:
          from: "config2"
`, "POST"},
		{"config3.yml", `
proxy:
  listen: "localhost:9000"
  target: "http://localhost:3000"

rules:
  - methods: DELETE
    paths: /remove
    on_request:
      - merge:
          from: "config3"
`, "DELETE"},
	}

	var configPaths []string
	for _, cfg := range configs {
		path := filepath.Join(tmpDir, cfg.name)
		if err := os.WriteFile(path, []byte(cfg.content), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", cfg.name, err)
		}
		configPaths = append(configPaths, path)
	}

	mergedCfg, err := Load(configPaths, CliOverrides{})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if mergedCfg.Proxy.Listen != "localhost:9000" {
		t.Errorf("Listen = %v, want localhost:9000", mergedCfg.Proxy.Listen)
	}

	if len(mergedCfg.Rules) != 3 {
		t.Fatalf("len(Rules) = %d, want 3", len(mergedCfg.Rules))
	}

	expectedMethods := []string{"GET", "POST", "DELETE"}
	for i, expected := range expectedMethods {
		if mergedCfg.Rules[i].Methods.Patterns[0] != expected {
			t.Errorf("Rules[%d].Methods = %v, want %v", i, mergedCfg.Rules[i].Methods.Patterns[0], expected)
		}
	}
}

func TestLoadNonexistent(t *testing.T) {
	tmpDir := t.TempDir()

	validConfig := `
proxy:
  listen: "localhost:9000"
  target: "http://localhost:3000"

rules:
  - methods: GET
    paths: /test
    on_request:
      - merge:
          from: "valid"
`
	validConfigPath := filepath.Join(tmpDir, "valid.yml")
	if err := os.WriteFile(validConfigPath, []byte(validConfig), 0644); err != nil {
		t.Fatalf("Failed to write valid config: %v", err)
	}

	_, err := Load([]string{validConfigPath, "nonexistent.yml"}, CliOverrides{})
	if err == nil {
		t.Fatal("Load() should fail when one config doesn't exist")
	}

	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Errorf("Error should mention read failure, got: %v", err)
	}
}

func TestLoadEmpty(t *testing.T) {
	_, err := Load([]string{}, CliOverrides{})
	if err == nil {
		t.Fatal("Load() should fail with empty config list")
	}

	if !strings.Contains(err.Error(), "at least one config file required") {
		t.Errorf("Error should mention empty config list, got: %v", err)
	}
}

func TestResolvePath(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		baseDir  string
		want     string
	}{
		{
			name:     "empty path returns empty",
			filePath: "",
			baseDir:  "/some/dir",
			want:     "",
		},
		{
			name:     "absolute path is preserved",
			filePath: "/absolute/path/cert.pem",
			baseDir:  "/config/dir",
			want:     "/absolute/path/cert.pem",
		},
		{
			name:     "relative path joined with base",
			filePath: "cert.pem",
			baseDir:  "/config/dir",
			want:     "/config/dir/cert.pem",
		},
		{
			name:     "relative path with subdirectory",
			filePath: "ssl/cert.pem",
			baseDir:  "/config/dir",
			want:     "/config/dir/ssl/cert.pem",
		},
		{
			name:     "relative path with parent reference",
			filePath: "../certs/cert.pem",
			baseDir:  "/config/dir",
			want:     "/config/certs/cert.pem",
		},
		{
			name:     "current dir reference",
			filePath: "./cert.pem",
			baseDir:  "/config/dir",
			want:     "/config/dir/cert.pem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePath(tt.filePath, tt.baseDir)
			// Normalize paths for comparison (handles OS differences)
			want := filepath.Clean(tt.want)
			got = filepath.Clean(got)

			if got != want {
				t.Errorf("ResolvePath(%q, %q) = %q, want %q", tt.filePath, tt.baseDir, got, want)
			}
		})
	}
}
