package config

import (
	"strings"
	"testing"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &Config{
				Proxies: ProxyEntries{{
					Listen: "localhost:8081",
					Target: "http://localhost:8080",
				}},
				Rules: []Rule{
					{
						Methods: newPatternField("POST"),
						Paths:   newPatternField("/v1/chat"),
						OnRequest: []Operation{
							{Merge: map[string]any{"temperature": 0.7}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing listen address",
			config: &Config{
				Proxies: ProxyEntries{{
					Target: "http://localhost:8080",
				}},
				Rules: []Rule{
					{
						Methods:   newPatternField("POST"),
						Paths:     newPatternField("/v1/chat"),
						OnRequest: []Operation{{Merge: map[string]any{"temp": 0.7}}},
					},
				},
			},
			wantErr: true,
			errMsg:  "proxy[0].listen is required",
		},
		{
			name: "missing target",
			config: &Config{
				Proxies: ProxyEntries{{
					Listen: "localhost:8081",
				}},
				Rules: []Rule{
					{
						Methods:   newPatternField("POST"),
						Paths:     newPatternField("/v1/chat"),
						OnRequest: []Operation{{Merge: map[string]any{"temp": 0.7}}},
					},
				},
			},
			wantErr: true,
			errMsg:  "proxy[0].target is required",
		},
		{
			name: "invalid target URL",
			config: &Config{
				Proxies: ProxyEntries{{
					Listen: "localhost:8081",
					Target: "://invalid",
				}},
				Rules: []Rule{
					{
						Methods:   newPatternField("POST"),
						Paths:     newPatternField("/v1/chat"),
						OnRequest: []Operation{{Merge: map[string]any{"temp": 0.7}}},
					},
				},
			},
			wantErr: true,
			errMsg:  "proxy[0].target URL is invalid",
		},
		{
			name: "SSL cert without key",
			config: &Config{
				Proxies: ProxyEntries{{
					Listen:  "localhost:8081",
					Target:  "http://localhost:8080",
					SSLCert: "cert.pem",
				}},
				Rules: []Rule{
					{
						Methods:   newPatternField("POST"),
						Paths:     newPatternField("/v1/chat"),
						OnRequest: []Operation{{Merge: map[string]any{"temp": 0.7}}},
					},
				},
			},
			wantErr: true,
			errMsg:  "both ssl_cert and ssl_key must be provided together",
		},
		{
			name: "SSL key without cert",
			config: &Config{
				Proxies: ProxyEntries{{
					Listen: "localhost:8081",
					Target: "http://localhost:8080",
					SSLKey: "key.pem",
				}},
				Rules: []Rule{
					{
						Methods:   newPatternField("POST"),
						Paths:     newPatternField("/v1/chat"),
						OnRequest: []Operation{{Merge: map[string]any{"temp": 0.7}}},
					},
				},
			},
			wantErr: true,
			errMsg:  "both ssl_cert and ssl_key must be provided together",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Validate() error = %v, want error containing %q", err, tt.errMsg)
			}
		})
	}
}

func TestValidateDuplicateListeners(t *testing.T) {
	cfg := &Config{
		Proxies: ProxyEntries{
			{Listen: "localhost:8081", Target: "http://t1"},
			{Listen: "localhost:8081", Target: "http://t2"},
		},
		Rules: []Rule{
			{
				Methods:   newPatternField("GET"),
				Paths:     newPatternField("/"),
				OnRequest: []Operation{{Merge: map[string]any{"x": 1}}},
			},
		},
	}

	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "proxy listeners must be unique") {
		t.Fatalf("expected duplicate listener error, got %v", err)
	}
}

func TestValidateOnResponseOnlyRules(t *testing.T) {
	cfg := &Config{
		Proxies: ProxyEntries{
			{Listen: "localhost:8081", Target: "http://t1"},
		},
		Rules: []Rule{
			{
				Methods:    newPatternField("GET"),
				Paths:      newPatternField("/ok"),
				OnResponse: []Operation{{Merge: map[string]any{"processed": true}}},
			},
		},
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("expected on_response-only rule to be valid, got %v", err)
	}
}

