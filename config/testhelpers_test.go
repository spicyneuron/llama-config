package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

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

	for i := range cfg.Proxies {
		if cfg.Proxies[i].Timeout == 0 {
			cfg.Proxies[i].Timeout = 60 * time.Second
		}
		// Inherit shared rules if none set on the proxy
		if len(cfg.Proxies[i].Rules) == 0 && len(cfg.Rules) > 0 {
			cfg.Proxies[i].Rules = append([]Rule(nil), cfg.Rules...)
		}
	}

	if err := Validate(&cfg); err != nil {
		return nil, err
	}

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

// writeTempConfig writes content into a temp file and returns its path.
func writeTempConfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write %s: %v", name, err)
	}
	return path
}
