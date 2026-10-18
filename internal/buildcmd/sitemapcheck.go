package buildcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// A committed sitemap.xml is copied verbatim into the output. When the build
// has a base path, that verbatim copy is almost always WRONG, and wrong in a
// way nothing else catches.
//
// Measured on the live fleet, 2026-10-18: forma.ffreis.com's `/en/` deployment
// serves a sitemap whose every <loc> is unprefixed —
// `https://forma.ffreis.com/brochures.html` — while the pages it names live at
// `/en/brochures.html`. Twenty-six URLs, all pointing somewhere the deployment
// does not serve, on a site whose own pages carry correct `/en/` canonicals. So
// the sitemap and the canonicals in the SAME build contradict each other, and a
// crawler is handed the losing side.
//
// Nothing caught it because every existing check is scoped to one file:
// linkcheck validates hrefs inside pages, the robots check reads robots.txt,
// and the sitemap is simply copied. ffreis-forma had noticed and worked around
// it with a `reprefix_sitemap` sed in its own Makefile — which the deployer
// never runs, so the workaround held locally and the real deploy shipped the
// unprefixed file. A per-site workaround for a compiler-wide gap is exactly the
// shape this fleet keeps paying for.

// sitemapLocRE extracts the URLs a sitemap advertises.
var sitemapLocRE = regexp.MustCompile(`(?is)<loc>\s*([^<\s]+)\s*</loc>`)

// maxReportedLocs caps how many offending URLs the error lists before
// summarising. The fix is one for all of them; the list is orientation.
const maxReportedLocs = 5

// checkSitemapBasePath fails the build when a committed sitemap.xml names URLs
// that this deployment does not serve.
//
// It only runs when there IS a base path: at the distribution root every
// root-absolute URL is correct by construction, and the compiler's own
// generated sitemap (-sitemap-base-url) already includes the prefix.
func checkSitemapBasePath(assetsDir string, opts buildOptions) error {
	base := strings.Trim(opts.basePath, "/")
	if base == "" {
		return nil
	}

	path := filepath.Join(assetsDir, sitemapXML)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no committed sitemap: the generated one is already correct
		}
		return fmt.Errorf("reading %s for base-path check: %w", path, err)
	}

	bad := locsMissingBasePath(string(data), base, hostOf(opts.sitemapBaseURL))
	if len(bad) == 0 {
		return nil
	}

	shown := bad
	suffix := ""
	if len(shown) > maxReportedLocs {
		shown = shown[:maxReportedLocs]
		suffix = fmt.Sprintf("\n  … and %d more", len(bad)-maxReportedLocs)
	}
	return fmt.Errorf(
		"%s advertises %d URL(s) that this deployment does not serve: the build has "+
			"base path %q, so its pages are published under /%s/, but these <loc> entries "+
			"are not:\n  %s%s\n\nA crawler following them gets a 404 while the pages' own "+
			"canonicals point at /%s/ — the sitemap and the canonicals in this same build "+
			"disagree. Either prefix the committed sitemap's URLs for this deployment, or "+
			"delete it and let -sitemap-base-url generate one, which is prefixed correctly "+
			"by construction",
		sitemapXML, len(bad), opts.basePath, base,
		strings.Join(shown, "\n  "), suffix, base)
}

// locsMissingBasePath returns the sitemap URLs whose path does not start with
// the deployment's base path.
//
// ownHost, when known, scopes the check to the site's own property: a sitemap
// may legitimately reference a sibling site, and failing a build over that
// would make this something sites switch off. It is derived from
// -sitemap-base-url, which is not always passed — when it is empty every
// absolute and root-absolute <loc> is judged, which is the strict reading and
// the right default for a check that fails the build.
func locsMissingBasePath(sitemap, base, ownHost string) []string {
	seen := map[string]bool{}
	var bad []string
	for _, m := range sitemapLocRE.FindAllStringSubmatch(sitemap, -1) {
		loc := strings.TrimSpace(m[1])
		p, ok := sitemapLocPath(loc)
		if !ok {
			continue
		}
		if ownHost != "" && hostOf(loc) != "" && hostOf(loc) != ownHost {
			continue // another property: not this deployment's concern
		}
		if p == "/"+base || strings.HasPrefix(p, "/"+base+"/") {
			continue
		}
		if !seen[loc] {
			seen[loc] = true
			bad = append(bad, loc)
		}
	}
	sort.Strings(bad)
	return bad
}

// sitemapLocPath returns the path component of a <loc>, and whether it is one
// this check judges at all.
func sitemapLocPath(loc string) (string, bool) {
	lower := strings.ToLower(loc)
	switch {
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		rest := loc[strings.Index(loc, "//")+2:]
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			return "/", true // bare origin, no path: cannot be under the base path
		}
		return rest[slash:], true
	case strings.HasPrefix(loc, "/"):
		return loc, true
	}
	// Relative or scheme-less: not something a sitemap should contain, and not
	// this check's business to adjudicate.
	return "", false
}

// hostOf returns the lowercased host of an absolute URL, or "" when there is
// none (a root-absolute path, or an empty value).
func hostOf(u string) string {
	lower := strings.ToLower(u)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return ""
	}
	rest := lower[strings.Index(lower, "//")+2:]
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		rest = rest[:slash]
	}
	return rest
}
