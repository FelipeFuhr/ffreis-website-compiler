package conventions

import (
	"path/filepath"
	"regexp"
	"strings"
)

const ruleNameNoHTMLExtensionURLs = "no-html-extension-urls"

// navDestinationAttr returns the attribute carrying a navigational destination
// for the given tag name, and whether the tag has one at all.
//
// Only these three: a link a person follows. `<link rel="stylesheet">` and
// `<img src>` are asset references the compiler fingerprints, and they are
// never page URLs.
func navDestinationAttr(tagName string) (string, bool) {
	switch tagName {
	case "a", "area":
		return "href", true
	case "form":
		return "action", true
	default:
		return "", false
	}
}

var ruleNoHTMLExtensionURLs = Rule{
	Name:   ruleNameNoHTMLExtensionURLs,
	Domain: DomainOutput,
	Doc: "No navigational link points at a `.html` URL. Pages are emitted as " +
		"`<name>/index.html` and served extension-free, so `login.html` names a file " +
		"that does not exist — and the CDN 301-redirects the extension form away, so " +
		"even where it resolves it costs every visitor an extra round trip. The " +
		"compiler rewrites authored `.html` hrefs automatically (buildcmd's " +
		"normaliseCleanURLHrefs), which means a finding here is a link it could not " +
		"reach: a `.html` inside a QUERY VALUE (`/login?next=account.html`), where " +
		"rewriting would mean guessing what the parameter means, or a single-quoted " +
		"attribute. Both are real breakage — the query form silently sends a " +
		"post-login redirect to a 404. Checks the built output, so it sees the final " +
		"URL rather than what a template typed. Also covers `sitemap.xml`, where a " +
		"stale `.html` URL is worst: a crawler reads it as authoritative. A committed " +
		"sitemap overrides the generated one, which is how forma's shipped 60 .html " +
		"URLs while its pages were already extension-free.",
	Selects: selectsHTMLOrSitemap,
	Scan: func(rel, contents string) []Finding {
		if isSitemapFile(rel) {
			return scanSitemapForHTMLExtensions(rel, contents)
		}

		var findings []Finding
		seen := map[string]bool{}

		for _, t := range tokenizeHTML(contents) {
			if t.Closing {
				continue
			}
			attr, ok := navDestinationAttr(t.Name)
			if !ok {
				continue
			}
			value := t.AttrValue(attr)
			if value == "" || isExternalHref(value) || strings.HasPrefix(value, "#") {
				continue
			}
			// A non-navigational scheme is not a page URL; `mailto:` cannot
			// carry one and `tel:` has no path.
			lower := strings.ToLower(value)
			if strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "tel:") ||
				strings.HasPrefix(lower, "sms:") || strings.HasPrefix(lower, "javascript:") {
				continue
			}
			if !strings.Contains(lower, ".html") {
				continue
			}
			if seen[value] {
				continue // one finding per distinct destination, not per occurrence
			}
			seen[value] = true

			findings = append(findings, Finding{
				Rule: ruleNameNoHTMLExtensionURLs, File: rel, Line: t.Line,
				Detail: "<" + t.Name + " " + attr + "=\"" + value + "\"> points at a .html URL. " +
					"Pages are served extension-free, so drop the extension — and if the " +
					".html sits in a query value, the value is a page reference the " +
					"compiler cannot rewrite for you: fix it at the source that writes it",
			})
		}
		return findings
	},
}

// selectsHTMLOrSitemap picks the built pages AND the sitemap. A sitemap is the
// one non-HTML file in the output that is made entirely of page URLs, so the
// same rule governs it — and it is the file where a stale `.html` URL does the
// most damage, because a crawler reads it as authoritative and spends the
// site's crawl budget following redirects.
func selectsHTMLOrSitemap(rel string) bool {
	return strings.EqualFold(filepath.Ext(rel), ".html") || isSitemapFile(rel)
}

func isSitemapFile(rel string) bool {
	return strings.EqualFold(filepath.Base(rel), "sitemap.xml")
}

// sitemapLocRE pulls the URL out of each <loc> entry. A tag tokenizer would be
// overkill: a sitemap is machine-generated XML with one URL per <loc>, and this
// rule only needs to know whether that URL carries an extension.
var sitemapLocRE = regexp.MustCompile(`(?is)<loc>\s*([^<\s]+)\s*</loc>`)

// scanSitemapForHTMLExtensions reports <loc> entries ending in .html.
//
// Found live on ffreis-forma: the compiler generates an extension-free sitemap,
// but forma ships a COMMITTED src/assets/sitemap.xml, and the well-known-asset
// copy puts that over the generated one byte for byte. So the build was green,
// the pages were extension-free, and the sitemap still advertised 25 .html URLs
// — a hand-maintained file that had silently become wrong. Nothing else in the
// pipeline looks at it.
//
// The fix is to EDIT such a file, not delete it. forma's is committed on purpose:
// its Makefile records that the generated sitemap has no robots:noindex
// awareness, so falling back on generation would publish account, cart, checkout
// and login URLs. An earlier version of this rule advised deleting it, which
// would have traded a redirect for an indexing leak.
func scanSitemapForHTMLExtensions(rel, contents string) []Finding {
	var findings []Finding
	for _, m := range sitemapLocRE.FindAllStringSubmatchIndex(contents, -1) {
		loc := contents[m[2]:m[3]]
		trimmed := loc
		if i := strings.IndexAny(trimmed, "?#"); i != -1 {
			trimmed = trimmed[:i]
		}
		if !strings.HasSuffix(strings.ToLower(trimmed), ".html") {
			continue
		}
		// index.html at a directory root is still a URL a crawler should not be
		// given — the canonical form is the directory.
		findings = append(findings, Finding{
			Rule: ruleNameNoHTMLExtensionURLs, File: rel,
			Line: 1 + strings.Count(contents[:m[0]], "\n"),
			Detail: "<loc>" + loc + "</loc> advertises a .html URL. Pages are served " +
				"extension-free and the CDN 301-redirects the extension form, so every " +
				"crawler following this spends a redirect to reach the same page. Drop the " +
				"extension. If the sitemap is committed rather than generated, edit it in " +
				"place -- do NOT delete it to fall back on generation, which has no " +
				"robots:noindex awareness and would publish account, cart, checkout and " +
				"login URLs",
		})
	}
	return findings
}
