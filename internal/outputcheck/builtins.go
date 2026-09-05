package outputcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	reg.RegisterCorpus("json_assets_exist", func(config map[string]any) (CorpusCheck, error) {
		path, err := stringField(config, "path")
		if err != nil {
			return nil, fmt.Errorf("json_assets_exist: %w", err)
		}
		field, err := stringField(config, "field")
		if err != nil {
			return nil, fmt.Errorf("json_assets_exist: %w", err)
		}
		return jsonAssetsExistCheck{path: path, field: field}, nil
	})

	return reg
}

// jsonAssetsExistCheck validates that every asset path referenced inside a
// compiled JSON output file (manifest.json, site.webmanifest, or any other
// JSON file a website emits) actually exists in the compiled output.
//
// It is deliberately generic over the JSON's shape rather than hardcoded to
// the web-app-manifest icons[] array: Field is a small dotted path where a
// segment suffixed "[]" fans out over a JSON array, e.g. "icons[].src" reaches
// every icons[i].src, "assets[]" reaches every string in a flat assets array,
// and "[]" fans out over a top-level array. See extractJSONRefs.
//
// This is the one built-in in this package that touches the filesystem — see
// the Target.Root doc comment for why that is unavoidable for this particular
// assertion (checking existence against the wider output tree, not just one
// page's HTML) — and why it is a CorpusCheck rather than a per-page Check: the
// question it answers ("does dist agree with this one JSON file") has nothing
// to do with any individual compiled page.
type jsonAssetsExistCheck struct {
	// path is the JSON file's route relative to the compiled output root, e.g.
	// "site.webmanifest" or "/manifest.json".
	path string
	// field is the field-path expression naming which values inside the parsed
	// JSON document are asset references. See extractJSONRefs.
	field string
}

func (c jsonAssetsExistCheck) Name() string { return "json_assets_exist" }

func (c jsonAssetsExistCheck) CheckAll(targets []Target) []Finding {
	root := ""
	for _, t := range targets {
		if t.Root != "" {
			root = t.Root
			break
		}
	}
	if root == "" {
		return []Finding{c.finding("no compiled output root available to resolve " + c.path)}
	}

	full := rootJoin(root, c.path)
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return []Finding{c.finding(fmt.Sprintf("%s does not exist in compiled output", c.path))}
		}
		return []Finding{c.finding(fmt.Sprintf("reading %s: %v", c.path, err))}
	}

	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return []Finding{c.finding(fmt.Sprintf("%s is not valid JSON: %v", c.path, err))}
	}

	var findings []Finding
	seen := make(map[string]bool)
	for _, ref := range extractJSONRefs(doc, parseFieldPath(c.field)) {
		if ref == "" || seen[ref] || looksExternal(ref) {
			continue
		}
		seen[ref] = true
		if _, err := os.Stat(rootJoin(root, ref)); err != nil {
			findings = append(findings, c.finding(
				fmt.Sprintf("%s references %q, which does not exist in compiled output", c.path, ref)))
		}
	}
	return findings
}

func (c jsonAssetsExistCheck) finding(message string) Finding {
	return Finding{Rule: c.Name(), Check: c.Name(), Route: c.path, Message: message}
}

// rootJoin resolves a route-relative path (with or without a leading "/")
// against root, matching how the rest of this package's callers treat routes.
func rootJoin(root, relPath string) string {
	return filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(relPath, "/")))
}

// looksExternal reports whether ref is an external URL or data URI rather than
// a local asset path — a JSON field a check is pointed at is not guaranteed to
// hold only local references (e.g. an icon hosted on a CDN), and such refs
// obviously cannot be resolved against the compiled output.
func looksExternal(ref string) bool {
	lower := strings.ToLower(strings.TrimSpace(ref))
	for _, prefix := range []string{"http://", "https://", "//", "data:"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// parseFieldPath splits a field-path expression like "icons[].src" into its
// dotted segments (["icons[]", "src"]). A segment ending in "[]" is resolved
// by extractJSONRefs as "look up (or use, if bare '[]') this array, and
// evaluate the remaining segments against every element".
func parseFieldPath(field string) []string {
	field = strings.TrimSpace(field)
	if field == "" {
		return nil
	}
	return strings.Split(field, ".")
}

// extractJSONRefs walks doc according to segments, returning every string
// value reached at the end of the path. A branch that is missing, the wrong
// shape, or whose terminal value is not a non-empty string is silently
// skipped rather than treated as an error — not every entry along the way
// need carry every field (e.g. a manifest icon with no "src").
//
// A segment ending in "[]" fans the walk out over a JSON array: the array is
// either doc[key] (for a "key[]" segment) or doc itself (for a bare "[]"
// segment, letting a field-path address a top-level array), and the remaining
// segments are evaluated against every element.
func extractJSONRefs(doc any, segments []string) []string {
	if len(segments) == 0 {
		if s, ok := doc.(string); ok && s != "" {
			return []string{s}
		}
		return nil
	}

	seg := segments[0]
	rest := segments[1:]
	fanOut := strings.HasSuffix(seg, "[]")
	key := strings.TrimSuffix(seg, "[]")

	next := doc
	if key != "" {
		m, ok := doc.(map[string]any)
		if !ok {
			return nil
		}
		val, ok := m[key]
		if !ok {
			return nil
		}
		next = val
	}

	if !fanOut {
		return extractJSONRefs(next, rest)
	}
	arr, ok := next.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range arr {
		out = append(out, extractJSONRefs(item, rest)...)
	}
	return out
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
