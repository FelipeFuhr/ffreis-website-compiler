package buildcmd

import (
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

// ── injectDefaultLazyLoading ──────────────────────────────────────────────────

func TestInjectDefaultLazyLoading_AddsLazyWhenNoLoadingAttr(t *testing.T) {
	doc := `<img src="images/photo.jpg" alt="A photo">`
	got, err := injectDefaultLazyLoading(doc)
	if err != nil {
		t.Fatalf("injectDefaultLazyLoading: %v", err)
	}
	if !strings.Contains(got, `loading="lazy"`) {
		t.Errorf("expected loading=lazy added, got: %s", got)
	}
}

func TestInjectDefaultLazyLoading_LeavesExplicitEagerUntouched(t *testing.T) {
	doc := `<img src="images/hero.jpg" alt="Hero" loading="eager">`
	got, err := injectDefaultLazyLoading(doc)
	if err != nil {
		t.Fatalf("injectDefaultLazyLoading: %v", err)
	}
	if got != doc {
		t.Errorf("expected eager img unchanged, got: %s", got)
	}
	if strings.Contains(got, `loading="lazy"`) {
		t.Errorf("expected no loading=lazy on an eager img, got: %s", got)
	}
}

func TestInjectDefaultLazyLoading_LeavesExplicitLazyUntouched(t *testing.T) {
	doc := `<img src="images/photo.jpg" alt="A photo" loading="lazy">`
	got, err := injectDefaultLazyLoading(doc)
	if err != nil {
		t.Fatalf("injectDefaultLazyLoading: %v", err)
	}
	if got != doc {
		t.Errorf("expected already-lazy img unchanged, got: %s", got)
	}
	if strings.Count(got, "loading=") != 1 {
		t.Errorf("expected exactly one loading attribute, got: %s", got)
	}
}

func TestInjectDefaultLazyLoading_MultipleImagesOnlyMissingOnesGetLazy(t *testing.T) {
	doc := `<img src="a.jpg" alt="A" loading="eager"><img src="b.jpg" alt="B">`
	got, err := injectDefaultLazyLoading(doc)
	if err != nil {
		t.Fatalf("injectDefaultLazyLoading: %v", err)
	}
	if !strings.Contains(got, `src="a.jpg" alt="A" loading="eager"`) {
		t.Errorf("expected eager img untouched, got: %s", got)
	}
	if !strings.Contains(got, `src="b.jpg" alt="B" loading="lazy"`) {
		t.Errorf("expected loading=lazy appended to the second img, got: %s", got)
	}
}

func TestInjectDefaultLazyLoading_NoImagesIsNoOp(t *testing.T) {
	doc := `<html><head><title>X</title></head><body><p>no images</p></body></html>`
	got, err := injectDefaultLazyLoading(doc)
	if err != nil {
		t.Fatalf("injectDefaultLazyLoading: %v", err)
	}
	if got != doc {
		t.Errorf("expected no-op when no <img> tags, got: %s", got)
	}
}

// ── pipeline ordering: default lazy-loading vs. LQIP eager detection ──────────

func TestRun_NonEagerImageGetsDefaultLazyLoading(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "images"))
	writePNG(t, filepath.Join(websiteRoot, "src", "assets"), "images/thumb.png", 40, 40)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<img src="/images/thumb.png" alt="thumb">{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	html := string(mustReadFile(t, filepath.Join(outDir, fileAgendaHTML)))
	if !strings.Contains(html, `loading="lazy"`) {
		t.Errorf("expected default loading=lazy on non-eager image, got: %s", html)
	}
	// It must not have been picked up by LQIP (which only processes loading="eager").
	if strings.Contains(html, "data:image/jpeg;base64,") {
		t.Errorf("expected non-eager image NOT to get an LQIP placeholder, got: %s", html)
	}
}

func TestRun_EagerImageStillGetsLQIPNotLazy(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "images"))
	writePNG(t, filepath.Join(websiteRoot, "src", "assets"), "images/hero.png", 100, 50)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<img src="/images/hero.png" loading="eager" alt="hero">{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	html := string(mustReadFile(t, filepath.Join(outDir, fileAgendaHTML)))
	if strings.Contains(html, `loading="lazy"`) {
		t.Errorf("expected eager image to keep loading=eager, not be overwritten with lazy, got: %s", html)
	}
	if !strings.Contains(html, "data:image/jpeg;base64,") {
		t.Errorf("expected eager image to still get an LQIP placeholder, got: %s", html)
	}
}
