package buildcmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The named acceptance criteria from docs/decisions/0001-generic-content-collections.md
// §5.4 and §6.1. TestGoldenProjects already pins every emitted byte; these
// tests exist on top of it so the INTENT behind three of those baselines is
// legible, and so a regression reports "the projects index grew hreflang
// alternates" instead of "4096 bytes differ".
//
// Q1 and Q2 assert behaviour the decision record classifies as BUGS. They are
// deliberately written as "this is still broken" tests. Fixing either is
// follow-up work (Q1 is one `passthrough: true`; Q2 needs the double-write in
// writePages/writePaginatedPages resolved) and must be its own reviewed PR —
// not a silent side effect of the collection migration.

var (
	hreflangLinkRE = regexp.MustCompile(`<link rel="alternate" hreflang="[^"]*" href="[^"]*">`)
	langBadgeRE    = regexp.MustCompile(`<span class="lang-badge" title="[^"]*">`)
	projectCardRE  = regexp.MustCompile(`<article class="project-card"`)
	locRE          = regexp.MustCompile(`<loc>([^<]*)</loc>`)
	projectTitleRE = regexp.MustCompile(`<h2 class="project-title">(?:<a [^>]*>)?([^<]+)`)
)

func readBuiltFile(t *testing.T, outDir string, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(outDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("reading %s from build output: %v", rel, err)
	}
	return string(raw)
}

func goldenCaseByName(t *testing.T, name string) goldenCase {
	t.Helper()
	for _, tc := range goldenCases {
		if tc.name == name {
			return tc
		}
	}
	t.Fatalf("no golden case named %q", name)
	return goldenCase{}
}

// TestProjectsQ2_CollectionPagesSkipHreflang pins decision record Q2.
//
// /projects/index.html is written TWICE: once by writePages (which injects
// hreflang alternates, validates the base href, and validates page structure)
// and then again by writePaginatedPages, which does none of those. The second
// write wins, so no collection page — index or paged — carries hreflang today.
//
// The home page is the CONTROL. Without it, "the projects index has no
// hreflang" would also pass on a site where hreflang never runs at all, and
// the test would be worthless.
func TestProjectsQ2_CollectionPagesSkipHreflang(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-real"))

	control := readBuiltFile(t, out, "index.html")
	controlLinks := hreflangLinkRE.FindAllString(control, -1)
	if len(controlLinks) == 0 {
		t.Fatalf("CONTROL FAILED: index.html has no hreflang alternates, so this " +
			"test cannot distinguish 'collection pages skip hreflang' from " +
			"'hreflang is not running at all'. Fix the fixture, not the assertion.")
	}

	for _, rel := range []string{"projects/index.html", "projects/page/2/index.html"} {
		got := hreflangLinkRE.FindAllString(readBuiltFile(t, out, rel), -1)
		if len(got) != 0 {
			t.Errorf("%s now carries %d hreflang alternate(s): %v.\n"+
				"This is decision-record Q2 changing behaviour. Collection pages "+
				"skip hreflang today because writePaginatedPages overwrites "+
				"writePages' output. If you INTENDED to fix that, do it in its own "+
				"PR and update this test plus the golden files together.",
				rel, len(got), got)
		}
	}
}

