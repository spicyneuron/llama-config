package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/spicyneuron/llama-config-proxy/logger"
	"gopkg.in/yaml.v3"
)

// Config represents the full proxy configuration
type Config struct {
	Proxies ProxyEntries `yaml:"proxy"`
	Rules   []Rule       `yaml:"rules"`
}

type watchList struct {
	paths []string
	seen  map[string]struct{}
}

func newWatchList() *watchList {
	return &watchList{paths: make([]string, 0), seen: make(map[string]struct{})}
}

func (w *watchList) Add(path string) {
	if path == "" {
		return
	}
	if _, ok := w.seen[path]; ok {
		return
	}
	w.seen[path] = struct{}{}
	w.paths = append(w.paths, path)
}

func (w *watchList) Paths() []string {
	return w.paths
}

// ProxyConfig contains proxy-level settings
type ProxyConfig struct {
	Listen  string        `yaml:"listen"`
	Target  string        `yaml:"target"`
	Timeout time.Duration `yaml:"timeout"`
	SSLCert string        `yaml:"ssl_cert"`
	SSLKey  string        `yaml:"ssl_key"`
	Debug   bool          `yaml:"debug"`
	Rules   []Rule        `yaml:"rules"`
}

// ProxyEntries allows proxy to be defined as a single map or a list
type ProxyEntries []ProxyConfig

// UnmarshalYAML accepts either a single proxy map or a sequence of proxies
func (p *ProxyEntries) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		var proxies []ProxyConfig
		if err := value.Decode(&proxies); err != nil {
			return err
		}
		*p = proxies
		return nil
	case yaml.MappingNode:
		var proxy ProxyConfig
		if err := value.Decode(&proxy); err != nil {
			return err
		}
		*p = []ProxyConfig{proxy}
		return nil
	case 0:
		return nil
	default:
		return fmt.Errorf("proxy must be a map or list")
	}
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
// Returns the config, list of watched files (including includes and SSL certs), and error
func Load(configPaths []string, overrides CliOverrides) (*Config, []string, error) {
	if len(configPaths) == 0 {
		return nil, nil, fmt.Errorf("at least one config file required")
	}

	var mergedConfig *Config
	watchedFiles := newWatchList()
	logger.Info("Loading configuration", "files", len(configPaths))

	for i, configPath := range configPaths {
		// Add main config file to watched files
		absPath, err := filepath.Abs(configPath)
		if err != nil {
			absPath = configPath
		}
		watchedFiles.Add(absPath)

		cfg, err := loadConfigFile(configPath, watchedFiles)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
		}

		logger.Debug("Loading config file", "index", i+1, "total", len(configPaths), "path", configPath)

		// Resolve paths relative to this config file's directory
		configDir := filepath.Dir(configPath)
		for i := range cfg.Proxies {
			cfg.Proxies[i].SSLCert = ResolvePath(cfg.Proxies[i].SSLCert, configDir)
			cfg.Proxies[i].SSLKey = ResolvePath(cfg.Proxies[i].SSLKey, configDir)

			// Add SSL cert/key files to watched files
			if cfg.Proxies[i].SSLCert != "" {
				watchedFiles.Add(cfg.Proxies[i].SSLCert)
			}
			if cfg.Proxies[i].SSLKey != "" {
				watchedFiles.Add(cfg.Proxies[i].SSLKey)
			}
		}

		if i == 0 {
			mergedConfig = &cfg
		} else {
			mergedConfig.Proxies = append(mergedConfig.Proxies, cfg.Proxies...)
			mergedConfig.Rules = append(mergedConfig.Rules, cfg.Rules...)
			logger.Debug("Merged config file", "path", configPath, "proxies_added", len(cfg.Proxies), "rules_added", len(cfg.Rules))
		}
	}

	// Get current working directory for resolving CLI override paths
	pwd, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	// Resolve to a final proxy list (supports either proxy or proxies)
	proxies := mergedConfig.Proxies
	if len(proxies) == 0 && overridesHasProxyValues(overrides) {
		proxies = append(proxies, ProxyConfig{})
	}
	if len(proxies) == 0 {
		return nil, nil, fmt.Errorf("no proxies configured; add a proxy or proxies section")
	}

	if len(proxies) > 1 && overridesHasProxyValues(overrides) {
		return nil, nil, fmt.Errorf("CLI overrides for listen/target/timeout/ssl are only supported with a single proxy; define multiple listeners in the config file instead")
	}

	for i := range proxies {
		if len(proxies) == 1 {
			// Resolve CLI override paths relative to PWD, then apply overrides
			applyOverrides(&proxies[i], overrides, pwd)
		} else if overrides.Debug {
			// Allow global debug enablement
			proxies[i].Debug = true
		}

		if proxies[i].Timeout == 0 {
			proxies[i].Timeout = 60 * time.Second
			logger.Debug("Using default timeout for proxy", "index", i, "timeout", proxies[i].Timeout)
		}

		// If proxy has no rules, inherit shared rules
		if len(proxies[i].Rules) == 0 && len(mergedConfig.Rules) > 0 {
			proxies[i].Rules = append([]Rule(nil), mergedConfig.Rules...)
		}
	}

	mergedConfig.Proxies = proxies

	logger.Info("Applied CLI overrides", "listen", overrides.Listen, "target", overrides.Target, "timeout", overrides.Timeout, "debug", overrides.Debug)

	if err := Validate(mergedConfig); err != nil {
		return nil, nil, fmt.Errorf("config validation failed: %w", err)
	}

	if err := CompileTemplates(mergedConfig); err != nil {
		return nil, nil, fmt.Errorf("template compilation failed: %w", err)
	}

	for i, p := range mergedConfig.Proxies {
		logger.Info("Proxy configured", "index", i, "listen", p.Listen, "target", p.Target, "rules", len(p.Rules), "timeout", p.Timeout, "ssl_cert", p.SSLCert != "")
	}

	logger.Info("Configuration ready", "proxies", len(mergedConfig.Proxies), "total_rules", len(mergedConfig.Rules))

	return mergedConfig, watchedFiles.Paths(), nil
}

