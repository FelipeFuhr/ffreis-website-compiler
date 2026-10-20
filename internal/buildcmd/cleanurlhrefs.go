package buildcmd

import (
	"path"
	"regexp"
	"strings"
)

// Clean-URL normalisation for navigational hrefs authored with a .html suffix.
//
// A page named `login` is written to `login/index.html` and
// served at `/login`. An href typed as `login.html` then points at a file that
// no longer exists — and a RELATIVE href breaks twice over, because the page's
// own URL gained a directory level: from a page now at `/en/404/`, the href
// `account.html` resolves to `/en/404/account.html`, not to the sibling page it
// meant.
//
// Measured on ffreis-forma at v1.2.1, before this transform existed: building it
// with extension-free output produced 2629 broken internal links from 163
// distinct authored href values (108 relative, 55 root-absolute). linkcheck
// caught every one, so the build failed loudly rather than shipping them — but
// that is precisely what made extension-free URLs un-adoptable for a site whose
// authors wrote the flat form, and the fleet carried a per-site
// `clean_urls: false` opt-out to work around it instead. Both the opt-out and the
// flag that backed it are now gone; this is what made removing them possible.
//
// Resolution is deterministic rather than a guess about intent. Under the old
// flat output a page named `<name>` was emitted at `<basePath>/<name>.html`, so a
// relative href on that page always resolved against `<basePath>/`. That is the
// base used here, which reproduces exactly what the author's link meant before
// the switch — it infers nothing from the link's text or position.

// navHrefTagRE matches the start tags whose links are NAVIGATIONAL — a
// destination a person follows, not a file the page loads.
//
// The distinction is load-bearing for the same reason it is in
// normaliseAuthoredHrefs: `<link rel="stylesheet" href="/css/app.css">` and
// `<img src>` are asset references owned by fingerprintLocalAssets. Assets never
// end in .html, so matching them would be a no-op today — but matching tags
// rather than bare attributes keeps that true if one ever does.
var navHrefTagRE = regexp.MustCompile(`(?is)<(a|area|form)(\s[^>]*)>`)

// navHrefValueRE picks the destination attribute out of a matched nav tag.
// Unlike normaliseAuthoredHrefs' equivalent this accepts any value, not only
// root-absolute ones, because a relative href is exactly the case that breaks.
var navHrefValueRE = regexp.MustCompile(`(?i)\b(href|action)\s*=\s*"([^"]*)"`)

// skippedHrefPrefixes are destinations this transform must never touch: another
// origin, or a non-navigational scheme. A protocol-relative URL is external
// despite starting with a slash.
var skippedHrefPrefixes = []string{
	"http://", "https://", "//",
	"mailto:", "tel:", "sms:", "javascript:", "data:",
}

// normaliseCleanURLHrefs rewrites navigational hrefs ending in .html to the
// extension-free form the build actually serves, resolving relative
// hrefs against the deployment root first.
//
// Sibling deployments are deliberately NOT skipped here, unlike in
// normaliseAuthoredHrefs. That function must skip them because PREFIXING
// `/pt/about/` while building `/en` would produce `/en/pt/about/`. Stripping an
// extension carries no such hazard: a sibling deployment is built by this same
// compiler and is therefore equally extension-free, so `/pt/about.html` →
// `/pt/about` is correct. Skipping them would leave every language-switcher
// link pointing at a file that does not exist.
func normaliseCleanURLHrefs(html, basePath string) string {
	base := "/" + strings.Trim(basePath, "/")

	return navHrefTagRE.ReplaceAllStringFunc(html, func(tag string) string {
		return navHrefValueRE.ReplaceAllStringFunc(tag, func(attrMatch string) string {
			groups := navHrefValueRE.FindStringSubmatch(attrMatch)
			if groups == nil {
				return attrMatch
			}
			attr, href := groups[1], groups[2]
			rewritten := cleanURLHref(href, base)
			if rewritten == href {
				return attrMatch
			}
			return attr + `="` + rewritten + `"`
		})
	})
}

// cleanURLHref converts one href value. It returns the input unchanged whenever
// the value is not a local navigational .html target, which is what makes the
// whole transform idempotent: its own output never ends in .html, so a second
// application cannot change anything.
func cleanURLHref(href, base string) string {
	if href == "" || strings.HasPrefix(href, "#") {
		return href
	}
	lower := strings.ToLower(href)
	for _, p := range skippedHrefPrefixes {
		if strings.HasPrefix(lower, p) {
			return href
		}
	}

	// Split the suffix off BEFORE testing the extension, so `about.html#team`
	// and `search.html?q=x` are handled rather than skipped — and so the suffix
	// survives verbatim on the way out.
	target, suffix := splitHrefSuffix(href)
	if !strings.HasSuffix(strings.ToLower(target), ".html") {
		return href
	}

	// Relative hrefs resolve against the deployment root, which is where this
	// page's own flat URL lived. Absolute ones are already rooted.
	abs := target
	if !strings.HasPrefix(abs, "/") {
		abs = base + "/" + abs
	}
	abs = path.Clean(abs)

	// `index.html` is not rewritten to an extension-free sibling: resolvePageTarget
	// keeps the index page at `<dir>/index.html`, so the
	// directory is its canonical URL and is what linkcheck registers.
	if path.Base(abs) == "index.html" {
		dir := path.Dir(abs)
		if dir == "/" {
			return "/" + suffix
		}
		return dir + "/" + suffix
	}

	return strings.TrimSuffix(abs, path.Ext(abs)) + suffix
}

// splitHrefSuffix separates a query string and/or fragment from the path part of
// an href, returning the path and the suffix to re-attach unchanged. A fragment
// may itself contain `?`, so the earliest delimiter wins.
func splitHrefSuffix(href string) (target, suffix string) {
	cut := len(href)
	if i := strings.IndexByte(href, '#'); i != -1 && i < cut {
		cut = i
	}
	if i := strings.IndexByte(href, '?'); i != -1 && i < cut {
		cut = i
	}
	return href[:cut], href[cut:]
}
