package servecmd

import (
	"bytes"
	"context"
	"html/template"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"log/slog"

	"ffreis-website-compiler/internal/sitegen"
	"ffreis-website-compiler/internal/testutil"
)

func TestRequestIDMiddleware_SetsHeaderAndContext(t *testing.T) {
	var ctxRequestID string
	handler := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxRequestID = requestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	headerID := rr.Header().Get("X-Request-ID")
	if headerID == "" {
		t.Fatal("expected X-Request-ID header")
	}
	if ctxRequestID != headerID {
		t.Fatalf("expected context request id %q to match header %q", ctxRequestID, headerID)
	}
}

func TestRecoveryMiddleware_Returns500OnPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := recoveryMiddleware(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/panic", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	if !strings.Contains(buf.String(), "panic recovered") {
		t.Fatalf("expected panic recovered log, got: %s", buf.String())
	}
}

const (
	serveLayoutTmpl  = `{{define "layout"}}{{template "page" .}}{{end}}`
	serveHeadTmpl    = `{{define "head"}}{{end}}`
	servePageTmpl    = `{{define "page"}}ok{{end}}`
	flagWebsiteRoot  = "-website-root"
	flagAddr         = "-addr"
	flagSanity       = "-sanity"
	flagSiteDataFlag = "-site-data"
)

func setupServeWebsiteRoot(t *testing.T) string {
	t.Helper()
	websiteRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(websiteRoot, "src", "assets"),
		filepath.Join(websiteRoot, "src", "templates", "layout"),
		filepath.Join(websiteRoot, "src", "templates", "partials"),
		filepath.Join(websiteRoot, "src", "templates", "pages"),
		filepath.Join(websiteRoot, "src", "data"),
	} {
		testutil.MustMkdirAll(t, dir)
	}
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"):           "",
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   serveLayoutTmpl,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): serveHeadTmpl,
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"):   servePageTmpl,
	})
	return websiteRoot
}

func TestParseServeOptions_Defaults(t *testing.T) {
	opts, err := parseServeOptions(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.websiteRoot != "." || opts.addr != ":8080" || !opts.enableSanity {
		t.Fatalf("unexpected defaults: %+v", opts)
	}
}

func TestParseServeOptions_Overrides(t *testing.T) {
	opts, err := parseServeOptions([]string{
		flagWebsiteRoot, "/tmp/site",
		flagAddr, ":9090",
		flagSanity + "=false",
		flagSiteDataFlag, "/tmp/site.yaml",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.websiteRoot != "/tmp/site" || opts.addr != ":9090" || opts.enableSanity || opts.siteDataSource != "/tmp/site.yaml" {
		t.Fatalf("unexpected overrides: %+v", opts)
	}
}

func TestParseServeOptions_RejectsUnknownFlag(t *testing.T) {
	if _, err := parseServeOptions([]string{"-does-not-exist"}); err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
}

func TestLoadAndValidateSiteData_Success(t *testing.T) {
	websiteRoot := setupServeWebsiteRoot(t)
	templatesRoot := filepath.Join(websiteRoot, "src", "templates")

	pages, result, err := loadAndValidateSiteData(testutil.DiscardLogger(), templatesRoot, "", true)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(pages) != 1 || pages[0].Name != "index" {
		t.Fatalf("expected a single 'index' page, got %+v", pages)
	}
	if result.Data == nil {
		t.Fatal("expected non-nil site data")
	}
}

func TestLoadAndValidateSiteData_SanityFailurePropagates(t *testing.T) {
	websiteRoot := setupServeWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "data", "site.yaml"): "courses:\n  demo:\n    variants:\n      early:\n        start_text: \"2026-01-11\"\n        duration_hours: 4\n        cronograma:\n          sessions:\n            - date: \"2026-01-10\"\n              hours: 4\n",
	})
	templatesRoot := filepath.Join(websiteRoot, "src", "templates")

	if _, _, err := loadAndValidateSiteData(testutil.DiscardLogger(), templatesRoot, "", true); err == nil {
		t.Fatal("expected sanity validation to fail on mismatched course start date")
	}

	// The same mismatch must NOT fail when sanity is disabled.
	if _, _, err := loadAndValidateSiteData(testutil.DiscardLogger(), templatesRoot, "", false); err != nil {
		t.Fatalf("expected success with sanity disabled, got %v", err)
	}
}

