package buildcmd

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The named acceptance criteria for the `courses` collection from
// docs/decisions/0001-generic-content-collections.md §5.4 (item 5), §6.1 and
// Q8. TestGoldenSite already pins every emitted byte; these tests exist on top
// of it so the INTENT behind those baselines is legible, and so a regression
// reports "the duration chip started rendering" instead of "2048 bytes differ".
//
// Q1 and Q2 assert behaviour the decision record classifies as BUGS. They are
// deliberately written as "this is still broken" tests. Fixing Q1 for courses is
// one `passthrough: true` (or one added field) in builtinCourses; fixing Q2
// needs the writePages/writePaginatedPages double-write resolved. Either is
// follow-up work in its own reviewed PR, not a silent side effect of the
// collection migration.

var (
	// The duration chip's leading glyph, as it appears in the fixture template
	// (⏱ written as a numeric character reference so the fixture file stays
	// ASCII). html/template passes it through verbatim, so this is exactly what
	// would show up in dist if duration_hours ever reached the template.
	durationChipRE = regexp.MustCompile(`<span class="course-meta-item">&#9201;`)
	levelChipRE    = regexp.MustCompile(`<span class="course-meta-item">&#128246;`)
	platformChipRE = regexp.MustCompile(`<span class="course-meta-item">&#127891;`)

	courseCardRE    = regexp.MustCompile(`<article class="course-card"`)
	courseTitleRE   = regexp.MustCompile(`<h2 class="course-title"><a href="[^"]*" class="course-title-link">([^<]+)`)
	coursePreviewRE = regexp.MustCompile(`<article class="course-card course-card--preview">`)
)

// courseDetailPages returns every generated /courses/<slug>/index.html, keyed by
// slug, so a test can assert over all of them rather than a hand-picked one.
func courseDetailPages(t *testing.T, outDir string) map[string]string {
	t.Helper()

	coursesDir := filepath.Join(outDir, "courses")
	entries, err := os.ReadDir(coursesDir)
	if err != nil {
		t.Fatalf("reading %s: %v", coursesDir, err)
	}

	pages := make(map[string]string)
	for _, e := range entries {
		// "page" holds the paginated index pages, not a course.
		if !e.IsDir() || e.Name() == "page" {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(coursesDir, e.Name(), "index.html"))
		if readErr != nil {
			t.Fatalf("reading detail page for %s: %v", e.Name(), readErr)
		}
		pages[e.Name()] = string(raw)
	}
	if len(pages) == 0 {
		t.Fatalf("CONTROL FAILED: no /courses/<slug>/index.html pages were generated in %s", outDir)
	}
	return pages
}

// TestCoursesQ1_DurationChipNeverRenders pins decision record Q1 in its courses
// form, and is the §5.4 item-5 gate for Phase 3.
//
// Every record in content/courses.yaml AND content/mock/courses.yaml sets
// duration_hours. courses.Course has no such field and yaml.v3 is non-strict, so
// the key is silently dropped and course.gohtml's
// `{{with dig $c "duration_hours"}}` chip never renders. builtinCourses' field
// list omits duration_hours deliberately (Passthrough: false) to reproduce that.
//
// The level and platform chips are the CONTROL: they come from the same
// <div class="course-meta"> block, so if they render and duration does not, the
// difference is the projection dropping the field — not the block being dead.
func TestCoursesQ1_DurationChipNeverRenders(t *testing.T) {
	for _, caseName := range []string{"pt-real", "pt-mock"} {
		t.Run(caseName, func(t *testing.T) {
			out := buildGoldenCase(t, goldenCaseByName(t, caseName))
			pages := courseDetailPages(t, out)

			var withLevel, withPlatform int
			for slug, page := range pages {
				if n := len(durationChipRE.FindAllString(page, -1)); n != 0 {
					t.Errorf("/courses/%s/ renders %d duration chip(s).\n"+
						"This is decision-record Q1 changing behaviour: duration_hours is set "+
						"on every course record but dropped by the typed courses.Course "+
						"struct, so the chip must still not render. If a record projection "+
						"started passing the field through, that is a deliberate fix and "+
						"belongs in its own PR (together with the golden files).", slug, n)
				}
				if levelChipRE.MatchString(page) {
					withLevel++
				}
				if platformChipRE.MatchString(page) {
					withPlatform++
				}
			}

			if withLevel == 0 {
				t.Fatal("CONTROL FAILED: no course detail page renders the level chip, so " +
					"'no duration chip' cannot be distinguished from 'the whole course-meta " +
					"block is dead'. Fix the fixture, not the assertion.")
			}
			if withPlatform == 0 {
				t.Fatal("CONTROL FAILED: no course detail page renders the platform chip " +
					"(same reason as the level control above).")
			}
		})
	}
}

