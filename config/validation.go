package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Validate checks the entire configuration for errors
func Validate(config *Config) error {
	if len(config.Proxies) == 0 {
		return fmt.Errorf("proxy configuration is required")
	}

	seenListeners := make(map[string]struct{})
	for i, proxy := range config.Proxies {
		if proxy.Listen == "" {
			return fmt.Errorf("proxy[%d].listen is required", i)
		}
		if proxy.Target == "" {
			return fmt.Errorf("proxy[%d].target is required", i)
		}

		if _, err := url.Parse(proxy.Target); err != nil {
			return fmt.Errorf("proxy[%d].target URL is invalid: %w", i, err)
		}

		if (proxy.SSLCert != "" && proxy.SSLKey == "") ||
			(proxy.SSLCert == "" && proxy.SSLKey != "") {
			return fmt.Errorf("proxy[%d]: both ssl_cert and ssl_key must be provided together", i)
		}

		if _, exists := seenListeners[proxy.Listen]; exists {
			return fmt.Errorf("proxy listeners must be unique; %s is duplicated", proxy.Listen)
		}
		seenListeners[proxy.Listen] = struct{}{}

		rules := proxy.Rules
		if len(rules) == 0 {
			rules = config.Rules
		}
		for j := range rules {
			if err := validateRule(&rules[j], j); err != nil {
				return err
			}
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
	for key := range op.MatchBody {
		patterns := op.MatchBody[key]
		if err := patterns.Validate(); err != nil {
			return fmt.Errorf("rule %d %s %d match_body '%s': %w", ruleIndex, opType, opIndex, key, err)
		}
		op.MatchBody[key] = patterns
	}

	// Validate match_headers patterns
	for key := range op.MatchHeaders {
		patterns := op.MatchHeaders[key]
		if err := patterns.Validate(); err != nil {
			return fmt.Errorf("rule %d %s %d match_headers '%s': %w", ruleIndex, opType, opIndex, key, err)
		}
		op.MatchHeaders[key] = patterns
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