func TestValidateAssetUsage_FailsOnMissingLocalAsset(t *testing.T) {
	websiteRoot := setupServeWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"): `{{define "page"}}<link rel="stylesheet" href="/css/missing.css">{{end}}`,
	})
	assetsRoot := filepath.Join(websiteRoot, "src", "assets")
	templatesRoot := filepath.Join(websiteRoot, "src", "templates")
	pages, result, err := loadAndValidateSiteData(testutil.DiscardLogger(), templatesRoot, "", true)
	if err != nil {
		t.Fatalf("loading site data: %v", err)
	}

	err = validateAssetUsage(assetsRoot, pages, result.Data)
	if err == nil {
		t.Fatal("expected missing asset reference to fail validation")
	}
	if !strings.Contains(err.Error(), "validating local css/js asset usage") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewServer_UsesEnvOverridesForTimeouts(t *testing.T) {
	t.Setenv("SERVE_READ_TIMEOUT", "3s")
	t.Setenv("SERVE_WRITE_TIMEOUT", "4s")
	t.Setenv("SERVE_IDLE_TIMEOUT", "5s")
	t.Setenv("SERVE_READ_HEADER_TIMEOUT", "1s")
	t.Setenv("SERVE_MAX_HEADER_BYTES", "2048")

	srv, shutdownTimeout := newServer(":0", t.TempDir(), nil, nil, testutil.DiscardLogger())
	if srv.ReadTimeout != 3*time.Second || srv.WriteTimeout != 4*time.Second || srv.IdleTimeout != 5*time.Second {
		t.Fatalf("expected env-overridden timeouts, got read=%v write=%v idle=%v", srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
	if srv.ReadHeaderTimeout != 1*time.Second {
		t.Fatalf("expected read header timeout override, got %v", srv.ReadHeaderTimeout)
	}
	if srv.MaxHeaderBytes != 2048 {
		t.Fatalf("expected max header bytes override, got %d", srv.MaxHeaderBytes)
	}
	if shutdownTimeout != 10*time.Second {
		t.Fatalf("expected default shutdown timeout, got %v", shutdownTimeout)
	}
}

func TestNewServer_DefaultsWhenEnvUnset(t *testing.T) {
	srv, shutdownTimeout := newServer(":0", t.TempDir(), nil, nil, testutil.DiscardLogger())
	if srv.ReadTimeout != 10*time.Second || srv.WriteTimeout != 15*time.Second || srv.IdleTimeout != 60*time.Second {
		t.Fatalf("unexpected default timeouts: read=%v write=%v idle=%v", srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
	if srv.MaxHeaderBytes != 1_048_576 {
		t.Fatalf("unexpected default max header bytes: %d", srv.MaxHeaderBytes)
	}
	if shutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected default shutdown timeout: %v", shutdownTimeout)
	}
}

func TestGetEnvDuration(t *testing.T) {
	t.Run("unset falls back", func(t *testing.T) {
		if got := getEnvDuration("SERVE_TEST_DUR_UNSET", 7*time.Second); got != 7*time.Second {
			t.Fatalf("expected fallback, got %v", got)
		}
	})
	t.Run("valid override", func(t *testing.T) {
		t.Setenv("SERVE_TEST_DUR", "2s")
		if got := getEnvDuration("SERVE_TEST_DUR", 7*time.Second); got != 2*time.Second {
			t.Fatalf("expected override, got %v", got)
		}
	})
	t.Run("invalid falls back", func(t *testing.T) {
		t.Setenv("SERVE_TEST_DUR_BAD", "not-a-duration")
		if got := getEnvDuration("SERVE_TEST_DUR_BAD", 7*time.Second); got != 7*time.Second {
			t.Fatalf("expected fallback on invalid value, got %v", got)
		}
	})
	t.Run("negative falls back", func(t *testing.T) {
		t.Setenv("SERVE_TEST_DUR_NEG", "-5s")
		if got := getEnvDuration("SERVE_TEST_DUR_NEG", 7*time.Second); got != 7*time.Second {
			t.Fatalf("expected fallback on negative value, got %v", got)
		}
	})
}

func TestGetEnvInt(t *testing.T) {
	t.Run("unset falls back", func(t *testing.T) {
		if got := getEnvInt("SERVE_TEST_INT_UNSET", 42); got != 42 {
			t.Fatalf("expected fallback, got %d", got)
		}
	})
	t.Run("valid override", func(t *testing.T) {
		t.Setenv("SERVE_TEST_INT", "99")
		if got := getEnvInt("SERVE_TEST_INT", 42); got != 99 {
			t.Fatalf("expected override, got %d", got)
		}
	})
	t.Run("invalid falls back", func(t *testing.T) {
		t.Setenv("SERVE_TEST_INT_BAD", "not-a-number")
		if got := getEnvInt("SERVE_TEST_INT_BAD", 42); got != 42 {
			t.Fatalf("expected fallback on invalid value, got %d", got)
		}
	})
	t.Run("negative falls back", func(t *testing.T) {
		t.Setenv("SERVE_TEST_INT_NEG", "-1")
		if got := getEnvInt("SERVE_TEST_INT_NEG", 42); got != 42 {
			t.Fatalf("expected fallback on negative value, got %d", got)
		}
	})
}

func TestRequestIDFromContext_NilContextReturnsEmpty(t *testing.T) {
	if got := requestIDFromContext(nil); got != "" { //nolint:staticcheck // deliberately exercising the nil-context guard
		t.Fatalf("expected empty string for nil context, got %q", got)
	}
}

func TestGenerateRequestID_ReturnsHexString(t *testing.T) {
	id := generateRequestID()
	if len(id) != 32 {
		t.Fatalf("expected a 32-char hex request id, got %q (len=%d)", id, len(id))
	}
}

func TestSecurityHeadersMiddleware_SetsExpectedHeaders(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	wantHeaders := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"X-Frame-Options":        "DENY",
	}
	for header, want := range wantHeaders {
		if got := rr.Header().Get(header); got != want {
			t.Fatalf("expected %s=%q, got %q", header, want, got)
		}
	}
	if csp := rr.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("expected a default-src CSP directive, got %q", csp)
	}
}

func TestLoggingMiddleware_LogsRequestCompletion(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := loggingMiddleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/tea", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	logged := buf.String()
	if !strings.Contains(logged, "http request completed") {
		t.Fatalf("expected completion log line, got: %s", logged)
	}
	if !strings.Contains(logged, "418") {
		t.Fatalf("expected the recorded status code 418 in the log, got: %s", logged)
	}
}

func TestLoggingMiddleware_RePanicsAfterRecordingStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	// Compose the same way newServer does: recovery outermost, logging inside it.
	handler := recoveryMiddleware(logger, loggingMiddleware(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	})))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/boom", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 after the panic propagated through logging middleware, got %d", rr.Code)
	}
	if !strings.Contains(buf.String(), "panic recovered") {
		t.Fatalf("expected the outer recovery middleware to log the panic, got: %s", buf.String())
	}
}

