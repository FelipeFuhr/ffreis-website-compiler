package buildcmd

import (
	"testing"

	"ffreis-website-compiler/internal/sitegen"
	"ffreis-website-compiler/internal/sitemap"
)

func TestSectionEnabled_DefaultsToEnabledWhenAbsent(t *testing.T) {
	if !sectionEnabled(map[string]any{}, "blog") {
		t.Fatal("section with no sections map should default to enabled")
	}
	sd := map[string]any{"sections": map[string]any{"courses": true}}
	if !sectionEnabled(sd, "blog") {
		t.Fatal("section absent from sections map should default to enabled")
	}
}

func TestSectionEnabled_RespectsFalse(t *testing.T) {
	sd := map[string]any{"sections": map[string]any{"blog": false, "courses": true}}
	if sectionEnabled(sd, "blog") {
		t.Fatal("blog explicitly false must be disabled")
	}
	if !sectionEnabled(sd, "courses") {
		t.Fatal("courses explicitly true must be enabled")
	}
}

func TestFilterInternalPages_DropsDisabledSectionPages(t *testing.T) {
	pages := []sitegen.PageTemplate{
		{Name: "index"}, {Name: "about"},
		{Name: "blog"}, {Name: "post"},
		{Name: "courses"}, {Name: "projects"},
		{Name: "listings"}, {Name: "listing"},
	}
	sd := map[string]any{"sections": map[string]any{"blog": false, "courses": false, "projects": true, "listings": false}}

	got := filterInternalPages(pages, sd)
	names := map[string]bool{}
	for _, p := range got {
		names[p.Name] = true
	}
	for _, dropped := range []string{"blog", "post", "courses", "listings", "listing"} {
		if names[dropped] {
			t.Errorf("disabled-section page %q should have been dropped", dropped)
		}
	}
	for _, kept := range []string{"index", "about", "projects"} {
		if !names[kept] {
			t.Errorf("page %q should have been kept", kept)
		}
	}
}

func TestFilterDisabledSectionURLs_DropsMatchingPaths(t *testing.T) {
	urls := []sitemap.URLItem{
		{Path: "/"}, {Path: "/about/"},
		{Path: "/blog/"}, {Path: "/courses/"}, {Path: "/projects/"}, {Path: "/listings/"},
	}
	sd := map[string]any{"sections": map[string]any{"blog": false, "courses": false, "projects": true, "listings": false}}

	got := filterDisabledSectionURLs(urls, sd)
	kept := map[string]bool{}
	for _, u := range got {
		kept[u.Path] = true
	}
	if kept["/blog/"] || kept["/courses/"] || kept["/listings/"] {
		t.Error("disabled-section sitemap paths should be dropped")
	}
	for _, want := range []string{"/", "/about/", "/projects/"} {
		if !kept[want] {
			t.Errorf("path %q should have been kept", want)
		}
	}
}

func TestSectionForPath(t *testing.T) {
	cases := map[string]string{
		"/blog/": "blog", "/blog/a-post/": "blog", "/pt/blog/": "blog", "/en/courses/": "courses",
		"/courses/": "courses", "/projects/": "projects", "/pt/projects/": "projects",
		"/listings/": "listings", "/listings/some-id/": "listings", "/pt/listings/": "listings",
		"/": "", "/about/": "", "/blogging/": "", "/pt/about/": "", "/listing/": "",
	}
	for path, want := range cases {
		if got := sectionForPath(path); got != want {
			t.Errorf("sectionForPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestPageSection(t *testing.T) {
	cases := map[string]string{
		"blog": "blog", "post": "blog",
		"projects": "projects",
		"courses":  "courses",
		"listings": "listings", "listing": "listings",
		"index": "", "about": "",
	}
	for name, want := range cases {
		if got := pageSection(name); got != want {
			t.Errorf("pageSection(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSectionNames_MatchesTable(t *testing.T) {
	got := sectionNames()
	if len(got) != len(sectionTable) {
		t.Fatalf("sectionNames() returned %d names, want %d (one per table entry)", len(got), len(sectionTable))
	}
	for i, s := range sectionTable {
		if got[i] != s.name {
			t.Errorf("sectionNames()[%d] = %q, want %q", i, got[i], s.name)
		}
	}
}

func TestPruneDisabledSectionData_ClearsStaleListingsCarouselData(t *testing.T) {
	sd := map[string]any{
		"listings": []any{map[string]any{"id": "stale", "title": "Stale"}},
	}
	pruneDisabledSectionData(sd, []string{"listings"})

	got, ok := sd["listings"].([]any)
	if !ok {
		t.Fatalf("expected siteData[\"listings\"] to remain a []any, got %T", sd["listings"])
	}
	if len(got) != 0 {
		t.Errorf("expected stale listings carousel data to be cleared, got %v", got)
	}
}

func TestPruneDisabledSectionData_LeavesListingsUntouchedWhenNotDisabled(t *testing.T) {
	sd := map[string]any{
		"listings": []any{map[string]any{"id": "kept", "title": "Kept"}},
	}
	pruneDisabledSectionData(sd, []string{"blog"})

	got, ok := sd["listings"].([]any)
	if !ok || len(got) != 1 {
		t.Errorf("expected listings data to be untouched when listings is not disabled, got %v", sd["listings"])
	}
}
