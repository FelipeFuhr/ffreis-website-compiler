package outputcheck

import "fmt"

// Preset returns a named, reusable bundle of rules.
//
// Presets exist so a baseline every site should meet is declared once here and
// composed in with `include:`, rather than copied into each website's manifest
// where it would drift. A site may still add or override rules alongside one.
func Preset(name string, reg *Registry) ([]Rule, error) {
	builder, ok := presets[name]
	if !ok {
		return nil, fmt.Errorf("unknown preset %q (known: %s)", name, "a11y-baseline")
	}
	return builder(reg)
}

var presets = map[string]func(*Registry) ([]Rule, error){
	"a11y-baseline": a11yBaseline,
}

// a11yBaseline asserts the structural minimum every page in the fleet should
// meet. It is deliberately markup-structural and language-neutral: nothing here
// inspects copy, so it is safe to run against every language tree.
func a11yBaseline(reg *Registry) ([]Rule, error) {
	landmark, err := reg.Build("matches", map[string]any{"pattern": `<main[\s>]`})
	if err != nil {
		return nil, err
	}
	documentLang, err := reg.Build("matches", map[string]any{"pattern": `<html[^>]*\slang\s*=\s*["'][^"']+["']`})
	if err != nil {
		return nil, err
	}
	return []Rule{
		{
			Name:   "a11y-baseline: every page has a main landmark",
			Checks: []Check{landmark},
		},
		{
			Name:   "a11y-baseline: every page declares its document language",
			Checks: []Check{documentLang},
		},
	}, nil
}
