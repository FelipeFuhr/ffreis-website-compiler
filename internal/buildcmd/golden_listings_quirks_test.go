package buildcmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The named acceptance criteria for the `listings` collection from
// docs/decisions/0001-generic-content-collections.md §1.3, §5.2, §6 (Phase 5)
// and Q7. TestGoldenSite already pins every emitted byte; these tests exist on
// top of it so the INTENT behind those baselines is legible, and so a
// regression reports "the listings array got re-sorted" instead of "8192 bytes
// differ".
//
// listings is the collection the decision record sequences LAST, because it is
// the only one whose defects ship silently: window.CASABOA_LISTINGS renders a
// perfectly valid page, passes linkcheck, and passes the sitemap check no
// matter how wrong it is. Everything below exists because of that.

var (
	// The rendered form of casaboa's base.gohtml line, copied verbatim into
	// the fixture's index.gohtml. Captures the JSON array as group 1.
	casaboaListingsRE = regexp.MustCompile(`window\.CASABOA_LISTINGS = (.*);`)

	listingDetailSectionRE = regexp.MustCompile(`<section id="listing-detail"`)
	listingSponsoredRE     = regexp.MustCompile(`<span class="badge sponsored">`)
	listingOrganicRE       = regexp.MustCompile(`<span class="badge">Organico</span>`)
)

// sourceListingOrder is the id order of content/listings.yaml, which is a
// verbatim copy of casaboa-data/site.d/30-listings.yaml.
//
// It is written out literally rather than re-parsed from the YAML on purpose:
// a test that derived the expectation from the same file the engine reads
// could not tell "order preserved" from "both sides re-sorted the same way".
// This slice is the independent record of what the file actually says.
var sourceListingOrder = []string{
	"pinheiros-72m2-001",
	"moinhos-94m2-002",
	"agua-verde-63m2-003",
	"jardins-88m2-004",
	"floresta-70m2-005",
	"batel-54m2-006",
	"savassi-105m2-007",
	"vila-madalena-58m2-008",
	"mooca-102m2-009",
	"cidade-baixa-48m2-010",
	"bela-vista-poa-85m2-011",
	"bigorrilho-67m2-012",
	"cabral-110m2-013",
	"lourdes-95m2-014",
	"buritis-60m2-015",
	"santo-antonio-75m2-016",
	"copacabana-55m2-017",
	"botafogo-80m2-018",
	"tijuca-65m2-019",
	"barra-tijuca-120m2-020",
	"ipanema-45m2-021",
	"trindade-70m2-022",
	"centro-floripa-58m2-023",
	"lagoa-conceicao-90m2-024",
	"coqueiros-62m2-025",
}

// listingDetailPages returns every generated /listings/<id>/index.html, keyed
// by id. Unlike courseDetailPages there is no "page" subdirectory to skip:
// listings has no paginated index at all.
func listingDetailPages(t *testing.T, outDir string) map[string]string {
	t.Helper()

	dir := filepath.Join(outDir, "listings")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	pages := make(map[string]string)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, e.Name(), "index.html"))
		if readErr != nil {
			t.Fatalf("reading detail page for %s: %v", e.Name(), readErr)
		}
		pages[e.Name()] = string(raw)
	}
	if len(pages) == 0 {
		t.Fatalf("CONTROL FAILED: no /listings/<id>/index.html pages were generated in %s", outDir)
	}
	return pages
}

// listingsBlob returns the parsed window.CASABOA_LISTINGS array from a built
// index.html, plus the raw JSON text.
func listingsBlob(t *testing.T, outDir string) ([]map[string]any, string) {
	t.Helper()

	m := casaboaListingsRE.FindStringSubmatch(readBuiltFile(t, outDir, "index.html"))
	if m == nil {
		t.Fatalf("index.html in %s has no window.CASABOA_LISTINGS line at all; the "+
			"fixture's index.gohtml must keep emitting it verbatim from casaboa's "+
			"base.gohtml, or the §1.3 byte contract is untested", outDir)
	}

	var parsed []map[string]any
	if err := json.Unmarshal([]byte(m[1]), &parsed); err != nil {
		t.Fatalf("window.CASABOA_LISTINGS is not valid JSON (%v): %s", err, m[1])
	}
	return parsed, m[1]
}

// blobIDOrder is the id sequence as it appears in the emitted JSON array.
func blobIDOrder(t *testing.T, records []map[string]any) []string {
	t.Helper()
	out := make([]string, 0, len(records))
	for i, rec := range records {
		id, ok := rec["id"].(string)
		if !ok {
			t.Fatalf("window.CASABOA_LISTINGS record %d has no string \"id\": %v", i, rec)
		}
		out = append(out, id)
	}
	return out
}

