package collections

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "records.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestLoadRecordsListShape(t *testing.T) {
	path := writeYAML(t, "- title: A\n- title: B\n")
	got, err := LoadRecords(Source{Kind: SourceFile, Shape: ShapeList}, LoadOptions{Path: path})
	if err != nil {
		t.Fatalf("LoadRecords: %v", err)
	}
	if len(got) != 2 || got[0]["title"] != "A" || got[1]["title"] != "B" {
		t.Errorf("got %v", got)
	}
}

func TestLoadRecordsWrappedListShape(t *testing.T) {
	path := writeYAML(t, "listings:\n  - id: one\n  - id: two\n")
	src := Source{Kind: SourceFile, Shape: ShapeWrappedList, Key: "listings"}
	got, err := LoadRecords(src, LoadOptions{Path: path})
	if err != nil {
		t.Fatalf("LoadRecords: %v", err)
	}
	if len(got) != 2 || got[0]["id"] != "one" {
		t.Errorf("got %v", got)
	}

	// A file missing the wrapper key is an error, not a silent empty list.
	bad := writeYAML(t, "other:\n  - id: one\n")
	if _, err := LoadRecords(src, LoadOptions{Path: bad}); err == nil {
		t.Error("expected an error when the wrapper key is absent")
	}
}

// TestLoadRecordsKeyedMapShape covers the shape flemming's courses and forma's
// products need: a slug-keyed map, with the key copied into id_field.
func TestLoadRecordsKeyedMapShape(t *testing.T) {
	path := writeYAML(t, "ssyb:\n  label: Yellow\nssgb:\n  label: Green\n")
	src := Source{Kind: SourceFile, Shape: ShapeKeyedMap, IDField: "slug"}
	got, err := LoadRecords(src, LoadOptions{Path: path})
	if err != nil {
		t.Fatalf("LoadRecords: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	// Keys are visited in sorted order so the result is deterministic.
	if got[0]["slug"] != "ssgb" || got[1]["slug"] != "ssyb" {
		t.Errorf("keyed map order = %v, %v; want ssgb, ssyb", got[0]["slug"], got[1]["slug"])
	}
	if got[0]["label"] != "Green" {
		t.Errorf("record fields lost: %v", got[0])
	}
}

func TestLoadRecordsSiteDataSource(t *testing.T) {
	siteData := map[string]any{
		"listings": []any{
			map[string]any{"id": "a"},
			map[string]any{"id": "b"},
		},
	}
	src := Source{Kind: SourceSiteData, Shape: ShapeList, Key: "listings"}
	got, err := LoadRecords(src, LoadOptions{SiteData: siteData})
	if err != nil {
		t.Fatalf("LoadRecords: %v", err)
	}
	if len(got) != 2 || got[0]["id"] != "a" {
		t.Errorf("got %v", got)
	}
}

func TestLoadRecordsSiteDataKeyedMap(t *testing.T) {
	siteData := map[string]any{
		"products": map[string]any{
			"catalog": map[string]any{"name": "Catalog"},
		},
	}
	src := Source{Kind: SourceSiteData, Shape: ShapeKeyedMap, Key: "products", IDField: "slug"}
	got, err := LoadRecords(src, LoadOptions{SiteData: siteData})
	if err != nil {
		t.Fatalf("LoadRecords: %v", err)
	}
	if len(got) != 1 || got[0]["slug"] != "catalog" || got[0]["name"] != "Catalog" {
		t.Errorf("got %v", got)
	}
}

// TestLoadRecordsFallsBackToSiteData pins the casaboa case from decision record
// §7.2: -listings-file is passed only when the file exists, but the same YAML
// is also copied into the site-data layer. Without the fallback, a build with
// the layer and no flag would publish an empty list — a silent regression in
// exactly the mode nobody tests.
func TestLoadRecordsFallsBackToSiteData(t *testing.T) {
	src := builtinListings().Source
	siteData := map[string]any{
		"listings": []any{map[string]any{"id": "from-layer", "title": "T"}},
	}

	// No -listings-file: the fallback supplies the records.
	got, err := LoadRecords(src, LoadOptions{SiteData: siteData})
	if err != nil {
		t.Fatalf("LoadRecords: %v", err)
	}
	if len(got) != 1 || got[0]["id"] != "from-layer" {
		t.Errorf("fallback did not run; got %v", got)
	}

	// With the flag: the file wins over the layer.
	path := writeYAML(t, "listings:\n  - id: from-file\n    title: T\n")
	got, err = LoadRecords(src, LoadOptions{Path: path, SiteData: siteData})
	if err != nil {
		t.Fatalf("LoadRecords: %v", err)
	}
	if len(got) != 1 || got[0]["id"] != "from-file" {
		t.Errorf("the file must win over the site-data fallback; got %v", got)
	}
}

func TestLoadRecordsAbsentSourceIsNotAnError(t *testing.T) {
	got, err := LoadRecords(Source{Kind: SourceFile, Shape: ShapeList}, LoadOptions{})
	if err != nil {
		t.Fatalf("an absent file source must not error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}

	got, err = LoadRecords(
		Source{Kind: SourceSiteData, Shape: ShapeList, Key: "nope"},
		LoadOptions{SiteData: map[string]any{}},
	)
	if err != nil {
		t.Fatalf("an absent site-data key must not error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestLoadRecordsReportsBadShapes(t *testing.T) {
	if _, err := LoadRecords(
		Source{Kind: SourceFile, Shape: ShapeList},
		LoadOptions{Path: writeYAML(t, "- just-a-string\n")},
	); err == nil {
		t.Error("expected an error when a list entry is not a map")
	}

	if _, err := LoadRecords(
		Source{Kind: SourceFile, Shape: ShapeList},
		LoadOptions{Path: filepath.Join(t.TempDir(), "missing.yaml")},
	); err == nil {
		t.Error("expected an error when the declared file does not exist")
	}

	if _, err := LoadRecords(
		Source{Kind: SourceSiteData, Shape: ShapeList, Key: "k"},
		LoadOptions{SiteData: map[string]any{"k": "not-a-list"}},
	); err == nil {
		t.Error("expected an error when a site-data value is not a list")
	}
}

// ── publish ──────────────────────────────────────────────────────────────────

// TestSetDottedSiteDataMatchesExistingInjectors pins the two differing
// behaviours the engine has to reproduce: a single-segment key is set directly
// (injectProjectsHomeCarousel), while pages.index.* is a NO-OP when "pages" is
// absent but CREATES "index" when only that is missing
// (injectCoursesHomeCarousel).
func TestSetDottedSiteDataMatchesExistingInjectors(t *testing.T) {
	t.Run("single segment sets directly", func(t *testing.T) {
		siteData := map[string]any{}
		SetDottedSiteData(siteData, "projects", []any{1})
		if !reflect.DeepEqual([]any{1}, siteData["projects"]) {
			t.Errorf("got %v", siteData["projects"])
		}
	})

	t.Run("no-op when the first segment is absent", func(t *testing.T) {
		siteData := map[string]any{}
		SetDottedSiteData(siteData, "pages.index.courses_carousel_items", []any{1})
		if len(siteData) != 0 {
			t.Errorf("expected a no-op when pages is absent, got %v", siteData)
		}
	})

	t.Run("creates missing intermediates below the first segment", func(t *testing.T) {
		siteData := map[string]any{"pages": map[string]any{}}
		SetDottedSiteData(siteData, "pages.index.courses_carousel_items", []any{1})

		pages, ok := siteData["pages"].(map[string]any)
		if !ok {
			t.Fatalf("pages is %T", siteData["pages"])
		}
		index, ok := pages["index"].(map[string]any)
		if !ok {
			t.Fatalf("index was not created: %v", pages)
		}
		if !reflect.DeepEqual([]any{1}, index["courses_carousel_items"]) {
			t.Errorf("got %v", index["courses_carousel_items"])
		}
	})

	t.Run("existing intermediates are preserved", func(t *testing.T) {
		siteData := map[string]any{
			"pages": map[string]any{"index": map[string]any{"heading": "keep me"}},
		}
		SetDottedSiteData(siteData, "pages.index.courses_carousel_items", []any{1})
		index := siteData["pages"].(map[string]any)["index"].(map[string]any)
		if index["heading"] != "keep me" {
			t.Errorf("sibling keys were clobbered: %v", index)
		}
	})
}

func TestApplyPublishHonoursLimits(t *testing.T) {
	records := []any{1, 2, 3, 4, 5}

	t.Run("limit from the items-per-page flag", func(t *testing.T) {
		siteData := map[string]any{}
		c := builtinProjects()
		c.ApplyPublish(siteData, records, PublishOptions{ItemsPerPage: 2})
		if got, _ := siteData["projects"].([]any); len(got) != 2 {
			t.Errorf("got %v, want the first 2 records", got)
		}
	})

	t.Run("no limit publishes everything", func(t *testing.T) {
		siteData := map[string]any{}
		c := builtinListings()
		c.ApplyPublish(siteData, records, PublishOptions{ItemsPerPage: 2})
		if got, _ := siteData["listings"].([]any); len(got) != 5 {
			t.Errorf("listings declares no limit, so all 5 records must publish; got %v", got)
		}
	})

	t.Run("a limit larger than the record count is harmless", func(t *testing.T) {
		siteData := map[string]any{}
		c := builtinProjects()
		c.ApplyPublish(siteData, records, PublishOptions{ItemsPerPage: 99})
		if got, _ := siteData["projects"].([]any); len(got) != 5 {
			t.Errorf("got %v, want all 5", got)
		}
	})

	t.Run("literal limit", func(t *testing.T) {
		siteData := map[string]any{}
		c := Collection{Publish: []Publish{{Key: "k", Value: ValueRecords, Limit: 3}}}
		c.ApplyPublish(siteData, records, PublishOptions{ItemsPerPage: 99})
		if got, _ := siteData["k"].([]any); len(got) != 3 {
			t.Errorf("got %v, want 3", got)
		}
	})
}