// TestCoursesQ8_TwoPhaseRenderGuardHolds pins decision record Q8.
//
// course.gohtml is executed TWICE: once by sitegen.RenderPages with no
// .CurrentCourse (so asset-usage validation sees /css/course.css), and once per
// record by the detail writer. pages.course.internal keeps the first render off
// disk, and the template's {{if .CurrentCourse}} guard keeps it from erroring.
//
// A regression in either — the internal flag lost, the guard removed, or the
// detail template resolved from the FILTERED page list so no detail page is
// written at all — shows up as /course/index.html appearing in dist, or as the
// per-slug pages disappearing. Both are asserted here.
func TestCoursesQ8_TwoPhaseRenderGuardHolds(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-real"))

	// The singular "course" page must never reach disk, in either URL shape.
	for _, rel := range []string{"course/index.html", "course.html"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s exists in the build output. The detail template's "+
				"un-rendered (.CurrentCourse-less) validation pass has started being "+
				"written to disk — pages.course.internal or the {{if .CurrentCourse}} "+
				"guard regressed (decision record Q8).", rel)
		}
	}

	// CONTROL: the plural index page and the per-record detail pages must exist,
	// otherwise "no /course/index.html" would pass on a build that emitted no
	// course pages at all.
	if _, err := os.Stat(filepath.Join(out, "courses", "index.html")); err != nil {
		t.Fatalf("CONTROL FAILED: /courses/index.html was not generated: %v", err)
	}
	if got := len(courseDetailPages(t, out)); got != 3 {
		t.Errorf("expected 3 course detail pages from content/courses.yaml, got %d", got)
	}
}

// TestCoursesQ2_CollectionPagesSkipHreflang pins decision record Q2 for courses.
//
// Neither the paginated /courses/ index (overwritten by writePaginatedPages
// after writePages produced it) nor the /courses/<slug>/ detail pages (never
// produced by writePages at all) carry hreflang alternates today. The home page
// is the CONTROL — see the projects version of this test.
func TestCoursesQ2_CollectionPagesSkipHreflang(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-real"))

	control := readBuiltFile(t, out, "index.html")
	if len(hreflangLinkRE.FindAllString(control, -1)) == 0 {
		t.Fatalf("CONTROL FAILED: index.html has no hreflang alternates, so this " +
			"test cannot distinguish 'collection pages skip hreflang' from " +
			"'hreflang is not running at all'. Fix the fixture, not the assertion.")
	}

	rels := []string{"courses/index.html", "courses/page/2/index.html"}
	for slug := range courseDetailPages(t, out) {
		rels = append(rels, "courses/"+slug+"/index.html")
	}
	sort.Strings(rels)

	for _, rel := range rels {
		if got := hreflangLinkRE.FindAllString(readBuiltFile(t, out, rel), -1); len(got) != 0 {
			t.Errorf("%s now carries %d hreflang alternate(s): %v.\n"+
				"This is decision-record Q2 changing behaviour. Collection pages skip "+
				"hreflang today. If you INTENDED to fix that, do it in its own PR and "+
				"update this test plus the golden files together.", rel, len(got), got)
		}
	}
}

// TestCoursesQ3_SitemapCarriesBothCoursesPaths pins decision record Q3 for
// courses, and pins the detail entries' sitemap attributes alongside it —
// changefreq/priority are declared per collection (builtinCourses' Detail
// SitemapAttrs) and were hardcoded in writeCourseLandingPages before.
func TestCoursesQ3_SitemapCarriesBothCoursesPaths(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-real"))
	sitemapXML := readBuiltFile(t, out, "sitemap.xml")

	locs := make(map[string]int)
	for _, m := range locRE.FindAllStringSubmatch(sitemapXML, -1) {
		locs[m[1]]++
	}

	for _, want := range []string{
		"https://golden.example/courses",  // from generateSitemapFromPages
		"https://golden.example/courses/", // from writePaginatedPages
	} {
		if locs[want] == 0 {
			t.Errorf("sitemap.xml is missing <loc>%s</loc>; both the trailing-slash and "+
				"non-trailing-slash forms are present today (decision record Q3). "+
				"Losing one is a behaviour change — intended or not, it needs its own PR.\n"+
				"got locs: %v", want, locs)
		}
	}

	// Every detail entry carries changefreq monthly / priority 0.6.
	for slug := range courseDetailPages(t, out) {
		want := "<loc>https://golden.example/courses/" + slug + "/</loc>\n" +
			"    <changefreq>monthly</changefreq>\n" +
			"    <priority>0.6</priority>"
		if !strings.Contains(sitemapXML, want) {
			t.Errorf("sitemap.xml is missing the monthly/0.6 entry for /courses/%s/; "+
				"the collection's detail sitemap attrs must reproduce what "+
				"writeCourseLandingPages hardcoded.", slug)
		}
	}
}

