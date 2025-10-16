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

// FindMatchingRules returns all rules that match the request sequentially
func FindMatchingRules(req *http.Request, cfg *config.Config) []*config.Rule {
	logDebug("Evaluating %d rules for %s %s", len(cfg.Rules), req.Method, req.URL.Path)

	var matchedRules []*config.Rule

	for i := range cfg.Rules {
		rule := &cfg.Rules[i]
		methodMatch := rule.Methods.Matches(req.Method)
		pathMatch := rule.Paths.Matches(req.URL.Path)

		logDebug("  Rule %d: methods=%v paths=%v (method_match=%v, path_match=%v)",
			i, rule.Methods.Patterns, rule.Paths.Patterns, methodMatch, pathMatch)

		if methodMatch && pathMatch {
			logDebug("  ✓ Rule %d matched!", i)
			matchedRules = append(matchedRules, rule)
		}
	}

	if len(matchedRules) == 0 {
		logDebug("  No rules matched")
	} else {
		logDebug("  Total matched rules: %d", len(matchedRules))
	}

	return matchedRules
}

// ModifyRequest processes the request through rules sequentially
// Each rule is checked and processed immediately before moving to the next rule
func ModifyRequest(req *http.Request, cfg *config.Config) {
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

	if debugMode && len(body) > 0 {
		var prettyJSON bytes.Buffer
		json.Indent(&prettyJSON, body, "", "  ")
		logDebug("Inbound request body:\n%s", prettyJSON.String())
	}

	// Parse JSON body if present
	var data map[string]any
	hasJSONBody := false
	if len(body) > 0 {
		if err := json.Unmarshal(body, &data); err == nil {
			hasJSONBody = true
		} else {
			logDebug("Request body is not JSON, will pass through unchanged")
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

	logDebug("Evaluating %d rules sequentially for %s %s", len(cfg.Rules), req.Method, req.URL.Path)

	// Track the last matched rule for response processing
	var lastMatchedRule *config.Rule
	anyModified := false
	allAppliedValues := make(map[string]any)
	matchedCount := 0

	// Process rules sequentially: check and apply each rule before moving to next
	for i := range cfg.Rules {
		rule := &cfg.Rules[i]

		// Check if this rule matches (method and path)
		methodMatch := rule.Methods.Matches(req.Method)
		pathMatch := rule.Paths.Matches(req.URL.Path)

		logDebug("  Rule %d: methods=%v paths=%v (method_match=%v, path_match=%v)",
			i, rule.Methods.Patterns, rule.Paths.Patterns, methodMatch, pathMatch)

		if !methodMatch || !pathMatch {
			logDebug("  Rule %d: skipped (no match)", i)
			continue
		}

		logDebug("  ✓ Rule %d matched!", i)
		matchedCount++
		lastMatchedRule = rule

		// Handle target path rewriting
		if rule.TargetPath != "" {
			originalPath := req.URL.Path
			req.URL.Path = rule.TargetPath
			logDebug("  Rule %d: rewrote path from %s to %s", i, originalPath, rule.TargetPath)
		}

		// Skip body processing if no JSON body or no operations
		if !hasJSONBody {
			logDebug("  Rule %d: skipping body processing (no JSON body)", i)
			continue
		}

		if len(rule.OnRequest) == 0 {
			logDebug("  Rule %d: no on_request operations", i)
			continue
		}

		// Apply operations to the current (possibly modified) data
		logDebug("  Rule %d: applying %d on_request operation(s)", i, len(rule.OnRequest))
		modified, appliedValues := config.ProcessRequest(data, headers, rule.OpRule)

		if modified {
			anyModified = true
			// Merge applied values for debug output
			for k, v := range appliedValues {
				allAppliedValues[k] = v
			}
			logDebug("  Rule %d: modified request", i)
		}
	}

	if matchedCount == 0 {
		logDebug("No rules matched for %s %s", req.Method, req.URL.Path)
	} else {
		logDebug("Total matched rules: %d", matchedCount)
	}

	// Store the last matching rule in context for response processing
	if lastMatchedRule != nil {
		ctx := context.WithValue(req.Context(), ruleContextKey, lastMatchedRule)
		*req = *req.WithContext(ctx)
	}

	// Write modified body back if JSON was processed
	if hasJSONBody {
		modifiedBody, err := json.Marshal(data)
		if err != nil {
			log.Printf("Failed to marshal modified JSON: %v", err)
			req.Body = io.NopCloser(bytes.NewReader(body))
			return
		}

		req.Body = io.NopCloser(bytes.NewReader(modifiedBody))
		req.ContentLength = int64(len(modifiedBody))

		if anyModified {
			appliedJSON, _ := json.MarshalIndent(allAppliedValues, "", "  ")
			logDebug("Total applied request changes:\n%s", string(appliedJSON))
		}
	} else if len(body) > 0 {
		// Restore original non-JSON body
		req.Body = io.NopCloser(bytes.NewReader(body))
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
