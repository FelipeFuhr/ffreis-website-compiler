package collections

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// A FROZEN copy of internal/listings as it stood immediately before Phase 5
// deleted it (docs/decisions/0001-generic-content-collections.md §6).
//
// Same rationale as frozen_projects_oracle_test.go and
// frozen_courses_oracle_test.go: the oracle test for a migrated collection has
// to keep running after its typed loader is gone, or the equivalence proof is
// deleted along with the production code. With this file in place, no oracle
// imports a live package any more — all three typed loaders are gone.
//
// Do NOT "fix" or refactor anything below. Its entire value is being a
// byte-faithful record of the behaviour the engine has to reproduce:
//
//   - the exact field set of baseMap — 19 struct fields plus the computed
//     "href" — since that map IS the window.CASABOA_LISTINGS byte contract
//     (§1.3), not merely an input to it
//   - the exact zero values: Sponsored false (never absent), Reasons []any{}
//     and Breakdown map[string]any{} (never nil), Score 0 as an int
//   - FILE ORDER, deliberately unsorted, because it encodes upstream ranking
//     (Q7) — the one ordering rule that differs from projects and courses
//   - the required-field errors on both `id` and `title`
//
// If the engine and this disagree, the engine is wrong.
//
// Source: internal/listings/listings.go at commit 7f1a13c.

type frozenListing struct {
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

type frozenListingsFile struct {
	Listings []frozenListing `yaml:"listings"`
}

func frozenListingPath(id string) string {
	return "/listings/" + id + "/"
}

func frozenLoadListingsFile(path string) ([]frozenListing, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading listings file %s: %w", path, err)
	}

	var doc frozenListingsFile
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

func frozenListingBaseMap(l frozenListing) map[string]any {
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
		"href":          frozenListingPath(l.ID),
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

func frozenListingsToSiteDataList(list []frozenListing) []any {
	out := make([]any, len(list))
	for i, l := range list {
		out[i] = frozenListingBaseMap(l)
	}
	return out
}

func frozenToCurrentListing(l frozenListing) map[string]any {
	return frozenListingBaseMap(l)
}
