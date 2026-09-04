package buildcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

// End-to-end coverage for the two features forma's product migration needs and
// no existing collection exercises (decision record §5.3):
//
//  1. a FLAT detail path — "/product-{{.slug}}.html", with no trailing slash —
//     which must write a literal file, not <path>/index.html. Both existing
//     detail writers hardcode the directory form, so this is new behaviour.
//  2. page_data_from — one shared detail template can no longer reach its own
//     page copy via pages.<PageName>, because .PageName is now the template
//     name for every record.
//
// The fixture is deliberately shaped like forma: records keyed by slug under
// site data, copy under pages.product-<slug>, and a single detail template.

const (
	productsCollectionYAML = `
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
`

	// Two products, with the copy deliberately NOT derivable from the record:
	// a template that fell back to the record (or to the wrong record) would
	// still render, and only a value comparison catches it.
	productsSiteDataYAML = `
products:
  catalog:
    name: "Catalog"
    price: "R$ 90"
  invoice:
    name: "Invoice"
    price: "R$ 70"
`

	// pages.product is the SHARED template's own entry. It still needs the keys
	// the shared head partial requires, because sitegen.RenderPages executes the
	// template once with no .CurrentX before any record is rendered; `internal`
	// only keeps that render off disk, it does not skip it (decision record Q8).
	productPagesYAML = `
pages:
  product:
    title: "Product — Golden"
    meta_description: "Shared product detail template."
    canonical_url: "https://golden.example/pt/product/"
    internal: true
  product-catalog:
    title: "Catalog — Golden"
    meta_description: "The catalog template."
    canonical_url: "https://golden.example/pt/product-catalog.html"
    heading: "Catalog copy heading"
  product-invoice:
    title: "Invoice — Golden"
    meta_description: "The invoice template."
    canonical_url: "https://golden.example/pt/product-invoice.html"
    heading: "Invoice copy heading"
`

	// Guards on .CurrentProduct because sitegen.RenderPages executes every page
	// template once with no .CurrentX for asset-usage validation (decision
	// record Q8); pages.product.internal keeps that render off disk.
	productTemplate = `{{define "page_title"}}{{if .CurrentPageData}}{{dig .CurrentPageData "title"}}{{else}}Product{{end}}{{end}}

{{define "page"}}
{{if .CurrentProduct}}
<main class="product-main" id="main-content">
  <h1 class="product-name">{{dig .CurrentProduct "name"}}</h1>
  <p class="product-price">{{dig .CurrentProduct "price"}}</p>
  <p class="product-copy">{{dig .CurrentPageData "heading"}}</p>
</main>
{{else}}
<main id="main-content"><h1>Product</h1></main>
{{end}}
{{end}}
`
)

