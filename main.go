package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Proxy ProxyConfig `yaml:"proxy"`
	Rules []Rule      `yaml:"rules"`
}

type CliOverrides struct {
	Listen  string
	Target  string
	Timeout time.Duration
	SSLCert string
	SSLKey  string
	Debug   bool
}

type ProxyConfig struct {
	Listen  string        `yaml:"listen"`
	Target  string        `yaml:"target"`
	Timeout time.Duration `yaml:"timeout"`
	SSLCert string        `yaml:"ssl_cert"`
	SSLKey  string        `yaml:"ssl_key"`
	Debug   bool          `yaml:"debug"`
}

type PatternField []string

func (p *PatternField) UnmarshalYAML(unmarshal func(any) error) error {
	var single string
	if err := unmarshal(&single); err == nil {
		*p = PatternField{single}
		return nil
	}

	var multiple []string
	if err := unmarshal(&multiple); err == nil {
		*p = PatternField(multiple)
		return nil
	}

	return fmt.Errorf("patterns must be string or []string")
}

func (p PatternField) Validate() error {
	for _, pattern := range p {
		if _, err := regexp.Compile(regexFlags + pattern); err != nil {
			return fmt.Errorf("invalid regex pattern '%s': %w", pattern, err)
		}
	}
	return nil
}

type Rule struct {
	Methods    PatternField `yaml:"methods"`
	Paths      PatternField `yaml:"paths"`
	TargetPath string       `yaml:"target_path"`
	Operations []Operation  `yaml:"operations"`
}

type Operation struct {
	Filters    map[string]PatternField `yaml:"filters"`
	Merge      map[string]any          `yaml:"merge,omitempty"`
	Default    map[string]any          `yaml:"default,omitempty"`
	Delete     []string                `yaml:"delete,omitempty"`
	operations []operation             // internal field for order preservation
}

type operation struct {
	opType string
	data   any
}

// UnmarshalYAML implements custom unmarshaling to preserve operation order
func (o *Operation) UnmarshalYAML(unmarshal func(any) error) error {
	var node yaml.Node
	if err := unmarshal(&node); err != nil {
		return err
	}

	// Initialize fields
	o.Filters = make(map[string]PatternField)
	o.Merge = make(map[string]any)
	o.Default = make(map[string]any)
	o.Delete = []string{}
	o.operations = []operation{}

	// Validate node structure
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("invalid rule structure: uneven key-value pairs")
	}

	// Process node content (key-value pairs)
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]

		if keyNode.Value == "" {
			return fmt.Errorf("empty key found in rule")
		}

		switch keyNode.Value {
		case "filters":
			if err := valueNode.Decode(&o.Filters); err != nil {
				return fmt.Errorf("failed to decode filters: %w", err)
			}
		case "merge":
			if err := valueNode.Decode(&o.Merge); err != nil {
				return fmt.Errorf("failed to decode merge: %w", err)
			}
			if len(o.Merge) == 0 {
				return fmt.Errorf("merge operation cannot be empty")
			}
			o.operations = append(o.operations, operation{"merge", o.Merge})
		case "default":
			if err := valueNode.Decode(&o.Default); err != nil {
				return fmt.Errorf("failed to decode default: %w", err)
			}
			if len(o.Default) == 0 {
				return fmt.Errorf("default operation cannot be empty")
			}
			o.operations = append(o.operations, operation{"default", o.Default})
		case "delete":
			if err := valueNode.Decode(&o.Delete); err != nil {
				return fmt.Errorf("failed to decode delete: %w", err)
			}
			if len(o.Delete) == 0 {
				return fmt.Errorf("delete operation cannot be empty")
			}
			for _, key := range o.Delete {
				if key == "" {
					return fmt.Errorf("delete operation cannot contain empty keys")
				}
			}
			o.operations = append(o.operations, operation{"delete", o.Delete})
		default:
			return fmt.Errorf("unknown rule field: %s", keyNode.Value)
		}
	}

	return nil
}

