package validatedatacmd

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_ValidatesExternalSiteDataAgainstContract(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(websiteRoot, "src", "templates", "layout"),
		filepath.Join(websiteRoot, "src", "templates", "partials"),
		filepath.Join(websiteRoot, "src", "templates", "pages"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	dataRoot := filepath.Join(websiteRoot, "src", "data")
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	files := map[string]string{
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}{{template "page" .}}{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "agenda.gohtml"):  `{{define "page"}}{{required (dig .SiteData "courses" "ssyb" "start_text") "missing start_text"}}{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "site.contract.yaml"), []byte("required:\n  - courses.ssyb.start_text\nallowed:\n  - courses.*.start_text\n"), 0o644); err != nil {
		t.Fatalf("write contract: %v", err)
	}

	externalDataPath := filepath.Join(t.TempDir(), "site.yaml")
	if err := os.WriteFile(externalDataPath, []byte("courses:\n  ssyb:\n    start_text: Em definição.\n"), 0o644); err != nil {
		t.Fatalf("write external data: %v", err)
	}

	if err := Run([]string{"-website-root", websiteRoot, "-site-data", externalDataPath}, logger); err != nil {
		t.Fatalf("expected validate-site-data success, got %v", err)
	}
}

func TestRun_FailsForInvalidExternalSiteData(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(websiteRoot, "src", "templates", "layout"),
		filepath.Join(websiteRoot, "src", "templates", "partials"),
		filepath.Join(websiteRoot, "src", "templates", "pages"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	dataRoot := filepath.Join(websiteRoot, "src", "data")
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	files := map[string]string{
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}{{template "page" .}}{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "agenda.gohtml"):  `{{define "page"}}{{required (dig .SiteData "courses" "ssyb" "start_text") "missing start_text"}}{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "site.contract.yaml"), []byte("allowed:\n  - courses.*.start_text\n"), 0o644); err != nil {
		t.Fatalf("write contract: %v", err)
	}

	externalDataPath := filepath.Join(t.TempDir(), "site.yaml")
	if err := os.WriteFile(externalDataPath, []byte("courses:\n  ssyb:\n    unexpected: value\n"), 0o644); err != nil {
		t.Fatalf("write external data: %v", err)
	}

	err := Run([]string{"-website-root", websiteRoot, "-site-data", externalDataPath}, logger)
	if err == nil {
		t.Fatal("expected validate-site-data to fail")
	}
	if !strings.Contains(err.Error(), "courses.ssyb.unexpected") {
		t.Fatalf("expected dangling path in error, got %v", err)
	}
}

func TestRun_FailsForUnusedContractPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(websiteRoot, "src", "templates", "layout"),
		filepath.Join(websiteRoot, "src", "templates", "partials"),
		filepath.Join(websiteRoot, "src", "templates", "pages"),
		filepath.Join(websiteRoot, "src", "data"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	files := map[string]string{
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}{{template "page" .}}{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "agenda.gohtml"):  `{{define "page"}}{{required (dig .SiteData "courses" "ssyb" "start_text") "missing start_text"}}{{end}}`,
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"): `allowed:
  - courses.*.start_text
  - courses.*.duration
`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	externalDataPath := filepath.Join(t.TempDir(), "site.yaml")
	if err := os.WriteFile(externalDataPath, []byte("courses:\n  ssyb:\n    start_text: Em definição.\n    duration: 16 horas\n"), 0o644); err != nil {
		t.Fatalf("write external data: %v", err)
	}

	err := Run([]string{"-website-root", websiteRoot, "-site-data", externalDataPath}, logger)
	if err == nil {
		t.Fatal("expected validate-site-data to fail for unused contract path")
	}
	if !strings.Contains(err.Error(), "allowed contract path not used by templates: courses.*.duration") {
		t.Fatalf("expected unused contract path error, got %v", err)
	}
}

func TestRun_FailsWhenLocalContractMissing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(websiteRoot, "src", "templates", "layout"),
		filepath.Join(websiteRoot, "src", "templates", "partials"),
		filepath.Join(websiteRoot, "src", "templates", "pages"),
		filepath.Join(websiteRoot, "src", "data"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	files := map[string]string{
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}{{template "page" .}}{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "agenda.gohtml"):  `{{define "page"}}ok{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	externalDataPath := filepath.Join(t.TempDir(), "site.yaml")
	if err := os.WriteFile(externalDataPath, []byte("courses:\n  ssyb:\n    start_text: Em definição.\n"), 0o644); err != nil {
		t.Fatalf("write external data: %v", err)
	}

	err := Run([]string{"-website-root", websiteRoot, "-site-data", externalDataPath}, logger)
	if err == nil {
		t.Fatal("expected validate-site-data to fail when local contract is missing")
	}
	if !strings.Contains(err.Error(), "required site data contract not found") {
		t.Fatalf("expected missing contract error, got %v", err)
	}
}
