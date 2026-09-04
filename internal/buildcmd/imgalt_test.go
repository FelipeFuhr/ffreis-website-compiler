package buildcmd

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

// ── validateImageAltText ──────────────────────────────────────────────────────

func TestValidateImageAltText_MissingAltWarns(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	validateImageAltText(logger, "testpage", `<img src="images/hero.png">`)

	if !strings.Contains(logBuf.String(), "img missing alt attribute") {
		t.Errorf("expected a warning about missing alt, got logs: %s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "images/hero.png") {
		t.Errorf("expected the warning to name the offending src, got logs: %s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "testpage") {
		t.Errorf("expected the warning to name the page, got logs: %s", logBuf.String())
	}
}

func TestValidateImageAltText_EmptyAltDoesNotWarn(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	validateImageAltText(logger, "testpage", `<img src="images/deco.png" alt="">`)

	if logBuf.Len() != 0 {
		t.Errorf("expected no warning for an intentionally empty alt, got logs: %s", logBuf.String())
	}
}

func TestValidateImageAltText_NonEmptyAltDoesNotWarn(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	validateImageAltText(logger, "testpage", `<img src="images/hero.png" alt="A hero shot">`)

	if logBuf.Len() != 0 {
		t.Errorf("expected no warning when alt is present, got logs: %s", logBuf.String())
	}
}

func TestValidateImageAltText_OnlyFlagsImagesMissingAlt(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	html := `<img src="a.png" alt="A"><img src="b.png"><img src="c.png" alt="">`
	validateImageAltText(logger, "testpage", html)

	logs := logBuf.String()
	if strings.Count(logs, "img missing alt attribute") != 1 {
		t.Errorf("expected exactly one warning (for b.png), got logs: %s", logs)
	}
	if !strings.Contains(logs, "b.png") {
		t.Errorf("expected the warning to identify b.png, got logs: %s", logs)
	}
	if strings.Contains(logs, "a.png") || strings.Contains(logs, "c.png") {
		t.Errorf("did not expect a.png or c.png to be flagged, got logs: %s", logs)
	}
}

func TestValidateImageAltText_NoImagesIsNoOp(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	validateImageAltText(logger, "testpage", `<html><head></head><body><p>no images</p></body></html>`)

	if logBuf.Len() != 0 {
		t.Errorf("expected no warnings when the page has no <img> tags, got logs: %s", logBuf.String())
	}
}

// ── Run integration: warning is non-fatal ─────────────────────────────────────

func TestRun_MissingAltWarnsButDoesNotFailBuild(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<img src="/images/missing-alt.png">{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, logger); err != nil {
		t.Fatalf("expected build to succeed despite the missing alt attribute, got: %v", err)
	}
	if !strings.Contains(logBuf.String(), "img missing alt attribute") {
		t.Errorf("expected a logged warning about the missing alt, got logs: %s", logBuf.String())
	}
}

func TestRun_SVGIconMissingAltIsCaughtBeforeInlining(t *testing.T) {
	// SVG icons under svgInlineSizeLimit get inlined into <svg> and lose their
	// <img> tag entirely — validateImageAltText must run on the pre-transform
	// HTML or it would never see these.
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	websiteRoot := newTestWebsiteRoot(t)
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "images"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "assets", "images", "icon.svg"):         smallSVG,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<img src="/images/icon.svg">{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, logger); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	html := string(mustReadFile(t, filepath.Join(outDir, fileAgendaHTML)))
	if strings.Contains(html, "<img") {
		t.Fatalf("expected the small SVG icon to be inlined (no <img> left), got: %s", html)
	}
	if !strings.Contains(logBuf.String(), "img missing alt attribute") {
		t.Errorf("expected the missing alt on the SVG icon to still be warned about, got logs: %s", logBuf.String())
	}
}
