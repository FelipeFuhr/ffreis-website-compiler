package buildcmd

import (
	"bytes"
	"html/template"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/sitegen"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

// mustListingTemplate builds a minimal but real *template.Template exposing
// the "layout" entry point buildcmd.Run always executes
// (ExecuteTemplate(&buf, "layout", ...)), rendering a field from
// .CurrentListing — the same shape a real site's templates/pages/listing.gohtml
// would provide.
func mustListingTemplate(t *testing.T, body string) sitegen.PageTemplate {
	t.Helper()
	tpl, err := template.New("layout").Parse(body)
	if err != nil {
		t.Fatalf("parsing test listing template: %v", err)
	}
	return sitegen.PageTemplate{Name: "listing", Tmpl: tpl}
}

func TestWriteListingDetailPages_RendersArbitrarySchemaFields(t *testing.T) {
	// heavy-heater-shaped data: none of these field names exist in the
	// listings package at all — that's the point of genericizing it.
	list := []map[string]any{
		{
			"id":      "bug-metro",
			"title":   "Bug Metro",
			"partner": "Pocket World Arcade",
			"tokens":  6,
		},
	}
	tpl := mustListingTemplate(t, `{{define "layout"}}<html><body><h1>{{.CurrentListing.title}}</h1><p>{{.CurrentListing.partner}} - {{.CurrentListing.tokens}} tokens</p><a href="{{.CurrentListing.href}}">link</a></body></html>{{end}}`)

	outDir := t.TempDir()
	opts := buildOptions{outDir: outDir}
	logger := discardLogger()

	urls, err := writeListingDetailPages(logger, opts, tpl, list, map[string]any{}, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("writeListingDetailPages: %v", err)
	}
	if len(urls) != 1 || urls[0].Path != "/listings/bug-metro/" {
		t.Fatalf("expected one sitemap URL /listings/bug-metro/, got %+v", urls)
	}

	htmlBytes, err := os.ReadFile(filepath.Join(outDir, "listings", "bug-metro", "index.html"))
	if err != nil {
		t.Fatalf("reading rendered listing page: %v", err)
	}
	html := string(htmlBytes)
	if !strings.Contains(html, "Bug Metro") {
		t.Errorf("expected title rendered, got %s", html)
	}
	if !strings.Contains(html, "Pocket World Arcade - 6 tokens") {
		t.Errorf("expected arbitrary fields (partner, tokens) rendered, got %s", html)
	}
	if !strings.Contains(html, `href="/listings/bug-metro/"`) {
		t.Errorf("expected computed href rendered, got %s", html)
	}
}

func TestMaybeWriteListingDetailPages_NoopWhenNoListingsLoaded(t *testing.T) {
	content := &optionalContent{}
	urls, err := maybeWriteListingDetailPages(discardLogger(), buildOptions{}, content, map[string]any{}, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if urls != nil {
		t.Errorf("expected no sitemap URLs when no listings are loaded, got %+v", urls)
	}
}
