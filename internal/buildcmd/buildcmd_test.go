package buildcmd

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_GeneratesHelloWorldOutput(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot, err := filepath.Abs(filepath.Join("..", "..", "examples", "hello-world"))
	if err != nil {
		t.Fatalf("resolving website root: %v", err)
	}
	outDir := t.TempDir()

	args := []string{
		"-website-root", websiteRoot,
		"-out", outDir,
		"-sitemap-base-url", "https://example.com",
	}
	if err := Run(args, logger); err != nil {
		t.Fatalf("build run failed: %v", err)
	}

	indexPath := filepath.Join(outDir, "index.html")
	content, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("reading %s: %v", indexPath, err)
	}
	html := string(content)
	for _, expected := range []string{
		"<title>Hello World</title>",
		"<p>Hello, world.</p>",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("expected %q in rendered html", expected)
		}
	}

	cssPath := filepath.Join(outDir, "css", "main.css")
	if _, err := os.Stat(cssPath); err != nil {
		t.Fatalf("expected copied css asset %s: %v", cssPath, err)
	}

	sitemapPath := filepath.Join(outDir, "sitemap.xml")
	sitemapRaw, err := os.ReadFile(sitemapPath)
	if err != nil {
		t.Fatalf("expected generated sitemap: %v", err)
	}
	if !strings.Contains(string(sitemapRaw), "<urlset") {
		t.Fatalf("expected sitemap.xml to contain urlset")
	}
}

