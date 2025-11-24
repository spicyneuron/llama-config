package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spicyneuron/llama-config-proxy/config"
	"github.com/spicyneuron/llama-config-proxy/logger"
)

type contextKey string

const ruleContextKey contextKey = "matched_rule"

type responseRuleContext struct {
	rule  *config.Rule
	index int
}

// FindMatchingRules returns all rules that match the request sequentially
func FindMatchingRules(req *http.Request, cfg *config.Config) []*config.Rule {
	logger.Debug("Evaluating rules for request", "rule_count", len(cfg.Rules), "method", req.Method, "path", req.URL.Path)

	var matchedRules []*config.Rule

	for i := range cfg.Rules {
		rule := &cfg.Rules[i]
		methodMatch := rule.Methods.Matches(req.Method)
		pathMatch := rule.Paths.Matches(req.URL.Path)

		logger.Debug("Rule evaluation", "index", i, "methods", rule.Methods.Patterns, "paths", rule.Paths.Patterns, "method_match", methodMatch, "path_match", pathMatch)

		if methodMatch && pathMatch {
			logger.Debug("Rule matched", "index", i)
			matchedRules = append(matchedRules, rule)
		}
	}

	if len(matchedRules) == 0 {
		logger.Debug("No rules matched for request")
	} else {
		logger.Debug("Matched rules for request", "count", len(matchedRules))
	}

	return matchedRules
}

