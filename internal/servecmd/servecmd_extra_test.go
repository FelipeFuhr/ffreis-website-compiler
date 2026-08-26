package servecmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ffreis-website-compiler/internal/sitegen"
	"ffreis-website-compiler/internal/testutil"
)

// ── fixtures ────────────────────────────────────────────────────────────────

const (
	serveLayoutTmpl = `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`
	serveHeadTmpl   = `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`
	serveIndexTmpl  = `{{define "page"}}<h1>{{required (dig .SiteData "title") "missing title"}}</h1>{{end}}`
	serveAboutTmpl  = `{{define "page"}}<h1>About</h1>{{end}}`
	serveCSS        = "body { color: #000; }\n"
)

// newServeSite builds a website root the serve command can load.
func newServeSite(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "assets", "css"))
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "data"))
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "templates", "layout"))
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "templates", "partials"))
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "templates", "pages"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(root, "src", "assets", "css", "main.css"):            serveCSS,
		filepath.Join(root, "src", "data", "site.yaml"):                    "title: Served Site\n",
		filepath.Join(root, "src", "data", "site.contract.yaml"):           "allowed:\n  - title\n",
		filepath.Join(root, "src", "templates", "layout", "base.gohtml"):   serveLayoutTmpl,
		filepath.Join(root, "src", "templates", "partials", "head.gohtml"): serveHeadTmpl,
		filepath.Join(root, "src", "templates", "pages", "index.gohtml"):   serveIndexTmpl,
		filepath.Join(root, "src", "templates", "pages", "about.gohtml"):   serveAboutTmpl,
	})
	return root
}

// loadServeSite returns the pages and site data for a fixture root.
func loadServeSite(t *testing.T, root string) ([]sitegen.PageTemplate, map[string]any) {
	t.Helper()
	templatesRoot := filepath.Join(root, "src", "templates")
	pages, result, err := loadAndValidateSiteData(testutil.DiscardLogger(), templatesRoot, "", true)
	if err != nil {
		t.Fatalf("loading fixture site: %v", err)
	}
	return pages, result.Data
}

// ── options ─────────────────────────────────────────────────────────────────

func TestParseServeOptions_Defaults(t *testing.T) {
	opts, err := parseServeOptions(nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.addr != ":8080" {
		t.Errorf("addr = %q, want :8080", opts.addr)
	}
	if opts.websiteRoot != "." {
		t.Errorf("websiteRoot = %q, want .", opts.websiteRoot)
	}
	if !opts.enableSanity {
		t.Error("sanity should default on")
	}
}

func TestParseServeOptions_RejectsAnUnknownFlag(t *testing.T) {
	if _, err := parseServeOptions([]string{"-not-a-real-flag"}); err == nil {
		t.Fatal("an unknown flag must be an error")
	}
}

// ── site loading ────────────────────────────────────────────────────────────

func TestLoadAndValidateSiteData_LoadsAValidSite(t *testing.T) {
	pages, data := loadServeSite(t, newServeSite(t))

	if len(pages) != 2 {
		t.Errorf("loaded %d pages, want 2 (index + about)", len(pages))
	}
	if data["title"] != "Served Site" {
		t.Errorf("title = %v, want Served Site", data["title"])
	}
}

func TestLoadAndValidateSiteData_FailsOnAContractViolation(t *testing.T) {
	root := newServeSite(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(root, "src", "data", "site.yaml"): "title: Served Site\nundeclared: nope\n",
	})

	_, _, err := loadAndValidateSiteData(testutil.DiscardLogger(),
		filepath.Join(root, "src", "templates"), "", true)
	if err == nil {
		t.Fatal("site data outside the contract must fail before the server starts")
	}
}

func TestLoadAndValidateSiteData_ReportsAMissingTemplatesRoot(t *testing.T) {
	_, _, err := loadAndValidateSiteData(testutil.DiscardLogger(),
		filepath.Join(t.TempDir(), "absent"), "", true)
	if err == nil {
		t.Fatal("a missing templates root must fail before the server starts")
	}
	// The contract lookup is what actually reports it: an empty template set
	// loads fine, so the first hard requirement to go missing is the contract.
	if !strings.Contains(err.Error(), "contract") {
		t.Errorf("err = %v, want the missing contract named so the cause is findable", err)
	}
}