// TestProjectsQ1_LanguageBadgesAlwaysRenderOnMock pins decision record Q1.
//
// Every record in the mock content declares `available_languages`, and several
// declare exactly one language. projects.Project has no such field and yaml.v3
// is non-strict, so the key is silently dropped and the template's
// `{{if or (not $availLangs) (has $availLangs .prefix)}}` filter always takes
// the `not $availLangs` branch — i.e. EVERY badge renders on EVERY card.
//
// A map-passthrough loader would start honouring the field and change the
// rendered HTML on day one of the migration. That is exactly the regression
// this test exists to catch.
func TestProjectsQ1_LanguageBadgesAlwaysRenderOnMock(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-mock"))
	page := readBuiltFile(t, out, "projects/index.html")

	cards := len(projectCardRE.FindAllString(page, -1))
	badges := len(langBadgeRE.FindAllString(page, -1))
	if cards == 0 {
		t.Fatal("CONTROL FAILED: no project cards rendered on the mock index page")
	}

	// The fixture declares two lang_links (en, pt-BR), so "unfiltered" is
	// exactly 2 badges per card.
	const langLinks = 2
	if want := cards * langLinks; badges != want {
		t.Errorf("mock /projects/ rendered %d language badges across %d cards, want %d "+
			"(%d per card, i.e. UNFILTERED).\n"+
			"This is decision-record Q1 changing behaviour: available_languages is "+
			"set on every mock record but dropped by the typed projects.Project "+
			"struct, so badges must still render unconditionally. If a record "+
			"projection started passing the field through, that is a deliberate "+
			"fix and belongs in its own PR.",
			badges, cards, want, langLinks)
	}

	// Sharpen it: the first mock record declares only `pt`, so under any
	// filtering it would lose its English badge. Assert both survive.
	first := page
	if i := strings.Index(first, `<article class="project-card"`); i >= 0 {
		first = first[i:]
		if j := strings.Index(first, "</article>"); j >= 0 {
			first = first[:j]
		}
	}
	if !strings.Contains(first, "🇺🇸") || !strings.Contains(first, "🇧🇷") {
		t.Errorf("the first mock card (available_languages: [pt]) should still render "+
			"BOTH badges today; got card markup:\n%s", first)
	}
}

// TestProjectsQ3_SitemapCarriesBothProjectsPaths pins decision record Q3.
//
// generateSitemapFromPages emits "/" + page.Name ("/projects") for the static
// page, and writePaginatedPages separately contributes
// pagination.PageHref("/projects", 1) ("/projects/"). Both land in sitemap.xml.
// This is a known duplicate-entry bug; the baseline captures it as-is.
func TestProjectsQ3_SitemapCarriesBothProjectsPaths(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-real"))
	sitemap := readBuiltFile(t, out, "sitemap.xml")

	locs := make(map[string]int)
	for _, m := range locRE.FindAllStringSubmatch(sitemap, -1) {
		locs[m[1]]++
	}

	for _, want := range []string{
		"https://golden.example/projects",  // from generateSitemapFromPages
		"https://golden.example/projects/", // from writePaginatedPages
	} {
		if locs[want] == 0 {
			t.Errorf("sitemap.xml is missing <loc>%s</loc>; both the trailing-slash and "+
				"non-trailing-slash forms are present today (decision record Q3). "+
				"Losing one is a behaviour change — intended or not, it needs its own PR.\n"+
				"got locs: %v", want, locs)
		}
	}
}

// TestProjectsHomeCarouselHonoursItemsPerPage pins the `publish:` half of the
// collection: injectProjectsHomeCarousel writes the FIRST -items-per-page
// records to siteData["projects"], which the home page ranges over.
func TestProjectsHomeCarouselHonoursItemsPerPage(t *testing.T) {
	// pt-real runs at -items-per-page 2 over 3 records, so the carousel must
	// truncate to 2 — a limit of 0 or "all" would render 3.
	tc := goldenCaseByName(t, "pt-real")
	out := buildGoldenCase(t, tc)
	home := readBuiltFile(t, out, "index.html")

	got := len(regexp.MustCompile(`<article class="project-card project-card--preview">`).FindAllString(home, -1))
	if got != tc.itemsPerPage {
		t.Errorf("home carousel rendered %d preview cards, want %d (-items-per-page); "+
			"injectProjectsHomeCarousel must truncate siteData[\"projects\"]", got, tc.itemsPerPage)
	}
}

// TestProjectsSortedByOrderThenTitle pins the (order asc, title asc) comparator
// that LoadProjectsFile applies and that the collection engine's `sort:` must
// reproduce.
func TestProjectsSortedByOrderThenTitle(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-real-default"))
	page := readBuiltFile(t, out, "projects/index.html")

	var titles []string
	for _, m := range projectTitleRE.FindAllStringSubmatch(page, -1) {
		titles = append(titles, strings.TrimSpace(m[1]))
	}

	want := []string{
		"Production ML Platform",       // order: 1
		"LLM/RAG Application",          // order: 2
		"Feature Engineering Pipeline", // order: 3
	}
	if len(titles) != len(want) {
		t.Fatalf("expected %d project cards, got %d: %v", len(want), len(titles), titles)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Errorf("project %d is %q, want %q (sort is order asc, then title asc); full order: %v",
				i, titles[i], want[i], titles)
		}
	}
}
