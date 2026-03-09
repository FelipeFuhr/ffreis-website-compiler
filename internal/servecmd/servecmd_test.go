package servecmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	if !strings.Contains(buf.String(), "panic recovered") {
		t.Fatalf("expected panic recovered log, got: %s", buf.String())
	}
}