// TestCoursesSlugifiedDetailPaths pins the Slugify-derived slug and the href
// derived from it. The three real titles exercise the transforms that matter:
// lowercasing, a colon and a slash collapsing to a single hyphen, and no
// trailing hyphen left behind.
func TestCoursesSlugifiedDetailPaths(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-real"))

	want := []string{
		"aws-for-ml-engineers-serverless-infrastructure",
		"mlops-in-production-from-notebook-to-platform",
		"retrieval-augmented-generation-build-production-rag-systems",
	}
	got := make([]string, 0, len(want))
	for slug := range courseDetailPages(t, out) {
		got = append(got, slug)
	}
	sort.Strings(got)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("course detail slugs are %v, want %v (Slugify of the title, since no "+
			"record sets an explicit slug)", got, want)
	}

	// The index card's href must be base_path + the derived href, and must
	// resolve to the page that was actually written.
	index := readBuiltFile(t, out, "courses/index.html")
	for _, slug := range want[:1] {
		href := `href="/pt/courses/` + slug + `/"`
		if !strings.Contains(index, href) && !strings.Contains(readBuiltFile(t, out, "courses/page/2/index.html"), href) {
			t.Errorf("no course card links to %s; the derived href (/courses/{{.slug}}/) "+
				"must still be prepended with base_path by the template, not by the engine "+
				"(decision record §3.4).", href)
		}
	}
}

// TestCoursesSortedByOrderThenTitle pins the (order asc, title asc) comparator
// LoadCoursesFile applies and the collection engine's `sort:` must reproduce.
func TestCoursesSortedByOrderThenTitle(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-real-default"))
	page := readBuiltFile(t, out, "courses/index.html")

	var titles []string
	for _, m := range courseTitleRE.FindAllStringSubmatch(page, -1) {
		titles = append(titles, strings.TrimSpace(m[1]))
	}

	want := []string{
		"MLOps in Production: From Notebook to Platform",               // order: 1
		"Retrieval-Augmented Generation: Build Production RAG Systems", // order: 2
		"AWS for ML Engineers: Serverless Infrastructure",              // order: 3
	}
	if len(titles) != len(want) {
		t.Fatalf("expected %d course cards, got %d: %v", len(want), len(titles), titles)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Errorf("course %d is %q, want %q (sort is order asc, then title asc); full order: %v",
				i, titles[i], want[i], titles)
		}
	}
}

// TestCoursesHomeCarouselHonoursItemsPerPage pins the `publish:` half of the
// courses collection: injectCoursesHomeCarousel writes the FIRST -items-per-page
// records to pages.index.courses_carousel_items, which the home page ranges over.
func TestCoursesHomeCarouselHonoursItemsPerPage(t *testing.T) {
	// pt-real runs at -items-per-page 2 over 3 records, so the carousel must
	// truncate to 2 — a limit of 0 or "all" would render 3.
	tc := goldenCaseByName(t, "pt-real")
	out := buildGoldenCase(t, tc)
	home := readBuiltFile(t, out, "index.html")

	if got := len(coursePreviewRE.FindAllString(home, -1)); got != tc.itemsPerPage {
		t.Errorf("home courses carousel rendered %d preview cards, want %d "+
			"(-items-per-page); the courses publish entry must truncate "+
			"pages.index.courses_carousel_items", got, tc.itemsPerPage)
	}
}

// TestCoursesEmptyCollectionStillWritesIndexPage pins decision record Q5 for
// courses: with no -courses-file the collection contributes nothing, so
// /courses/index.html is the plain writePages render showing the template's
// empty_state branch, and no detail page exists.
func TestCoursesEmptyCollectionStillWritesIndexPage(t *testing.T) {
	out := buildGoldenCase(t, goldenCaseByName(t, "pt-projects-only"))

	page := readBuiltFile(t, out, "courses/index.html")
	if !strings.Contains(page, "Nenhum curso por enquanto.") {
		t.Errorf("/courses/index.html should show pages.courses.empty_state when no "+
			"-courses-file is passed; got:\n%s", page)
	}
	if n := len(courseCardRE.FindAllString(page, -1)); n != 0 {
		t.Errorf("/courses/index.html rendered %d course cards with no content file", n)
	}
	if entries, err := os.ReadDir(filepath.Join(out, "courses")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				t.Errorf("no -courses-file was passed, but /courses/%s/ was generated", e.Name())
			}
		}
	}

	// CONTROL: hreflang injection still runs on this build, so the empty page is
	// genuinely the writePages render rather than a stray artefact.
	if len(hreflangLinkRE.FindAllString(readBuiltFile(t, out, "index.html"), -1)) == 0 {
		t.Fatal("CONTROL FAILED: this build produced no hreflang alternates at all")
	}
}