// TestListingsQ7_FileOrderPreserved is the decision record's Q7 gate.
//
// projects and courses re-sort by (order, title); listings must NOT, because
// the file order already encodes upstream ranking (the `score` field). The
// engine expresses that as an explicitly empty `Sort: []SortBy{}` in
// builtinListings, which is a single easy-to-"tidy-away" line.
//
// This test is deliberately NOT satisfied by the detail-page golden files:
// those are written to /listings/<id>/index.html, so the golden file SET is
// keyed by id and is completely order-insensitive. Two artefacts do encode the
// order — sitemap.xml (extraSitemapURLs is emitted in record order and
// generateSitemapFromPages does not re-sort it) and the JSON array in
// window.CASABOA_LISTINGS — and both are asserted here.
//
// The two CONTROLS are what keep this from passing vacuously: they prove that
// the alphabetical and score-descending orders a regression would produce are
// actually DIFFERENT from the file order, so "matches file order" is a real
// claim about this fixture rather than a coincidence.
func TestListingsQ7_FileOrderPreserved(t *testing.T) {
	// CONTROL 1: file order must differ from alphabetical, or a sort-by-id
	// regression would still pass.
	alphabetical := append([]string(nil), sourceListingOrder...)
	sort.Strings(alphabetical)
	if strings.Join(alphabetical, ",") == strings.Join(sourceListingOrder, ",") {
		t.Fatal("CONTROL FAILED: content/listings.yaml is already in alphabetical id " +
			"order, so this test cannot distinguish 'file order preserved' from " +
			"'records were sorted by id'. Fix the fixture, not the assertion.")
	}

	for _, caseName := range []string{"pt-real", "en-real", "pt-listings-only"} {
		t.Run(caseName, func(t *testing.T) {
			out := buildGoldenCase(t, goldenCaseByName(t, caseName))

			// (a) The JS blob's array order.
			records, _ := listingsBlob(t, out)
			gotBlob := blobIDOrder(t, records)
			if strings.Join(gotBlob, ",") != strings.Join(sourceListingOrder, ",") {
				t.Errorf("window.CASABOA_LISTINGS is in a different order than "+
					"content/listings.yaml.\n got:  %v\n want: %v\n"+
					"listings must preserve file order — it encodes upstream ranking "+
					"(decision record Q7). A `sort:` added to builtinListings, or the "+
					"empty Sort slice being 'tidied away', causes exactly this.",
					gotBlob, sourceListingOrder)
			}

			// (b) The sitemap's entry order.
			gotSitemap := sitemapListingIDs(t, out)
			if strings.Join(gotSitemap, ",") != strings.Join(sourceListingOrder, ",") {
				t.Errorf("sitemap.xml lists /listings/<id>/ entries in a different order "+
					"than content/listings.yaml.\n got:  %v\n want: %v",
					gotSitemap, sourceListingOrder)
			}

			// CONTROL 2: score-descending must also differ from file order, so a
			// regression that re-sorted by the ranking field would be caught too.
			byScore := listingIDsSortedByScore(t, records)
			if strings.Join(byScore, ",") == strings.Join(sourceListingOrder, ",") {
				t.Fatal("CONTROL FAILED: content/listings.yaml is already in " +
					"score-descending order, so this test cannot distinguish 'file order " +
					"preserved' from 'records were re-sorted by score'.")
			}
		})
	}
}

// sitemapListingIDs returns the ids of the /listings/<id>/ <loc> entries, in
// the order sitemap.xml lists them.
func sitemapListingIDs(t *testing.T, outDir string) []string {
	t.Helper()
	var out []string
	for _, m := range locRE.FindAllStringSubmatch(readBuiltFile(t, outDir, "sitemap.xml"), -1) {
		const prefix = "https://golden.example/listings/"
		if strings.HasPrefix(m[1], prefix) {
			out = append(out, strings.TrimSuffix(strings.TrimPrefix(m[1], prefix), "/"))
		}
	}
	return out
}

// listingIDsSortedByScore returns the ids ordered by descending score, which is
// the most plausible "helpful" re-sort someone could introduce.
func listingIDsSortedByScore(t *testing.T, records []map[string]any) []string {
	t.Helper()
	type scored struct {
		id    string
		score float64
	}
	items := make([]scored, 0, len(records))
	for i, rec := range records {
		score, ok := rec["score"].(float64)
		if !ok {
			t.Fatalf("window.CASABOA_LISTINGS record %d has no numeric \"score\": %v", i, rec)
		}
		id, _ := rec["id"].(string)
		items = append(items, scored{id: id, score: score})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].score > items[j].score })

	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.id)
	}
	return out
}

