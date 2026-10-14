package listings

import (
	"os"
	"path/filepath"
	"testing"
)

func writeListingsFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "listings.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing temp listings file: %v", err)
	}
	return path
}

func TestLoadListingsFile_ParsesArbitraryFieldsVerbatim(t *testing.T) {
	// A real-estate-shaped site (casaboa-data's original schema).
	path := writeListingsFile(t, `
listings:
  - id: pinheiros-72m2-001
    title: "Apartamento iluminado em Pinheiros"
    transaction: rent
    score: 86
    sponsored: false
    reasons:
      - "Good size-to-location ratio."
      - "Photos suggest natural light."
    breakdown: { "Price": 88, "Location": 90 }
`)
	list, err := LoadListingsFile(path)
	if err != nil {
		t.Fatalf("LoadListingsFile: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 listing, got %d", len(list))
	}
	l := list[0]
	if ID(l) != "pinheiros-72m2-001" {
		t.Errorf("ID = %q, want pinheiros-72m2-001", ID(l))
	}
	if l["score"] != 86 {
		t.Errorf("score = %v, want 86", l["score"])
	}
	if l["sponsored"] != false {
		t.Errorf("sponsored = %v, want false", l["sponsored"])
	}
	reasons, ok := l["reasons"].([]any)
	if !ok || len(reasons) != 2 {
		t.Fatalf("reasons should be a 2-element list, got %v", l["reasons"])
	}
	breakdown, ok := l["breakdown"].(map[string]any)
	if !ok || breakdown["Price"] != 88 {
		t.Fatalf("breakdown[Price] = %v, want 88", l["breakdown"])
	}
}

func TestLoadListingsFile_ParsesACompletelyDifferentSchema(t *testing.T) {
	// A game-catalog-shaped site (heavy-heater's schema) — none of these
	// field names exist anywhere in the loader; that's the point.
	path := writeListingsFile(t, `
listings:
  - id: bug-metro
    title: "Bug Metro"
    partner: "Pocket World Arcade"
    tokens: 6
    tags: ["co-op routing", "tiny city"]
`)
	list, err := LoadListingsFile(path)
	if err != nil {
		t.Fatalf("LoadListingsFile: %v", err)
	}
	l := list[0]
	if l["partner"] != "Pocket World Arcade" {
		t.Errorf("partner = %v, want Pocket World Arcade", l["partner"])
	}
	if l["tokens"] != 6 {
		t.Errorf("tokens = %v, want 6", l["tokens"])
	}
	tags, ok := l["tags"].([]any)
	if !ok || len(tags) != 2 {
		t.Fatalf("tags should be a 2-element list, got %v", l["tags"])
	}
}

func TestLoadListingsFile_PreservesFileOrder(t *testing.T) {
	path := writeListingsFile(t, `
listings:
  - id: second
    title: "Second"
  - id: first
    title: "First"
`)
	list, err := LoadListingsFile(path)
	if err != nil {
		t.Fatalf("LoadListingsFile: %v", err)
	}
	if ID(list[0]) != "second" || ID(list[1]) != "first" {
		t.Errorf("expected file order preserved, got %v then %v", ID(list[0]), ID(list[1]))
	}
}

func TestLoadListingsFile_MissingIDIsError(t *testing.T) {
	path := writeListingsFile(t, `
listings:
  - title: "No id here"
`)
	if _, err := LoadListingsFile(path); err == nil {
		t.Fatal("expected an error for a listing with no id, got nil")
	}
}

func TestLoadListingsFile_NonStringIDIsError(t *testing.T) {
	path := writeListingsFile(t, `
listings:
  - id: 123
    title: "Numeric id"
`)
	if _, err := LoadListingsFile(path); err == nil {
		t.Fatal("expected an error for a non-string id, got nil")
	}
}

func TestLoadListingsFile_MissingTitleIsError(t *testing.T) {
	path := writeListingsFile(t, `
listings:
  - id: no-title
`)
	if _, err := LoadListingsFile(path); err == nil {
		t.Fatal("expected an error for a listing with no title, got nil")
	}
}

func TestLoadListingsFile_MissingFileIsError(t *testing.T) {
	if _, err := LoadListingsFile("/no/such/path/listings.yaml"); err == nil {
		t.Fatal("expected an error for a missing file, got nil")
	}
}

func TestToSiteDataList_AddsComputedHref(t *testing.T) {
	list := []map[string]any{{"id": "moinhos-94m2-002", "title": "Planta ampla", "score": 81}}
	got := ToSiteDataList(list)
	m := got[0].(map[string]any)
	if m["href"] != "/listings/moinhos-94m2-002/" {
		t.Errorf("href = %v, want /listings/moinhos-94m2-002/", m["href"])
	}
	if m["title"] != "Planta ampla" {
		t.Errorf("title = %v, want Planta ampla", m["title"])
	}
	if m["score"] != 81 {
		t.Errorf("score = %v, want 81", m["score"])
	}
}

func TestToSiteDataList_DoesNotMutateTheInputMap(t *testing.T) {
	src := map[string]any{"id": "x", "title": "X"}
	list := []map[string]any{src}
	_ = ToSiteDataList(list)
	if _, ok := src["href"]; ok {
		t.Error("ToSiteDataList must not add href to the caller's original map")
	}
}

func TestToCurrentListing_MatchesGridFields(t *testing.T) {
	l := map[string]any{"id": "y", "title": "Y", "place": "Somewhere", "concern": "old building"}
	m := ToCurrentListing(l)
	if m["place"] != "Somewhere" {
		t.Errorf("place = %v, want Somewhere", m["place"])
	}
	if m["concern"] != "old building" {
		t.Errorf("concern = %v, want 'old building'", m["concern"])
	}
	if m["href"] != "/listings/y/" {
		t.Errorf("href = %v, want /listings/y/", m["href"])
	}
}

func TestID_ReturnsEmptyForMissingOrNonStringID(t *testing.T) {
	if got := ID(map[string]any{}); got != "" {
		t.Errorf("ID with no id key = %q, want empty", got)
	}
	if got := ID(map[string]any{"id": 123}); got != "" {
		t.Errorf("ID with non-string id = %q, want empty", got)
	}
}