func TestRun_PassesPageNameToTemplates(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "assets", "css"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "layout"), 0o755); err != nil {
		t.Fatalf("mkdir layout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "partials"), 0o755); err != nil {
		t.Fatalf("mkdir partials: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "pages"), 0o755); err != nil {
		t.Fatalf("mkdir pages: %v", err)
	}

	files := map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"):            "body { color: #000; }\n",
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"):           "",
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "agenda.gohtml"):  `{{define "page"}}<main data-page="{{.PageName}}">agenda</main>{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	outDir := t.TempDir()
	args := []string{
		"-website-root", websiteRoot,
		"-out", outDir,
	}
	if err := Run(args, logger); err != nil {
		t.Fatalf("build run failed: %v", err)
	}

	rendered, err := os.ReadFile(filepath.Join(outDir, "agenda.html"))
	if err != nil {
		t.Fatalf("reading agenda output: %v", err)
	}
	if !strings.Contains(string(rendered), `data-page="agenda"`) {
		t.Fatalf("expected page name in rendered html, got %s", string(rendered))
	}
}

func TestRun_PassesSiteDataToTemplates(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "assets", "css"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "layout"), 0o755); err != nil {
		t.Fatalf("mkdir layout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "partials"), 0o755); err != nil {
		t.Fatalf("mkdir partials: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "pages"), 0o755); err != nil {
		t.Fatalf("mkdir pages: %v", err)
	}

	files := map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"):            "body { color: #000; }\n",
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"):           "",
		filepath.Join(websiteRoot, "src", "data", "site.yaml"):                    "courses:\n  agenda:\n    title: Agenda Centralizada\n",
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "agenda.gohtml"):  `{{define "page"}}<main data-title="{{required (dig .SiteData "courses" "agenda" "title") "missing courses.agenda.title"}}">{{required (dig .SiteData "courses" "agenda" "title") "missing courses.agenda.title"}}</main>{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	outDir := t.TempDir()
	args := []string{
		"-website-root", websiteRoot,
		"-out", outDir,
	}
	if err := Run(args, logger); err != nil {
		t.Fatalf("build run failed: %v", err)
	}

	rendered, err := os.ReadFile(filepath.Join(outDir, "agenda.html"))
	if err != nil {
		t.Fatalf("reading agenda output: %v", err)
	}
	if !strings.Contains(string(rendered), `data-title="Agenda Centralizada"`) {
		t.Fatalf("expected site data in rendered html, got %s", string(rendered))
	}
}

func TestRun_RequiredFailsForMissingSiteData(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "assets", "css"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "layout"), 0o755); err != nil {
		t.Fatalf("mkdir layout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "partials"), 0o755); err != nil {
		t.Fatalf("mkdir partials: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "pages"), 0o755); err != nil {
		t.Fatalf("mkdir pages: %v", err)
	}

	files := map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"):            "body { color: #000; }\n",
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"):           "",
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "agenda.gohtml"):  `{{define "page"}}{{required (dig .SiteData "courses" "agenda" "title") "missing courses.agenda.title"}}{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	outDir := t.TempDir()
	args := []string{
		"-website-root", websiteRoot,
		"-out", outDir,
	}
	err := Run(args, logger)
	if err == nil {
		t.Fatal("expected build run to fail when required site data is missing")
	}
	if !strings.Contains(err.Error(), "missing courses.agenda.title") {
		t.Fatalf("expected missing site data error, got %v", err)
	}
}

func TestRun_FailsWhenSiteDataContractMissing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(websiteRoot, "src", "assets", "css"),
		filepath.Join(websiteRoot, "src", "templates", "layout"),
		filepath.Join(websiteRoot, "src", "templates", "partials"),
		filepath.Join(websiteRoot, "src", "templates", "pages"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	files := map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"):            "body { color: #000; }\n",
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "agenda.gohtml"):  `{{define "page"}}ok{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	err := Run([]string{"-website-root", websiteRoot, "-out", t.TempDir()}, logger)
	if err == nil {
		t.Fatal("expected build run to fail without site contract")
	}
	if !strings.Contains(err.Error(), "required site data contract not found") {
		t.Fatalf("expected missing contract error, got %v", err)
	}
}

func TestRun_FailsWhenSiteDataViolatesContract(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "assets", "css"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "layout"), 0o755); err != nil {
		t.Fatalf("mkdir layout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "partials"), 0o755); err != nil {
		t.Fatalf("mkdir partials: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "pages"), 0o755); err != nil {
		t.Fatalf("mkdir pages: %v", err)
	}

	files := map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"):            "body { color: #000; }\n",
		filepath.Join(websiteRoot, "src", "data", "site.yaml"):                    "courses:\n  ssyb:\n    start_text: Em definição.\n    unexpected: value\n",
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"):           "allowed:\n  - courses.*.start_text\n",
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "agenda.gohtml"):  `{{define "page"}}ok{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	outDir := t.TempDir()
	err := Run([]string{"-website-root", websiteRoot, "-out", outDir}, logger)
	if err == nil {
		t.Fatal("expected build run to fail when site data violates contract")
	}
	if !strings.Contains(err.Error(), "dangling site data path not declared in contract: courses.ssyb.unexpected") {
		t.Fatalf("expected dangling path error, got %v", err)
	}
}

func TestRun_FailsWhenContractDeclaresUnusedTemplatePath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(websiteRoot, "src", "assets", "css"),
		filepath.Join(websiteRoot, "src", "data"),
		filepath.Join(websiteRoot, "src", "templates", "layout"),
		filepath.Join(websiteRoot, "src", "templates", "partials"),
		filepath.Join(websiteRoot, "src", "templates", "pages"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	files := map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"): "body { color: #000; }\n",
		filepath.Join(websiteRoot, "src", "data", "site.yaml"): `courses:
  ssyb:
    investment:
      total: R$ 100,00
      installments_text: Em até 2 parcelas
`,
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"): `allowed:
  - courses.*.investment.total
  - courses.*.investment.installments_text
`,
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "agenda.gohtml"):  `{{define "page"}}{{required (dig .SiteData "courses" "ssyb" "investment" "total") "missing total"}}{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	err := Run([]string{"-website-root", websiteRoot, "-out", t.TempDir()}, logger)
	if err == nil {
		t.Fatal("expected build run to fail for unused contract path")
	}
	if !strings.Contains(err.Error(), "allowed contract path not used by templates: courses.*.investment.installments_text") {
		t.Fatalf("expected unused contract path error, got %v", err)
	}
}

func TestRun_SiteDataOverrideWinsAndWarns(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	websiteRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "assets", "css"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "layout"), 0o755); err != nil {
		t.Fatalf("mkdir layout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "partials"), 0o755); err != nil {
		t.Fatalf("mkdir partials: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(websiteRoot, "src", "templates", "pages"), 0o755); err != nil {
		t.Fatalf("mkdir pages: %v", err)
	}

	overridePath := filepath.Join(t.TempDir(), "site.yaml")
	files := map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"):  "body { color: #000; }\n",
		filepath.Join(websiteRoot, "src", "data", "site.yaml"):          "courses:\n  agenda:\n    title: Local\n",
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"): "",
		overridePath: "courses:\n  agenda:\n    title: External\n",
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "agenda.gohtml"):  `{{define "page"}}{{required (dig .SiteData "courses" "agenda" "title") "missing courses.agenda.title"}}{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	outDir := t.TempDir()
	args := []string{
		"-website-root", websiteRoot,
		"-out", outDir,
		"-site-data", overridePath,
	}
	if err := Run(args, logger); err != nil {
		t.Fatalf("build run failed: %v", err)
	}

	rendered, err := os.ReadFile(filepath.Join(outDir, "agenda.html"))
	if err != nil {
		t.Fatalf("reading agenda output: %v", err)
	}
	if !strings.Contains(string(rendered), "External") {
		t.Fatalf("expected external site data to win, got %s", string(rendered))
	}
	if !strings.Contains(logBuf.String(), "site data override supersedes local site data file") {
		t.Fatalf("expected warning about site data override, got logs: %s", logBuf.String())
	}
}

func TestRun_MirrorsExternalAssetsIntoOutput(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	server := newMirrorAssetsTestServer(t)
	defer server.Close()

	websiteRoot := newTestWebsiteRoot(t)

	files := map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"):            "body { background-image: url('" + server.URL + "/local-bg.png'); }\n",
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"):           "",
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}<link rel="stylesheet" href="/css/main.css"><link rel="stylesheet" href="` + server.URL + `/remote.css">{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"):   `{{define "page"}}<img src="` + server.URL + `/inline-image.png" alt="remote">{{end}}`,
	}
	writeFiles(t, files)

	outDir := t.TempDir()
	args := []string{
		"-website-root", websiteRoot,
		"-out", outDir,
		"-mirror-external-assets",
	}
	if err := Run(args, logger); err != nil {
		t.Fatalf("build run failed: %v", err)
	}

	indexPath := filepath.Join(outDir, "index.html")
	indexHTML := string(mustReadFile(t, indexPath))
	assertNoExternalRefs(t, "index.html", indexHTML, server.URL)
	assertContainsAll(t, "index.html", indexHTML, []string{`href="/external/`, `src="/external/`})

	localCSSPath := filepath.Join(outDir, "css", "main.css")
	localCSS := string(mustReadFile(t, localCSSPath))
	assertNoExternalRefs(t, "css/main.css", localCSS, server.URL)
	assertContainsAll(t, "css/main.css", localCSS, []string{`url("/external/`})

	mustStat(t, filepath.Join(outDir, "external"))

	mirroredCSS := readFirstMirroredCSS(t, filepath.Join(outDir, "external"))
	if mirroredCSS == "" {
		t.Fatal("expected mirrored external css file")
	}
	assertNoExternalRefs(t, "mirrored css", mirroredCSS, server.URL)
	assertContainsAll(t, "mirrored css", mirroredCSS, []string{`url("/external/`})
}

func newMirrorAssetsTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	baseURL := new(string)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/remote.css" {
			w.Header().Set("Content-Type", "text/css")
			_, _ = io.WriteString(w, "@font-face { src: url('/font.woff2'); } .hero { background-image: url('"+*baseURL+"/bg.png'); }")
			return
		}
		if r.URL.Path == "/font.woff2" {
			w.Header().Set("Content-Type", "font/woff2")
			_, _ = w.Write([]byte("woff2-data"))
			return
		}
		if r.URL.Path == "/bg.png" || r.URL.Path == "/inline-image.png" || r.URL.Path == "/local-bg.png" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("png-data"))
			return
		}
		http.NotFound(w, r)
	})

	server := httptest.NewServer(handler)
	*baseURL = server.URL
	return server
}

