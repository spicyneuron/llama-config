package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Validate checks the entire configuration for errors
func Validate(config *Config) error {
	if config.Proxy.Listen == "" {
		return fmt.Errorf("proxy.listen is required")
	}
	if config.Proxy.Target == "" {
		return fmt.Errorf("proxy.target is required")
	}

	if _, err := url.Parse(config.Proxy.Target); err != nil {
		return fmt.Errorf("invalid proxy.target URL: %w", err)
	}

	if (config.Proxy.SSLCert != "" && config.Proxy.SSLKey == "") ||
		(config.Proxy.SSLCert == "" && config.Proxy.SSLKey != "") {
		return fmt.Errorf("both ssl_cert and ssl_key must be provided together")
	}

	for i := range config.Rules {
		if err := validateRule(&config.Rules[i], i); err != nil {
			return err
		}
	}

	return nil
}

func validateRule(rule *Rule, index int) error {
	if rule.Methods.Len() == 0 {
		return fmt.Errorf("match rule %d: methods required", index)
	}
	if rule.Paths.Len() == 0 {
		return fmt.Errorf("match rule %d: paths required", index)
	}

	if len(rule.OnRequest) == 0 && len(rule.OnResponse) == 0 {
		return fmt.Errorf("match rule %d: at least one operation required (on_request or on_response)", index)
	}

	if rule.TargetPath != "" && !strings.HasPrefix(rule.TargetPath, "/") {
		return fmt.Errorf("match rule %d: target_path must be absolute", index)
	}

	if err := rule.Methods.Validate(); err != nil {
		return fmt.Errorf("match rule %d methods: %w", index, err)
	}
	if err := rule.Paths.Validate(); err != nil {
		return fmt.Errorf("match rule %d paths: %w", index, err)
	}

	// Validate on_request operations
	for opIdx, op := range rule.OnRequest {
		if err := validateOperation(&op, index, opIdx, "on_request"); err != nil {
			return err
		}
	}

	// Validate on_response operations
	for opIdx, op := range rule.OnResponse {
		if err := validateOperation(&op, index, opIdx, "on_response"); err != nil {
			return err
		}
	}

	return nil
}

func validateOperation(op *Operation, ruleIndex, opIndex int, opType string) error {
	// Validate match_body patterns
	for key, patterns := range op.MatchBody {
		if err := patterns.Validate(); err != nil {
			return fmt.Errorf("rule %d %s %d match_body '%s': %w", ruleIndex, opType, opIndex, key, err)
		}
	}

	// Validate match_headers patterns
	for key, patterns := range op.MatchHeaders {
		if err := patterns.Validate(); err != nil {
			return fmt.Errorf("rule %d %s %d match_headers '%s': %w", ruleIndex, opType, opIndex, key, err)
		}
	}

	// Template is a valid standalone operation
	if op.Template != "" {
		return nil
	}

	if len(op.Merge) == 0 && len(op.Default) == 0 && len(op.Delete) == 0 {
		return fmt.Errorf("rule %d %s %d: must have at least one action (template, merge, default, or delete)", ruleIndex, opType, opIndex)
	}

	return nil
}
