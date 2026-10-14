// Package listings loads a site's own curated catalog/listing entries from a
// YAML file and converts them into the map shapes Go templates (and a site's
// own client-rendered JS, if it reads the same site-data key) expect.
//
// Unlike internal/courses and internal/projects, a listing's fields are not
// a fixed Go struct: a site's own schema passes through verbatim. The first
// consumer (casaboa-data) used real-estate-listing field names
// (transaction, place, locationLabel, sponsored, breakdown, …), and those
// aren't generic to every catalog a different site might need — a game
// catalog, a product list, a team roster all have their own field sets. Only
// "id" and "title" are required.
package listings

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// listingsFile is the on-disk shape: a top-level "listings:" key holding the
// list (not a bare YAML list), matching the convention casaboa-data
// established.
type listingsFile struct {
	Listings []map[string]any `yaml:"listings"`
}

// listingPath is the site path for a listing's detail page.
func listingPath(id string) string {
	return "/listings/" + id + "/"
}

// ID returns a listing's "id" field, or "" if it's missing or not a string.
// LoadListingsFile already guarantees every listing it returns has a
// non-empty string id, so callers that only ever see loaded listings can
// treat a "" result as "can't happen" — this stays a plain accessor (no
// panic) so it's still safe to call on a hand-built map in a test.
func ID(l map[string]any) string {
	id, _ := l["id"].(string)
	return id
}

// LoadListingsFile reads a YAML file shaped as `listings: [...]` and returns
// the listings in file order — each entry's fields verbatim, whatever shape
// the site's own schema uses. Order is preserved rather than re-sorted: it
// already reflects upstream curation/ranking (e.g. a site-defined "score"
// field), unlike courses/projects which have an explicit "order" field to
// sort by.
func LoadListingsFile(path string) ([]map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading listings file %s: %w", path, err)
	}

	var doc listingsFile
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing listings file %s: %w", path, err)
	}

	for i, l := range doc.Listings {
		id := ID(l)
		if id == "" {
			return nil, fmt.Errorf("listings[%d]: missing required field 'id'", i)
		}
		title, _ := l["title"].(string)
		if title == "" {
			return nil, fmt.Errorf("listings[%d] (id=%s): missing required field 'title'", i, id)
		}
	}

	return doc.Listings, nil
}

// withHref returns a shallow copy of l with a computed "href" added,
// pointing at its detail page. A copy, not an in-place mutation, so the
// slice LoadListingsFile returned (held in optionalContent.listings for the
// whole build) never gains a field its own YAML source didn't declare.
func withHref(l map[string]any) map[string]any {
	out := make(map[string]any, len(l)+1)
	for k, v := range l {
		out[k] = v
	}
	out["href"] = listingPath(ID(l))
	return out
}

// ToSiteDataList converts loaded listings to the []any format Go templates
// expect, and that a site's own client-side JS blob (e.g.
// window.CASABOA_LISTINGS) may also read verbatim from site data. Each entry
// now also carries a computed "href" so client-side rendering can link to a
// real, crawlable per-listing URL instead of an in-page anchor.
func ToSiteDataList(list []map[string]any) []any {
	out := make([]any, len(list))
	for i, l := range list {
		out[i] = withHref(l)
	}
	return out
}

// ToCurrentListing builds the per-listing detail-page data map (the
// "CurrentListing" template key). Kept as its own function, mirroring
// ToCurrentCourse, so the detail template has a stable, explicit contract
// independent of the grid's even though the fields are currently identical.
func ToCurrentListing(l map[string]any) map[string]any {
	return withHref(l)
}
