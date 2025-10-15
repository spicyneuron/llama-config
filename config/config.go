package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the full proxy configuration
type Config struct {
	Proxy ProxyConfig `yaml:"proxy"`
	Rules []Rule      `yaml:"rules"`
}

// ProxyConfig contains proxy-level settings
type ProxyConfig struct {
	Listen  string        `yaml:"listen"`
	Target  string        `yaml:"target"`
	Timeout time.Duration `yaml:"timeout"`
	SSLCert string        `yaml:"ssl_cert"`
	SSLKey  string        `yaml:"ssl_key"`
	Debug   bool          `yaml:"debug"`
}

// CliOverrides holds command-line flag overrides
type CliOverrides struct {
	Listen  string
	Target  string
	Timeout time.Duration
	SSLCert string
	SSLKey  string
	Debug   bool
}

// Rule defines matching criteria and operations with compiled templates
type Rule struct {
	Methods    PatternField `yaml:"methods"`
	Paths      PatternField `yaml:"paths"`
	TargetPath string       `yaml:"target_path"`

	OnRequest  []Operation `yaml:"on_request,omitempty"`
	OnResponse []Operation `yaml:"on_response,omitempty"`

	// Compiled templates (not serialized)
	OpRule *CompiledRule `yaml:"-"`
}

// Operation defines a transformation to apply
type Operation struct {
	// Matching criteria
	MatchBody    map[string]PatternField `yaml:"match_body,omitempty"`
	MatchHeaders map[string]PatternField `yaml:"match_headers,omitempty"`

	// Transformations
	Template string         `yaml:"template,omitempty"`
	Merge    map[string]any `yaml:"merge,omitempty"`
	Default  map[string]any `yaml:"default,omitempty"`
	Delete   []string       `yaml:"delete,omitempty"`
	Stop     bool           `yaml:"stop,omitempty"`
}

// PatternField can be a single pattern or array of patterns
type PatternField struct {
	Patterns []string
	Compiled []*regexp.Regexp
}

// UnmarshalYAML allows both string and []string for pattern fields
func (p *PatternField) UnmarshalYAML(unmarshal func(any) error) error {
	var single string
	if err := unmarshal(&single); err == nil {
		p.Patterns = []string{single}
		return nil
	}

	var multiple []string
	if err := unmarshal(&multiple); err == nil {
		p.Patterns = multiple
		return nil
	}

	return fmt.Errorf("patterns must be string or []string")
}

// Validate checks if all patterns are valid regex and compiles them
func (p *PatternField) Validate() error {
	const regexFlags = "(?i)"
	p.Compiled = make([]*regexp.Regexp, 0, len(p.Patterns))

	for _, pattern := range p.Patterns {
		re, err := regexp.Compile(regexFlags + pattern)
		if err != nil {
			return fmt.Errorf("invalid regex pattern '%s': %w", pattern, err)
		}
		p.Compiled = append(p.Compiled, re)
	}
	return nil
}

// Matches checks if input matches any compiled pattern
func (p PatternField) Matches(input string) bool {
	for _, re := range p.Compiled {
		if re.MatchString(input) {
			return true
		}
	}
	return false
}

// Len returns the number of patterns
func (p PatternField) Len() int {
	return len(p.Patterns)
}

// Load loads and merges one or more config files
// Later configs override earlier proxy settings, all rules are appended in order
func Load(configPaths []string, overrides CliOverrides) (*Config, error) {
	if len(configPaths) == 0 {
		return nil, fmt.Errorf("at least one config file required")
	}

	var mergedConfig *Config
	var configDir string

	for i, configPath := range configPaths {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
		}

		var cfg Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
		}

		if i == 0 {
			mergedConfig = &cfg
			configDir = filepath.Dir(configPath)
		} else {
			if cfg.Proxy.Listen != "" {
				mergedConfig.Proxy.Listen = cfg.Proxy.Listen
			}
			if cfg.Proxy.Target != "" {
				mergedConfig.Proxy.Target = cfg.Proxy.Target
			}
			if cfg.Proxy.Timeout != 0 {
				mergedConfig.Proxy.Timeout = cfg.Proxy.Timeout
			}
			if cfg.Proxy.SSLCert != "" {
				mergedConfig.Proxy.SSLCert = cfg.Proxy.SSLCert
			}
			if cfg.Proxy.SSLKey != "" {
				mergedConfig.Proxy.SSLKey = cfg.Proxy.SSLKey
			}
			if cfg.Proxy.Debug {
				mergedConfig.Proxy.Debug = cfg.Proxy.Debug
			}

			mergedConfig.Rules = append(mergedConfig.Rules, cfg.Rules...)
		}
	}

	applyOverrides(&mergedConfig.Proxy, overrides)

	if mergedConfig.Proxy.Timeout == 0 {
		mergedConfig.Proxy.Timeout = 60 * time.Second
	}

	mergedConfig.Proxy.SSLCert = resolveSSLPath(mergedConfig.Proxy.SSLCert, configDir)
	mergedConfig.Proxy.SSLKey = resolveSSLPath(mergedConfig.Proxy.SSLKey, configDir)

	if err := Validate(mergedConfig); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	if err := CompileTemplates(mergedConfig); err != nil {
		return nil, fmt.Errorf("template compilation failed: %w", err)
	}

	return mergedConfig, nil
}

func applyOverrides(proxy *ProxyConfig, overrides CliOverrides) {
	if overrides.Listen != "" {
		proxy.Listen = overrides.Listen
	}
	if overrides.Target != "" {
		proxy.Target = overrides.Target
	}
	if overrides.Timeout > 0 {
		proxy.Timeout = overrides.Timeout
	}
	if overrides.SSLCert != "" {
		proxy.SSLCert = overrides.SSLCert
	}
	if overrides.SSLKey != "" {
		proxy.SSLKey = overrides.SSLKey
	}
	if overrides.Debug {
		proxy.Debug = overrides.Debug
	}
}

func resolveSSLPath(sslPath, configDir string) string {
	if sslPath == "" {
		return ""
	}

	if filepath.IsAbs(sslPath) {
		return sslPath
	}

	return filepath.Join(configDir, sslPath)
}
