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

func TestLoadListingsFile_ParsesAllFields(t *testing.T) {
	path := writeListingsFile(t, `
listings:
  - id: pinheiros-72m2-001
    title: "Apartamento iluminado em Pinheiros"
    transaction: rent
    price: "R$ 4.200/mês"
    total: "Total estimado R$ 5.180/mês"
    meta: "72 m² · 2 quartos"
    place: "Pinheiros · São Paulo"
    source: "QuintoAndar"
    sourceNote: "Verify availability in the original listing."
    tone: sp
    photoLabel: "bright living room"
    score: 86
    priceLabel: "Fair price"
    locationLabel: "Excellent"
    imageLabel: "Trustworthy photos"
    concern: "Old building."
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
	if l.ID != "pinheiros-72m2-001" {
		t.Errorf("ID = %q, want pinheiros-72m2-001", l.ID)
	}
	if l.Score != 86 {
		t.Errorf("Score = %d, want 86", l.Score)
	}
	if l.Sponsored {
		t.Errorf("Sponsored = true, want false")
	}
	if len(l.Reasons) != 2 {
		t.Errorf("Reasons len = %d, want 2", len(l.Reasons))
	}
	if l.Breakdown["Price"] != 88 {
		t.Errorf("Breakdown[Price] = %d, want 88", l.Breakdown["Price"])
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
	if list[0].ID != "second" || list[1].ID != "first" {
		t.Errorf("expected file order preserved, got %v then %v", list[0].ID, list[1].ID)
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
	list := []Listing{{ID: "moinhos-94m2-002", Title: "Planta ampla", Score: 81}}
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

func TestToSiteDataList_PreservesReasonsAndBreakdown(t *testing.T) {
	list := []Listing{{
		ID:        "x",
		Title:     "X",
		Reasons:   []string{"a", "b"},
		Breakdown: map[string]int{"Preço": 70},
	}}
	m := ToSiteDataList(list)[0].(map[string]any)
	reasons, ok := m["reasons"].([]any)
	if !ok || len(reasons) != 2 {
		t.Fatalf("reasons should be a 2-element list, got %v", m["reasons"])
	}
	breakdown, ok := m["breakdown"].(map[string]any)
	if !ok || breakdown["Preço"] != 70 {
		t.Fatalf("breakdown[Preço] = %v, want 70", breakdown["Preço"])
	}
}

func TestToCurrentListing_MatchesGridFields(t *testing.T) {
	l := Listing{ID: "y", Title: "Y", Place: "Somewhere", Concern: "old building"}
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
