package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

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

	cfg, _, err := Load([]string{configPath}, CliOverrides{})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify basic fields
	if cfg.Proxies[0].Listen != "localhost:8081" {
		t.Errorf("Listen = %v, want localhost:8081", cfg.Proxies[0].Listen)
	}
	if cfg.Proxies[0].Target != "http://localhost:8080" {
		t.Errorf("Target = %v, want http://localhost:8080", cfg.Proxies[0].Target)
	}
	if cfg.Proxies[0].Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Proxies[0].Timeout)
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

	cfg, _, err := Load([]string{configPath}, overrides)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify overrides were applied
	if cfg.Proxies[0].Listen != "0.0.0.0:9000" {
		t.Errorf("Listen = %v, want 0.0.0.0:9000", cfg.Proxies[0].Listen)
	}
	if cfg.Proxies[0].Target != "http://backend:5000" {
		t.Errorf("Target = %v, want http://backend:5000", cfg.Proxies[0].Target)
	}
	if cfg.Proxies[0].Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s", cfg.Proxies[0].Timeout)
	}
	if !cfg.Proxies[0].Debug {
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
	if cfg.Proxies[0].Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s (default)", cfg.Proxies[0].Timeout)
	}
}

func TestLoadWithAdditionalProxies(t *testing.T) {
	configContent := `
proxy:
  - listen: "localhost:8081"
    target: "http://localhost:8080"
  - listen: "localhost:8082"
    target: "http://localhost:8083"

rules:
  - methods: POST
    paths: /v1/chat
    on_request:
      - merge:
          temperature: 0.7
`
	cfg := mustParseConfig(t, configContent)

	if len(cfg.Proxies) != 2 {
		t.Fatalf("len(Proxies) = %d, want 2", len(cfg.Proxies))
	}

	if cfg.Proxies[0].Listen != "localhost:8081" {
		t.Errorf("Primary proxy listen = %v, want localhost:8081", cfg.Proxies[0].Listen)
	}

	if cfg.Proxies[0].Target != "http://localhost:8080" {
		t.Errorf("Primary proxy target = %v, want http://localhost:8080", cfg.Proxies[0].Target)
	}

	if cfg.Proxies[1].Listen != "localhost:8082" {
		t.Errorf("Second proxy listen = %v, want localhost:8082", cfg.Proxies[1].Listen)
	}

	if cfg.Proxies[1].Target != "http://localhost:8083" {
		t.Errorf("Second proxy target = %v, want http://localhost:8083", cfg.Proxies[1].Target)
	}

	if cfg.Proxies[0].Timeout != 60*time.Second || cfg.Proxies[1].Timeout != 60*time.Second {
		t.Errorf("Expected default timeout to be applied to all proxies, got %v and %v", cfg.Proxies[0].Timeout, cfg.Proxies[1].Timeout)
	}
}