// ModifyRequest processes the request through rules sequentially
// Each rule is checked and processed immediately before moving to the next rule
func ModifyRequest(req *http.Request, cfg *config.Config) {
	method := req.Method
	path := req.URL.Path
	// Read and limit body size to 10MB to prevent memory exhaustion
	var body []byte
	var err error
	if req.Body != nil {
		limitedBody := io.LimitReader(req.Body, 10*1024*1024)
		body, err = io.ReadAll(limitedBody)
		req.Body.Close()
		if err != nil {
			logger.Error("Failed to read request body", "method", method, "path", path, "err", err)
			return
		}
	}

	if logger.IsDebug() {
		logger.Debug("Inbound request", "method", method, "path", path)

		for key, values := range sanitizeHeaders(req.Header) {
			for _, value := range values {
				logger.Debug("Request header", "key", key, "value", value)
			}
		}

		if len(body) > 0 {
			safeBody, truncated := sanitizeBody(body, 4096)
			logger.Debug("Request body", "body", safeBody, "truncated", truncated)
		} else {
			logger.Debug("Request body omitted", "reason", "empty")
		}
	}

	// Parse JSON body if present
	var data map[string]any
	hasJSONBody := false
	if len(body) > 0 {
		if err := json.Unmarshal(body, &data); err == nil {
			hasJSONBody = true
		} else {
			if logger.IsDebug() {
				logger.Debug("Request body is not JSON, passing through unchanged")
			}
			req.Body = io.NopCloser(bytes.NewReader(body))
			// Still check for matching rules that might modify path/headers
		}
	}

	// Extract headers as map[string]string for matching
	headers := make(map[string]string)
	for key, values := range req.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	// Track the last matched rule for response processing
	var lastMatchedRule responseRuleContext
	anyModified := false
	allAppliedValues := make(map[string]any)
	matchedCount := 0
	var matchedRuleIndices []int

	// Process rules sequentially: check and apply each rule before moving to next
	for i := range cfg.Rules {
		rule := &cfg.Rules[i]

		// Check if this rule matches (method and path)
		methodMatch := rule.Methods.Matches(req.Method)
		pathMatch := rule.Paths.Matches(req.URL.Path)

		if logger.IsDebug() {
			logger.Debug("Rule evaluation", "index", i, "method_match", methodMatch, "path_match", pathMatch, "methods", rule.Methods.Patterns, "paths", rule.Paths.Patterns)
		}

		if !methodMatch || !pathMatch {
			if logger.IsDebug() {
				logger.Debug("Rule skipped", "index", i)
			}
			continue
		}

		matchedCount++
		lastMatchedRule = responseRuleContext{rule: rule, index: i}
		matchedRuleIndices = append(matchedRuleIndices, i)

		// Handle target path rewriting
		if rule.TargetPath != "" {
			originalPath := req.URL.Path
			req.URL.Path = rule.TargetPath
			if logger.IsDebug() {
				logger.Debug("Path rewrite applied", "index", i, "from", originalPath, "to", rule.TargetPath)
			}
		}

		// Skip body processing if no JSON body or no operations
		if !hasJSONBody {
			if logger.IsDebug() {
				logger.Debug("Rule has no JSON body to process", "index", i)
			}
			continue
		}

		if len(rule.OnRequest) == 0 {
			if logger.IsDebug() {
				logger.Debug("Rule has no on_request operations", "index", i)
			}
			continue
		}

		// Apply operations to the current (possibly modified) data
		modified, appliedValues := config.ProcessRequest(data, headers, rule.OpRule, i, method, path)

		if modified {
			anyModified = true
			// Merge applied values for debug output
			for k, v := range appliedValues {
				allAppliedValues[k] = v
			}
		}

		if logger.IsDebug() {
			if modified {
				logger.Debug("Rule applied changes", "index", i, "change_count", len(appliedValues))
			} else {
				logger.Debug("Rule made no changes", "index", i)
			}
		}
	}

	if logger.IsDebug() && matchedCount == 0 {
		logger.Debug("No rules matched request", "method", req.Method, "path", req.URL.Path)
	}

	// Store the last matching rule in context for response processing
	if lastMatchedRule.rule != nil {
		ctx := context.WithValue(req.Context(), ruleContextKey, &lastMatchedRule)
		*req = *req.WithContext(ctx)
	}

	// Write modified body back if JSON was processed
	if hasJSONBody {
		modifiedBody, err := json.Marshal(data)
		if err != nil {
			logger.Error("Failed to marshal modified request JSON", "method", method, "path", path, "err", err)
			req.Body = io.NopCloser(bytes.NewReader(body))
			return
		}

		req.Body = io.NopCloser(bytes.NewReader(modifiedBody))
		req.ContentLength = int64(len(modifiedBody))

		if anyModified && logger.IsDebug() {
			logger.Debug("Request modifications applied", "changes", len(allAppliedValues))

			for key, value := range allAppliedValues {
				if value == "<deleted>" {
					logger.Debug("Request field deleted", "key", key)
				} else {
					var originalData map[string]any
					_ = json.Unmarshal(body, &originalData)
					if _, existed := originalData[key]; existed {
						logger.Debug("Request field updated", "key", key, "value", value)
					} else {
						logger.Debug("Request field added", "key", key, "value", value)
					}
				}
			}

			if matchedCount > 0 {
				logger.Debug("Request rule summary", "method", req.Method, "path", req.URL.Path, "matched_rules", matchedRuleIndices, "changes", len(allAppliedValues))
			}
			if matchedCount == 0 {
				logger.Debug("Request matched no rules", "method", req.Method, "path", req.URL.Path)
			}

			finalBody, _ := json.MarshalIndent(data, "  ", "  ")
			logger.Debug("Outbound request body", "body", string(finalBody))
		}
	} else if len(body) > 0 {
		// Restore original non-JSON body
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
}

// ModifyResponse processes the response through matching rules
func ModifyResponse(resp *http.Response, cfg *config.Config) error {
	method := resp.Request.Method
	path := resp.Request.URL.Path
	contentType := resp.Header.Get("Content-Type")
	logger.Debug("Processing response", "status", resp.StatusCode, "content_type", contentType)

	// Get the rule from context (may be nil)
	var matchingRule *config.Rule
	ruleIndex := -1
	switch v := resp.Request.Context().Value(ruleContextKey).(type) {
	case *responseRuleContext:
		if v != nil {
			matchingRule = v.rule
			ruleIndex = v.index
		}
	case *config.Rule:
		matchingRule = v
	}

	// Route to streaming handler if SSE (log events even without on_response operations)
	if strings.Contains(contentType, "text/event-stream") {
		if matchingRule == nil || len(matchingRule.OnResponse) == 0 {
			logger.Info("Streaming response (no on_response operations)", "method", method, "path", path, "status", resp.StatusCode, "content_type", contentType)
		} else {
			logger.Info("Streaming response with transformations", "method", method, "path", path, "status", resp.StatusCode, "content_type", contentType, "rule_index", ruleIndex)
		}
		if logger.IsDebug() {
			for key, values := range sanitizeHeaders(resp.Header) {
				for _, value := range values {
					logger.Debug("Streaming response header", "key", key, "value", value)
				}
			}
		}
		return ModifyStreamingResponse(resp, matchingRule, ruleIndex)
	}

	// Read response body (limit to 10MB)
	limitedBody := io.LimitReader(resp.Body, 10*1024*1024)
	body, err := io.ReadAll(limitedBody)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if logger.IsDebug() {
		logger.Debug("Inbound response", "status", resp.StatusCode, "status_text", resp.Status)

		for key, values := range sanitizeHeaders(resp.Header) {
			for _, value := range values {
				logger.Debug("Response header", "key", key, "value", value)
			}
		}

		if len(body) > 0 {
			safeBody, truncated := sanitizeBody(body, 4096)
			logger.Debug("Response body", "body", safeBody, "truncated", truncated)
		} else {
			logger.Debug("Response body omitted", "reason", "empty")
		}
	}

	// Restore body for downstream use
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))

	if matchingRule == nil {
		logger.Info("Response skipped: no matching rule", "method", method, "path", path, "status", resp.StatusCode, "content_type", contentType)
		logger.Debug("No matching rule in context for response")
		return nil
	}

	// Skip if no response operations
	if len(matchingRule.OnResponse) == 0 {
		logger.Info("Response skipped: no on_response operations", "method", method, "path", path, "status", resp.StatusCode, "content_type", contentType)
		logger.Debug("No response operations defined for this rule")
		return nil
	}

	// Skip if not JSON
	if !strings.Contains(contentType, "application/json") {
		logger.Info("Response skipped: non-JSON content type", "method", method, "path", path, "status", resp.StatusCode, "content_type", contentType)
		logger.Debug("Skipping response modification (not JSON)")
		return nil
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		// If not JSON, return original body
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}

	// Extract response headers as map[string]string for matching
	headers := make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	if logger.IsDebug() {
		logger.Debug("Processing response operations")
	}

	modified, appliedValues := config.ProcessResponse(data, headers, matchingRule.OpRule, ruleIndex, method, path)

	modifiedBody, err := json.Marshal(data)
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return fmt.Errorf("failed to marshal modified response JSON: %w", err)
	}

	resp.Body = io.NopCloser(bytes.NewReader(modifiedBody))
	resp.ContentLength = int64(len(modifiedBody))

	if modified {
		logger.Info("Response transformed", "method", method, "path", path, "status", resp.StatusCode, "changes", len(appliedValues), "rule_index", ruleIndex)
	} else {
		logger.Info("Response unchanged after on_response", "method", method, "path", path, "status", resp.StatusCode, "rule_index", ruleIndex)
	}

	if modified && logger.IsDebug() {
		logger.Debug("Response modifications applied", "changes", len(appliedValues))

		var originalData map[string]any
		_ = json.Unmarshal(body, &originalData)

		for key, value := range appliedValues {
			if value == "<deleted>" {
				logger.Debug("Response field deleted", "key", key)
			} else if _, existed := originalData[key]; existed {
				logger.Debug("Response field updated", "key", key, "value", value)
			} else {
				logger.Debug("Response field added", "key", key, "value", value)
			}
		}

		finalBody, _ := json.MarshalIndent(data, "  ", "  ")
		logger.Debug("Outbound response body", "body", string(finalBody))
	}

	return nil
}