func loadConfigFile(configPath string, watchedFiles *watchList) (Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return Config{}, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	if err := expandIncludes(&root, filepath.Dir(configPath), watchedFiles); err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := root.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("failed to decode config %s: %w", configPath, err)
	}

	return cfg, nil
}

func expandIncludes(node *yaml.Node, baseDir string, watchedFiles *watchList) error {
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			if err := expandIncludes(child, baseDir, watchedFiles); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			val := node.Content[i+1]

			if key.Value == "include" && len(node.Content) == 2 {
				included, err := loadIncludeNode(val, baseDir, watchedFiles)
				if err != nil {
					return err
				}
				*node = *included
				return expandIncludes(node, baseDir, watchedFiles)
			}

			// Allow include as the value of a mapping (e.g., on_request: { include: file.yml })
			if val.Kind == yaml.MappingNode && isIncludeNode(val) {
				included, err := loadIncludeNode(val.Content[1], baseDir, watchedFiles)
				if err != nil {
					return err
				}
				node.Content[i+1] = included
				if err := expandIncludes(included, baseDir, watchedFiles); err != nil {
					return err
				}
				continue
			}

			if err := expandIncludes(val, baseDir, watchedFiles); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		var newContent []*yaml.Node
		for _, item := range node.Content {
			if isIncludeNode(item) {
				included, err := loadIncludeNode(item.Content[1], baseDir, watchedFiles)
				if err != nil {
					return err
				}

				if included.Kind == yaml.SequenceNode {
					for _, child := range included.Content {
						if err := expandIncludes(child, baseDir, watchedFiles); err != nil {
							return err
						}
						newContent = append(newContent, child)
					}
				} else {
					if err := expandIncludes(included, baseDir, watchedFiles); err != nil {
						return err
					}
					newContent = append(newContent, included)
				}
				continue
			}

			if err := expandIncludes(item, baseDir, watchedFiles); err != nil {
				return err
			}
			newContent = append(newContent, item)
		}
		node.Content = newContent
	}
	return nil
}

func isIncludeNode(node *yaml.Node) bool {
	return node.Kind == yaml.MappingNode &&
		len(node.Content) == 2 &&
		node.Content[0].Value == "include"
}

func loadIncludeNode(pathNode *yaml.Node, baseDir string, watchedFiles *watchList) (*yaml.Node, error) {
	if pathNode.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("include path must be a string")
	}

	includePath := ResolvePath(pathNode.Value, baseDir)

	// Track this included file
	absPath, err := filepath.Abs(includePath)
	if err != nil {
		absPath = includePath
	}
	watchedFiles.Add(absPath)

	data, err := os.ReadFile(includePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read include file %s: %w", includePath, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("failed to parse include file %s: %w", includePath, err)
	}

	if err := expandIncludes(&root, filepath.Dir(includePath), watchedFiles); err != nil {
		return nil, err
	}

	// yaml.Unmarshal produces a DocumentNode with single child
	if len(root.Content) > 0 {
		return root.Content[0], nil
	}
	return &root, nil
}

func applyOverrides(proxy *ProxyConfig, overrides CliOverrides, pwd string) {
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
		// Resolve CLI paths relative to PWD
		proxy.SSLCert = ResolvePath(overrides.SSLCert, pwd)
	}
	if overrides.SSLKey != "" {
		// Resolve CLI paths relative to PWD
		proxy.SSLKey = ResolvePath(overrides.SSLKey, pwd)
	}
	if overrides.Debug {
		proxy.Debug = overrides.Debug
	}
}

func overridesHasProxyValues(overrides CliOverrides) bool {
	return overrides.Listen != "" ||
		overrides.Target != "" ||
		overrides.Timeout > 0 ||
		overrides.SSLCert != "" ||
		overrides.SSLKey != ""
}

// ResolvePath resolves a file path relative to baseDir if not absolute
func ResolvePath(filePath, baseDir string) string {
	if filePath == "" {
		return ""
	}

	if filepath.IsAbs(filePath) {
		return filePath
	}

	return filepath.Join(baseDir, filePath)
}