func TestRegisterStatic_ServesKnownRoutes(t *testing.T) {
	siteRoot := t.TempDir()
	files := map[string]string{
		filepath.Join(siteRoot, "css", "main.css"):    "body{}",
		filepath.Join(siteRoot, "js", "main.js"):      "console.log(1)",
		filepath.Join(siteRoot, "images", "logo.svg"): "<svg></svg>",
		filepath.Join(siteRoot, "fonts", "a.woff2"):   "font-bytes",
		filepath.Join(siteRoot, "ld", "org.json"):     "{}",
		filepath.Join(siteRoot, "robots.txt"):         "User-agent: *\n",
		filepath.Join(siteRoot, "sitemap.xml"):        "<urlset></urlset>",
		filepath.Join(siteRoot, "send.js"):            "send()",
		filepath.Join(siteRoot, "contactScript.js"):   "contact()",
	}
	for path := range files {
		testutil.MustMkdirAll(t, filepath.Dir(path))
	}
	testutil.WriteFiles(t, files)

	mux := http.NewServeMux()
	registerStatic(mux, siteRoot)
	server := httptest.NewServer(mux)
	defer server.Close()

	cases := []struct {
		path string
		want string
	}{
		{"/css/main.css", "body{}"},
		{"/js/main.js", "console.log(1)"},
		{"/images/logo.svg", "<svg></svg>"},
		{"/fonts/a.woff2", "font-bytes"},
		{"/ld/org.json", "{}"},
		{"/robots.txt", "User-agent: *\n"},
		{"/sitemap.xml", "<urlset></urlset>"},
		{"/send.js", "send()"},
		{"/contactScript.js", "contact()"},
	}
	for _, tc := range cases {
		resp := httpGetContext(t, server.URL+tc.path)
		body := readAllAndClose(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: expected 200, got %d", tc.path, resp.StatusCode)
		}
		if body != tc.want {
			t.Fatalf("GET %s: expected body %q, got %q", tc.path, tc.want, body)
		}
	}
}

