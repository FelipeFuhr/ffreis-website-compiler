package sitegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDictEvenPairs(t *testing.T) {
	got, err := dict("a", 1, "b", "two")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["a"] != 1 || got["b"] != "two" {
		t.Fatalf("unexpected map values: %#v", got)
	}
}

func TestDictOddPairsError(t *testing.T) {
	if _, err := dict("a"); err == nil {
		t.Fatal("expected error for odd argument count")
	}
}

func TestValidateSiteData_PassesWithContract(t *testing.T) {
	siteData := map[string]any{
		"fictional_courses": map[string]any{
			"fictional_id": map[string]any{
				"start_text": "FIC-START",
				"investment": map[string]any{
					"total":             "FIC-TOTAL",
					"installments_text": "FIC-INSTALLMENTS",
					"max_installments":  42,
					"cash_discount":     "FIC-CASH-DISCOUNT",
					"group_discount":    "FIC-GROUP-DISCOUNT",
				},
			},
		},
		"fictional_agenda": []any{"fictional_id"},
	}
	contract := SiteDataContract{
		Required: []string{
			"fictional_agenda.0",
			"fictional_courses.fictional_id.start_text",
			"fictional_courses.fictional_id.investment.total",
			"fictional_courses.fictional_id.investment.installments_text",
			"fictional_courses.fictional_id.investment.max_installments",
		},
		Allowed: []string{
			"fictional_agenda.*",
			"fictional_courses.*.start_text",
			"fictional_courses.*.investment.total",
			"fictional_courses.*.investment.installments_text",
			"fictional_courses.*.investment.max_installments",
			"fictional_courses.*.investment.cash_discount",
			"fictional_courses.*.investment.group_discount",
		},
	}

	if err := ValidateSiteData(siteData, contract); err != nil {
		t.Fatalf("expected validation success, got %v", err)
	}
}

func TestValidateSiteData_FailsForDanglingPath(t *testing.T) {
	siteData := map[string]any{
		"fictional_courses": map[string]any{
			"fictional_id": map[string]any{
				"start_text": "FIC-START",
				"unexpected": "FIC-UNEXPECTED",
			},
		},
	}
	contract := SiteDataContract{
		Allowed: []string{"fictional_courses.*.start_text"},
	}

	err := ValidateSiteData(siteData, contract)
	if err == nil {
		t.Fatal("expected validation to fail for dangling path")
	}
	if !strings.Contains(err.Error(), "fictional_courses.fictional_id.unexpected") {
		t.Fatalf("expected dangling path in error, got %v", err)
	}
}

func TestLoadSiteDataContract_UsesDefaultPathWhenPresent(t *testing.T) {
	websiteRoot := t.TempDir()
	templatesRoot := filepath.Join(websiteRoot, "src", "templates")
	if err := os.MkdirAll(templatesRoot, 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	dataRoot := filepath.Join(websiteRoot, "src", "data")
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	contractPath := filepath.Join(dataRoot, "site.contract.yaml")
	if err := os.WriteFile(contractPath, []byte("required:\n  - fictional_courses.fictional_id.start_text\nallowed:\n  - fictional_courses.*.start_text\n"), 0o644); err != nil {
		t.Fatalf("write contract: %v", err)
	}

	result, err := LoadSiteDataContract(templatesRoot)
	if err != nil {
		t.Fatalf("expected contract load success, got %v", err)
	}
	if len(result.Contract.Required) != 1 || result.Contract.Required[0] != "fictional_courses.fictional_id.start_text" {
		t.Fatalf("unexpected contract contents: %#v", result.Contract)
	}
}

func TestLoadSiteDataContract_FailsWhenMissing(t *testing.T) {
	websiteRoot := t.TempDir()
	templatesRoot := filepath.Join(websiteRoot, "src", "templates")
	if err := os.MkdirAll(templatesRoot, 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}

	_, err := LoadSiteDataContract(templatesRoot)
	if err == nil {
		t.Fatal("expected missing contract error")
	}
	if !strings.Contains(err.Error(), "required site data contract not found") {
		t.Fatalf("expected missing contract error, got %v", err)
	}
}

func TestTraceSiteDataUsage_CollectsDigPaths(t *testing.T) {
	websiteRoot := t.TempDir()
	templatesRoot := filepath.Join(websiteRoot, "src", "templates")
	for _, dir := range []string{
		filepath.Join(templatesRoot, "layout"),
		filepath.Join(templatesRoot, "partials"),
		filepath.Join(templatesRoot, "pages"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	files := map[string]string{
		filepath.Join(templatesRoot, "layout", "base.gohtml"):   `{{define "layout"}}FIC-TEMPLATE-LAYOUT{{end}}`,
		filepath.Join(templatesRoot, "partials", "head.gohtml"): `{{define "head"}}FIC-TEMPLATE-HEAD{{end}}`,
		filepath.Join(templatesRoot, "pages", "agenda.gohtml"):  `{{define "page"}}{{required (dig .SiteData "fictional_agenda") "missing fictional_agenda"}}{{required (dig .SiteData "fictional_courses" "fictional_id" "investment" "total") "missing total"}}{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	pages, err := LoadPageTemplatesFromRoot(templatesRoot)
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}

	usedPaths, err := TraceSiteDataUsage(pages, map[string]any{
		"fictional_agenda": []any{"fictional_id"},
		"fictional_courses": map[string]any{
			"fictional_id": map[string]any{
				"investment": map[string]any{
					"total": "FIC-TOTAL",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("trace usage: %v", err)
	}

	got := strings.Join(usedPaths, ",")
	for _, expected := range []string{"fictional_agenda", "fictional_courses.fictional_id.investment.total"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("expected %q in used paths, got %v", expected, usedPaths)
		}
	}
}

func TestValidateSiteDataContractUsage_FailsForUnusedAllowedPath(t *testing.T) {
	contract := SiteDataContract{
		Allowed: []string{
			"fictional_agenda",
			"fictional_courses.*.investment.total",
			"fictional_courses.*.investment.installments_text",
		},
	}

	err := ValidateSiteDataContractUsage(contract, []string{
		"fictional_agenda",
		"fictional_courses.fictional_id.investment.total",
	})
	if err == nil {
		t.Fatal("expected unused contract path failure")
	}
	if !strings.Contains(err.Error(), "allowed contract path not used by templates: fictional_courses.*.investment.installments_text") {
		t.Fatalf("expected unused allowed path in error, got %v", err)
	}
}
