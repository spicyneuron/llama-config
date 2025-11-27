package config

import (
	"fmt"
	"text/template"

	"github.com/spicyneuron/llama-matchmaker/logger"
)

// CompileTemplates compiles all template strings in routes
func CompileTemplates(cfg *Config) error {
	for i := range cfg.Proxies {
		if len(cfg.Proxies[i].Routes) == 0 {
			continue
		}
		if err := compileRouteTemplates(cfg.Proxies[i].Routes, fmt.Sprintf("proxy_%d", i)); err != nil {
			return err
		}
	}

	return nil
}

func compileRouteTemplates(routes []Route, prefix string) error {
	for i := range routes {
		route := &routes[i]

		// Convert config operations to execution types
		compiled := &CompiledRoute{
			OnRequest:  make([]ActionExec, len(route.OnRequest)),
			OnResponse: make([]ActionExec, len(route.OnResponse)),
		}

		// Convert OnRequest operations
		for j, op := range route.OnRequest {
			compiled.OnRequest[j] = convertAction(op)

			if op.Template != "" {
				tmpl, err := template.New(fmt.Sprintf("%s_rule_%d_request_%d", prefix, i, j)).
					Funcs(TemplateFuncs).
					Parse(op.Template)
				if err != nil {
					return fmt.Errorf("rule %d request operation %d: %w", i, j, err)
				}
				logger.Debug("Compiled request template", "scope", prefix, "rule_index", i, "operation_index", j)
				compiled.OnRequestTemplates = append(compiled.OnRequestTemplates, tmpl)
			} else {
				compiled.OnRequestTemplates = append(compiled.OnRequestTemplates, nil)
			}
		}

		// Convert OnResponse operations
		for j, op := range route.OnResponse {
			compiled.OnResponse[j] = convertAction(op)

			if op.Template != "" {
				tmpl, err := template.New(fmt.Sprintf("%s_rule_%d_response_%d", prefix, i, j)).
					Funcs(TemplateFuncs).
					Parse(op.Template)
				if err != nil {
					return fmt.Errorf("rule %d response operation %d: %w", i, j, err)
				}
				logger.Debug("Compiled response template", "scope", prefix, "rule_index", i, "operation_index", j)
				compiled.OnResponseTemplates = append(compiled.OnResponseTemplates, tmpl)
			} else {
				compiled.OnResponseTemplates = append(compiled.OnResponseTemplates, nil)
			}
		}

		route.Compiled = compiled
	}
	return nil
}

func convertAction(op Action) ActionExec {
	return ActionExec(op)
}
