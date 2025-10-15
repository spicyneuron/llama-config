package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/spicyneuron/llama-config-proxy/config"
)

type contextKey string

const ruleContextKey contextKey = "matched_rule"

var debugMode bool

// SetDebugMode enables or disables debug logging
func SetDebugMode(enabled bool) {
	debugMode = enabled
}

func logDebug(format string, args ...any) {
	if debugMode {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// FindMatchingRule returns the first rule that matches the request
func FindMatchingRule(req *http.Request, cfg *config.Config) *config.Rule {
	for i := range cfg.Rules {
		rule := &cfg.Rules[i]
		if rule.Methods.Matches(req.Method) && rule.Paths.Matches(req.URL.Path) {
			return rule
		}
	}
	return nil
}

// ModifyRequest processes the request through matching rules
func ModifyRequest(req *http.Request, cfg *config.Config) {
	matchingRule := FindMatchingRule(req, cfg)
	if matchingRule == nil {
		logDebug("No matching rule for %s %s", req.Method, req.URL.Path)
		return
	}

	// Store the matching rule in context for response processing
	ctx := context.WithValue(req.Context(), ruleContextKey, matchingRule)
	*req = *req.WithContext(ctx)

	if matchingRule.TargetPath != "" {
		originalPath := req.URL.Path
		req.URL.Path = matchingRule.TargetPath
		logDebug("Rewrote request path from %s to %s", originalPath, matchingRule.TargetPath)
	}

	// Read and limit body size to 10MB to prevent memory exhaustion
	limitedBody := io.LimitReader(req.Body, 10*1024*1024)
	body, err := io.ReadAll(limitedBody)
	req.Body.Close()
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		return
	}

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

	modified, appliedValues := config.ProcessRequest(data, matchingRule.OpRule)

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
		logDebug("Applied request changes:\n%s", string(appliedJSON))
	}
}

// ModifyResponse processes the response through matching rules
func ModifyResponse(resp *http.Response, cfg *config.Config) error {
	// Skip if not JSON
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		return nil
	}

	// Get the rule from context
	matchingRule, ok := resp.Request.Context().Value(ruleContextKey).(*config.Rule)
	if !ok || matchingRule == nil {
		return nil
	}

	// Skip if no response operations
	if len(matchingRule.OnResponse) == 0 {
		return nil
	}

	// Read response body (limit to 10MB)
	limitedBody := io.LimitReader(resp.Body, 10*1024*1024)
	body, err := io.ReadAll(limitedBody)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if debugMode && len(body) > 0 {
		var prettyJSON bytes.Buffer
		json.Indent(&prettyJSON, body, "", "  ")
		logDebug("Inbound response body:\n%s", prettyJSON.String())
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		// If not JSON, return original body
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}

	modified, appliedValues := config.ProcessResponse(data, matchingRule.OpRule)

	modifiedBody, err := json.Marshal(data)
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return fmt.Errorf("failed to marshal modified response JSON: %w", err)
	}

	resp.Body = io.NopCloser(bytes.NewReader(modifiedBody))
	resp.ContentLength = int64(len(modifiedBody))

	if modified {
		appliedJSON, _ := json.MarshalIndent(appliedValues, "", "  ")
		logDebug("Applied response changes:\n%s", string(appliedJSON))
	}

	return nil
}