// =============================================================================
// Configuration
// =============================================================================

var debugMode bool

const regexFlags = "(?i)"

func loadConfig(configPath string, overrides CliOverrides) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	applyOverrides(&config.Proxy, overrides)

	if config.Proxy.Timeout == 0 {
		config.Proxy.Timeout = 60 * time.Second
	}

	configDir := filepath.Dir(configPath)
	config.Proxy.SSLCert = resolveSSLPath(config.Proxy.SSLCert, configDir)
	config.Proxy.SSLKey = resolveSSLPath(config.Proxy.SSLKey, configDir)

	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

func validateConfig(config *Config) error {
	if config.Proxy.Listen == "" {
		return fmt.Errorf("proxy.listen is required")
	}
	if config.Proxy.Target == "" {
		return fmt.Errorf("proxy.target is required")
	}

	if _, err := url.Parse(config.Proxy.Target); err != nil {
		return fmt.Errorf("invalid proxy.target URL: %w", err)
	}

	if (config.Proxy.SSLCert != "" && config.Proxy.SSLKey == "") ||
		(config.Proxy.SSLCert == "" && config.Proxy.SSLKey != "") {
		return fmt.Errorf("both ssl_cert and ssl_key must be provided together")
	}

	for i, rule := range config.Rules {
		if err := validateRule(&rule, i); err != nil {
			return err
		}
	}

	return nil
}

func validateRule(rule *Rule, index int) error {
	if len(rule.Methods) == 0 {
		return fmt.Errorf("match rule %d: methods required", index)
	}
	if len(rule.Paths) == 0 {
		return fmt.Errorf("match rule %d: paths required", index)
	}
	if len(rule.Operations) == 0 {
		return fmt.Errorf("match rule %d: at least one operation required", index)
	}
	if rule.TargetPath != "" && !strings.HasPrefix(rule.TargetPath, "/") {
		return fmt.Errorf("match rule %d: target_path must be absolute", index)
	}

	if err := rule.Methods.Validate(); err != nil {
		return fmt.Errorf("match rule %d methods: %w", index, err)
	}
	if err := rule.Paths.Validate(); err != nil {
		return fmt.Errorf("match rule %d paths: %w", index, err)
	}

	for opIdx, op := range rule.Operations {
		if err := validateOperation(&op, index, opIdx); err != nil {
			return err
		}
	}

	return nil
}

