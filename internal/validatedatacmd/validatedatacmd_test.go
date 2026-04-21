package validatedatacmd

import (
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

const (
	validateDataLayoutTmpl = `{{define "layout"}}{{template "page" .}}{{end}}`
	validateDataHeadTmpl   = `{{define "head"}}{{end}}`
	validateDataAgendaMain = `{{define "page"}}{{required (dig .SiteData "courses" "ssyb" "start_text") "missing start_text"}}{{end}}`

	contractFileName = "site.contract.yaml"
	externalDataFile = "site.yaml"
	flagWebsiteRoot  = "-website-root"
	flagSiteData     = "-site-data"
)

func setupWebsiteRoot(t *testing.T, pageTemplate string) (websiteRoot, dataRoot string) {
	t.Helper()
	websiteRoot = t.TempDir()
	for _, dir := range []string{
		filepath.Join(websiteRoot, "src", "templates", "layout"),
		filepath.Join(websiteRoot, "src", "templates", "partials"),
		filepath.Join(websiteRoot, "src", "templates", "pages"),
		filepath.Join(websiteRoot, "src", "data"),
	} {
		testutil.MustMkdirAll(t, dir)
	}
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   validateDataLayoutTmpl,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): validateDataHeadTmpl,
		filepath.Join(websiteRoot, "src", "templates", "pages", "agenda.gohtml"):  pageTemplate,
	})
	dataRoot = filepath.Join(websiteRoot, "src", "data")
	return websiteRoot, dataRoot
}

func TestRun_ValidatesExternalSiteDataAgainstContract(t *testing.T) {
	websiteRoot, dataRoot := setupWebsiteRoot(t, validateDataAgendaMain)
	externalDataPath := filepath.Join(t.TempDir(), externalDataFile)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(dataRoot, contractFileName): "required:\n  - courses.ssyb.start_text\nallowed:\n  - courses.*.start_text\n",
		externalDataPath: "courses:\n  ssyb:\n    start_text: Em definição.\n",
	})

	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagSiteData, externalDataPath}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("expected validate-site-data success, got %v", err)
	}
}

func TestRun_FailsForInvalidExternalSiteData(t *testing.T) {
	websiteRoot, dataRoot := setupWebsiteRoot(t, validateDataAgendaMain)
	externalDataPath := filepath.Join(t.TempDir(), externalDataFile)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(dataRoot, contractFileName): "allowed:\n  - courses.*.start_text\n",
		externalDataPath: "courses:\n  ssyb:\n    unexpected: value\n",
	})

	err := Run([]string{flagWebsiteRoot, websiteRoot, flagSiteData, externalDataPath}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected validate-site-data to fail")
	}
	if !strings.Contains(err.Error(), "courses.ssyb.unexpected") {
		t.Fatalf("expected dangling path in error, got %v", err)
	}
}

func TestRun_FailsForUnusedContractPath(t *testing.T) {
	websiteRoot, dataRoot := setupWebsiteRoot(t, validateDataAgendaMain)
	externalDataPath := filepath.Join(t.TempDir(), externalDataFile)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(dataRoot, contractFileName): "allowed:\n  - courses.*.start_text\n  - courses.*.duration\n",
		externalDataPath: "courses:\n  ssyb:\n    start_text: Em definição.\n    duration: 16 horas\n",
	})

	err := Run([]string{flagWebsiteRoot, websiteRoot, flagSiteData, externalDataPath}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected validate-site-data to fail for unused contract path")
	}
	if !strings.Contains(err.Error(), "allowed contract path not used by templates: courses.*.duration") {
		t.Fatalf("expected unused contract path error, got %v", err)
	}
}

func TestRun_FailsWhenLocalContractMissing(t *testing.T) {
	websiteRoot, _ := setupWebsiteRoot(t, `{{define "page"}}ok{{end}}`)
	externalDataPath := filepath.Join(t.TempDir(), externalDataFile)
	testutil.WriteFiles(t, map[string]string{
		externalDataPath: "courses:\n  ssyb:\n    start_text: Em definição.\n",
	})

	err := Run([]string{flagWebsiteRoot, websiteRoot, flagSiteData, externalDataPath}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected validate-site-data to fail when local contract is missing")
	}
	if !strings.Contains(err.Error(), "required site data contract not found") {
		t.Fatalf("expected missing contract error, got %v", err)
	}
}