func TestRegisterPages_IndexAndNamedPageRouting(t *testing.T) {
	indexTpl := template.Must(template.New("layout").Parse(`index:{{.PageName}}`))
	aboutTpl := template.Must(template.New("layout").Parse(`about:{{.PageName}}`))
	pages := []sitegen.PageTemplate{
		{Name: "index", Tmpl: indexTpl},
		{Name: "about", Tmpl: aboutTpl},
	}

	mux := http.NewServeMux()
	registerPages(mux, pages, map[string]any{}, testutil.DiscardLogger())
	server := httptest.NewServer(mux)
	defer server.Close()

	assertGetBody(t, server.URL+"/", http.StatusOK, "index:index")
	assertGetBody(t, server.URL+"/about.html", http.StatusOK, "about:about")
	assertGetBody(t, server.URL+"/index.html", http.StatusOK, "index:index")

	// Any unmatched path falls through to the "/" catch-all pattern, whose
	// handler 404s on anything that isn't the exact root path.
	resp := httpGetContext(t, server.URL+"/does-not-exist")
	_ = readAllAndClose(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for an unmatched path, got %d", resp.StatusCode)
	}
}

func TestRenderTemplate_ExecutionErrorReturns500AndLogs(t *testing.T) {
	// Accessing a field on a string value is a template execution error.
	badTpl := template.Must(template.New("layout").Parse(`{{.PageName.NoSuchField}}`))
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/broken.html", nil)
	renderTemplate(rr, req, badTpl, "broken", map[string]any{}, logger)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on template execution failure, got %d", rr.Code)
	}
	if !strings.Contains(buf.String(), "template execution failed") {
		t.Fatalf("expected the execution failure to be logged, got: %s", buf.String())
	}
}

func TestRun_FailsWhenAddressAlreadyInUse(t *testing.T) {
	websiteRoot := setupServeWebsiteRoot(t)

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	defer ln.Close()

	err = Run([]string{flagWebsiteRoot, websiteRoot, flagAddr, ln.Addr().String()}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected Run to fail when the address is already bound")
	}
	if !strings.Contains(err.Error(), "address already in use") && !strings.Contains(err.Error(), "bind") {
		t.Fatalf("expected an address-in-use style error, got: %v", err)
	}
}

func TestRun_FailsOnUnresolvableWebsiteRoot(t *testing.T) {
	err := Run([]string{flagWebsiteRoot, filepath.Join(t.TempDir(), "does-not-exist")}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected Run to fail for an unresolvable website root")
	}
}

func readAllAndClose(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	return buf.String()
}

func assertGetBody(t *testing.T, url string, wantStatus int, wantBody string) {
	t.Helper()
	resp := httpGetContext(t, url)
	body := readAllAndClose(t, resp)
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s: expected status %d, got %d (body=%q)", url, wantStatus, resp.StatusCode, body)
	}
	if body != wantBody {
		t.Fatalf("GET %s: expected body %q, got %q", url, wantBody, body)
	}
}

func httpGetContext(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building GET request for %s: %v", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}
