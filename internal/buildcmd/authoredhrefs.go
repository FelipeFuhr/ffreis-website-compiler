package buildcmd

import (
	"regexp"
	"strings"
)

// Base-path normalisation for root-absolute hrefs authored in site data.
//
// The compiler prepends basePath to the links it generates itself, and to every
// asset reference (fingerprint.go). An href typed into site data is passed
// through verbatim — so on a deployment at /en, a data author who wrote
// `/blog/` ships a link that 302s to the language root instead of reaching the
// page, and `/about/` likewise. Worse, the link checker skips exactly those
// hrefs as "another deployment's concern" (linkcheck.stripBasePath), so the
// build is GREEN while shipping them.
//
// Measured on the real fleet at v1.0.1: `https://ffreis.com/en/blog/` 404s and
// `https://ffreis.com/about/` 302s, both from data-authored hrefs; the EN build
// was green while the PT build of the SAME page failed, because the two
// languages' data had drifted to opposite conventions in the same file. The
// gate therefore penalised whichever author wrote the more correct href.
//
// Normalising here fixes both halves at once: the link is right in the shipped
// page, and because it now sits under basePath the checker stops skipping it,
// so a link into a section that genuinely does not exist becomes a real build
// failure instead of a silent 404.
//
// This can only improve a shipped page. An unprefixed root-absolute href on a
// prefixed deployment is already wrong in production — there is no deployment
// in which it resolves.

// navTagRE matches the start tags whose links are NAVIGATIONAL — a destination
// a person follows, not a file the page loads.
//
// The distinction is load-bearing. `<link rel="stylesheet" href="/css/app.css">`
// and `<img src>` are asset references, and fingerprintLocalAssets already
// prepends basePath to those; rewriting them here too would produce
// "/en/en/css/app.css". So this matches tags, not bare attributes, and only
// these three.
var navTagRE = regexp.MustCompile(`(?is)<(a|area|form)(\s[^>]*)>`)

// navHrefAttrRE picks the destination attribute out of a matched nav tag.
var navHrefAttrRE = regexp.MustCompile(`(?i)\b(href|action)\s*=\s*"(/[^"]*)"`)

// normaliseAuthoredHrefs prepends basePath to every root-absolute href that
// does not already start with it, with two exceptions that must not be
// rewritten:
//
//   - a sibling deployment's prefix (`/pt/...` while building `/en`), which is
//     a deliberate cross-language link;
//   - a protocol-relative URL (`//cdn.example/x`), which is external despite
//     starting with a slash.
//
// basePath "" (the root deployment) is a no-op: there is nothing to prepend,
// and every root-absolute href is already correct.
func normaliseAuthoredHrefs(html, basePath string, siblingBasePaths []string) string {
	base := strings.TrimRight(basePath, "/")
	if base == "" {
		return html
	}
	siblings := normalisedSiblingPrefixes(siblingBasePaths)

	return navTagRE.ReplaceAllStringFunc(html, func(tag string) string {
		return navHrefAttrRE.ReplaceAllStringFunc(tag, func(attrMatch string) string {
			groups := navHrefAttrRE.FindStringSubmatch(attrMatch)
			if groups == nil {
				return attrMatch
			}
			attr, href := groups[1], groups[2]
			normalised := normaliseOneHref(href, base, siblings)
			if normalised == href {
				return attrMatch
			}
			return attr + `="` + normalised + `"`
		})
	})
}

func normaliseOneHref(href, base string, siblings []string) string {
	if strings.HasPrefix(href, "//") {
		return href // protocol-relative: external
	}
	if href == base || strings.HasPrefix(href, base+"/") {
		return href // already this deployment's namespace
	}
	for _, s := range siblings {
		if href == s || strings.HasPrefix(href, s+"/") {
			return href // deliberate cross-deployment link
		}
	}
	if href == "/" {
		return base + "/"
	}
	return base + href
}

// normalisedSiblingPrefixes turns the -sibling-base-paths values ("en", "/jp",
// "pt/") into leading-slash, no-trailing-slash prefixes. The flag is documented
// as accepting bare names, and a prefix compared in the wrong shape would
// silently fail to match — rewriting a cross-language link into
// "/en/pt/about/".
func normalisedSiblingPrefixes(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		s = strings.Trim(s, "/")
		if s == "" {
			continue
		}
		out = append(out, "/"+s)
	}
	return out
}
