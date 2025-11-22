package config

import (
	"fmt"
	"text/template"

	"github.com/spicyneuron/llama-config-proxy/logger"
)

// CompileTemplates compiles all template strings in rules
func CompileTemplates(cfg *Config) error {
	// Compile shared rules first
	if err := compileRuleTemplates(cfg.Rules, "global"); err != nil {
		return err
	}

	for i := range cfg.Proxies {
		if len(cfg.Proxies[i].Rules) == 0 {
			continue
		}
		if err := compileRuleTemplates(cfg.Proxies[i].Rules, fmt.Sprintf("proxy_%d", i)); err != nil {
			return err
		}
	}

	return nil
}

func compileRuleTemplates(rules []Rule, prefix string) error {
	for i := range rules {
		rule := &rules[i]

		// Convert config operations to execution types
		opRule := &CompiledRule{
			OnRequest:  make([]OperationExec, len(rule.OnRequest)),
			OnResponse: make([]OperationExec, len(rule.OnResponse)),
		}

		// Convert OnRequest operations
		for j, op := range rule.OnRequest {
			opRule.OnRequest[j] = convertOperation(op)

			if op.Template != "" {
				tmpl, err := template.New(fmt.Sprintf("%s_rule_%d_request_%d", prefix, i, j)).
					Funcs(TemplateFuncs).
					Parse(op.Template)
				if err != nil {
					return fmt.Errorf("rule %d request operation %d: %w", i, j, err)
				}
				logger.Debug("Compiled request template", "scope", prefix, "rule_index", i, "operation_index", j)
				opRule.OnRequestTemplates = append(opRule.OnRequestTemplates, tmpl)
			} else {
				opRule.OnRequestTemplates = append(opRule.OnRequestTemplates, nil)
			}
		}

		// Convert OnResponse operations
		for j, op := range rule.OnResponse {
			opRule.OnResponse[j] = convertOperation(op)

			if op.Template != "" {
				tmpl, err := template.New(fmt.Sprintf("%s_rule_%d_response_%d", prefix, i, j)).
					Funcs(TemplateFuncs).
					Parse(op.Template)
				if err != nil {
					return fmt.Errorf("rule %d response operation %d: %w", i, j, err)
				}
				logger.Debug("Compiled response template", "scope", prefix, "rule_index", i, "operation_index", j)
				opRule.OnResponseTemplates = append(opRule.OnResponseTemplates, tmpl)
			} else {
				opRule.OnResponseTemplates = append(opRule.OnResponseTemplates, nil)
			}
		}

		rule.OpRule = opRule
	}
	return nil
}

func convertOperation(op Operation) OperationExec {
	return OperationExec(op)
}