func TestValidateAssetUsage_FailsOnAnOrphanedAsset(t *testing.T) {
	root := newServeSite(t)
	pages, data := loadServeSite(t, root)
	assetsRoot := filepath.Join(root, "src", "assets")

	if err := validateAssetUsage(assetsRoot, pages, data); err != nil {
		t.Fatalf("the fixture site should validate cleanly: %v", err)
	}

	testutil.WriteFiles(t, map[string]string{filepath.Join(assetsRoot, "css", "orphan.css"): "a{}\n"})
	if err := validateAssetUsage(assetsRoot, pages, data); err == nil {
		t.Fatal("an asset no page references must fail validation")
	}
}

// ── the served handler ──────────────────────────────────────────────────────

// newTestServer builds the same handler stack Run serves, so these tests
// exercise the real middleware chain rather than a hand-assembled one.
func newTestServer(t *testing.T) (http.Handler, string) {
	t.Helper()
	root := newServeSite(t)
	pages, data := loadServeSite(t, root)
	srv, shutdown := newServer(":0", filepath.Join(root, "src", "assets"), pages, data, testutil.DiscardLogger())
	if shutdown <= 0 {
		t.Errorf("shutdown timeout = %v, want a positive default", shutdown)
	}
	return srv.Handler, root
}

func TestNewServer_ServesTheIndexPageAtRoot(t *testing.T) {
	handler, _ := newTestServer(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Served Site") {
		t.Errorf("body did not render the index page:\n%s", rec.Body.String())
	}
}

func TestNewServer_ServesNamedPagesAtTheirHTMLPath(t *testing.T) {
	handler, _ := newTestServer(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/about.html", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "About") {
		t.Errorf("body did not render the about page:\n%s", rec.Body.String())
	}
}

// The index handler is registered at "/", which otherwise matches everything.
func TestNewServer_UnknownPathIs404NotTheIndexPage(t *testing.T) {
	handler, _ := newTestServer(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/no-such-page", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — '/' must not swallow unknown paths", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "Served Site") {
		t.Error("an unknown path rendered the index page")
	}
}

func TestNewServer_ServesStaticAssets(t *testing.T) {
	handler, _ := newTestServer(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/css/main.css", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a static asset", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "color") {
		t.Errorf("static asset body = %q, want the css file contents", rec.Body.String())
	}
}

func TestNewServer_SetsSecurityHeadersOnEveryResponse(t *testing.T) {
	handler, _ := newTestServer(t)

	for _, path := range []string{"/", "/about.html", "/no-such-page"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

			for header, want := range map[string]string{
				"X-Content-Type-Options": "nosniff",
				"Referrer-Policy":        "no-referrer",
				"X-Frame-Options":        "DENY",
			} {
				if got := rec.Header().Get(header); got != want {
					t.Errorf("%s = %q, want %q", header, got, want)
				}
			}
			if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
				t.Errorf("CSP = %q, want a self-only default-src", csp)
			}
		})
	}
}

func TestNewServer_SetsARequestIDOnEveryResponse(t *testing.T) {
	handler, _ := newTestServer(t)

	seen := map[string]bool{}
	for range 3 {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		id := rec.Header().Get("X-Request-ID")
		if id == "" {
			t.Fatal("no X-Request-ID on the response")
		}
		if seen[id] {
			t.Errorf("request id %q was reused across requests", id)
		}
		seen[id] = true
	}
}

// A template that fails midway through execution leaves a TRUNCATED page with a
// 200, not a 500. renderTemplate executes straight into the ResponseWriter, so
// by the time the error surfaces the header is already sent and the subsequent
// http.Error call cannot change it.
//
// This is pinned rather than fixed because it only affects `website-compiler
// serve`, the local preview server — a truncated page is immediately visible to
// the developer looking at it, and buffering every response to fix it would cost
// memory on every request. The BUILD path is unaffected: it renders to memory
// and a failing template fails the build. If this is ever fixed, this test
// should be inverted, not deleted.
func TestRenderTemplate_MidRenderFailureLeavesATruncatedPage(t *testing.T) {
	root := t.TempDir()
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "assets", "css"))
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "data"))
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "templates", "layout"))
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "templates", "partials"))
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "templates", "pages"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(root, "src", "assets", "css", "main.css"):            serveCSS,
		filepath.Join(root, "src", "data", "site.yaml"):                    "title: T\n",
		filepath.Join(root, "src", "data", "site.contract.yaml"):           "allowed:\n  - title\n",
		filepath.Join(root, "src", "templates", "layout", "base.gohtml"):   serveLayoutTmpl,
		filepath.Join(root, "src", "templates", "partials", "head.gohtml"): serveHeadTmpl,
		// `required` on an absent key fails at execution time, not parse time.
		filepath.Join(root, "src", "templates", "pages", "index.gohtml"): `{{define "page"}}{{required (dig .SiteData "definitely" "absent") "boom"}}{{end}}`,
	})

	pages, err := sitegen.LoadPageTemplatesFromRoot(filepath.Join(root, "src", "templates"))
	if err != nil {
		t.Fatalf("loading templates: %v", err)
	}
	srv, _ := newServer(":0", filepath.Join(root, "src", "assets"), pages, map[string]any{"title": "T"}, testutil.DiscardLogger())

	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — the header is already sent when the failure surfaces", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "</html>") {
		t.Errorf("page rendered completely; the fixture template was supposed to fail:\n%s", body)
	}
	if !strings.Contains(body, "<html>") {
		t.Errorf("expected the already-flushed prefix of the page, got:\n%s", body)
	}
}