func TestValidateRule(t *testing.T) {
	tests := []struct {
		name    string
		rule    Rule
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid rule",
			rule: Rule{
				Methods:   newPatternField("POST"),
				Paths:     newPatternField("/v1/chat"),
				OnRequest: []Operation{{Merge: map[string]any{"temp": 0.7}}},
			},
			wantErr: false,
		},
		{
			name: "missing methods",
			rule: Rule{
				Paths:     newPatternField("/v1/chat"),
				OnRequest: []Operation{{Merge: map[string]any{"temp": 0.7}}},
			},
			wantErr: true,
			errMsg:  "methods required",
		},
		{
			name: "missing paths",
			rule: Rule{
				Methods:   newPatternField("POST"),
				OnRequest: []Operation{{Merge: map[string]any{"temp": 0.7}}},
			},
			wantErr: true,
			errMsg:  "paths required",
		},
		{
			name: "no operations",
			rule: Rule{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("/v1/chat"),
			},
			wantErr: true,
			errMsg:  "at least one operation required",
		},
		{
			name: "invalid target path (not absolute)",
			rule: Rule{
				Methods:    newPatternField("POST"),
				Paths:      newPatternField("/v1/chat"),
				TargetPath: "relative/path",
				OnRequest:  []Operation{{Merge: map[string]any{"temp": 0.7}}},
			},
			wantErr: true,
			errMsg:  "target_path must be absolute",
		},
		{
			name: "invalid regex in methods",
			rule: Rule{
				Methods:   newPatternField("[invalid"),
				Paths:     newPatternField("/v1/chat"),
				OnRequest: []Operation{{Merge: map[string]any{"temp": 0.7}}},
			},
			wantErr: true,
			errMsg:  "invalid regex pattern",
		},
		{
			name: "invalid regex in paths",
			rule: Rule{
				Methods:   newPatternField("POST"),
				Paths:     newPatternField("[invalid"),
				OnRequest: []Operation{{Merge: map[string]any{"temp": 0.7}}},
			},
			wantErr: true,
			errMsg:  "invalid regex pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRule(&tt.rule, 0)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRule() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("validateRule() error = %v, want error containing %q", err, tt.errMsg)
			}
		})
	}
}

func TestValidateOperation(t *testing.T) {
	tests := []struct {
		name    string
		op      Operation
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid merge operation",
			op: Operation{
				Merge: map[string]any{"temperature": 0.7},
			},
			wantErr: false,
		},
		{
			name: "valid default operation",
			op: Operation{
				Default: map[string]any{"max_tokens": 1000},
			},
			wantErr: false,
		},
		{
			name: "valid delete operation",
			op: Operation{
				Delete: []string{"field1", "field2"},
			},
			wantErr: false,
		},
		{
			name: "valid template operation",
			op: Operation{
				Template: `{"model": "{{ .model }}"}`,
			},
			wantErr: false,
		},
		{
			name: "valid match_body filter",
			op: Operation{
				MatchBody: map[string]PatternField{
					"model": newPatternField("llama.*"),
				},
				Merge: map[string]any{"temperature": 0.7},
			},
			wantErr: false,
		},
		{
			name: "valid match_headers filter",
			op: Operation{
				MatchHeaders: map[string]PatternField{
					"Content-Type": newPatternField("application/json"),
				},
				Merge: map[string]any{"temperature": 0.7},
			},
			wantErr: false,
		},
		{
			name: "valid match_body and match_headers",
			op: Operation{
				MatchBody: map[string]PatternField{
					"model": newPatternField("gpt.*"),
				},
				MatchHeaders: map[string]PatternField{
					"X-API-Key": newPatternField(".*"),
				},
				Merge: map[string]any{"temperature": 0.7},
			},
			wantErr: false,
		},
		{
			name:    "no actions",
			op:      Operation{},
			wantErr: true,
			errMsg:  "must have at least one action",
		},
		{
			name: "invalid regex in match_body",
			op: Operation{
				MatchBody: map[string]PatternField{
					"model": newPatternField("[invalid"),
				},
				Merge: map[string]any{"temp": 0.7},
			},
			wantErr: true,
			errMsg:  "invalid regex pattern",
		},
		{
			name: "invalid regex in match_headers",
			op: Operation{
				MatchHeaders: map[string]PatternField{
					"Content-Type": newPatternField("[invalid"),
				},
				Merge: map[string]any{"temp": 0.7},
			},
			wantErr: true,
			errMsg:  "invalid regex pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOperation(&tt.op, 0, 0, "on_request")
			if (err != nil) != tt.wantErr {
				t.Errorf("validateOperation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("validateOperation() error = %v, want error containing %q", err, tt.errMsg)
			}
		})
	}
}

func TestPatternFieldValidate(t *testing.T) {
	tests := []struct {
		name    string
		pattern PatternField
		wantErr bool
	}{
		{
			name:    "valid simple pattern",
			pattern: PatternField{Patterns: []string{"llama3"}},
			wantErr: false,
		},
		{
			name:    "valid regex pattern",
			pattern: PatternField{Patterns: []string{"llama.*", "gpt-?[0-9]+"}},
			wantErr: false,
		},
		{
			name:    "invalid regex",
			pattern: PatternField{Patterns: []string{"[unclosed"}},
			wantErr: true,
		},
		{
			name:    "one invalid in multiple",
			pattern: PatternField{Patterns: []string{"valid", "[invalid", "also-valid"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pattern.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("PatternField.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
