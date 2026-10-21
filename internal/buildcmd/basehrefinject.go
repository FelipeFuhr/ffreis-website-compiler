package buildcmd

import (
	"regexp"
	"strings"
)

// Automatic <base href> for pages served as a directory index.
//
// A page named `login` is written to `login/index.html` and served at
// `/login`, one directory deeper than the `/login.html` it used to be. Relative
// URLs resolve against the page's own directory, so every relative reference in
// the document silently moves with it:
//
//	served at /en/login.html   src="js/app.js" → /en/js/app.js       (correct)
//	served at /en/login        src="js/app.js" → /en/login/js/app.js (404)
//
// Found on ffreis-forma, which declares its per-page scripts as
// `<script src="js/{{.}}">`: EVERY script and stylesheet on EVERY sub-page
// would have 404'd. Nothing caught it — linkcheck only reads <a href>, and
// assetusage checks that a referenced asset EXISTS in the output tree, not that
// the URL resolves from the page's location. The build was green and the site
// would have shipped with no JS below the home page.
//
// `<base href>` restores the old resolution exactly: pointing it at the page's
// own served URL makes the browser resolve relative references against the
// directory ABOVE it — `<base href="/en/login">` has its last segment treated
// as a file, so `js/app.js` resolves to `/en/js/app.js`, which is where it is.
// That is the same invariant normaliseCleanURLHrefs uses for <a href>, applied
// to the references it deliberately does not touch.
//
// This is the pattern the fleet already proved: flemming-website has always set
// `<base href="{{$bp}}/{{pageSlug ...}}">` by hand in its own head partial, and
// is the one site whose relative assets survived the switch. Injecting it makes
// that work for every consumer instead of only the one that remembered.
//
// A page that already declares <base> is left alone: the author has taken
// responsibility for resolution, and validatePageBaseHref separately checks
// that their value matches where the page is served.

// headOpenTagRE matches the opening <head> tag, with or without attributes.
var headOpenTagRE = regexp.MustCompile(`(?is)<head(\s[^>]*)?>`)

// baseHrefForPage returns the <base href> value for a page, matching
// validatePageBaseHref's expectation exactly so an injected tag can never fail
// the validator that runs right after it.
func baseHrefForPage(basePath, slug string) string {
	base := strings.TrimRight(basePath, "/")
	if slug == "index" {
		return base + "/"
	}
	return base + "/" + slug
}

// injectBaseHref inserts a <base href> for the page's served URL when the
// document does not already declare one.
//
// It is inserted immediately after <head> opens, not before </head>: per the
// HTML spec a relative URL resolves against the document URL unless a PRECEDING
// <base> says otherwise, so a <base> appended at the end of <head> would not
// apply to the stylesheets and scripts declared above it — the exact references
// this exists to fix.
func injectBaseHref(html, basePath, slug string) string {
	if baseHrefTagRE.MatchString(html) {
		return html // author-declared; validatePageBaseHref checks it separately
	}
	loc := headOpenTagRE.FindStringIndex(html)
	if loc == nil {
		return html // no <head> (e.g. a fragment page); nothing to anchor to
	}
	tag := `<base href="` + baseHrefForPage(basePath, slug) + `">`
	return html[:loc[1]] + "\n" + tag + html[loc[1]:]
}
