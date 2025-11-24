package proxy

import (
	"bytes"
	"encoding/json"
	"strings"
)

// sanitizeBody returns a redacted, truncated string for logging JSON bodies.
func sanitizeBody(body []byte, maxBytes int) (string, bool) {
	truncated := false
	if len(body) > maxBytes {
		body = body[:maxBytes]
		truncated = true
	}

	var buf bytes.Buffer
	if err := json.Indent(&buf, body, "", "  "); err != nil {
		// Non-JSON, return as string (still truncated)
		return suffixIfTruncated(string(body), truncated), truncated
	}
	return suffixIfTruncated(buf.String(), truncated), truncated
}

func suffixIfTruncated(val string, truncated bool) string {
	if !truncated {
		return val
	}
	return val + "...[truncated]"
}

// sanitizeHeaders redacts common auth headers.
func sanitizeHeaders(headers map[string][]string) map[string][]string {
	safe := make(map[string][]string, len(headers))
	for k, vals := range headers {
		if isAuthHeader(k) {
			safe[k] = []string{"[REDACTED]"}
			continue
		}
		safe[k] = vals
	}
	return safe
}

func isAuthHeader(key string) bool {
	l := strings.ToLower(key)
	return l == "authorization" || l == "x-api-key" || l == "api-key" || l == "x-auth-token"
}
