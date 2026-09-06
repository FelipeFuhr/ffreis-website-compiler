package collections

import (
	"strings"
	"testing"
)

func TestLoadRecordsKeyedMapExcludeKeys(t *testing.T) {
	src := Source{
		Kind:        SourceSiteData,
		Shape:       ShapeKeyedMap,
		Key:         "courses",
		IDField:     "slug",
		ExcludeKeys: []string{"ssbb"},
	}
	siteData := map[string]any{"courses": map[string]any{
		"ssyb": map[string]any{"title": "Yellow"},
		"ssbb": map[string]any{"title": "Black"},
		"cqe":  map[string]any{"title": "CQE"},
	}}

	got, err := LoadRecords(src, LoadOptions{SiteData: siteData})
	if err != nil {
		t.Fatalf("LoadRecords: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2 (ssbb excluded): %v", len(got), got)
	}
	for _, rec := range got {
		if rec["slug"] == "ssbb" {
			t.Fatalf("excluded key ssbb still present: %v", got)
		}
	}
	// Order and the id field are unaffected by the exclusion.
	if got[0]["slug"] != "cqe" || got[1]["slug"] != "ssyb" {
		t.Errorf("unexpected order: %v, %v", got[0]["slug"], got[1]["slug"])
	}
}

func TestLoadRecordsKeyedMapWithoutExcludeKeysKeepsEverything(t *testing.T) {
	src := Source{Kind: SourceSiteData, Shape: ShapeKeyedMap, Key: "courses", IDField: "slug"}
	siteData := map[string]any{"courses": map[string]any{
		"ssyb": map[string]any{"title": "Yellow"},
		"ssbb": map[string]any{"title": "Black"},
	}}

	got, err := LoadRecords(src, LoadOptions{SiteData: siteData})
	if err != nil {
		t.Fatalf("LoadRecords: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
}

func TestValidateRejectsExcludeKeysOnNonKeyedMap(t *testing.T) {
	c := Collection{
		Name: "listings",
		Source: Source{
			Kind:        SourceFile,
			Shape:       ShapeList,
			ExcludeKeys: []string{"nope"},
		},
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected exclude_keys on a list source to be rejected")
	}
	if !strings.Contains(err.Error(), "exclude_keys") {
		t.Errorf("error should name the offending field, got %v", err)
	}
}

func TestRenderPageName(t *testing.T) {
	got, err := RenderPageName("{{.slug}}", map[string]any{"slug": "ssyb"}, "/pt")
	if err != nil {
		t.Fatalf("RenderPageName: %v", err)
	}
	if got != "ssyb" {
		t.Errorf("got %q, want %q", got, "ssyb")
	}
}
