package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"text/template"
	"time"
)

// CompiledRule holds a rule with compiled templates
type CompiledRule struct {
	OnRequest           []OperationExec
	OnResponse          []OperationExec
	OnRequestTemplates  []*template.Template
	OnResponseTemplates []*template.Template
}

// OperationExec represents an operation during execution (converted from Operation)
type OperationExec struct {
	MatchBody    map[string]PatternField
	MatchHeaders map[string]PatternField
	Template     string
	Merge        map[string]any
	Default      map[string]any
	Delete       []string
	Stop         bool
}

// toStringMap converts map[string]any to map[string]string for pattern matching
func toStringMap(data map[string]any) map[string]string {
	result := make(map[string]string, len(data))
	for key, value := range data {
		result[key] = fmt.Sprintf("%v", value)
	}
	return result
}

// ProcessRequest applies all request operations to data
func ProcessRequest(data map[string]any, headers map[string]string, rule *CompiledRule, ruleIndex int) (bool, map[string]any) {
	return processOperations(data, headers, rule.OnRequest, rule.OnRequestTemplates)
}

// ProcessResponse applies all response operations to data
func ProcessResponse(data map[string]any, headers map[string]string, rule *CompiledRule) (bool, map[string]any) {
	return processOperations(data, headers, rule.OnResponse, rule.OnResponseTemplates)
}

// processOperations applies operations to data with their compiled templates
func processOperations(data map[string]any, headers map[string]string, operations []OperationExec, templates []*template.Template) (bool, map[string]any) {
	appliedValues := make(map[string]any)
	anyApplied := false

	// Convert body data to strings for pattern matching
	bodyStrings := toStringMap(data)

	for i, op := range operations {
		logDebug("  Operation %d:", i)

		// Show match conditions
		hasConditions := len(op.MatchBody) > 0 || len(op.MatchHeaders) > 0
		if hasConditions {
			logDebug("    Conditions:")
		}

		// Check body matching
		bodyMatch := true
		if len(op.MatchBody) > 0 {
			for key, pattern := range op.MatchBody {
				actualValue, exists := bodyStrings[key]
				if !exists {
					logDebug("      ✗ %s (key not found)", key)
					bodyMatch = false
					break
				}
				if !pattern.Matches(actualValue) {
					logDebug("      ✗ %s=\"%s\" does not match %v", key, actualValue, pattern.Patterns)
					bodyMatch = false
					break
				}
				logDebug("      ✓ %s=\"%s\" matches %v", key, actualValue, pattern.Patterns)
			}
		}

		if !bodyMatch {
			logDebug("    Status: SKIPPED")
			logDebug("")
			continue
		}

		// Check headers matching
		headersMatch := true
		if len(op.MatchHeaders) > 0 {
			for key, pattern := range op.MatchHeaders {
				actualValue, exists := headers[key]
				if !exists {
					logDebug("      ✗ %s (header not found)", key)
					headersMatch = false
					break
				}
				if !pattern.Matches(actualValue) {
					logDebug("      ✗ %s=\"%s\" does not match %v", key, actualValue, pattern.Patterns)
					headersMatch = false
					break
				}
				logDebug("      ✓ %s=\"%s\" (header)", key, actualValue)
			}
		}

		if !headersMatch {
			logDebug("    Status: SKIPPED")
			logDebug("")
			continue
		}

		if hasConditions {
			logDebug("")
		}

		// Capture values before for diff
		beforeValues := make(map[string]any)
		for k, v := range data {
			beforeValues[k] = v
		}

		// Track changes for this specific operation
		opChanges := make(map[string]any)

		// Execute template if present
		if op.Template != "" && templates[i] != nil {
			if ExecuteTemplate(templates[i], data, data) {
				maps.Copy(appliedValues, data)
				maps.Copy(opChanges, data)
				anyApplied = true
			}
		}

		// Apply other operations
		if len(op.Default) > 0 {
			applyDefault(data, op.Default, opChanges)
			for k, v := range opChanges {
				appliedValues[k] = v
			}
		}
		if len(op.Merge) > 0 {
			applyMerge(data, op.Merge, opChanges)
			for k, v := range opChanges {
				appliedValues[k] = v
			}
		}
		if len(op.Delete) > 0 {
			applyDelete(data, op.Delete, opChanges)
			for k, v := range opChanges {
				appliedValues[k] = v
			}
		}

		// Show changes if any
		if len(opChanges) > 0 {
			anyApplied = true

			logDebug("    Changes:")
			for key, newValue := range opChanges {
				if newValue == "<deleted>" {
					logDebug("      - %s", key)
				} else if oldValue, existed := beforeValues[key]; existed {
					logDebug("      ~ %s: %v -> %v", key, oldValue, newValue)
				} else {
					logDebug("      + %s: %v", key, newValue)
				}
			}
			logDebug("")
		} else if len(op.Default) > 0 || len(op.Merge) > 0 || len(op.Delete) > 0 {
			logDebug("    No changes (all conditions already met)")
			logDebug("")
		}

		if op.Stop {
			logDebug("    Stop flag set - halting operation processing")
			logDebug("")
			break
		}
	}
	return anyApplied, appliedValues
}

func applyMerge(data map[string]any, mergeValues map[string]any, appliedValues map[string]any) {
	for key, value := range mergeValues {
		data[key] = value
		appliedValues[key] = value
	}
}

