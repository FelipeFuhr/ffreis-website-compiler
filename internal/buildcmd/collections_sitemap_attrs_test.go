package buildcmd

import (
	"testing"

	"ffreis-website-compiler/internal/collections"
	"ffreis-website-compiler/internal/sitemap"
)

// SitemapAttrs mirrors sitemap.URLItem field-for-field on purpose, and
// lastmod_from is the field the decision record calls out as the genuinely new
// capability (§3.3) — it is what lets a collection replace hand-written
// sitemap.yaml entries whose lastmod comes from a template file's mtime.
// Dropping it here would silently emit entries with no <lastmod> at all.
func TestApplySitemapAttrsPropagatesLastmodFrom(t *testing.T) {
	urls := []sitemap.URLItem{{Path: "/ssyb/"}, {Path: "/ssgb/"}}
	attrs := collections.SitemapAttrs{
		Changefreq:  "monthly",
		Priority:    "0.8",
		LastmodFrom: "src/templates/pages/course.gohtml",
	}

	got := applySitemapAttrs(urls, attrs)

	for _, item := range got {
		if item.Changefreq != "monthly" || item.Priority != "0.8" {
			t.Errorf("%s: changefreq/priority not applied: %+v", item.Path, item)
		}
		if item.LastmodFrom != "src/templates/pages/course.gohtml" {
			t.Errorf("%s: lastmod_from dropped: %+v", item.Path, item)
		}
	}
}

func TestApplySitemapAttrsEmptyAttrsLeavesItemsAlone(t *testing.T) {
	urls := []sitemap.URLItem{{Path: "/projects/"}}
	got := applySitemapAttrs(urls, collections.SitemapAttrs{})
	if got[0] != (sitemap.URLItem{Path: "/projects/"}) {
		t.Errorf("empty attrs should leave the item untouched, got %+v", got[0])
	}
}
