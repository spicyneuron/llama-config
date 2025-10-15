package config

import (
	"fmt"
	"text/template"
)

// CompileTemplates compiles all template strings in rules
func CompileTemplates(cfg *Config) error {
	for i := range cfg.Rules {
		rule := &cfg.Rules[i]

		// Convert config operations to execution types
		opRule := &CompiledRule{
			OnRequest:  make([]OperationExec, len(rule.OnRequest)),
			OnResponse: make([]OperationExec, len(rule.OnResponse)),
		}

		// Convert OnRequest operations
		for j, op := range rule.OnRequest {
			opRule.OnRequest[j] = convertOperation(op)

			if op.Template != "" {
				tmpl, err := template.New(fmt.Sprintf("rule_%d_request_%d", i, j)).
					Funcs(TemplateFuncs).
					Parse(op.Template)
				if err != nil {
					return fmt.Errorf("rule %d request operation %d: %w", i, j, err)
				}
				opRule.OnRequestTemplates = append(opRule.OnRequestTemplates, tmpl)
			} else {
				opRule.OnRequestTemplates = append(opRule.OnRequestTemplates, nil)
			}
		}

		// Convert OnResponse operations
		for j, op := range rule.OnResponse {
			opRule.OnResponse[j] = convertOperation(op)

			if op.Template != "" {
				tmpl, err := template.New(fmt.Sprintf("rule_%d_response_%d", i, j)).
					Funcs(TemplateFuncs).
					Parse(op.Template)
				if err != nil {
					return fmt.Errorf("rule %d response operation %d: %w", i, j, err)
				}
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
	return OperationExec{
		Template: op.Template,
		Filters:  op.Filters,
		Merge:    op.Merge,
		Default:  op.Default,
		Delete:   op.Delete,
		Stop:     op.Stop,
	}
}