// TestListingsJSBlobByteContract pins decision record §1.3 — the exact shape of
// the window.CASABOA_LISTINGS line — as a permanent gate rather than the
// one-time manual diff the Phase 5 checklist calls for.
//
// The three things asserted are exactly the three the record names:
//
//  1. The per-entry key set and its lexicographic order (encoding/json sorts
//     map keys), including `href`, which is COMPUTED — a projection that lost
//     the derive would still render every detail page correctly and only break
//     app.js's links at runtime.
//  2. `sponsored` present on every record. §5.2 singles this out: it is typed
//     bool with a false default so an absent `sponsored:` still serialises as
//     false instead of vanishing from the JSON.
//  3. The `{{else}}` branch — a build with no listings emits a literal `[]`,
//     not `null` and not an empty string.
func TestListingsJSBlobByteContract(t *testing.T) {
	// The concrete per-entry key order from decision record §1.3.
	wantKeys := []string{
		"breakdown", "concern", "href", "id", "imageLabel", "locationLabel",
		"meta", "photoLabel", "place", "price", "priceLabel", "reasons",
		"score", "source", "sourceNote", "sponsored", "title", "tone",
		"total", "transaction",
	}

	out := buildGoldenCase(t, goldenCaseByName(t, "pt-listings-only"))
	records, raw := listingsBlob(t, out)

	if len(records) != len(sourceListingOrder) {
		t.Fatalf("window.CASABOA_LISTINGS has %d records, want %d", len(records), len(sourceListingOrder))
	}

	for i, rec := range records {
		got := make([]string, 0, len(rec))
		for k := range rec {
			got = append(got, k)
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(wantKeys, ",") {
			t.Errorf("record %d (%v) has key set %v, want %v — the §1.3 byte contract "+
				"is exactly 20 keys per entry, no more and no less",
				i, rec["id"], got, wantKeys)
		}
		if _, ok := rec["sponsored"].(bool); !ok {
			t.Errorf("record %d (%v) has sponsored=%v (%T), want a bool. Decision record "+
				"§5.2: sponsored is typed bool with a false default precisely so an "+
				"absent `sponsored:` cannot silently vanish from the JSON.",
				i, rec["id"], rec["sponsored"], rec["sponsored"])
		}
		if href, _ := rec["href"].(string); href != "/listings/"+rec["id"].(string)+"/" {
			t.Errorf("record %d has href=%q, want /listings/%v/ — href is a DERIVED field "+
				"and losing it breaks app.js's links without breaking any page",
				i, href, rec["id"])
		}
	}

	// json.Marshal emits no indentation and no trailing newline.
	if strings.ContainsAny(raw, "\n\t") {
		t.Errorf("window.CASABOA_LISTINGS contains whitespace formatting; toJSON is a "+
			"plain json.Marshal and must stay compact:\n%s", raw[:minInt(200, len(raw))])
	}

	// CONTROL: the empty branch. A cell with no -listings-file must render the
	// literal `[]`, which is what tells app.js "no listings" rather than
	// throwing on `null.filter`.
	empty := buildGoldenCase(t, goldenCaseByName(t, "pt-courses-only"))
	if _, rawEmpty := listingsBlob(t, empty); rawEmpty != "[]" {
		t.Errorf("a build with no listings emits window.CASABOA_LISTINGS = %s, want []", rawEmpty)
	}
}

// TestListingsQ8_TwoPhaseRenderGuardHolds is the listings form of the courses
// Q8 test: listing.gohtml is executed once with no .CurrentListing for
// asset-usage validation, and pages.listing.internal must keep that render off
// disk.
func TestListingsQ8_TwoPhaseRenderGuardHolds(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-listings-only"))

	for _, rel := range []string{"listing/index.html", "listing.html"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s exists in the build output. The detail template's "+
				"un-rendered (.CurrentListing-less) validation pass has started being "+
				"written to disk — pages.listing.internal or the {{if .CurrentListing}} "+
				"guard regressed (decision record Q8).", rel)
		}
	}

	// CONTROL: the per-record detail pages must exist, otherwise "no
	// /listing/index.html" would pass on a build that emitted nothing.
	pages := listingDetailPages(t, out)
	if len(pages) != len(sourceListingOrder) {
		t.Errorf("expected %d listing detail pages from content/listings.yaml, got %d",
			len(sourceListingOrder), len(pages))
	}
	for _, page := range pages {
		if !listingDetailSectionRE.MatchString(page) {
			t.Error("a listing detail page rendered without its #listing-detail section, " +
				"so the {{if .CurrentListing}} branch did not fire")
		}
	}
}

