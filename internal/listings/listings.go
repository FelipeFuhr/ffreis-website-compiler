// Package listings loads curated real-estate listing entries from a YAML
// file and converts them into the map shapes consumed by Go templates: the
// existing client-rendered grid data, and a new per-listing detail-page data
// map. It is the last typed content loader; decision-record Phase 5 replaces
// it with the internal/collections `listings` built-in, which
// TestBuiltinListingsMatchesListingsPackage already proves equivalent.
package listings

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Listing holds data for a single curated listing entry. Field names and YAML
// tags match the existing casaboa-data schema exactly, since ToSiteDataList's
// output also becomes the window.CASABOA_LISTINGS JS blob already consumed by
// the site's client-side rendering — changing a key here would be a breaking
// change to that contract.
type Listing struct {
	ID            string         `yaml:"id"`
	Title         string         `yaml:"title"`
	Transaction   string         `yaml:"transaction"`
	Price         string         `yaml:"price"`
	Total         string         `yaml:"total"`
	Meta          string         `yaml:"meta"`
	Place         string         `yaml:"place"`
	Source        string         `yaml:"source"`
	SourceNote    string         `yaml:"sourceNote"`
	Tone          string         `yaml:"tone"`
	PhotoLabel    string         `yaml:"photoLabel"`
	Score         int            `yaml:"score"`
	PriceLabel    string         `yaml:"priceLabel"`
	LocationLabel string         `yaml:"locationLabel"`
	ImageLabel    string         `yaml:"imageLabel"`
	Concern       string         `yaml:"concern"`
	Sponsored     bool           `yaml:"sponsored"`
	Reasons       []string       `yaml:"reasons"`
	Breakdown     map[string]int `yaml:"breakdown"`
}

// listingsFile is the on-disk shape: a top-level "listings:" key holding the
// list (not a bare YAML list), matching casaboa-data's existing file.
type listingsFile struct {
	Listings []Listing `yaml:"listings"`
}

// listingPath is the site path for a listing's detail page.
func listingPath(id string) string {
	return "/listings/" + id + "/"
}

// LoadListingsFile reads a YAML file shaped as `listings: [...]` and returns
// the listings in file order. Order is preserved rather than re-sorted: it
// already reflects upstream curation/ranking (see the "score" field), unlike
// courses/projects which have an explicit "order" field to sort by.
func LoadListingsFile(path string) ([]Listing, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading listings file %s: %w", path, err)
	}

	var doc listingsFile
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing listings file %s: %w", path, err)
	}

	for i := range doc.Listings {
		if doc.Listings[i].ID == "" {
			return nil, fmt.Errorf("listings[%d]: missing required field 'id'", i)
		}
		if doc.Listings[i].Title == "" {
			return nil, fmt.Errorf("listings[%d] (id=%s): missing required field 'title'", i, doc.Listings[i].ID)
		}
	}

	return doc.Listings, nil
}

// baseMap returns every field of a Listing as the map[string]any shape Go
// templates and JSON serialisation expect, plus a computed "href" pointing at
// its new detail page. Used for both the grid/JS-blob data and the detail page.
func baseMap(l Listing) map[string]any {
	reasons := make([]any, len(l.Reasons))
	for i, r := range l.Reasons {
		reasons[i] = r
	}
	breakdown := make(map[string]any, len(l.Breakdown))
	for k, v := range l.Breakdown {
		breakdown[k] = v
	}
	return map[string]any{
		"id":            l.ID,
		"title":         l.Title,
		"href":          listingPath(l.ID),
		"transaction":   l.Transaction,
		"price":         l.Price,
		"total":         l.Total,
		"meta":          l.Meta,
		"place":         l.Place,
		"source":        l.Source,
		"sourceNote":    l.SourceNote,
		"tone":          l.Tone,
		"photoLabel":    l.PhotoLabel,
		"score":         l.Score,
		"priceLabel":    l.PriceLabel,
		"locationLabel": l.LocationLabel,
		"imageLabel":    l.ImageLabel,
		"concern":       l.Concern,
		"sponsored":     l.Sponsored,
		"reasons":       reasons,
		"breakdown":     breakdown,
	}
}

// ToSiteDataList converts a slice of Listing to the []any map format expected
// by Go templates and by the window.CASABOA_LISTINGS JS blob. Each entry now
// also carries a computed "href" so client-side rendering can link to a real,
// crawlable per-listing URL instead of an in-page anchor.
func ToSiteDataList(list []Listing) []any {
	out := make([]any, len(list))
	for i, l := range list {
		out[i] = baseMap(l)
	}
	return out
}

// ToCurrentListing builds the per-listing detail-page data map (the
// "CurrentListing" template key). Kept as its own function, mirroring
// ToCurrentCourse, so the detail template has a stable, explicit contract
// independent of the grid's even though the fields are currently identical.
func ToCurrentListing(l Listing) map[string]any {
	return baseMap(l)
}
