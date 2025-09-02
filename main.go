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
	Match []MatchRule `yaml:"match"`
}

type ProxyConfig struct {
	Listen  string        `yaml:"listen"`
	Target  string        `yaml:"target"`
	Timeout time.Duration `yaml:"timeout"`
	SSLCert string        `yaml:"ssl_cert"`
	SSLKey  string        `yaml:"ssl_key"`
	Debug   bool          `yaml:"debug"`
}

type MatchRule struct {
	Methods   interface{}     `yaml:"methods"`   // string or []string
	Endpoints interface{}     `yaml:"endpoints"` // string or []string
	Overrides []ModelOverride `yaml:"overrides"`
}

type ModelOverride struct {
    Models interface{}            `yaml:"models"` // string or []string
    All    bool                   `yaml:"all"`
    Params map[string]interface{} `yaml:"params"`
}

// =============================================================================
// Configuration
// =============================================================================

var debugMode bool

func loadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	configDir := filepath.Dir(configPath)
	config.Proxy.SSLCert = resolveSSLPath(config.Proxy.SSLCert, configDir)
	config.Proxy.SSLKey = resolveSSLPath(config.Proxy.SSLKey, configDir)

	if config.Proxy.Timeout == 0 {
		config.Proxy.Timeout = 60 * time.Second
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

	for i, rule := range config.Match {
		if rule.Methods == nil {
			return fmt.Errorf("match rule %d: methods is required", i)
		}
		if rule.Endpoints == nil {
			return fmt.Errorf("match rule %d: endpoints is required", i)
		}
		if len(rule.Overrides) == 0 {
			return fmt.Errorf("match rule %d: at least one override is required", i)
		}

        for j, override := range rule.Overrides {
            if override.Params == nil {
                return fmt.Errorf("match rule %d, override %d: params must be specified", i, j)
            }
            if override.Models == nil && !override.All {
                return fmt.Errorf("match rule %d, override %d: either models or all: true must be specified", i, j)
            }
        }
	}

	return nil
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

func logDebug(format string, args ...interface{}) {
	if debugMode {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// =============================================================================
// Server & Main
// =============================================================================

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", "", "Path to YAML configuration file (required)")
	flag.StringVar(&configFile, "c", "", "Path to YAML configuration file (required)")

	flag.Usage = func() {
		fmt.Println("llama-config: Automatically apply optimal settings to LLM requests")
		fmt.Println()
		fmt.Println("Usage: llama-config --config <config.yml>")
		fmt.Println()
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("For more information and examples, visit:")
		fmt.Println("  https://github.com/spicyneuron/llama-config")
	}

	flag.Parse()

	if configFile == "" {
		flag.Usage()
		os.Exit(1)
	}

	config, err := loadConfig(configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	debugMode = config.Proxy.Debug
	log.Printf("Loaded config from: %s", configFile)

	targetURL, err := url.Parse(config.Proxy.Target)
	if err != nil {
		log.Fatalf("Invalid target server URL: %v", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

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

	listenAddr := config.Proxy.Listen
	server := createServer(listenAddr, proxy, config)

	if config.Proxy.SSLCert != "" && config.Proxy.SSLKey != "" {
		log.Printf("Proxying https://%s to %s", listenAddr, config.Proxy.Target)
		log.Fatalf("HTTPS server failed: %v", server.ListenAndServeTLS(config.Proxy.SSLCert, config.Proxy.SSLKey))
	} else {
		log.Printf("Proxying http://%s to %s", listenAddr, config.Proxy.Target)
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
// Request Processing & Matching
// =============================================================================

func modifyRequest(req *http.Request, config *Config) {
	matchingRule := findMatchingRule(req, config)
	if matchingRule == nil {
		return
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

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		log.Printf("Failed to parse JSON request: %v", err)
		req.Body = io.NopCloser(bytes.NewReader(body))
		return
	}

    model, _ := data["model"].(string)

	modified, appliedOverrides := applyModelOverrides(data, model, matchingRule.Overrides)

	modifiedBody, err := json.Marshal(data)
	if err != nil {
		log.Printf("Failed to marshal modified JSON: %v", err)
		req.Body = io.NopCloser(bytes.NewReader(body))
		return
	}

	req.Body = io.NopCloser(bytes.NewReader(modifiedBody))
	req.ContentLength = int64(len(modifiedBody))

    if modified {
        overridesJSON, _ := json.MarshalIndent(appliedOverrides, "", "  ")
        if model == "" {
            logDebug("Updating request with:\n%s", string(overridesJSON))
        } else {
            logDebug("Updating request for model '%s' with:\n%s", model, string(overridesJSON))
        }
    }
}

func findMatchingRule(req *http.Request, config *Config) *MatchRule {
	for _, rule := range config.Match {
		if matchesMethod(req.Method, rule.Methods) && matchesEndpoint(req.URL.Path, rule.Endpoints) {
			return &rule
		}
	}
	return nil
}

func matchesMethod(method string, methods interface{}) bool {
	methodList := convertToStringSlice(methods)
	for _, m := range methodList {
		if strings.EqualFold(method, m) {
			return true
		}
	}
	return false
}

func matchesEndpoint(path string, endpoints interface{}) bool {
	endpointList := convertToStringSlice(endpoints)
	for _, endpoint := range endpointList {
		if matched, _ := regexp.MatchString("(?i)"+endpoint, path); matched {
			return true
		}
	}
	return false
}

func applyModelOverrides(data map[string]interface{}, model string, overrides []ModelOverride) (bool, map[string]interface{}) {
    appliedOverrides := make(map[string]interface{})
    for _, override := range overrides {
        if override.All || matchesModel(model, override) {
            for key, value := range override.Params {
                data[key] = value
                appliedOverrides[key] = value
            }
            return true, appliedOverrides
        }
    }
    return false, appliedOverrides
}

func matchesModel(model string, override ModelOverride) bool {
	if override.Models != nil {
		modelList := convertToStringSlice(override.Models)
		for _, m := range modelList {
			if matched, _ := regexp.MatchString("(?i)"+m, model); matched {
				return true
			}
		}
	}
	return false
}

func convertToStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		return []string{}
	}
}