// TestListingsHasNoIndexPage pins Index.Enabled=false — listings is the first
// migrated collection with no index page of its own (casaboa's hub is a normal
// index.gohtml, client-rendered from the JS blob).
//
// The CONTROL is the courses index in a cell that HAS courses: without it,
// "no /listings/index.html" would also pass on a build where the index writer
// was broken for every collection.
func TestListingsHasNoIndexPage(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-real"))

	for _, rel := range []string{
		"listings/index.html",
		"listings/page/2/index.html",
		"listings.html",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s exists in the build output, but builtinListings declares "+
				"Index.Enabled=false — casaboa's hub page is a normal index.gohtml that "+
				"renders client-side from window.CASABOA_LISTINGS (decision record §5.2).", rel)
		}
	}

	if _, err := os.Stat(filepath.Join(out, "courses", "index.html")); err != nil {
		t.Fatalf("CONTROL FAILED: /courses/index.html was not generated either, so this "+
			"test cannot distinguish 'listings has no index' from 'no collection "+
			"produced an index': %v", err)
	}
}

// TestListingsSitemapAttrs pins the weekly/0.7 attributes builtinListings
// declares and writeListingDetailPages hardcoded before it.
//
// The courses entries are the CONTROL: they carry monthly/0.6 from the same
// mechanism, so a bug that stamped one collection's attrs onto every entry
// fails here.
func TestListingsSitemapAttrs(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-real"))
	sitemapXML := readBuiltFile(t, out, "sitemap.xml")

	for id := range listingDetailPages(t, out) {
		want := "<loc>https://golden.example/listings/" + id + "/</loc>\n" +
			"    <changefreq>weekly</changefreq>\n" +
			"    <priority>0.7</priority>"
		if !strings.Contains(sitemapXML, want) {
			t.Errorf("sitemap.xml is missing the weekly/0.7 entry for /listings/%s/; the "+
				"collection's detail sitemap attrs must reproduce what "+
				"writeListingDetailPages hardcoded.", id)
		}
	}

	if !strings.Contains(sitemapXML, "<changefreq>monthly</changefreq>") {
		t.Fatal("CONTROL FAILED: no monthly entry in sitemap.xml, so 'listings are " +
			"weekly' cannot be distinguished from 'every entry is weekly'.")
	}
}

// TestListingsQ2_DetailPagesSkipHreflang pins decision record Q2 for listings.
// The home page is the CONTROL — see the projects version of this test.
func TestListingsQ2_DetailPagesSkipHreflang(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-real"))

	control := readBuiltFile(t, out, "index.html")
	if len(hreflangLinkRE.FindAllString(control, -1)) == 0 {
		t.Fatalf("CONTROL FAILED: index.html has no hreflang alternates, so this " +
			"test cannot distinguish 'detail pages skip hreflang' from " +
			"'hreflang is not running at all'. Fix the fixture, not the assertion.")
	}

	for id, page := range listingDetailPages(t, out) {
		if got := hreflangLinkRE.FindAllString(page, -1); len(got) != 0 {
			t.Errorf("/listings/%s/ now carries %d hreflang alternate(s): %v.\n"+
				"This is decision-record Q2 changing behaviour. Detail pages skip "+
				"hreflang today.", id, len(got), got)
		}
	}
}

// TestListingsSponsoredRendersBothBranches is the §5.2 acid test in its
// page-level form.
//
// The fixture content has 7 sponsored:true and 18 sponsored:false records, so
// BOTH branches of the template's {{if dig $l "sponsored"}} must fire. If the
// projection ever dropped the field (or turned false into absent), the false
// records would take the same branch as... nothing — `dig` on a missing key and
// `dig` on false are indistinguishable to the template, which is exactly why
// the JSON-level assertion in TestListingsJSBlobByteContract exists alongside
// this one. This test's job is to prove the two branches are BOTH live, so that
// assertion is not checking a field nothing reads.
func TestListingsSponsoredRendersBothBranches(t *testing.T) {
	pages := listingDetailPages(t, buildGoldenCase(t, goldenCaseByName(t, "pt-listings-only")))

	var sponsored, organic int
	for _, page := range pages {
		if listingSponsoredRE.MatchString(page) {
			sponsored++
		}
		if listingOrganicRE.MatchString(page) {
			organic++
		}
	}

	if sponsored != 7 {
		t.Errorf("%d listing pages render the sponsored badge, want 7 "+
			"(content/listings.yaml sets sponsored: true on 7 records)", sponsored)
	}
	if organic != 18 {
		t.Errorf("%d listing pages render the organic badge, want 18 "+
			"(content/listings.yaml sets sponsored: false on 18 records)", organic)
	}
	if sponsored+organic != len(pages) {
		t.Errorf("sponsored(%d) + organic(%d) != %d pages: some page rendered neither "+
			"branch, which means `sponsored` reached the template as something other "+
			"than a bool", sponsored, organic, len(pages))
	}
}
