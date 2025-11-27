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

		if len(proxy.Routes) == 0 {
			return fmt.Errorf("proxy[%d].routes is required", i)
		}
		for j := range proxy.Routes {
			if err := validateRoute(&proxy.Routes[j], j); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateRoute(route *Route, index int) error {
	if route.Methods.Len() == 0 {
		return fmt.Errorf("route %d: methods required", index)
	}
	if route.Paths.Len() == 0 {
		return fmt.Errorf("route %d: paths required", index)
	}

	if len(route.OnRequest) == 0 && len(route.OnResponse) == 0 {
		return fmt.Errorf("route %d: at least one action required (on_request or on_response)", index)
	}

	if route.TargetPath != "" && !strings.HasPrefix(route.TargetPath, "/") {
		return fmt.Errorf("route %d: target_path must be absolute", index)
	}

	if err := route.Methods.Validate(); err != nil {
		return fmt.Errorf("route %d methods: %w", index, err)
	}
	if err := route.Paths.Validate(); err != nil {
		return fmt.Errorf("route %d paths: %w", index, err)
	}

	// Validate on_request actions
	for opIdx, op := range route.OnRequest {
		if err := validateAction(&op, index, opIdx, "on_request"); err != nil {
			return err
		}
	}

	// Validate on_response actions
	for opIdx, op := range route.OnResponse {
		if err := validateAction(&op, index, opIdx, "on_response"); err != nil {
			return err
		}
	}

	return nil
}

func validateAction(op *Action, ruleIndex, opIndex int, opType string) error {
	// Validate match_body patterns
	for key := range op.MatchBody {
		patterns := op.MatchBody[key]
		if err := patterns.Validate(); err != nil {
			return fmt.Errorf("route %d %s %d match_body '%s': %w", ruleIndex, opType, opIndex, key, err)
		}
		op.MatchBody[key] = patterns
	}

	// Validate match_headers patterns
	for key := range op.MatchHeaders {
		patterns := op.MatchHeaders[key]
		if err := patterns.Validate(); err != nil {
			return fmt.Errorf("route %d %s %d match_headers '%s': %w", ruleIndex, opType, opIndex, key, err)
		}
		op.MatchHeaders[key] = patterns
	}

	// Template is a valid standalone action
	if op.Template != "" {
		return nil
	}

	if len(op.Merge) == 0 && len(op.Default) == 0 && len(op.Delete) == 0 {
		return fmt.Errorf("route %d %s %d: must have at least one action (template, merge, default, or delete)", ruleIndex, opType, opIndex)
	}

	return nil
}