// ModifyStreamingResponse processes Server-Sent Events (SSE) line-by-line
func ModifyStreamingResponse(resp *http.Response, rule *config.Rule, ruleIndex int) error {
	method := resp.Request.Method
	path := resp.Request.URL.Path

	// Create a pipe for streaming transformation
	pipeReader, pipeWriter := io.Pipe()
	originalBody := resp.Body

	// Replace response body with pipe reader
	resp.Body = pipeReader

	// Start goroutine to transform and write to pipe
	go func() {
		defer pipeWriter.Close()
		defer originalBody.Close()

		scanner := bufio.NewScanner(originalBody)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 64KB initial, 1MB max line size
		logger.Info("Streaming response start", "method", method, "path", path)
		logger.Debug("Initialized streaming scanner", "max_line_size", "1MB")

		// Extract response headers for matching
		headers := make(map[string]string)
		for key, values := range resp.Header {
			if len(values) > 0 {
				headers[key] = values[0]
			}
		}

		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()

			if logger.IsDebug() {
				safeLine, truncated := sanitizeBody([]byte(line), 4096)
				logger.Debug("Streaming event received", "line", lineNum, "body", safeLine, "truncated", truncated)
			}

			if lineNum == 1 && logger.IsDebug() {
				logger.Debug("Streaming first line", "line", lineNum)
			} else if lineNum%50 == 0 && logger.IsDebug() {
				logger.Debug("Streaming heartbeat", "line", lineNum)
			}

			// Empty lines are SSE delimiters - pass through
			if line == "" {
				if _, err := pipeWriter.Write([]byte("\n")); err != nil {
					logger.Error("Failed to write empty streaming line", "err", err)
					return
				}
				continue
			}

			// Detect format and extract JSON
			var jsonData []byte
			var isSSE bool

			if strings.HasPrefix(line, "data: ") {
				// OpenAI SSE format: "data: {...}"
				isSSE = true
				jsonStr := strings.TrimPrefix(line, "data: ")

				// Handle [DONE] marker
				if jsonStr == "[DONE]" {
					if _, err := pipeWriter.Write([]byte(line + "\n")); err != nil {
						logger.Error("Failed to write streaming [DONE] marker", "err", err)
					}
					continue
				}

				jsonData = []byte(jsonStr)
			} else {
				// Ollama raw JSON format
				jsonData = []byte(line)
			}

			// Parse JSON chunk
			var data map[string]any
			if err := json.Unmarshal(jsonData, &data); err != nil {
				// Not JSON, pass through unchanged
				if _, err := pipeWriter.Write([]byte(line + "\n")); err != nil {
					logger.Error("Failed to write non-JSON streaming line", "err", err)
				}
				continue
			}

			// Apply response transformations
			modified := false
			var appliedValues map[string]any
			if rule != nil && len(rule.OnResponse) > 0 && rule.OpRule != nil {
				modified, appliedValues = config.ProcessResponse(data, headers, rule.OpRule, ruleIndex, method, path)
			}

			if logger.IsDebug() && modified {
				appliedJSON, _ := json.MarshalIndent(appliedValues, "", "  ")
				logger.Debug("Applied streaming chunk transformation", "line", lineNum, "changes", string(appliedJSON))
			}

			// Marshal back to JSON
			modifiedJSON, err := json.Marshal(data)
			if err != nil {
				logger.Error("Failed to marshal modified streaming chunk", "err", err)
				if _, err := pipeWriter.Write([]byte(line + "\n")); err != nil {
					return
				}
				continue
			}

			// Write in original format
			if isSSE {
				if _, err := pipeWriter.Write([]byte("data: ")); err != nil {
					return
				}
			}
			if _, err := pipeWriter.Write(modifiedJSON); err != nil {
				return
			}
			if _, err := pipeWriter.Write([]byte("\n")); err != nil {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			logger.Error("Streaming scanner error", "err", err)
			pipeWriter.CloseWithError(err)
		}
	}()

	return nil
}
