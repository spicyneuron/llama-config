package proxy

import (
	"bufio"
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
	logDebug("Evaluating %d rules for %s %s", len(cfg.Rules), req.Method, req.URL.Path)

	for i := range cfg.Rules {
		rule := &cfg.Rules[i]
		methodMatch := rule.Methods.Matches(req.Method)
		pathMatch := rule.Paths.Matches(req.URL.Path)

		logDebug("  Rule %d: methods=%v paths=%v (method_match=%v, path_match=%v)",
			i, rule.Methods.Patterns, rule.Paths.Patterns, methodMatch, pathMatch)

		if methodMatch && pathMatch {
			logDebug("  ✓ Rule %d matched!", i)
			return rule
		}
	}
	logDebug("  No rules matched")
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

	logDebug("Processing request with matching rule (on_request ops: %d)", len(matchingRule.OnRequest))

	if matchingRule.TargetPath != "" {
		originalPath := req.URL.Path
		req.URL.Path = matchingRule.TargetPath
		logDebug("Rewrote request path from %s to %s", originalPath, matchingRule.TargetPath)
	}

	// Read and limit body size to 10MB to prevent memory exhaustion
	var body []byte
	var err error
	if req.Body != nil {
		limitedBody := io.LimitReader(req.Body, 10*1024*1024)
		body, err = io.ReadAll(limitedBody)
		req.Body.Close()
		if err != nil {
			log.Printf("Failed to read request body: %v", err)
			return
		}
	}

	// Skip processing if there's no body
	if len(body) == 0 {
		logDebug("Skipping request body processing (no body)")
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

	// Extract headers as map[string]string for matching
	headers := make(map[string]string)
	for key, values := range req.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	modified, appliedValues := config.ProcessRequest(data, headers, matchingRule.OpRule)

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
	contentType := resp.Header.Get("Content-Type")
	logDebug("Processing response: status=%d, content-type=%s", resp.StatusCode, contentType)

	// Get the rule from context
	matchingRule, ok := resp.Request.Context().Value(ruleContextKey).(*config.Rule)
	if !ok || matchingRule == nil {
		logDebug("No matching rule in context for response")
		return nil
	}

	// Skip if no response operations
	if len(matchingRule.OnResponse) == 0 {
		logDebug("No response operations defined for this rule")
		return nil
	}

	// Route to streaming handler if SSE
	if strings.Contains(contentType, "text/event-stream") {
		logDebug("Routing to streaming response handler")
		return ModifyStreamingResponse(resp, matchingRule)
	}

	// Skip if not JSON
	if !strings.Contains(contentType, "application/json") {
		logDebug("Skipping response modification (not JSON)")
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

	// Extract response headers as map[string]string for matching
	headers := make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	modified, appliedValues := config.ProcessResponse(data, headers, matchingRule.OpRule)

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

// ModifyStreamingResponse processes Server-Sent Events (SSE) line-by-line
func ModifyStreamingResponse(resp *http.Response, rule *config.Rule) error {
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
		logDebug("Initialized streaming scanner (max line size: 1MB)")

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

			// Log every 10 lines to avoid spam
			if lineNum%10 == 1 {
				logDebug("Processing streaming line %d", lineNum)
			}

			// Empty lines are SSE delimiters - pass through
			if line == "" {
				if _, err := pipeWriter.Write([]byte("\n")); err != nil {
					logDebug("Failed to write empty line: %v", err)
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
						logDebug("Failed to write [DONE]: %v", err)
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
					logDebug("Failed to write non-JSON line: %v", err)
				}
				continue
			}

			// Apply response transformations
			modified, appliedValues := config.ProcessResponse(data, headers, rule.OpRule)

			if debugMode && modified {
				appliedJSON, _ := json.MarshalIndent(appliedValues, "", "  ")
				logDebug("Applied streaming chunk transformation (line %d):\n%s", lineNum, string(appliedJSON))
			}

			// Marshal back to JSON
			modifiedJSON, err := json.Marshal(data)
			if err != nil {
				logDebug("Failed to marshal modified chunk: %v", err)
				// Write original on error
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
			logDebug("Scanner error: %v", err)
			pipeWriter.CloseWithError(err)
		}
	}()

	return nil
}
