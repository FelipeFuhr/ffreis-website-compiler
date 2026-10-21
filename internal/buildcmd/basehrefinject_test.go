package buildcmd

import (
	"strings"
	"testing"
)

func TestBaseHrefForPage(t *testing.T) {
	cases := []struct{ basePath, slug, want string }{
		{"/en", "login", "/en/login"},
		{"/en", "index", "/en/"},
		{"", "login", "/login"},
		{"", "index", "/"},
		{"/en/", "login", "/en/login"}, // trailing slash on basePath is tolerated
	}
	for _, tc := range cases {
		if got := baseHrefForPage(tc.basePath, tc.slug); got != tc.want {
			t.Errorf("baseHrefForPage(%q, %q) = %q, want %q", tc.basePath, tc.slug, got, tc.want)
		}
	}
}

// The injected value must satisfy validatePageBaseHref, which runs immediately
// after injection. If these two ever disagree the build fails on a tag the
// compiler wrote itself.
func TestInjectedBaseHrefSatisfiesItsOwnValidator(t *testing.T) {
	for _, tc := range []struct{ basePath, slug string }{
		{"/en", "login"}, {"/en", "index"}, {"", "about"}, {"", "index"}, {"/pt", "contato"},
	} {
		html := injectBaseHref(`<html><head><title>x</title></head><body></body></html>`, tc.basePath, tc.slug)
		if err := validatePageBaseHref(html, tc.basePath, tc.slug, tc.slug); err != nil {
			t.Errorf("basePath=%q slug=%q: injected tag rejected by validator: %v", tc.basePath, tc.slug, err)
		}
	}
}

func TestInjectBaseHrefPlacesItFirstInHead(t *testing.T) {
	// Placement is the whole point: a <base> after the stylesheet link would not
	// apply to it, so the relative href this exists to fix would still break.
	html := `<html><head><link rel="stylesheet" href="css/app.css"><script src="js/app.js"></script></head><body></body></html>`
	got := injectBaseHref(html, "/en", "login")

	baseIdx := strings.Index(got, "<base href=")
	linkIdx := strings.Index(got, "<link rel=")
	scriptIdx := strings.Index(got, "<script src=")
	if baseIdx == -1 {
		t.Fatalf("no <base> injected: %s", got)
	}
	if baseIdx > linkIdx || baseIdx > scriptIdx {
		t.Errorf("<base> must precede every relative reference; base=%d link=%d script=%d\n%s",
			baseIdx, linkIdx, scriptIdx, got)
	}
}

func TestInjectBaseHrefLeavesAnAuthoredBaseAlone(t *testing.T) {
	// flemming declares its own; overwriting it would fight the template author
	// and could contradict validatePageBaseHref's check of their value.
	html := `<html><head><base href="/en/treinamentos"><title>x</title></head><body></body></html>`
	if got := injectBaseHref(html, "/en", "login"); got != html {
		t.Errorf("author-declared <base> must be untouched:\n%s", got)
	}
	if strings.Count(injectBaseHref(html, "/en", "login"), "<base") != 1 {
		t.Error("must never add a second <base>")
	}
}

func TestInjectBaseHrefNoHeadIsNoOp(t *testing.T) {
	// A fragment page is a bare snippet with no <head> to anchor to.
	html := `<div class="widget">standalone</div>`
	if got := injectBaseHref(html, "/en", "widget"); got != html {
		t.Errorf("a document with no <head> must be untouched, got: %s", got)
	}
}

func TestInjectBaseHrefHandlesHeadWithAttributesAndCase(t *testing.T) {
	html := `<HTML><HEAD lang="en"><title>x</title></HEAD><body></body></HTML>`
	got := injectBaseHref(html, "/en", "login")
	if !strings.Contains(got, `<base href="/en/login">`) {
		t.Errorf("uppercase <HEAD> with attributes must still match, got: %s", got)
	}
}

func TestInjectBaseHrefIsIdempotent(t *testing.T) {
	html := `<html><head><title>x</title></head><body></body></html>`
	once := injectBaseHref(html, "/en", "login")
	twice := injectBaseHref(once, "/en", "login")
	if once != twice {
		t.Errorf("second application must be a no-op:\n once: %s\ntwice: %s", once, twice)
	}
}
