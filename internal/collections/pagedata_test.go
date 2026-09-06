package collections

import (
	"reflect"
	"strings"
	"testing"
)

// formaSiteData mirrors the shape page_data_from exists for: records under one
// site-data namespace (products.<slug>, structure) and their page copy under
// another (pages.product-<slug>, title/description). Two products, so a test
// that resolved the WRONG record would still find data and had to be caught by
// comparing values rather than by an error.
func formaSiteData() map[string]any {
	return map[string]any{
		"products": map[string]any{
			"catalog": map[string]any{"name": "Catalog", "price": "R$ 90"},
			"invoice": map[string]any{"name": "Invoice", "price": "R$ 70"},
		},
		"pages": map[string]any{
			"product-catalog": map[string]any{
				"title":       "Catalog — Forma",
				"description": "The catalog template.",
			},
			"product-invoice": map[string]any{
				"title":       "Invoice — Forma",
				"description": "The invoice template.",
			},
		},
	}
}

func TestResolvePageDataFindsThisRecordsPageCopy(t *testing.T) {
	siteData := formaSiteData()

	for _, slug := range []string{"catalog", "invoice"} {
		got, err := ResolvePageData("pages.product-{{.slug}}", map[string]any{"slug": slug}, siteData, "")
		if err != nil {
			t.Fatalf("ResolvePageData(%s): %v", slug, err)
		}
		want := siteData["pages"].(map[string]any)["product-"+slug]
		if !reflect.DeepEqual(got, want) {
			t.Errorf("slug %q resolved to %#v, want %#v", slug, got, want)
		}
	}
}

// The template is resolved per record, so two records must not share one
// lookup. This is the failure a single-record test cannot see: a resolver that
// cached the first render would give every product the first product's copy and
// still produce nine valid-looking pages.
func TestResolvePageDataIsPerRecord(t *testing.T) {
	siteData := formaSiteData()

	catalog, err := ResolvePageData("pages.product-{{.slug}}", map[string]any{"slug": "catalog"}, siteData, "")
	if err != nil {
		t.Fatalf("ResolvePageData(catalog): %v", err)
	}
	invoice, err := ResolvePageData("pages.product-{{.slug}}", map[string]any{"slug": "invoice"}, siteData, "")
	if err != nil {
		t.Fatalf("ResolvePageData(invoice): %v", err)
	}
	if reflect.DeepEqual(catalog, invoice) {
		t.Errorf("both records resolved to the same page data %#v", catalog)
	}
	if got := catalog.(map[string]any)["title"]; got != "Catalog — Forma" {
		t.Errorf("catalog title = %v", got)
	}
	if got := invoice.(map[string]any)["title"]; got != "Invoice — Forma" {
		t.Errorf("invoice title = %v", got)
	}
}

// An unresolvable path must fail the build. A page whose copy silently resolved
// to nil renders valid HTML with no title and no body text, which linkcheck,
// the sitemap check and structure validation all pass — so the error here is
// the ONLY thing standing between a typo and a live contentless page.
func TestResolvePageDataMissingPathIsAnError(t *testing.T) {
	siteData := formaSiteData()

	cases := map[string]struct {
		tmpl   string
		record map[string]any
	}{
		"missing leaf":         {"pages.product-{{.slug}}", map[string]any{"slug": "nope"}},
		"missing intermediate": {"nosuch.product-{{.slug}}", map[string]any{"slug": "catalog"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ResolvePageData(tc.tmpl, tc.record, siteData, "")
			if err == nil {
				t.Fatalf("expected an error, got %#v", got)
			}
			if !strings.Contains(err.Error(), "not present in site data") {
				t.Errorf("error should say the path is absent, got: %v", err)
			}
		})
	}
}

// An explicit nil leaf is PRESENT. Distinguishing it from an absent one is what
// keeps "the site set this key to nothing on purpose" from failing the build
// like a typo does.
func TestResolvePageDataAcceptsAnExplicitNilLeaf(t *testing.T) {
	siteData := map[string]any{"pages": map[string]any{"product-x": nil}}
	got, err := ResolvePageData("pages.product-{{.slug}}", map[string]any{"slug": "x"}, siteData, "")
	if err != nil {
		t.Fatalf("an explicitly nil leaf is present, not missing: %v", err)
	}
	if got != nil {
		t.Errorf("got %#v, want nil", got)
	}
}

func TestResolvePageDataRejectsABrokenTemplate(t *testing.T) {
	if _, err := ResolvePageData("pages.{{.slug", map[string]any{"slug": "x"}, formaSiteData(), ""); err == nil {
		t.Fatal("expected a parse error for an unclosed action")
	}
}

// page_data_from shares renderRecordTemplate with detail paths, so base_path is
// available to it under the same reserved key.
func TestResolvePageDataSeesBasePath(t *testing.T) {
	siteData := map[string]any{"pages": map[string]any{"pt": map[string]any{"x": "ok"}}}
	got, err := ResolvePageData("pages.{{."+BasePathKey+"}}.x", map[string]any{}, siteData, "pt")
	if err != nil {
		t.Fatalf("ResolvePageData: %v", err)
	}
	if got != "ok" {
		t.Errorf("got %#v, want \"ok\"", got)
	}
}

