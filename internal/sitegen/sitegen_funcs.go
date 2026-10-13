package sitegen

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"reflect"
	"strings"
	"time"
)

// pageSlugFunc returns the URL slug for pageName by reading pages.<pageName>.slug
// from siteData, falling back to pageName when the field is absent.
func pageSlugFunc(siteData any, pageName string) string {
	sd, _ := siteData.(map[string]any)
	pagesData, _ := sd["pages"].(map[string]any)
	pageData, _ := pagesData[pageName].(map[string]any)
	slug, _ := pageData["slug"].(string)
	if slug == "" {
		return pageName
	}
	return slug
}

// add, sub, mul, div, and mod give templates the arithmetic html/template's
// stdlib FuncMap has no equivalent for (pagination offsets, ordinal
// numbering, zero/one-based index conversion). Each accepts int or float64 --
// the two numeric kinds YAML/JSON site data and template range indexes
// actually produce -- and returns float64, matching how Go's fmt prints
// whole-number floats without a trailing ".0" (3.0 -> "3").
func add(a, b any) (float64, error) {
	x, y, err := numericOperands("add", a, b)
	if err != nil {
		return 0, err
	}
	return x + y, nil
}

func sub(a, b any) (float64, error) {
	x, y, err := numericOperands("sub", a, b)
	if err != nil {
		return 0, err
	}
	return x - y, nil
}

func mul(a, b any) (float64, error) {
	x, y, err := numericOperands("mul", a, b)
	if err != nil {
		return 0, err
	}
	return x * y, nil
}

func div(a, b any) (float64, error) {
	x, y, err := numericOperands("div", a, b)
	if err != nil {
		return 0, err
	}
	if y == 0 {
		return 0, fmt.Errorf("div: division by zero")
	}
	return x / y, nil
}

func mod(a, b any) (int, error) {
	x, y, err := numericOperands("mod", a, b)
	if err != nil {
		return 0, err
	}
	if y == 0 {
		return 0, fmt.Errorf("mod: division by zero")
	}
	return int(x) % int(y), nil
}

func numericOperands(funcName string, a, b any) (float64, float64, error) {
	x, ok := toFloat64(a)
	if !ok {
		return 0, 0, fmt.Errorf("%s: left operand is not a number, got %T", funcName, a)
	}
	y, ok := toFloat64(b)
	if !ok {
		return 0, 0, fmt.Errorf("%s: right operand is not a number, got %T", funcName, b)
	}
	return x, y, nil
}

func toFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func dict(values ...any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("dict expects an even number of arguments")
	}
	m := make(map[string]any, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		k, ok := values[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict keys must be strings")
		}
		m[k] = values[i+1] //nolint:gosec // template dict helper; keys and values come from trusted site YAML, not user input
	}
	return m, nil
}

func list(values ...any) []any {
	return values
}

func safeHTML(v string) template.HTML {
	return template.HTML(v) //nolint:gosec // false positive: input is pre-rendered HTML from trusted site YAML, not user content
}

// hasString reports whether val is present in slice. The slice argument accepts
// []any (as returned by template data) and []string. Any other type returns false.
// Used by listing templates to check content item language availability.
func hasString(slice any, val string) bool {
	switch s := slice.(type) {
	case []any:
		for _, item := range s {
			if str, ok := item.(string); ok && str == val {
				return true
			}
		}
	case []string:
		for _, str := range s {
			if str == val {
				return true
			}
		}
	}
	return false
}

// toJSON marshals v to JSON and marks it safe for embedding in a <script> block.
func toJSON(v any) (template.JS, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("toJSON: %w", err)
	}
	return template.JS(b), nil //nolint:gosec // json.Marshal output is safe for <script> embedding; it auto-escapes <, >, & as < etc.
}

func required(value any, message string) (any, error) {
	if isMissingValue(value) {
		return nil, errors.New(message)
	}
	return value, nil
}

func normalizeYAMLValue(value any) (any, error) {
	switch typed := value.(type) {
	case time.Time:
		// A bare scalar like `date: 2026-05-19` in YAML is resolved to the
		// !!timestamp tag and decoded as time.Time, not a string. Left as-is,
		// html/template prints it via time.Time.String() ("2026-09-03
		// 00:00:00 +0000 UTC"), which is not a valid HTML datetime string and
		// fails vnu validation wherever a template embeds it in a <time
		// datetime="..."> attribute. Normalize to RFC3339 here so every
		// consumer (templates and export-site-data's JSON output) sees a
		// plain, valid string -- this matches the RFC3339 shape
		// time.Time's own JSON marshaling already produced, so JSON output
		// is unchanged.
		return typed.UTC().Format(time.RFC3339), nil
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, item := range typed {
			next, err := normalizeYAMLValue(item)
			if err != nil {
				return nil, err
			}
			normalized[key] = next
		}
		return normalized, nil
	case []any:
		normalized := make([]any, len(typed))
		for i, item := range typed {
			next, err := normalizeYAMLValue(item)
			if err != nil {
				return nil, err
			}
			normalized[i] = next
		}
		return normalized, nil
	default:
		return value, nil
	}
}

func isMissingValue(value any) bool {
	if value == nil {
		return true
	}

	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface:
		return rv.IsNil()
	case reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() == 0
	}

	return false
}
