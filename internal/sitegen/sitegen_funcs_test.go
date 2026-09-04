package sitegen

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestTemplateFunctionNames_MatchesRegisteredFuncMap pins the exported
// accessor the CLI's version --json capability manifest depends on: it must
// report every function actually registered in templateFuncMap (not a
// hand-duplicated, driftable list), sorted.
func TestTemplateFunctionNamesMatchesRegisteredFuncMap(t *testing.T) {
	got := TemplateFunctionNames()

	want := make([]string, 0, len(templateFuncMap))
	for name := range templateFuncMap {
		want = append(want, name)
	}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("TemplateFunctionNames() = %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("TemplateFunctionNames()[%d] = %q, want %q", i, got[i], name)
		}
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("TemplateFunctionNames() = %v, not sorted", got)
	}
}

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
