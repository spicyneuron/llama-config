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

	"github.com/spicyneuron/llama-matchmaker/config"
	"github.com/spicyneuron/llama-matchmaker/logger"
)

type contextKey string

const routeContextKey contextKey = "matched_route"

type responseRouteContext struct {
	rules   []*config.Route
	indices []int
}

func headersJSON(headers map[string][]string) string {
	safe := sanitizeHeaders(headers)
	flattened := make(map[string]any, len(safe))
	for k, vals := range safe {
		if len(vals) == 1 {
			flattened[k] = vals[0]
		} else {
			flattened[k] = vals
		}
	}

	b, err := json.MarshalIndent(flattened, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

// FindMatchingRoutes returns all routes that match the request sequentially
func FindMatchingRoutes(req *http.Request, cfg *config.Config) []*config.Route {
	logger.Debug("Evaluating routes for request", "route_count", len(cfg.Routes), "method", req.Method, "path", req.URL.Path)

	var matchedRoutes []*config.Route

	for i := range cfg.Routes {
		route := &cfg.Routes[i]
		methodMatch := route.Methods.Matches(req.Method)
		pathMatch := route.Paths.Matches(req.URL.Path)

		logger.Debug("Route evaluation", "index", i, "methods", route.Methods.Patterns, "paths", route.Paths.Patterns, "method_match", methodMatch, "path_match", pathMatch)

		if methodMatch && pathMatch {
			logger.Debug("Route matched", "index", i)
			matchedRoutes = append(matchedRoutes, route)
		}
	}

	if len(matchedRoutes) == 0 {
		logger.Debug("No routes matched for request")
	} else {
		logger.Debug("Matched routes for request", "count", len(matchedRoutes))
	}

	return matchedRoutes
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

	logger.Info("Inbound request", "method", method, "path", path)

	if logger.IsDebug() {
		logger.Debug("Request headers", "headers", headersJSON(req.Header))

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

	// Track matched routes for response processing
	var matchedResponseRoutes responseRouteContext
	anyModified := false
	allAppliedValues := make(map[string]any)
	matchedCount := 0

	// Process rules sequentially: check and apply each rule before moving to next
	for i := range cfg.Routes {
		rule := &cfg.Routes[i]

		// Check if this rule matches (method and path)
		methodMatch := rule.Methods.Matches(req.Method)
		pathMatch := rule.Paths.Matches(req.URL.Path)

		if !methodMatch || !pathMatch {
			logger.Debug("Route skipped", "index", i, "methods", rule.Methods.Patterns, "paths", rule.Paths.Patterns)
			continue
		}

		logger.Debug("Route matched", "index", i, "methods", rule.Methods.Patterns, "paths", rule.Paths.Patterns)

		matchedCount++
		matchedResponseRoutes.rules = append(matchedResponseRoutes.rules, rule)
		matchedResponseRoutes.indices = append(matchedResponseRoutes.indices, i)

		// Handle target path rewriting
		if rule.TargetPath != "" {
			originalPath := req.URL.Path
			if rule.TargetPath != originalPath {
				req.URL.Path = rule.TargetPath
				logger.Debug("Route path rewrite applied", "index", i, "from", originalPath, "to", rule.TargetPath)
			}
		}

		// Skip body processing if no JSON body or no actions
		if !hasJSONBody || len(rule.OnRequest) == 0 {
			continue
		}

		// Apply actions to the current (possibly modified) data
		modified, appliedValues := config.ProcessRequest(data, headers, rule.Compiled, i, method, path)

		if modified {
			anyModified = true
			// Merge applied values for debug output
			for k, v := range appliedValues {
				allAppliedValues[k] = v
			}
		}

	}

	// Store the matching routes in context for response processing
	if len(matchedResponseRoutes.rules) > 0 {
		ctx := context.WithValue(req.Context(), routeContextKey, &matchedResponseRoutes)
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

		fields := []any{
			"method", method,
			"path", path,
			"changes", len(allAppliedValues),
		}
		if matchedCount > 0 {
			fields = append(fields, "matched_routes", matchedResponseRoutes.indices)
		}
		logger.Info("Outbound request", fields...)

		if anyModified && logger.IsDebug() {
			finalBody, _ := json.MarshalIndent(data, "  ", "  ")
			logger.Debug("Outbound request body", "body", string(finalBody))
		}
	} else if len(body) > 0 {
		// Restore original non-JSON body
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
}

// ModifyResponse processes the response through matching routes
func ModifyResponse(resp *http.Response, cfg *config.Config) error {
	method := resp.Request.Method
	path := resp.Request.URL.Path
	contentType := resp.Header.Get("Content-Type")

	// Get the routes from context (may be nil)
	var matchedRoutes []*config.Route
	var matchedRouteIndices []int
	switch v := resp.Request.Context().Value(routeContextKey).(type) {
	case *responseRouteContext:
		if v != nil {
			matchedRoutes = v.rules
			matchedRouteIndices = v.indices
		}
	case *config.Route:
		matchedRoutes = []*config.Route{v}
	}

	if len(matchedRoutes) > 0 && len(matchedRouteIndices) != len(matchedRoutes) {
		// Ensure indices slice aligns with routes length (backward compatibility for contexts without indices)
		matchedRouteIndices = make([]int, len(matchedRoutes))
		for i := range matchedRouteIndices {
			matchedRouteIndices[i] = -1
		}
	}

	// Route to streaming handler if SSE (log events even without on_response operations)
	if strings.Contains(contentType, "text/event-stream") {
		if len(matchedRoutes) == 0 {
			logger.Info("Streaming response", "method", method, "path", path, "status", resp.StatusCode, "content_type", contentType)
		} else {
			logger.Info("Streaming response", "method", method, "path", path, "status", resp.StatusCode, "content_type", contentType, "matched_routes", matchedRouteIndices)
		}
		if logger.IsDebug() {
			logger.Debug("Streaming response headers", "headers", headersJSON(resp.Header))
		}
		return ModifyStreamingResponse(resp, matchedRoutes, matchedRouteIndices)
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

		logger.Debug("Response headers", "headers", headersJSON(resp.Header))

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

	if len(matchedRoutes) == 0 {
		logger.Info("Outbound response", "method", method, "path", path, "status", resp.StatusCode, "changes", 0, "reason", "no_matching_rule", "content_type", contentType)
		return nil
	}

	// Skip if no response operations in any matched rule
	hasResponseOps := false
	for _, r := range matchedRoutes {
		if len(r.OnResponse) > 0 {
			hasResponseOps = true
			break
		}
	}
	if !hasResponseOps {
		logger.Info("Outbound response", "method", method, "path", path, "status", resp.StatusCode, "changes", 0, "reason", "no_on_response_operations", "matched_routes", matchedRouteIndices, "content_type", contentType)
		return nil
	}

	// Skip if not JSON
	if !strings.Contains(contentType, "application/json") {
		logger.Info("Outbound response", "method", method, "path", path, "status", resp.StatusCode, "changes", 0, "reason", "non_json", "matched_routes", matchedRouteIndices, "content_type", contentType)
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

	anyModified := false
	appliedValues := make(map[string]any)
	for i, route := range matchedRoutes {
		if len(route.OnResponse) == 0 || route.Compiled == nil {
			continue
		}
		modified, vals := config.ProcessResponse(data, headers, route.Compiled, matchedRouteIndices[i], method, path)
		if modified {
			anyModified = true
		}
		for k, v := range vals {
			appliedValues[k] = v
		}
	}

	modifiedBody, err := json.Marshal(data)
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return fmt.Errorf("failed to marshal modified response JSON: %w", err)
	}

	resp.Body = io.NopCloser(bytes.NewReader(modifiedBody))
	resp.ContentLength = int64(len(modifiedBody))

	fields := []any{
		"method", method,
		"path", path,
		"status", resp.StatusCode,
		"changes", len(appliedValues),
	}
	if len(matchedRouteIndices) > 0 {
		fields = append(fields, "matched_routes", matchedRouteIndices)
	}
	logger.Info("Outbound response", fields...)

	if anyModified && logger.IsDebug() {
		finalBody, _ := json.MarshalIndent(data, "  ", "  ")
		logger.Debug("Outbound response body", "body", string(finalBody))
	}

	return nil
}

// ModifyStreamingResponse processes Server-Sent Events (SSE) line-by-line
func ModifyStreamingResponse(resp *http.Response, routes []*config.Route, routeIndices []int) error {
	method := resp.Request.Method
	path := resp.Request.URL.Path

	if len(routes) > 0 && len(routeIndices) != len(routes) {
		routeIndices = make([]int, len(routes))
		for i := range routeIndices {
			routeIndices[i] = -1
		}
	}

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
			appliedValues := make(map[string]any)
			for i, rule := range routes {
				if rule == nil || len(rule.OnResponse) == 0 || rule.Compiled == nil {
					continue
				}
				changed, vals := config.ProcessResponse(data, headers, rule.Compiled, routeIndices[i], method, path)
				if changed {
					modified = true
					for k, v := range vals {
						appliedValues[k] = v
					}
				}
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
