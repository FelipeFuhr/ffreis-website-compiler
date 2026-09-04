package buildcmd

import "strings"

// section describes one gate-able content section: its name (used as a
// -disable-sections value, the sections.<name> site-data key sectionEnabled
// reads, and the URL path segment sectionForPath matches against), the
// page-template names it owns, and an optional prune hook that clears
// section-specific data pruneDisabledSectionData must scrub from siteData
// when the section is off — data a template can render unconditionally,
// independent of whether the compiler injected it this build (e.g. a
// home-page carousel fed directly from raw site data that predates the
// compiler-managed content-loading path).
//
// sectionTable is the single source of truth every section-gating mechanism
// in this package resolves against: pageSection, sectionForPath, the
// -disable-sections flag's help text (options.go), buildDevDataPayload, and
// pruneDisabledSectionData all derive from it. Adding a new gated section
// means adding one entry here.
//
// This table is orthogonal to sectionEnabled (siteinputs.go), which decides
// whether a named section is ON given sections.<name> in site data — this
// table only answers "which pages/paths belong to section X" and "what
// stale data needs clearing when X is off".
type section struct {
	name  string
	pages []string
	prune func(siteData map[string]any)
}

var sectionTable = []section{
	{
		name:  "blog",
		pages: []string{"blog", "post"},
		prune: func(siteData map[string]any) {
			// Home blog block + sitemap post list read pages.blog.posts.
			pages, _ := siteData["pages"].(map[string]any)
			if blog, ok := pages["blog"].(map[string]any); ok {
				blog["posts"] = []any{}
			}
		},
	},
	{
		name:  "projects",
		pages: []string{"projects"},
	},
	{
		name:  "courses",
		pages: []string{"courses"},
		prune: func(siteData map[string]any) {
			// Home courses block gates on pages.index.courses_section.
			pages, _ := siteData["pages"].(map[string]any)
			if idx, ok := pages["index"].(map[string]any); ok {
				delete(idx, "courses_section")
			}
		},
	},
	{
		name:  "listings",
		pages: []string{"listings", "listing"},
		prune: func(siteData map[string]any) {
			// siteData["listings"] feeds both the hub page grid and any other
			// page's carousel/preview block (e.g. home); it can be populated
			// directly from raw site data independent of -listings-file, so it
			// must be cleared explicitly rather than relying on the loader
			// simply never running.
			siteData["listings"] = []any{}
		},
	},
}

// sectionNames returns every section name in the table, in table order —
// used to build the -disable-sections flag's help text so it can't drift from
// what pageSection/sectionForPath actually recognize.
func sectionNames() []string {
	names := make([]string, len(sectionTable))
	for i, s := range sectionTable {
		names[i] = s.name
	}
	return names
}

// pageSection maps a page-template name to the content section that gates it,
// or "" when the page is not section-gated. Used to drop a whole section's
// pages (hub/listing + detail) when that section is disabled.
func pageSection(pageName string) string {
	for _, s := range sectionTable {
		for _, p := range s.pages {
			if p == pageName {
				return s.name
			}
		}
	}
	return ""
}

// sectionForPath maps a URL path to the content section that gates it, or ""
// when the path is not section-gated. Segment-based so it matches whether or
// not the path carries a language base prefix: "/blog/", "/pt/blog/",
// "/blog/x/" → blog.
func sectionForPath(path string) string {
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		for _, s := range sectionTable {
			if seg == s.name {
				return s.name
			}
		}
	}
	return ""
}