func TestLookupDottedSiteData(t *testing.T) {
	siteData := map[string]any{
		"top":   "flat",
		"empty": nil,
		"pages": map[string]any{
			"index": map[string]any{"title": "Home"},
			"leaf":  "not a map",
		},
	}

	cases := map[string]struct {
		path      string
		want      any
		wantFound bool
	}{
		"single segment":            {"top", "flat", true},
		"nested":                    {"pages.index.title", "Home", true},
		"whole subtree":             {"pages.index", map[string]any{"title": "Home"}, true},
		"present but nil":           {"empty", nil, true},
		"absent leaf":               {"pages.nope", nil, false},
		"absent root":               {"nope.deep", nil, false},
		"descends through a nonmap": {"pages.leaf.title", nil, false},
		"descends through nil":      {"empty.deep", nil, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, found := LookupDottedSiteData(siteData, tc.path)
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v", found, tc.wantFound)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

// LookupDottedSiteData is the READ mirror of SetDottedSiteData and must not
// share its create-on-demand behaviour: a failed lookup that left an empty map
// behind would add a dangling site-data path the contract validator then
// rejects, turning a missing page-copy entry into an unrelated error.
func TestLookupDottedSiteDataCreatesNothing(t *testing.T) {
	siteData := map[string]any{"pages": map[string]any{}}
	if _, found := LookupDottedSiteData(siteData, "pages.a.b.c"); found {
		t.Fatal("expected not found")
	}
	pages := siteData["pages"].(map[string]any)
	if len(pages) != 0 {
		t.Errorf("lookup mutated site data: pages = %#v", pages)
	}
}

func TestDetailSpecPageDataFromIsOptional(t *testing.T) {
	// The whole knob is opt-in: courses and listings declare no page_data_from
	// and must keep validating and rendering exactly as before.
	for name, c := range Builtins() {
		if c.Detail != nil && c.Detail.PageDataFrom != "" {
			t.Errorf("built-in %q unexpectedly declares page_data_from %q", name, c.Detail.PageDataFrom)
		}
	}
	if err := (Collection{
		Name:    "products",
		Source:  Source{Kind: SourceSiteData, Shape: ShapeKeyedMap, Key: "products", IDField: "slug"},
		Records: RecordSpec{Passthrough: true, Require: []string{"slug"}},
		Index:   &IndexSpec{Enabled: false},
		Detail: &DetailSpec{
			Enabled:  true,
			Template: "product",
			Path:     "/product-{{.slug}}.html",
			DataKey:  "CurrentProduct",
		},
	}).Validate(); err != nil {
		t.Errorf("a detail spec without page_data_from must validate: %v", err)
	}
}

// A keyed_map id_field the projection would drop leaves every record sharing
// one identity. With a flat detail path that is not a visible failure — the
// collection writes ONE file N times — so it has to be caught at config load.
func TestIDFieldMustSurviveTheProjection(t *testing.T) {
	base := Collection{
		Source: Source{Kind: SourceSiteData, Shape: ShapeKeyedMap, Key: "products", IDField: "slug"},
		Detail: &DetailSpec{
			Enabled: true, Template: "product",
			Path: "/product-{{.slug}}.html", DataKey: "CurrentProduct",
		},
	}

	t.Run("dropped by the projection", func(t *testing.T) {
		c := base
		c.Records = RecordSpec{Fields: []Field{{Name: "name", Type: TypeString}}}
		err := c.Validate()
		if err == nil {
			t.Fatal("expected an error: slug is not in records.fields and passthrough is off")
		}
		if !strings.Contains(err.Error(), "id_field") {
			t.Errorf("error should name id_field, got: %v", err)
		}
	})

	t.Run("declared as a field", func(t *testing.T) {
		c := base
		c.Records = RecordSpec{Fields: []Field{{Name: "slug", Type: TypeString, Required: true}}}
		if err := c.Validate(); err != nil {
			t.Errorf("declaring the id_field must satisfy the check: %v", err)
		}
	})

	t.Run("kept by passthrough", func(t *testing.T) {
		c := base
		c.Records = RecordSpec{Passthrough: true}
		if err := c.Validate(); err != nil {
			t.Errorf("passthrough keeps the id_field: %v", err)
		}
	})

	t.Run("no id_field at all", func(t *testing.T) {
		c := base
		c.Source.IDField = ""
		c.Source.Shape = ShapeList
		c.Source.Key = "products"
		if err := c.Validate(); err != nil {
			t.Errorf("a source with no id_field is unaffected: %v", err)
		}
	})

	// The built-ins must keep validating — this guard is new, and none of them
	// uses a keyed_map source today.
	for name, c := range Builtins() {
		if err := c.Validate(); err != nil {
			t.Errorf("built-in %q no longer validates: %v", name, err)
		}
	}
}

func TestPageDataFromParsesFromYAML(t *testing.T) {
	path := writeTemp(t, "collections.yaml", `
version: 1
collections:
  products:
    source: {kind: site_data, shape: keyed_map, key: products, id_field: slug}
    records:
      passthrough: true
      require: [slug]
    index: {enabled: false}
    detail:
      enabled: true
      template: product
      path: "/product-{{.slug}}.html"
      data_key: CurrentProduct
      page_data_from: "pages.product-{{.slug}}"
      sitemap: {changefreq: monthly, priority: "0.6"}
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	got := cfg.Collections["products"].Detail
	if got.PageDataFrom != "pages.product-{{.slug}}" {
		t.Errorf("page_data_from = %q", got.PageDataFrom)
	}
	if got.Path != "/product-{{.slug}}.html" {
		t.Errorf("path = %q", got.Path)
	}
}
