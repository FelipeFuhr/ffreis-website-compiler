package sitegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A bare YAML scalar like `date: 2026-09-03` resolves to the !!timestamp tag
// and decodes as time.Time, not a string. Left unnormalized, html/template
// prints it via time.Time.String() ("2026-09-03 00:00:00 +0000 UTC"), which
// fails HTML <time datetime="..."> validation. normalizeYAMLValue must
// convert it to a plain RFC3339 string so every consumer (templates and
// export-site-data's JSON output) sees a valid value.
func TestLoadSiteData_NormalizesBareDateScalar(t *testing.T) {
	t.Parallel()

	templatesRoot, dataRoot := newTestSiteRoot(t)
	basePath := filepath.Join(dataRoot, siteYAMLFile)
	yamlContent := "courses:\n  cqe:\n    variants:\n      online:\n        cronograma:\n          sessions:\n            - date: 2026-09-03\n"
	if err := os.WriteFile(basePath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf(writeSiteYAMLFmt, err)
	}

	result, err := LoadSiteData(templatesRoot, "")
	if err != nil {
		t.Fatalf(unexpectedErrorFmt, err)
	}

	sessions := digPathMap(t, result.Data, "courses", "cqe", "variants", "online", "cronograma")
	rawSessions, ok := sessions["sessions"].([]any)
	if !ok || len(rawSessions) != 1 {
		t.Fatalf("expected one session, got: %#v", sessions["sessions"])
	}
	session, ok := rawSessions[0].(map[string]any)
	if !ok {
		t.Fatalf("session is not a map: %#v", rawSessions[0])
	}

	date, ok := session["date"].(string)
	if !ok {
		t.Fatalf("expected date to be normalized to a string, got %T: %#v", session["date"], session["date"])
	}
	if date != "2026-09-03T00:00:00Z" {
		t.Fatalf("unexpected normalized date: %q", date)
	}
}

// TestTemplateFunctionNames_MirrorsTheParsedFuncMap locks the exported
// accessor the `version --json` capability manifest reads to the same
// templateFuncMap that LoadPageTemplatesFromRoot actually parses templates
// with. The single-source-of-truth property is the whole point: a website's
// templates can only use a helper the compiler registered, so a manifest that
// advertised a helper the parser does not register (or omitted one it does)
// would be worse than no manifest, because consumers act on it.
func TestTemplateFunctionNames_MirrorsTheParsedFuncMap(t *testing.T) {
	t.Parallel()

	got := TemplateFunctionNames()

	if len(got) != len(templateFuncMap) {
		t.Fatalf("TemplateFunctionNames() returned %d names but templateFuncMap registers %d: %v",
			len(got), len(templateFuncMap), got)
	}

	for _, name := range got {
		if _, ok := templateFuncMap[name]; !ok {
			t.Errorf("TemplateFunctionNames() advertises %q, which templateFuncMap does not register", name)
		}
	}
	for name := range templateFuncMap {
		found := false
		for _, g := range got {
			if g == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("templateFuncMap registers %q but TemplateFunctionNames() omits it", name)
		}
	}

	// Sorted output keeps the manifest byte-stable across builds, so a
	// consumer diffing two manifests sees real changes only.
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("TemplateFunctionNames() not sorted at index %d: %q then %q", i, got[i-1], got[i])
		}
	}
}

// TestTemplateFuncMap_IsWhatTemplatesActuallyParseWith guards the hoist that
// made the manifest possible: templateFuncMap was lifted out of
// LoadPageTemplatesFromRoot, so if someone later re-inlines a FuncMap literal
// at the parse site, the manifest would silently stop describing reality.
// Parsing a template that calls each registered helper by name proves the
// hoisted map is the one in effect.
func TestTemplateFuncMap_IsWhatTemplatesActuallyParseWith(t *testing.T) {
	t.Parallel()

	templatesRoot := filepath.Join(t.TempDir(), "src", "templates")
	for _, dir := range []string{"layout", "partials", "pages"} {
		if err := os.MkdirAll(filepath.Join(templatesRoot, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	// One action per registered helper, referenced by name only — enough for
	// the parser to reject an unregistered function at parse time, without
	// needing valid arguments for each one.
	var body strings.Builder
	body.WriteString(`{{define "page"}}`)
	for name := range templateFuncMap {
		body.WriteString("{{ if false }}{{ " + name + " }}{{ end }}")
	}
	body.WriteString(`{{end}}`)

	files := map[string]string{
		filepath.Join(templatesRoot, "layout", "base.gohtml"):     `{{define "layout"}}<!doctype html><html><body>{{template "page" .}}</body></html>{{end}}`,
		filepath.Join(templatesRoot, "pages", "funcprobe.gohtml"): body.String(),
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	if _, err := LoadPageTemplatesFromRoot(templatesRoot); err != nil {
		t.Fatalf("LoadPageTemplatesFromRoot rejected a template calling a registered helper: %v", err)
	}
}
