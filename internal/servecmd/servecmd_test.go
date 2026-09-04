package servecmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"log/slog"
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

// TestRegisterStatic_ServesJSAssetsUnderJSPrefix proves that per-site
// contact-form scripts like send.js and contactScript.js — which real
// sites (e.g. flemming-website) place under src/assets/js/ and reference
// from templates via relative "js/send.js" src attributes — are already
// served correctly by the generic "/js/" prefix route, with no need for
// a root-level special case for their bare filenames.
func TestRegisterStatic_ServesJSAssetsUnderJSPrefix(t *testing.T) {
	siteRoot := t.TempDir()
	jsDir := filepath.Join(siteRoot, "js")
	if err := os.MkdirAll(jsDir, 0o755); err != nil {
		t.Fatalf("mkdir js dir: %v", err)
	}
	files := map[string]string{
		"send.js":          "// send.js contents",
		"contactScript.js": "// contactScript.js contents",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(jsDir, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	mux := http.NewServeMux()
	registerStatic(mux, siteRoot)

	for name, contents := range files {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/js/"+name, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("GET /js/%s: expected 200, got %d", name, rr.Code)
		}
		if rr.Body.String() != contents {
			t.Fatalf("GET /js/%s: expected body %q, got %q", name, contents, rr.Body.String())
		}
	}

	// The bare root-level paths (no /js/ prefix) are not, and never were,
	// how any real site references these scripts — templates use the
	// relative "js/send.js" src, which resolves to /js/send.js on the
	// page. Confirm no root-level route is registered for them.
	for name := range files {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+name, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("GET /%s: expected 404 (no root-level route), got %d", name, rr.Code)
		}
	}
}