func applyDefault(data map[string]any, defaultValues map[string]any, appliedValues map[string]any) {
	for key, value := range defaultValues {
		if _, exists := data[key]; !exists {
			data[key] = value
			appliedValues[key] = value
		}
	}
}

func applyDelete(data map[string]any, deleteKeys []string, appliedValues map[string]any) {
	for _, key := range deleteKeys {
		if _, exists := data[key]; exists {
			delete(data, key)
			appliedValues[key] = "<deleted>"
		}
	}
}

// TemplateFuncs provides helper functions for Go templates
var TemplateFuncs = template.FuncMap{
	// JSON marshaling
	"toJson": func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			log.Printf("toJson error: %v", err)
			return "null"
		}
		return string(b)
	},

	// Default value if nil/missing
	"default": func(def, val any) any {
		if val == nil {
			return def
		}
		// Check for zero values
		switch v := val.(type) {
		case string:
			if v == "" {
				return def
			}
		case float64:
			if v == 0 {
				return def
			}
		case bool:
			if !v {
				return def
			}
		}
		return val
	},

	// Time functions
	"now": time.Now,
	"isoTime": func(t time.Time) string {
		return t.Format(time.RFC3339)
	},
	"unixTime": func(t time.Time) int64 {
		return t.Unix()
	},

	// UUID generation
	"uuid": func() string {
		return generateUUID()
	},

	// Array/slice access - provides consistent interface with other helpers
	// Note: Go templates have built-in 'index', but we expose it explicitly for clarity
	"index": func(item any, indices ...any) any {
		return templateIndex(item, indices...)
	},

	// Math operations
	"add": func(a, b any) any {
		return toNumber(a) + toNumber(b)
	},
	"mul": func(a, b any) any {
		return toNumber(a) * toNumber(b)
	},

	// Create map/dict - variadic key-value pairs
	// Usage: {{ dict "key1" "value1" "key2" "value2" }}
	"dict": func(pairs ...any) map[string]any {
		if len(pairs)%2 != 0 {
			log.Printf("dict: odd number of arguments")
			return map[string]any{}
		}
		result := make(map[string]any, len(pairs)/2)
		for i := 0; i < len(pairs); i += 2 {
			key, ok := pairs[i].(string)
			if !ok {
				log.Printf("dict: non-string key at position %d: %T", i, pairs[i])
				continue
			}
			result[key] = pairs[i+1]
		}
		return result
	},

	// Type checking
	// Usage: {{ kindIs "string" .value }} or {{ kindIs "slice" .items }}
	"kindIs": func(kind string, value any) bool {
		return checkKind(kind, value)
	},
}

func generateUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}

	// Set version (4) and variant (RFC 4122) bits
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant RFC 4122

	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]))
}

// templateIndex provides array/slice/map access for templates
// Supports: index array 0, index map "key", index array 0 "subkey"
func templateIndex(item any, indices ...any) any {
	if len(indices) == 0 {
		return item
	}

	current := item
	for _, idx := range indices {
		switch v := current.(type) {
		case []any:
			i, ok := toInt(idx)
			if !ok || i < 0 || i >= len(v) {
				log.Printf("index: invalid array index %v for array of length %d", idx, len(v))
				return nil
			}
			current = v[i]
		case map[string]any:
			key, ok := idx.(string)
			if !ok {
				log.Printf("index: non-string key %v for map", idx)
				return nil
			}
			var exists bool
			current, exists = v[key]
			if !exists {
				log.Printf("index: key %q not found in map", key)
				return nil
			}
		default:
			log.Printf("index: cannot index type %T", current)
			return nil
		}
	}
	return current
}

// toInt converts any numeric value to int
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		// Try to parse string as int
		var i int
		if _, err := fmt.Sscanf(n, "%d", &i); err == nil {
			return i, true
		}
	}
	return 0, false
}

// toNumber converts any numeric value to float64 for math operations
func toNumber(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	case float32:
		return float64(n)
	case string:
		var f float64
		if _, err := fmt.Sscanf(n, "%f", &f); err == nil {
			return f
		}
	}
	return 0
}

// checkKind checks if a value is of a specific kind
// Supported kinds: "string", "number", "bool", "slice", "array", "map", "nil"
func checkKind(kind string, value any) bool {
	if value == nil {
		return kind == "nil"
	}

	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "number", "float", "int":
		switch value.(type) {
		case int, int64, float64, float32:
			return true
		}
		return false
	case "bool":
		_, ok := value.(bool)
		return ok
	case "slice", "array":
		_, ok := value.([]any)
		return ok
	case "map":
		_, ok := value.(map[string]any)
		return ok
	case "nil":
		return value == nil
	default:
		log.Printf("kindIs: unknown kind %q", kind)
		return false
	}
}

// ExecuteTemplate applies a template to input data and updates output
func ExecuteTemplate(tmpl *template.Template, input map[string]any, output map[string]any) bool {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, input); err != nil {
		log.Printf("Template execution error: %v", err)
		return false
	}

	// Parse the template output as JSON
	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		log.Printf("Template output is not valid JSON: %v\nOutput: %s", err, buf.String())
		return false
	}

	// Replace output map contents with template result
	for k := range output {
		delete(output, k)
	}
	for k, v := range result {
		output[k] = v
	}

	return true
}