// assembleProductsSite builds a golden site extended with the forma-shaped
// products collection. pagesYAML is a parameter so a test can remove one page
// copy entry and prove the build fails.
func assembleProductsSite(t *testing.T, pagesYAML string) string {
	t.Helper()

	websiteRoot := assembleGoldenSite(t, "pt")
	siteD := filepath.Join(websiteRoot, "src", "data", "site.d")

	writeFixtureFile(t, filepath.Join(siteD, "70-products.yaml"), productsSiteDataYAML)
	writeFixtureFile(t, filepath.Join(siteD, "80-product-pages.yaml"), pagesYAML)
	writeFixtureFile(t,
		filepath.Join(websiteRoot, "src", "templates", "pages", "product.gohtml"),
		productTemplate)
	writeCollectionsYAML(t, websiteRoot, productsCollectionYAML)

	// The collection reads siteData["products"] itself, so it is compiler-
	// consumed rather than template-referenced — declaring it under `allowed`
	// would trip -strict-contract's "allowed path not used by any template".
	contractPath := filepath.Join(websiteRoot, "src", "data", "site.contract.yaml")
	contract := string(mustReadFile(t, contractPath))
	writeFixtureFile(t, contractPath, strings.Replace(contract,
		"compiler_consumed:\n", "compiler_consumed:\n  - products\n", 1))

	return websiteRoot
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func buildProductsSite(t *testing.T, websiteRoot string) string {
	t.Helper()
	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-clean-urls",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	return outDir
}

// TestFlatDetailPathWritesALiteralFile is the §5.3 acid test: a detail path with
// no trailing slash must produce dist/product-<slug>.html as a FILE. The
// directory form would still be a valid build — every page would be reachable
// at product-<slug>.html/ — so the assertion has to be on the filesystem node
// type, not just on existence.
func TestFlatDetailPathWritesALiteralFile(t *testing.T) {
	outDir := buildProductsSite(t, assembleProductsSite(t, productPagesYAML))

	for _, slug := range []string{"catalog", "invoice"} {
		target := filepath.Join(outDir, "product-"+slug+".html")
		info, err := os.Stat(target)
		if err != nil {
			t.Fatalf("expected a flat detail file at %s: %v", target, err)
		}
		if info.IsDir() {
			t.Errorf("%s is a directory — the writer treated a flat path like a "+
				"trailing-slash one and wrote <path>/index.html", target)
		}
		// The directory form is the specific regression to catch.
		if _, err := os.Stat(filepath.Join(target, "index.html")); err == nil {
			t.Errorf("%s/index.html exists; a flat path must not become a directory", target)
		}
	}
}

// TestTrailingSlashDetailPathStillWritesIndexHTML is the other half of the
// branch. Without it, "fixing" resolveDetailTarget to always write literally
// would pass the flat test and silently break courses and listings.
func TestTrailingSlashDetailPathStillWritesIndexHTML(t *testing.T) {
	websiteRoot := assembleProductsSite(t, productPagesYAML)
	writeCollectionsYAML(t, websiteRoot,
		strings.Replace(productsCollectionYAML,
			`path: "/product-{{.slug}}.html"`, `path: "/product/{{.slug}}/"`, 1))

	outDir := buildProductsSite(t, websiteRoot)

	for _, slug := range []string{"catalog", "invoice"} {
		target := filepath.Join(outDir, "product", slug, "index.html")
		if _, err := os.Stat(target); err != nil {
			t.Errorf("a trailing-slash detail path must still write %s: %v", target, err)
		}
	}
}

// TestPageDataFromRendersEachRecordsOwnCopy proves the join actually resolves
// per record. A resolver that returned the same page data for every record
// would still produce two complete, valid pages — so both pages are checked for
// their own copy AND for the absence of the other's.
func TestPageDataFromRendersEachRecordsOwnCopy(t *testing.T) {
	outDir := buildProductsSite(t, assembleProductsSite(t, productPagesYAML))

	cases := map[string]struct{ name, price, heading, title, otherHeading string }{
		"catalog": {"Catalog", "R$ 90", "Catalog copy heading", "Catalog — Golden", "Invoice copy heading"},
		"invoice": {"Invoice", "R$ 70", "Invoice copy heading", "Invoice — Golden", "Catalog copy heading"},
	}
	for slug, want := range cases {
		t.Run(slug, func(t *testing.T) {
			page := string(mustReadFile(t, filepath.Join(outDir, "product-"+slug+".html")))

			// .CurrentProduct — the record half of the join.
			for _, s := range []string{want.name, want.price} {
				if !strings.Contains(page, s) {
					t.Errorf("missing record value %q from .CurrentProduct", s)
				}
			}
			// .CurrentPageData — the copy half, in both the body and <title>.
			if !strings.Contains(page, want.heading) {
				t.Errorf("missing %q from .CurrentPageData", want.heading)
			}
			if !strings.Contains(page, want.title) {
				t.Errorf("missing %q — page_data_from should reach page_title too", want.title)
			}
			if strings.Contains(page, want.otherHeading) {
				t.Errorf("page carries the OTHER record's copy %q — page_data_from is not "+
					"being resolved per record", want.otherHeading)
			}
		})
	}
}

// TestOneSharedTemplateReplacesPerRecordStubs pins what page_data_from buys: a
// single template named "product" serves every record, and its own
// internal-marked render never reaches disk (decision record Q8).
func TestOneSharedTemplateReplacesPerRecordStubs(t *testing.T) {
	outDir := buildProductsSite(t, assembleProductsSite(t, productPagesYAML))

	if _, err := os.Stat(filepath.Join(outDir, "product", "index.html")); !os.IsNotExist(err) {
		t.Errorf("the shared detail template's own render must stay off disk "+
			"(pages.product.internal: true), got err=%v", err)
	}

	sitemapRaw := string(mustReadFile(t, filepath.Join(outDir, "sitemap.xml")))
	for _, slug := range []string{"catalog", "invoice"} {
		if !strings.Contains(sitemapRaw, "/product-"+slug+".html") {
			t.Errorf("sitemap missing /product-%s.html:\n%s", slug, sitemapRaw)
		}
	}
	if strings.Contains(sitemapRaw, "<loc>https://golden.example/pt/product</loc>") {
		t.Errorf("the internal shared template leaked into the sitemap:\n%s", sitemapRaw)
	}
	// The sitemap attributes the collection declared must reach the entries.
	if !strings.Contains(sitemapRaw, "<changefreq>monthly</changefreq>") ||
		!strings.Contains(sitemapRaw, "<priority>0.6</priority>") {
		t.Errorf("detail sitemap attrs missing:\n%s", sitemapRaw)
	}
}

// TestMissingPageDataFailsTheBuild is the control for the strictness choice: a
// record whose page_data_from path resolves to nothing must stop the build.
// Without this, dropping one pages.product-* entry ships a page with an empty
// title and no body copy that every other guard happily passes.
func TestMissingPageDataFailsTheBuild(t *testing.T) {
	// Drop the product-invoice block by cutting from its key to the end of the
	// document, so the fixture stays correct if the entry's fields change.
	cut := strings.Index(productPagesYAML, "  product-invoice:")
	if cut < 0 {
		t.Fatal("fixture setup failed: product-invoice not found in productPagesYAML")
	}
	pagesWithoutInvoice := productPagesYAML[:cut]
	if strings.Contains(pagesWithoutInvoice, "product-invoice") {
		t.Fatal("fixture setup failed: product-invoice was not removed")
	}
	if !strings.Contains(pagesWithoutInvoice, "product-catalog") {
		t.Fatal("fixture setup failed: product-catalog should have survived the cut")
	}

	err := Run([]string{
		flagWebsiteRoot, assembleProductsSite(t, pagesWithoutInvoice),
		flagOut, t.TempDir(),
		"-clean-urls",
	}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected the build to fail for a record with no page copy")
	}
	for _, want := range []string{"pages.product-invoice", "not present in site data"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q, got: %v", want, err)
		}
	}
}

func TestResolveDetailTarget(t *testing.T) {
	cases := map[string]struct{ urlPath, want string }{
		"flat html":       {"/product-catalog.html", filepath.Join("out", "product-catalog.html")},
		"trailing slash":  {"/courses/go/", filepath.Join("out", "courses", "go", "index.html")},
		"nested flat":     {"/store/product-a.html", filepath.Join("out", "store", "product-a.html")},
		"lang-prefixed":   {"/pt/product-a.html", filepath.Join("out", "pt", "product-a.html")},
		"lang plus slash": {"/pt/listings/1/", filepath.Join("out", "pt", "listings", "1", "index.html")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := resolveDetailTarget("out", tc.urlPath); got != tc.want {
				t.Errorf("resolveDetailTarget(%q) = %q, want %q", tc.urlPath, got, tc.want)
			}
		})
	}
}