func TestLoadInvalidFile(t *testing.T) {
	_, _, err := Load([]string{"/nonexistent/config.yml"}, CliOverrides{})
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
	if !strings.Contains(err.Error(), "proxy[0].listen is required") {
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

	modified, appliedValues := ProcessRequest(data, headers, cfg.Rules[0].OpRule, 0, "", "")

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
  - listen: "localhost:8081"
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

	cfg, _, err := Load([]string{baseConfigPath, rulesConfigPath}, CliOverrides{})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Proxies[0].Listen != "localhost:8080" {
		t.Errorf("Listen = %v, want localhost:8080", cfg.Proxies[0].Listen)
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
  target: "http://localhost:3000"

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
  listen: "localhost:8081"
  target: "http://localhost:3001"
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

	cfg, _, err := Load([]string{config1Path, config2Path}, CliOverrides{})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if len(cfg.Proxies) != 2 {
		t.Fatalf("expected two proxies, got %d", len(cfg.Proxies))
	}

	if cfg.Proxies[0].Listen != "localhost:8080" || cfg.Proxies[0].Target != "http://localhost:3000" {
		t.Errorf("first proxy = %+v, want listen localhost:8080 target http://localhost:3000", cfg.Proxies[0])
	}

	if cfg.Proxies[1].Listen != "localhost:8081" || cfg.Proxies[1].Target != "http://localhost:3001" || !cfg.Proxies[1].Debug {
		t.Errorf("second proxy = %+v, want listen localhost:8081 target http://localhost:3001 debug true", cfg.Proxies[1])
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

	cfg, _, err := Load([]string{basePath}, CliOverrides{
		Listen: "localhost:9000",
		Debug:  true,
	})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Proxies[0].Listen != "localhost:9000" {
		t.Errorf("Listen = %v, want localhost:9000 (overridden by CLI)", cfg.Proxies[0].Listen)
	}

	if cfg.Proxies[0].Target != "http://localhost:3000" {
		t.Errorf("Target = %v, want http://localhost:3000 (from base, not overridden)", cfg.Proxies[0].Target)
	}

	if cfg.Proxies[0].Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s (from base, not overridden)", cfg.Proxies[0].Timeout)
	}

	if !cfg.Proxies[0].Debug {
		t.Error("Debug should be true (from CLI override)")
	}
}

func TestLoadMultipleProxiesWithOverridesFail(t *testing.T) {
	tmpDir := t.TempDir()

	content := `
proxy:
  - listen: "localhost:8080"
    target: "http://localhost:3000"
  - listen: "localhost:8081"
    target: "http://localhost:3001"

rules:
  - methods: GET
    paths: /health
    on_request:
      - merge:
          source: "proxies"
`
	configPath := filepath.Join(tmpDir, "multi.yml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	_, _, err := Load([]string{configPath}, CliOverrides{Listen: "127.0.0.1:9999"})
	if err == nil {
		t.Fatal("Load() should fail when CLI overrides are used with multiple proxies")
	}

	if !strings.Contains(err.Error(), "CLI overrides for listen/target/timeout/ssl") {
		t.Fatalf("Unexpected error: %v", err)
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

	mergedCfg, _, err := Load(configPaths, CliOverrides{})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
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

func TestLoadProxySpecificRulesOverrideShared(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "main.yml")

	configContent := `
proxy:
  - listen: "localhost:8081"
    target: "http://localhost:8080"
  - listen: "localhost:8082"
    target: "http://localhost:8080"
    rules:
      - methods: POST
        paths: ^/special$
        on_request:
          - merge:
              from: "proxy-override"

rules:
  - methods: POST
    paths: ^/special$
    on_request:
      - merge:
          from: "shared-rule"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, _, err := Load([]string{configPath}, CliOverrides{})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Proxies) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(cfg.Proxies))
	}

	// First proxy should inherit shared rules
	if got := cfg.Proxies[0].Rules; len(got) != 1 || got[0].OnRequest[0].Merge["from"] != "shared-rule" {
		t.Fatalf("proxy[0] rules not inherited correctly: %+v", got)
	}

	// Second proxy should keep its own
	if got := cfg.Proxies[1].Rules; len(got) != 1 || got[0].OnRequest[0].Merge["from"] != "proxy-override" {
		t.Fatalf("proxy[1] rules not preserved: %+v", got)
	}
}

func TestLoadIncludesAreExpanded(t *testing.T) {
	tmpDir := t.TempDir()

	sharedRules := `
- methods: POST
  paths: ^/included$
  on_request:
    - merge:
        marker: "included"
`
	sharedPath := filepath.Join(tmpDir, "shared_rules.yml")
	if err := os.WriteFile(sharedPath, []byte(sharedRules), 0644); err != nil {
		t.Fatalf("Failed to write shared rules: %v", err)
	}

	configContent := fmt.Sprintf(`
proxy:
  - listen: "localhost:8081"
    target: "http://localhost:8080"
    rules:
      - include: %s

rules:
  - include: %s
`, sharedPath, sharedPath)

	configPath := filepath.Join(tmpDir, "main.yml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, _, err := Load([]string{configPath}, CliOverrides{})
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if len(cfg.Proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(cfg.Proxies))
	}

	if len(cfg.Proxies[0].Rules) != 1 {
		t.Fatalf("proxy rules include not expanded, got %d", len(cfg.Proxies[0].Rules))
	}
	if cfg.Proxies[0].Rules[0].OnRequest[0].Merge["marker"] != "included" {
		t.Errorf("expected proxy rule from include, got %+v", cfg.Proxies[0].Rules[0].OnRequest[0].Merge)
	}

	if len(cfg.Rules) != 1 {
		t.Fatalf("shared rules include not expanded, got %d", len(cfg.Rules))
	}
	if cfg.Rules[0].OnRequest[0].Merge["marker"] != "included" {
		t.Errorf("expected shared rule from include, got %+v", cfg.Rules[0].OnRequest[0].Merge)
	}
}

func TestLoadOperationIncludesAreExpanded(t *testing.T) {
	tmpDir := t.TempDir()

	requestOps := filepath.Join(tmpDir, "request_ops.yml")
	if err := os.WriteFile(requestOps, []byte(`
- merge:
    marker: "request-include"
- delete:
    - drop_me
`), 0644); err != nil {
		t.Fatalf("Failed to write request ops include: %v", err)
	}

	responseOps := filepath.Join(tmpDir, "response_ops.yml")
	if err := os.WriteFile(responseOps, []byte(`
- default:
    marker: "response-include"
`), 0644); err != nil {
		t.Fatalf("Failed to write response ops include: %v", err)
	}

	configPath := filepath.Join(tmpDir, "main.yml")
	configContent := fmt.Sprintf(`
proxy:
  listen: "localhost:8081"
  target: "http://localhost:8080"

rules:
  - methods: POST
    paths: ^/chat$
    on_request:
      - include: %s
    on_response:
      - include: %s
`, filepath.Base(requestOps), filepath.Base(responseOps))

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, watched, err := Load([]string{configPath}, CliOverrides{})
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if got := cfg.Rules[0].OnRequest; len(got) != 2 {
		t.Fatalf("expected two request operations from include, got %d", len(got))
	} else {
		if got[0].Merge["marker"] != "request-include" {
			t.Fatalf("expected merged marker from include, got %+v", got[0].Merge)
		}
		if len(got[1].Delete) != 1 || got[1].Delete[0] != "drop_me" {
			t.Fatalf("expected delete from include, got %+v", got[1].Delete)
		}
	}

	if got := cfg.Rules[0].OnResponse; len(got) != 1 {
		t.Fatalf("expected one response operation from include, got %d", len(got))
	} else if got[0].Default["marker"] != "response-include" {
		t.Fatalf("expected default marker from include, got %+v", got[0].Default)
	}

	containsBase := func(paths []string, target string) bool {
		base := filepath.Base(target)
		for _, p := range paths {
			if filepath.Base(p) == base {
				return true
			}
		}
		return false
	}

	if !containsBase(watched, requestOps) || !containsBase(watched, responseOps) {
		t.Fatalf("expected includes to be watched, got %v", watched)
	}
}

func TestLoadOperationIncludesFromMappingStyle(t *testing.T) {
	tmpDir := t.TempDir()

	requestOps := filepath.Join(tmpDir, "request_ops.yml")
	if err := os.WriteFile(requestOps, []byte(`
- stop: true
  merge:
    marker: "from-request-mapping"
`), 0644); err != nil {
		t.Fatalf("Failed to write request ops include: %v", err)
	}

	responseOps := filepath.Join(tmpDir, "response_ops.yml")
	if err := os.WriteFile(responseOps, []byte(`
- merge:
    marker: "from-response-mapping"
`), 0644); err != nil {
		t.Fatalf("Failed to write response ops include: %v", err)
	}

	configPath := filepath.Join(tmpDir, "main.yml")
	configContent := fmt.Sprintf(`
proxy:
  listen: "localhost:8081"
  target: "http://localhost:8080"

rules:
  - methods: POST
    paths: ^/chat$
    on_request:
      include: %s
    on_response:
      include: %s
`, filepath.Base(requestOps), filepath.Base(responseOps))

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, _, err := Load([]string{configPath}, CliOverrides{})
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if got := cfg.Rules[0].OnRequest; len(got) != 1 || !got[0].Stop || got[0].Merge["marker"] != "from-request-mapping" {
		t.Fatalf("expected stop operation from mapping-style include, got %+v", got)
	}

	if got := cfg.Rules[0].OnResponse; len(got) != 1 || got[0].Merge["marker"] != "from-response-mapping" {
		t.Fatalf("expected merge from response mapping include, got %+v", got)
	}
}

func TestLoadIncludeMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	configContent := `
proxy:
  listen: "localhost:8081"
  target: "http://localhost:8080"

rules:
  - include: does_not_exist.yml
`
	configPath := filepath.Join(tmpDir, "main.yml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	_, _, err := Load([]string{configPath}, CliOverrides{})
	if err == nil || !strings.Contains(err.Error(), "failed to read include file") {
		t.Fatalf("expected include read error, got %v", err)
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

	_, _, err := Load([]string{validConfigPath, "nonexistent.yml"}, CliOverrides{})
	if err == nil {
		t.Fatal("Load() should fail when one config doesn't exist")
	}

	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Errorf("Error should mention read failure, got: %v", err)
	}
}

func TestLoadDuplicateListenersAcrossIncludes(t *testing.T) {
	tmpDir := t.TempDir()
	include := filepath.Join(tmpDir, "include.yml")
	base := filepath.Join(tmpDir, "config.yml")

	if err := os.WriteFile(include, []byte(`
listen: "localhost:8081"
target: "http://localhost:9001"
`), 0644); err != nil {
		t.Fatalf("Failed to write include: %v", err)
	}

	if err := os.WriteFile(base, []byte(`
proxy:
  - listen: "localhost:8081"
    target: "http://localhost:9000"
  - include: "include.yml"
rules:
  - methods: GET
    paths: /.*
    on_request:
      - merge:
          ok: true
`), 0644); err != nil {
		t.Fatalf("Failed to write base config: %v", err)
	}

	_, _, err := Load([]string{base}, CliOverrides{})
	if err == nil {
		t.Fatal("Load() should fail when duplicate listeners are present across includes")
	}
	if !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("Expected duplicate listener error, got: %v", err)
	}
}

func TestLoadEmpty(t *testing.T) {
	_, _, err := Load([]string{}, CliOverrides{})
	if err == nil {
		t.Fatal("Load() should fail with empty config list")
	}

	if !strings.Contains(err.Error(), "at least one config file required") {
		t.Errorf("Error should mention empty config list, got: %v", err)
	}
}

func TestLoadResolvesSSLCliOverridesRelativeToCwd(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := `
proxy:
  listen: "localhost:9000"
  target: "http://localhost:3000"
rules:
  - methods: GET
    paths: /test
    on_request:
      - merge:
          x: 1
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	cfg, _, err := Load([]string{configPath}, CliOverrides{
		SSLCert: "certs/cert.pem",
		SSLKey:  "certs/key.pem",
	})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	wantCert := filepath.Join(tmpDir, "certs", "cert.pem")
	wantKey := filepath.Join(tmpDir, "certs", "key.pem")

	normalize := func(p string) string {
		p = filepath.Clean(p)
		p = strings.TrimPrefix(p, "/private")
		return p
	}

	if got := normalize(cfg.Proxies[0].SSLCert); got != normalize(wantCert) {
		t.Errorf("SSLCert = %s, want %s", got, normalize(wantCert))
	}
	if got := normalize(cfg.Proxies[0].SSLKey); got != normalize(wantKey) {
		t.Errorf("SSLKey = %s, want %s", got, normalize(wantKey))
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
