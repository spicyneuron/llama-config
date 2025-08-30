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
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Configuration structures
type Config struct {
	Proxy ProxyConfig  `yaml:"proxy"`
	Match []MatchRule `yaml:"match"`
}

type ProxyConfig struct {
	Listen  string        `yaml:"listen"`
	Target  string        `yaml:"target"`
	Timeout time.Duration `yaml:"timeout"`
	SSLCert string        `yaml:"ssl_cert"`
	SSLKey  string        `yaml:"ssl_key"`
}

type MatchRule struct {
	Methods   interface{}     `yaml:"methods"`   // string or []string
	Endpoints interface{}     `yaml:"endpoints"` // string or []string
	Overrides []ModelOverride `yaml:"overrides"`
}

type ModelOverride struct {
	Models interface{}            `yaml:"models"` // string or []string
	Params map[string]interface{} `yaml:"params"`
}

// Configuration loading
func loadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", "", "Path to YAML configuration file")
	flag.StringVar(&configFile, "c", "", "Path to YAML configuration file")
	flag.Parse()

	if configFile == "" {
		fmt.Println("Usage: llm-config-proxy --config <config.yml> or llm-config-proxy -c <config.yml>")
		fmt.Println("  -c, --config string    Path to YAML configuration file")
		os.Exit(1)
	}

	config, err := loadConfig(configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
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
		originalDirector(req)
		modifyRequest(req, config)
	}

	listenAddr := config.Proxy.Listen
	log.Printf("Proxy server running on %s", listenAddr)
	log.Printf("Forwarding to: %s", config.Proxy.Target)

	// Start server with SSL support if certificates are provided
	if config.Proxy.SSLCert != "" && config.Proxy.SSLKey != "" {
		log.Printf("Starting HTTPS server with SSL cert: %s", config.Proxy.SSLCert)
		cert, err := tls.LoadX509KeyPair(config.Proxy.SSLCert, config.Proxy.SSLKey)
		if err != nil {
			log.Fatalf("Failed to load SSL certificates: %v", err)
		}

		server := &http.Server{
			Addr:    listenAddr,
			Handler: proxy,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
			},
		}

		if config.Proxy.Timeout > 0 {
			server.ReadTimeout = config.Proxy.Timeout
			server.WriteTimeout = config.Proxy.Timeout
		}

		log.Fatalf("HTTPS server failed: %v", server.ListenAndServeTLS("", ""))
	} else {
		server := &http.Server{
			Addr:    listenAddr,
			Handler: proxy,
		}

		if config.Proxy.Timeout > 0 {
			server.ReadTimeout = config.Proxy.Timeout
			server.WriteTimeout = config.Proxy.Timeout
		}

		log.Fatalf("HTTP server failed: %v", server.ListenAndServe())
	}
}

// Request modification
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

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		log.Printf("Failed to parse JSON request: %v", err)
		req.Body = io.NopCloser(bytes.NewReader(body))
		return
	}

	model, exists := data["model"].(string)
	if !exists {
		req.Body = io.NopCloser(bytes.NewReader(body))
		return
	}

	modified := applyModelOverrides(data, model, matchingRule.Overrides)

	modifiedBody, err := json.Marshal(data)
	if err != nil {
		log.Printf("Failed to marshal modified JSON: %v", err)
		req.Body = io.NopCloser(bytes.NewReader(body))
		return
	}

	req.Body = io.NopCloser(bytes.NewReader(modifiedBody))
	req.ContentLength = int64(len(modifiedBody))

	if modified {
		log.Printf("Modified request for model: %s", model)
	}
}

// Helper functions
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

func applyModelOverrides(data map[string]interface{}, model string, overrides []ModelOverride) bool {
	modified := false
	for _, override := range overrides {
		if matchesModel(model, override) {
			for key, value := range override.Params {
				data[key] = value
			}
			modified = true
		}
	}
	return modified
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
	case []string:
		return v
	default:
		return []string{}
	}
}