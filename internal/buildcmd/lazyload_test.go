package buildcmd

import (
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

// ── injectDefaultLazyLoading ──────────────────────────────────────────────────

func TestInjectDefaultLazyLoading_AddsLazyWhenNoLoadingAttr(t *testing.T) {
	// The second image onwards: unmarked means lazy.
	doc := `<img src="images/hero.jpg" alt="Hero"><img src="images/photo.jpg" alt="A photo">`
	got, err := injectDefaultLazyLoading(doc)
	if err != nil {
		t.Fatalf("injectDefaultLazyLoading: %v", err)
	}
	if !strings.Contains(got, `src="images/photo.jpg" alt="A photo" loading="lazy"`) {
		t.Errorf("expected loading=lazy added to the second img, got: %s", got)
	}
}

// TestInjectDefaultLazyLoading_FirstImageIsPrioritisedNotLazied pins the
// behaviour change: lazy-loading the page's first image was this transform's
// own anti-pattern, since that image is the typical Largest-Contentful-Paint
// candidate and loading="lazy" delays its request until after layout.
func TestInjectDefaultLazyLoading_FirstImageIsPrioritisedNotLazied(t *testing.T) {
	doc := `<img src="images/hero.jpg" alt="Hero">`
	got, err := injectDefaultLazyLoading(doc)
	if err != nil {
		t.Fatalf("injectDefaultLazyLoading: %v", err)
	}
	if strings.Contains(got, `loading=`) {
		t.Errorf("the first image must carry no loading attribute at all — absent is eager, and "+
			`writing loading="eager" would opt it into LQIP placeholder generation; got: %s`, got)
	}
	for _, want := range []string{`fetchpriority="high"`, `decoding="async"`} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %s on the first image, got: %s", want, got)
		}
	}
}

func TestInjectDefaultLazyLoading_FirstImageKeepsAnExplicitLoadingValue(t *testing.T) {
	// A hand-written loading="lazy" on the first image is respected here rather
	// than silently overridden — the author may know something the heuristic
	// does not. The `lcp-image-not-lazy` convention gate is what reports it, so
	// the decision is made in review instead of by a transform.
	doc := `<img src="images/hero.jpg" alt="Hero" loading="lazy">`
	got, err := injectDefaultLazyLoading(doc)
	if err != nil {
		t.Fatalf("injectDefaultLazyLoading: %v", err)
	}
	if got != doc {
		t.Errorf("expected an explicitly lazy first image left exactly as written, got: %s", got)
	}
}

func TestInjectDefaultLazyLoading_ExplicitEagerFirstImageIsStillPrioritised(t *testing.T) {
	// loading="eager" says "this is above the fold"; fetchpriority says "fetch it
	// before the other above-the-fold things". The author's own attribute is
	// untouched, and the two hints are added.
	doc := `<img src="images/hero.jpg" alt="Hero" loading="eager">`
	got, err := injectDefaultLazyLoading(doc)
	if err != nil {
		t.Fatalf("injectDefaultLazyLoading: %v", err)
	}
	if !strings.Contains(got, `loading="eager"`) || strings.Contains(got, `loading="lazy"`) {
		t.Errorf("expected loading=eager preserved, got: %s", got)
	}
	if !strings.Contains(got, `fetchpriority="high"`) {
		t.Errorf("expected fetchpriority=high on an explicitly eager first image, got: %s", got)
	}
}

func TestInjectDefaultLazyLoading_DoesNotOverrideAuthoredPriorityHints(t *testing.T) {
	doc := `<img src="images/hero.jpg" alt="Hero" fetchpriority="low" decoding="sync">`
	got, err := injectDefaultLazyLoading(doc)
	if err != nil {
		t.Fatalf("injectDefaultLazyLoading: %v", err)
	}
	if got != doc {
		t.Errorf("expected authored fetchpriority/decoding left alone, got: %s", got)
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
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS): mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML): "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}` +
			`<img src="/images/hero.png" alt="hero"><img src="/images/thumb.png" alt="thumb">{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	html := string(mustReadFile(t, filepath.Join(outDir, fileAgendaHTML)))
	if !strings.Contains(html, `loading="lazy"`) {
		t.Errorf("expected default loading=lazy on the non-first, non-eager image, got: %s", html)
	}
	if !strings.Contains(html, `fetchpriority="high"`) {
		t.Errorf("expected the first image prioritised through the full pipeline, got: %s", html)
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