func newTestWebsiteRoot(t *testing.T) string {
	t.Helper()

	websiteRoot := t.TempDir()
	mustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "css"))
	mustMkdirAll(t, filepath.Join(websiteRoot, "src", "data"))
	mustMkdirAll(t, filepath.Join(websiteRoot, "src", "templates", "layout"))
	mustMkdirAll(t, filepath.Join(websiteRoot, "src", "templates", "partials"))
	mustMkdirAll(t, filepath.Join(websiteRoot, "src", "templates", "pages"))
	return websiteRoot
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func writeFiles(t *testing.T, files map[string]string) {
	t.Helper()
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return raw
}

func mustStat(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s: %v", path, err)
	}
}

func assertNoExternalRefs(t *testing.T, label, content, external string) {
	t.Helper()
	if strings.Contains(content, external) {
		t.Fatalf("expected %s to avoid external references, got %s", label, content)
	}
}

func assertContainsAll(t *testing.T, label, content string, expected []string) {
	t.Helper()
	for _, e := range expected {
		if !strings.Contains(content, e) {
			t.Fatalf("expected %s to contain %q, got %s", label, e, content)
		}
	}
}

func readFirstMirroredCSS(t *testing.T, externalDir string) string {
	t.Helper()

	var mirroredCSS string
	err := filepath.WalkDir(externalDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".css" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mirroredCSS = string(content)
		return filepath.SkipAll
	})
	if err != nil {
		t.Fatalf("walking mirrored assets: %v", err)
	}
	return mirroredCSS
}