func TestStatusRecorder_RecordsTheWrittenStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	recorder.WriteHeader(http.StatusTeapot)

	if recorder.status != http.StatusTeapot {
		t.Errorf("recorded status = %d, want %d", recorder.status, http.StatusTeapot)
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("underlying writer got %d, want the status passed through", rec.Code)
	}
}

// ── server tuning from the environment ──────────────────────────────────────

func TestGetEnvDuration_ParsesOrFallsBack(t *testing.T) {
	const key = "SERVE_TEST_DURATION"
	fallback := 7 * time.Second

	for _, tc := range []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"unset falls back", "", fallback},
		{"whitespace falls back", "   ", fallback},
		{"valid duration is used", "250ms", 250 * time.Millisecond},
		{"surrounding whitespace is trimmed", "  2s  ", 2 * time.Second},
		{"unparseable falls back", "not-a-duration", fallback},
		{"negative falls back", "-5s", fallback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(key, tc.value)
			if tc.value == "" {
				if err := os.Unsetenv(key); err != nil {
					t.Fatalf("unsetenv: %v", err)
				}
			}
			if got := getEnvDuration(key, fallback); got != tc.want {
				t.Errorf("getEnvDuration(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestGetEnvInt_ParsesOrFallsBack(t *testing.T) {
	const key = "SERVE_TEST_INT"
	fallback := 4096

	for _, tc := range []struct {
		name  string
		value string
		want  int
	}{
		{"unset falls back", "", fallback},
		{"valid int is used", "8192", 8192},
		{"surrounding whitespace is trimmed", " 512 ", 512},
		{"unparseable falls back", "lots", fallback},
		{"negative falls back", "-1", fallback},
		{"zero is honoured", "0", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(key, tc.value)
			if tc.value == "" {
				if err := os.Unsetenv(key); err != nil {
					t.Fatalf("unsetenv: %v", err)
				}
			}
			if got := getEnvInt(key, fallback); got != tc.want {
				t.Errorf("getEnvInt(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestNewServer_AppliesEnvironmentTuningToTheServer(t *testing.T) {
	t.Setenv("SERVE_READ_TIMEOUT", "1s")
	t.Setenv("SERVE_WRITE_TIMEOUT", "2s")
	t.Setenv("SERVE_IDLE_TIMEOUT", "3s")
	t.Setenv("SERVE_READ_HEADER_TIMEOUT", "4s")
	t.Setenv("SERVE_MAX_HEADER_BYTES", "2048")
	t.Setenv("SERVE_SHUTDOWN_TIMEOUT", "5s")

	root := newServeSite(t)
	pages, data := loadServeSite(t, root)
	srv, shutdown := newServer(":0", filepath.Join(root, "src", "assets"), pages, data, testutil.DiscardLogger())

	for name, got := range map[string]time.Duration{
		"ReadTimeout":       srv.ReadTimeout,
		"WriteTimeout":      srv.WriteTimeout,
		"IdleTimeout":       srv.IdleTimeout,
		"ReadHeaderTimeout": srv.ReadHeaderTimeout,
	} {
		if got <= 0 {
			t.Errorf("%s = %v, want the configured value", name, got)
		}
	}
	if srv.ReadTimeout != time.Second {
		t.Errorf("ReadTimeout = %v, want 1s from the environment", srv.ReadTimeout)
	}
	if srv.MaxHeaderBytes != 2048 {
		t.Errorf("MaxHeaderBytes = %d, want 2048 from the environment", srv.MaxHeaderBytes)
	}
	if shutdown != 5*time.Second {
		t.Errorf("shutdown timeout = %v, want 5s from the environment", shutdown)
	}
}

func TestRun_ReportsAnUnresolvableWebsiteRoot(t *testing.T) {
	err := Run([]string{"-website-root", t.TempDir(), "-addr", "127.0.0.1:0"}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("an unresolvable website root must fail before the server starts")
	}
}
