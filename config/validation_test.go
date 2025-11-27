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
					Routes: []Route{
						{
							Methods: newPatternField("POST"),
							Paths:   newPatternField("/v1/chat"),
							OnRequest: []Action{
								{Merge: map[string]any{"temperature": 0.7}},
							},
						},
					},
				}},
			},
			wantErr: false,
		},
		{
			name: "missing listen address",
			config: &Config{
				Proxies: ProxyEntries{{
					Target: "http://localhost:8080",
					Routes: []Route{
						{
							Methods:   newPatternField("POST"),
							Paths:     newPatternField("/v1/chat"),
							OnRequest: []Action{{Merge: map[string]any{"temp": 0.7}}},
						},
					},
				}},
			},
			wantErr: true,
			errMsg:  "proxy[0].listen is required",
		},
		{
			name: "missing target",
			config: &Config{
				Proxies: ProxyEntries{{
					Listen: "localhost:8081",
					Routes: []Route{
						{
							Methods:   newPatternField("POST"),
							Paths:     newPatternField("/v1/chat"),
							OnRequest: []Action{{Merge: map[string]any{"temp": 0.7}}},
						},
					},
				}},
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
					Routes: []Route{
						{
							Methods:   newPatternField("POST"),
							Paths:     newPatternField("/v1/chat"),
							OnRequest: []Action{{Merge: map[string]any{"temp": 0.7}}},
						},
					},
				}},
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
					Routes: []Route{
						{
							Methods:   newPatternField("POST"),
							Paths:     newPatternField("/v1/chat"),
							OnRequest: []Action{{Merge: map[string]any{"temp": 0.7}}},
						},
					},
				}},
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
					Routes: []Route{
						{
							Methods:   newPatternField("POST"),
							Paths:     newPatternField("/v1/chat"),
							OnRequest: []Action{{Merge: map[string]any{"temp": 0.7}}},
						},
					},
				}},
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
			{
				Listen: "localhost:8081", Target: "http://t1",
				Routes: []Route{
					{
						Methods:   newPatternField("GET"),
						Paths:     newPatternField("/"),
						OnRequest: []Action{{Merge: map[string]any{"x": 1}}},
					},
				},
			},
			{
				Listen: "localhost:8081", Target: "http://t2",
				Routes: []Route{
					{
						Methods:   newPatternField("GET"),
						Paths:     newPatternField("/"),
						OnRequest: []Action{{Merge: map[string]any{"x": 1}}},
					},
				},
			},
		},
	}

	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "proxy listeners must be unique") {
		t.Fatalf("expected duplicate listener error, got %v", err)
	}
}

func TestValidateOnResponseOnlyRoutes(t *testing.T) {
	cfg := &Config{
		Proxies: ProxyEntries{
			{
				Listen: "localhost:8081", Target: "http://t1",
				Routes: []Route{
					{
						Methods:    newPatternField("GET"),
						Paths:      newPatternField("/ok"),
						OnResponse: []Action{{Merge: map[string]any{"processed": true}}},
					},
				},
			},
		},
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("expected on_response-only rule to be valid, got %v", err)
	}
}

func TestValidateRoute(t *testing.T) {
	tests := []struct {
		name    string
		rule    Route
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid rule",
			rule: Route{
				Methods:   newPatternField("POST"),
				Paths:     newPatternField("/v1/chat"),
				OnRequest: []Action{{Merge: map[string]any{"temp": 0.7}}},
			},
			wantErr: false,
		},
		{
			name: "missing methods",
			rule: Route{
				Paths:     newPatternField("/v1/chat"),
				OnRequest: []Action{{Merge: map[string]any{"temp": 0.7}}},
			},
			wantErr: true,
			errMsg:  "methods required",
		},
		{
			name: "missing paths",
			rule: Route{
				Methods:   newPatternField("POST"),
				OnRequest: []Action{{Merge: map[string]any{"temp": 0.7}}},
			},
			wantErr: true,
			errMsg:  "paths required",
		},
		{
			name: "no operations",
			rule: Route{
				Methods: newPatternField("POST"),
				Paths:   newPatternField("/v1/chat"),
			},
			wantErr: true,
			errMsg:  "at least one action required",
		},
		{
			name: "invalid target path (not absolute)",
			rule: Route{
				Methods:    newPatternField("POST"),
				Paths:      newPatternField("/v1/chat"),
				TargetPath: "relative/path",
				OnRequest:  []Action{{Merge: map[string]any{"temp": 0.7}}},
			},
			wantErr: true,
			errMsg:  "target_path must be absolute",
		},
		{
			name: "invalid regex in methods",
			rule: Route{
				Methods:   newPatternField("[invalid"),
				Paths:     newPatternField("/v1/chat"),
				OnRequest: []Action{{Merge: map[string]any{"temp": 0.7}}},
			},
			wantErr: true,
			errMsg:  "invalid regex pattern",
		},
		{
			name: "invalid regex in paths",
			rule: Route{
				Methods:   newPatternField("POST"),
				Paths:     newPatternField("[invalid"),
				OnRequest: []Action{{Merge: map[string]any{"temp": 0.7}}},
			},
			wantErr: true,
			errMsg:  "invalid regex pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRoute(&tt.rule, 0)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRoute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("validateRoute() error = %v, want error containing %q", err, tt.errMsg)
			}
		})
	}
}

func TestValidateAction(t *testing.T) {
	tests := []struct {
		name    string
		op      Action
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid merge operation",
			op: Action{
				Merge: map[string]any{"temperature": 0.7},
			},
			wantErr: false,
		},
		{
			name: "valid default operation",
			op: Action{
				Default: map[string]any{"max_tokens": 1000},
			},
			wantErr: false,
		},
		{
			name: "valid delete operation",
			op: Action{
				Delete: []string{"field1", "field2"},
			},
			wantErr: false,
		},
		{
			name: "valid template operation",
			op: Action{
				Template: `{"model": "{{ .model }}"}`,
			},
			wantErr: false,
		},
		{
			name: "valid match_body filter",
			op: Action{
				MatchBody: map[string]PatternField{
					"model": newPatternField("llama.*"),
				},
				Merge: map[string]any{"temperature": 0.7},
			},
			wantErr: false,
		},
		{
			name: "valid match_headers filter",
			op: Action{
				MatchHeaders: map[string]PatternField{
					"Content-Type": newPatternField("application/json"),
				},
				Merge: map[string]any{"temperature": 0.7},
			},
			wantErr: false,
		},
		{
			name: "valid match_body and match_headers",
			op: Action{
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
			op:      Action{},
			wantErr: true,
			errMsg:  "must have at least one action",
		},
		{
			name: "invalid regex in match_body",
			op: Action{
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
			op: Action{
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
			err := validateAction(&tt.op, 0, 0, "on_request")
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAction() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("validateAction() error = %v, want error containing %q", err, tt.errMsg)
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