func validateOperation(op *Operation, ruleIndex, opIndex int) error {
	for key, patterns := range op.Filters {
		if err := patterns.Validate(); err != nil {
			return fmt.Errorf("rule %d operation %d filter '%s': %w", ruleIndex, opIndex, key, err)
		}
	}

	if len(op.operations) == 0 && len(op.Merge) == 0 && len(op.Default) == 0 && len(op.Delete) == 0 {
		return fmt.Errorf("rule %d operation %d: must have at least one action (merge, default, or delete)", ruleIndex, opIndex)
	}

	for key, value := range op.Merge {
		if key == "" {
			return fmt.Errorf("rule %d operation %d: merge action cannot have empty key", ruleIndex, opIndex)
		}
		if value == nil {
			return fmt.Errorf("rule %d operation %d: merge action key '%s' cannot have nil value", ruleIndex, opIndex, key)
		}
	}

	for key, value := range op.Default {
		if key == "" {
			return fmt.Errorf("rule %d operation %d: default action cannot have empty key", ruleIndex, opIndex)
		}
		if value == nil {
			return fmt.Errorf("rule %d operation %d: default action key '%s' cannot have nil value", ruleIndex, opIndex, key)
		}
	}

	for i, key := range op.Delete {
		if key == "" {
			return fmt.Errorf("rule %d operation %d: delete action cannot have empty key at index %d", ruleIndex, opIndex, i)
		}
	}

	return nil
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

func logDebug(format string, args ...any) {
	if debugMode {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// =============================================================================
// Server & Main
// =============================================================================

func main() {
	var (
		configFile = flag.String("config", "", "Path to YAML configuration (required)")
		listenAddr = flag.String("listen", "", "Address to listen on (ex: localhost:8081)")
		targetURL  = flag.String("target", "", "Target URL to proxy to (ex: http://localhost:8080)")
		sslCert    = flag.String("ssl-cert", "", "SSL certificate file (ex: cert.pem)")
		sslKey     = flag.String("ssl-key", "", "SSL key file (ex: key.pem)")
		timeout    = flag.Duration("timeout", 0, "Timeout for requests to target (ex: 60s)")
		debug      = flag.Bool("debug", false, "Print debug logs")
	)

	flag.Usage = func() {
		fmt.Println("llama-config-proxy: Automatically apply optimal settings to LLM requests")
		fmt.Println()
		fmt.Println("Usage: llama-config-proxy --config <config.yml>")
		fmt.Println()
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("For more information and examples, visit:")
		fmt.Println("  https://github.com/spicyneuron/llama-config-proxy")
	}

	flag.Parse()

	if *configFile == "" {
		flag.Usage()
		os.Exit(1)
	}

	overrides := CliOverrides{*listenAddr, *targetURL, *timeout, *sslCert, *sslKey, *debug}

	config, err := loadConfig(*configFile, overrides)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	debugMode = config.Proxy.Debug
	log.Printf("Loaded config from: %s", *configFile)

	targetURLParsed, err := url.Parse(config.Proxy.Target)
	if err != nil {
		log.Fatalf("Invalid target server URL: %v", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURLParsed)

	if config.Proxy.Timeout > 0 {
		proxy.Transport = &http.Transport{
			TLSHandshakeTimeout:   config.Proxy.Timeout,
			ResponseHeaderTimeout: config.Proxy.Timeout,
		}
		log.Printf("Configured timeout: %v", config.Proxy.Timeout)
	}

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		log.Printf("%s %s", req.Method, req.URL.Path)
		originalDirector(req)
		modifyRequest(req, config)
	}

	listenAddrFinal := config.Proxy.Listen
	server := createServer(listenAddrFinal, proxy, config)

	if config.Proxy.SSLCert != "" && config.Proxy.SSLKey != "" {
		log.Printf("Proxying https://%s to %s", listenAddrFinal, config.Proxy.Target)
		log.Fatalf("HTTPS server failed: %v", server.ListenAndServeTLS(config.Proxy.SSLCert, config.Proxy.SSLKey))
	} else {
		log.Printf("Proxying http://%s to %s", listenAddrFinal, config.Proxy.Target)
		log.Fatalf("HTTP server failed: %v", server.ListenAndServe())
	}
}

func createServer(addr string, handler http.Handler, config *Config) *http.Server {
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	if config.Proxy.SSLCert != "" && config.Proxy.SSLKey != "" {
		cert, err := tls.LoadX509KeyPair(config.Proxy.SSLCert, config.Proxy.SSLKey)
		if err != nil {
			log.Fatalf("Failed to load SSL certificates: %v", err)
		}
		server.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
	}

	server.ReadTimeout = config.Proxy.Timeout
	server.WriteTimeout = config.Proxy.Timeout

	return server
}

// =============================================================================
// Request Processing & Rule Engine
// =============================================================================

func modifyRequest(req *http.Request, config *Config) {
	matchingRule := findMatchingRule(req, config)
	if matchingRule == nil {
		return
	}

	if matchingRule.TargetPath != "" {
		originalPath := req.URL.Path
		req.URL.Path = matchingRule.TargetPath
		logDebug("Rewrote request path from %s to %s", originalPath, matchingRule.TargetPath)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		return
	}
	req.Body.Close()

	if debugMode && len(body) > 0 {
		var prettyJSON bytes.Buffer
		json.Indent(&prettyJSON, body, "", "  ")
		logDebug("Inbound request body:\n%s", prettyJSON.String())
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		log.Printf("Failed to parse JSON request: %v", err)
		req.Body = io.NopCloser(bytes.NewReader(body))
		return
	}

	modified, appliedValues := processRules(data, matchingRule.Operations)

	modifiedBody, err := json.Marshal(data)
	if err != nil {
		log.Printf("Failed to marshal modified JSON: %v", err)
		req.Body = io.NopCloser(bytes.NewReader(body))
		return
	}

	req.Body = io.NopCloser(bytes.NewReader(modifiedBody))
	req.ContentLength = int64(len(modifiedBody))

	if modified {
		appliedJSON, _ := json.MarshalIndent(appliedValues, "", "  ")
		logDebug("Applied rules with values:\n%s", string(appliedJSON))
	}
}

func findMatchingRule(req *http.Request, config *Config) *Rule {
	for _, rule := range config.Rules {
		if matchesMethod(req.Method, rule.Methods) && matchesPath(req.URL.Path, rule.Paths) {
			return &rule
		}
	}
	return nil
}

func matchesPattern(input string, patterns PatternField) bool {
	for _, pattern := range patterns {
		if matched, err := regexp.MatchString(regexFlags+pattern, input); err == nil && matched {
			return true
		}
	}
	return false
}

func matchesMethod(method string, methods PatternField) bool {
	return matchesPattern(method, methods)
}

func matchesPath(path string, paths PatternField) bool {
	return matchesPattern(path, paths)
}

func processRules(data map[string]any, operations []Operation) (bool, map[string]any) {
	appliedValues := make(map[string]any)

	for _, op := range operations {
		if satisfiesFilter(data, op.Filters) {
			for _, operation := range op.operations {
				switch operation.opType {
				case "merge":
					applyMergeOperation(data, operation, appliedValues)
				case "default":
					applyDefaultOperation(data, operation, appliedValues)
				case "delete":
					applyDeleteOperation(data, operation, appliedValues)
				}
			}
			return true, appliedValues
		}
	}
	return false, appliedValues
}

func applyMergeOperation(data map[string]any, operation operation, appliedValues map[string]any) {
	if mergeValues, ok := operation.data.(map[string]any); ok {
		logDebug("Applying merge operation with %d values", len(mergeValues))
		for key, value := range mergeValues {
			originalValue := data[key]
			data[key] = value
			appliedValues[key] = value
			logDebug("Merged %s: %v -> %v", key, originalValue, value)
		}
	} else {
		logDebug("Merge operation data type assertion failed")
	}
}

func applyDefaultOperation(data map[string]any, operation operation, appliedValues map[string]any) {
	if defaultValues, ok := operation.data.(map[string]any); ok {
		logDebug("Applying default operation with %d values", len(defaultValues))
		for key, value := range defaultValues {
			if _, exists := data[key]; !exists {
				data[key] = value
				appliedValues[key] = value
				logDebug("Set default %s: %v", key, value)
			} else {
				logDebug("Skipped default %s (already exists): %v", key, data[key])
			}
		}
	} else {
		logDebug("Default operation data type assertion failed")
	}
}

func applyDeleteOperation(data map[string]any, operation operation, appliedValues map[string]any) {
	if deleteKeys, ok := operation.data.([]string); ok {
		logDebug("Applying delete operation for %d keys", len(deleteKeys))
		for _, key := range deleteKeys {
			if originalValue, exists := data[key]; exists {
				delete(data, key)
				appliedValues[key] = "<deleted>"
				logDebug("Deleted %s (was: %v)", key, originalValue)
			} else {
				logDebug("Skipped delete %s (not found)", key)
			}
		}
	} else {
		logDebug("Delete operation data type assertion failed")
	}
}

func satisfiesFilter(data map[string]any, filters map[string]PatternField) bool {
	if len(filters) == 0 {
		return true
	}

	for key, patterns := range filters {
		actualValue, exists := data[key]
		if !exists {
			return false
		}

		actualStr := fmt.Sprintf("%v", actualValue)
		if !matchesPattern(actualStr, patterns) {
			return false
		}
	}
	return true
}
