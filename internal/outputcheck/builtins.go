package outputcheck

import (
	"fmt"
	"regexp"
	"strings"
)

// DefaultRegistry returns a registry with the built-in check types.
//
// Each built-in lives behind the same [Factory] contract a website-supplied
// extension would use, so nothing here is privileged.
func DefaultRegistry() *Registry {
	reg := NewRegistry()

	reg.Register("contains", func(config map[string]any) (Check, error) {
		value, err := stringField(config, "value")
		if err != nil {
			return nil, fmt.Errorf("contains: %w", err)
		}
		return CheckFunc{CheckName: "contains", Fn: func(t Target) []string {
			if strings.Contains(t.HTML, value) {
				return nil
			}
			return []string{fmt.Sprintf("expected to contain %q", value)}
		}}, nil
	})

	reg.Register("not_contains", func(config map[string]any) (Check, error) {
		value, err := stringField(config, "value")
		if err != nil {
			return nil, fmt.Errorf("not_contains: %w", err)
		}
		return CheckFunc{CheckName: "not_contains", Fn: func(t Target) []string {
			if !strings.Contains(t.HTML, value) {
				return nil
			}
			return []string{fmt.Sprintf("must not contain %q", value)}
		}}, nil
	})

	reg.Register("matches", func(config map[string]any) (Check, error) {
		expr, err := compiledPattern(config)
		if err != nil {
			return nil, fmt.Errorf("matches: %w", err)
		}
		return CheckFunc{CheckName: "matches", Fn: func(t Target) []string {
			if expr.MatchString(t.HTML) {
				return nil
			}
			return []string{fmt.Sprintf("expected to match /%s/", expr.String())}
		}}, nil
	})

	reg.Register("not_matches", func(config map[string]any) (Check, error) {
		expr, err := compiledPattern(config)
		if err != nil {
			return nil, fmt.Errorf("not_matches: %w", err)
		}
		return CheckFunc{CheckName: "not_matches", Fn: func(t Target) []string {
			if !expr.MatchString(t.HTML) {
				return nil
			}
			return []string{fmt.Sprintf("must not match /%s/", expr.String())}
		}}, nil
	})

	return reg
}

// compiledPattern validates a regex when the manifest is loaded rather than
// when a page is inspected, so a typo fails immediately and identically for
// every page.
func compiledPattern(config map[string]any) (*regexp.Regexp, error) {
	pattern, err := stringField(config, "pattern")
	if err != nil {
		return nil, err
	}
	expr, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern %q: %w", pattern, err)
	}
	return expr, nil
}
