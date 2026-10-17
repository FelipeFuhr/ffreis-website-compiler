package buildcmd

import (
	"strings"
	"testing"
)

func TestNormaliseAuthoredHrefs_PrependsBasePathToAnUnprefixedLink(t *testing.T) {
	// The exact production bug: ffreis-website-data authored `/blog/` on a
	// deployment at /en, which 302s to the language root instead of reaching
	// the page — and which the link checker skipped, so the build was green.
	got := normaliseAuthoredHrefs(`<a href="/blog/">Blog</a>`, "/en", nil)
	if got != `<a href="/en/blog/">Blog</a>` {
		t.Fatalf("got %s", got)
	}
}

func TestNormaliseAuthoredHrefs_LeavesAnAlreadyPrefixedLinkAlone(t *testing.T) {
	// Double-prefixing is the failure mode that would break every
	// correctly-authored link in the fleet, so it gets its own test.
	for _, href := range []string{"/en", "/en/", "/en/about/"} {
		in := `<a href="` + href + `">x</a>`
		if got := normaliseAuthoredHrefs(in, "/en", nil); got != in {
			t.Errorf("href %q was rewritten to %s", href, got)
		}
	}
}

func TestNormaliseAuthoredHrefs_LeavesASiblingDeploymentLinkAlone(t *testing.T) {
	// The language switcher's whole job. Rewriting these would turn a
	// cross-language link into "/en/pt/about/".
	in := `<a href="/pt/about/">PT</a><a href="/jp/">JP</a>`
	if got := normaliseAuthoredHrefs(in, "/en", []string{"pt", "jp"}); got != in {
		t.Fatalf("sibling link rewritten: %s", got)
	}
}

func TestNormaliseAuthoredHrefs_AcceptsSiblingPrefixesInEveryDocumentedShape(t *testing.T) {
	// -sibling-base-paths is documented as taking bare names ("en,jp"), but a
	// caller may pass "/pt" or "pt/". A prefix compared in the wrong shape would
	// silently fail to match and the link would be rewritten — the worst kind of
	// failure here, because it looks like it worked.
	in := `<a href="/pt/about/">PT</a>`
	for _, spelling := range [][]string{{"pt"}, {"/pt"}, {"pt/"}, {"/pt/"}, {" pt "}} {
		if got := normaliseAuthoredHrefs(in, "/en", spelling); got != in {
			t.Errorf("sibling spelled %v was not recognised: %s", spelling, got)
		}
	}
}

func TestNormaliseAuthoredHrefs_RootDeploymentIsANoOp(t *testing.T) {
	in := `<a href="/blog/">Blog</a>`
	if got := normaliseAuthoredHrefs(in, "", []string{"en"}); got != in {
		t.Fatalf("root deployment rewrote a link: %s", got)
	}
}

func TestNormaliseAuthoredHrefs_DoesNotTouchAssetReferences(t *testing.T) {
	// fingerprintLocalAssets prepends basePath to these itself, and runs after
	// this transform. Rewriting them here produces "/en/en/css/app.css" — which
	// is why this matches nav TAGS rather than bare href attributes.
	in := `<link rel="stylesheet" href="/css/app.css">` +
		`<link rel="icon" href="/favicon.ico">` +
		`<img src="/img/hero.webp" alt="h">`
	if got := normaliseAuthoredHrefs(in, "/en", nil); got != in {
		t.Fatalf("an asset reference was rewritten: %s", got)
	}
}

func TestNormaliseAuthoredHrefs_SkipsNonInternalDestinations(t *testing.T) {
	in := `<a href="https://example.com/x">abs</a>` +
		`<a href="//cdn.example/x">proto-relative</a>` +
		`<a href="about/">relative</a>` +
		`<a href="#top">fragment</a>` +
		`<a href="mailto:a@b.c">mail</a>`
	if got := normaliseAuthoredHrefs(in, "/en", nil); got != in {
		t.Fatalf("a non-internal destination was rewritten: %s", got)
	}
}

func TestNormaliseAuthoredHrefs_HandlesFormActionAndArea(t *testing.T) {
	got := normaliseAuthoredHrefs(
		`<form action="/search/"><input name="q"></form><area href="/map/" alt="m">`, "/en", nil)
	for _, want := range []string{`action="/en/search/"`, `href="/en/map/"`} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %s in %s", want, got)
		}
	}
}

func TestNormaliseAuthoredHrefs_BareRootBecomesTheDeploymentRoot(t *testing.T) {
	// "/" on a /en deployment means "the site root", which from inside /en is
	// /en/ — not the distribution root, which 302s.
	got := normaliseAuthoredHrefs(`<a href="/">Home</a>`, "/en", nil)
	if got != `<a href="/en/">Home</a>` {
		t.Fatalf("got %s", got)
	}
}

func TestNormaliseAuthoredHrefs_PreservesQueryAndFragment(t *testing.T) {
	got := normaliseAuthoredHrefs(`<a href="/search/?q=a#r">s</a>`, "/en", nil)
	if got != `<a href="/en/search/?q=a#r">s</a>` {
		t.Fatalf("got %s", got)
	}
}

func TestNormaliseAuthoredHrefs_PreservesOtherAttributesOnTheTag(t *testing.T) {
	got := normaliseAuthoredHrefs(
		`<a class="cta" href="/blog/" data-x="1" aria-current="page">B</a>`, "/en", nil)
	for _, want := range []string{`class="cta"`, `href="/en/blog/"`, `data-x="1"`, `aria-current="page"`} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %s in %s", want, got)
		}
	}
}

func TestNormaliseAuthoredHrefs_IsIdempotent(t *testing.T) {
	// The transform may be reached twice on a page that goes through more than
	// one pass; applying it to its own output must change nothing.
	once := normaliseAuthoredHrefs(`<a href="/blog/">B</a><a href="/pt/x/">P</a>`, "/en", []string{"pt"})
	twice := normaliseAuthoredHrefs(once, "/en", []string{"pt"})
	if once != twice {
		t.Fatalf("not idempotent:\n once: %s\ntwice: %s", once, twice)
	}
}
