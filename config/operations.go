package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
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
	Template string
	Filters  map[string]PatternField
	Merge    map[string]any
	Default  map[string]any
	Delete   []string
	Stop     bool
}

// SatisfiesFilter checks if data matches all filter conditions
func SatisfiesFilter(data map[string]any, filters map[string]PatternField) bool {
	if len(filters) == 0 {
		return true
	}

	for key, patterns := range filters {
		actualValue, exists := data[key]
		if !exists {
			return false
		}

		actualStr := fmt.Sprintf("%v", actualValue)
		if !patterns.Matches(actualStr) {
			return false
		}
	}
	return true
}

// ProcessRequest applies all request operations to data
func ProcessRequest(data map[string]any, rule *CompiledRule) (bool, map[string]any) {
	return processOperations(data, rule.OnRequest, rule.OnRequestTemplates)
}

// ProcessResponse applies all response operations to data
func ProcessResponse(data map[string]any, rule *CompiledRule) (bool, map[string]any) {
	return processOperations(data, rule.OnResponse, rule.OnResponseTemplates)
}

// processOperations applies operations to data with their compiled templates
func processOperations(data map[string]any, operations []OperationExec, templates []*template.Template) (bool, map[string]any) {
	appliedValues := make(map[string]any)
	anyApplied := false

	for i, op := range operations {
		if !SatisfiesFilter(data, op.Filters) {
			continue
		}

		// Execute template if present
		if op.Template != "" && templates[i] != nil {
			if ExecuteTemplate(templates[i], data, data) {
				appliedValues["template"] = "<applied>"
				anyApplied = true
			}
		}

		// Apply other operations
		beforeLen := len(appliedValues)
		if len(op.Default) > 0 {
			applyDefault(data, op.Default, appliedValues)
		}
		if len(op.Merge) > 0 {
			applyMerge(data, op.Merge, appliedValues)
		}
		if len(op.Delete) > 0 {
			applyDelete(data, op.Delete, appliedValues)
		}
		// Only mark as applied if something actually changed
		if len(appliedValues) > beforeLen {
			anyApplied = true
		}

		if op.Stop {
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

