package buildcmd

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/collections"
	"ffreis-website-compiler/internal/testutil"
)

// captureLogger returns a logger writing into buf, for asserting on warnings.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestEveryBuiltinCollectionRegistersItsSection pins the decision record's Q10
// requirement that a collection registers INTO sectionTable rather than
// carrying its own gating logic. A built-in whose section is not in the table
// would silently ignore -disable-sections, sitemap pruning and the dev payload.
func TestEveryBuiltinCollectionRegistersItsSection(t *testing.T) {
	known := make(map[string]bool, len(sectionTable))
	for _, s := range sectionTable {
		known[s.name] = true
	}
	for name, c := range collections.Builtins() {
		if c.Section == "" {
			t.Errorf("built-in collection %q declares no section", name)
			continue
		}
		if !known[c.Section] {
			t.Errorf("built-in collection %q declares section %q, which is not in sectionTable (sections.go); "+
				"-disable-sections, sitemap pruning and the dev-data payload all resolve against that table",
				name, c.Section)
		}
	}
}

// TestBuiltinIndexTemplatesAreGatedBySection guards the pairing the other
// direction: each collection's section must own the page template its index
// renders, or disabling the section would drop the collection's pages while
// leaving the base page behind.
func TestBuiltinIndexTemplatesAreGatedBySection(t *testing.T) {
	for name, c := range collections.Builtins() {
		if c.Index == nil || !c.Index.Enabled {
			continue
		}
		if got := pageSection(c.Index.Template); got != c.Section {
			t.Errorf("collection %q: page template %q maps to section %q, want %q",
				name, c.Index.Template, got, c.Section)
		}
	}
}

// TestCollectionOrderIsDeclarationOrder pins the ordering contract the sitemap
// depends on. collections.Order must NOT be alphabetical: before the migration
// each collection had its own writer call and projects' index pages were
// written before courses'. Sorting the names would swap them and rewrite
// sitemap.xml for no reason, which is exactly what the golden files exist to
// catch.
func TestCollectionOrderIsDeclarationOrder(t *testing.T) {
	got := collections.Order(collections.Builtins())
	want := []string{"projects", "courses", "listings"}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("collections.Order = %v, want %v (declaration order, not alphabetical — "+
			"see builtinOrder for why the order is part of the output contract)", got, want)
	}
	// Sharpen it: alphabetical would put courses first, so this asserts the
	// difference rather than a coincidence.
	if got[0] != "projects" {
		t.Errorf("courses sorts before projects alphabetically; Order must still yield projects first")
	}
}

// ── the -projects-file deprecation adapter ───────────────────────────────────

func TestLegacyProjectsFileFlagPopulatesCollectionSource(t *testing.T) {
	opts, err := parseBuildOptions([]string{"-projects-file", "/tmp/p.yaml"})
	if err != nil {
		t.Fatalf("parseBuildOptions: %v", err)
	}
	if got := opts.collectionSources["projects"]; got != "/tmp/p.yaml" {
		t.Errorf("collectionSources[projects] = %q, want /tmp/p.yaml", got)
	}
	if len(opts.deprecations) != 1 || !strings.Contains(opts.deprecations[0], "-projects-file is deprecated") {
		t.Errorf("expected a deprecation notice, got %v", opts.deprecations)
	}
}

func TestCollectionSourceFlagIsRepeatableAndValidated(t *testing.T) {
	opts, err := parseBuildOptions([]string{
		"-collection-source", "projects=/tmp/p.yaml",
		"-collection-source", "courses=/tmp/c.yaml",
	})
	if err != nil {
		t.Fatalf("parseBuildOptions: %v", err)
	}
	if opts.collectionSources["projects"] != "/tmp/p.yaml" || opts.collectionSources["courses"] != "/tmp/c.yaml" {
		t.Errorf("collectionSources = %v", opts.collectionSources)
	}

	for _, bad := range []string{"noequals", "=/tmp/p.yaml", "projects="} {
		if _, err := parseBuildOptions([]string{"-collection-source", bad}); err == nil {
			t.Errorf("expected -collection-source %q to be rejected", bad)
		}
	}

	if _, err := parseBuildOptions([]string{
		"-collection-source", "projects=/a.yaml",
		"-collection-source", "projects=/b.yaml",
	}); err == nil {
		t.Error("expected a duplicate -collection-source for the same collection to be rejected")
	}
}

// TestLegacyFlagLosesToExplicitCollectionSource keeps the precedence explicit
// and reported rather than silently resolved.
func TestLegacyFlagLosesToExplicitCollectionSource(t *testing.T) {
	opts, err := parseBuildOptions([]string{
		"-collection-source", "projects=/new.yaml",
		"-projects-file", "/old.yaml",
	})
	if err != nil {
		t.Fatalf("parseBuildOptions: %v", err)
	}
	if got := opts.collectionSources["projects"]; got != "/new.yaml" {
		t.Errorf("collectionSources[projects] = %q, want /new.yaml", got)
	}
	if len(opts.deprecations) != 1 || !strings.Contains(opts.deprecations[0], "was ignored") {
		t.Errorf("expected the clash to be reported, got %v", opts.deprecations)
	}
}

// TestMockGuardCoversCollectionSource pins decision record Q9 / §6.1: the
// /mock/ anti-leak guard enumerates content paths by hand, so -collection-source
// had to join that enumeration in the same change that introduced the flag.
func TestMockGuardCoversCollectionSource(t *testing.T) {
	for _, contentSource := range []string{"prod", "dev"} {
		t.Run(contentSource, func(t *testing.T) {
			_, err := parseBuildOptions([]string{
				"-collection-source", "projects=/checkout/projects/mock/projects.yaml",
				"-content-source", contentSource,
			})
			if err == nil {
				t.Fatalf("a /mock/ -collection-source path must be rejected with -content-source=%s", contentSource)
			}
			if !strings.Contains(err.Error(), "mock content cannot be used outside") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}

	// -content-source=mock is the one build that may reference /mock/ paths.
	if _, err := parseBuildOptions([]string{
		"-collection-source", "projects=/checkout/projects/mock/projects.yaml",
		"-content-source", "mock",
	}); err != nil {
		t.Errorf("-content-source=mock must accept a /mock/ path: %v", err)
	}
}

// ── end-to-end wiring ────────────────────────────────────────────────────────

// TestCollectionSourceMatchesLegacyProjectsFlagOutput proves the deprecated
// adapter is a true alias: both spellings must produce identical bytes.
func TestCollectionSourceMatchesLegacyProjectsFlagOutput(t *testing.T) {
	tc := goldenCaseByName(t, "pt-real")
	projectsFile, err := filepath.Abs(filepath.Join(goldenSiteDir, tc.projectsRel))
	if err != nil {
		t.Fatalf("resolving projects file: %v", err)
	}

	build := func(sourceFlag []string) string {
		outDir := t.TempDir()
		args := append([]string{
			flagWebsiteRoot, assembleGoldenSite(t, tc.lang),
			flagOut, outDir,
			"-clean-urls",
			"-items-per-page", "2",
		}, sourceFlag...)
		if err := Run(args, testutil.DiscardLogger()); err != nil {
			t.Fatalf("build with %v failed: %v", sourceFlag, err)
		}
		return string(mustReadFile(t, filepath.Join(outDir, "projects", "index.html")))
	}

	legacy := build([]string{"-projects-file", projectsFile})
	modern := build([]string{"-collection-source", "projects=" + projectsFile})
	if legacy != modern {
		t.Error("-projects-file and -collection-source projects=… produced different /projects/index.html; " +
			"the deprecated flag must be a pure alias")
	}
}

// TestCollectionSourceMatchesLegacyCoursesFlagOutput is the courses half of the
// alias proof, and covers the whole /courses tree rather than one page: the
// paginated index AND every per-record detail page must be byte-identical
// whichever flag spelling supplied the file.
func TestCollectionSourceMatchesLegacyCoursesFlagOutput(t *testing.T) {
	tc := goldenCaseByName(t, "pt-real")
	coursesFile, err := filepath.Abs(filepath.Join(goldenSiteDir, tc.coursesRel))
	if err != nil {
		t.Fatalf("resolving courses file: %v", err)
	}

	build := func(sourceFlag []string) map[string]string {
		outDir := t.TempDir()
		args := append([]string{
			flagWebsiteRoot, assembleGoldenSite(t, tc.lang),
			flagOut, outDir,
			"-clean-urls",
			"-items-per-page", "2",
		}, sourceFlag...)
		if err := Run(args, testutil.DiscardLogger()); err != nil {
			t.Fatalf("build with %v failed: %v", sourceFlag, err)
		}
		tree := courseDetailPages(t, outDir)
		tree["index"] = string(mustReadFile(t, filepath.Join(outDir, "courses", "index.html")))
		tree["sitemap"] = string(mustReadFile(t, filepath.Join(outDir, "sitemap.xml")))
		return tree
	}

	legacy := build([]string{"-courses-file", coursesFile})
	modern := build([]string{"-collection-source", "courses=" + coursesFile})

	if len(legacy) != len(modern) {
		t.Fatalf("-courses-file produced %d files, -collection-source produced %d", len(legacy), len(modern))
	}
	for name, want := range legacy {
		if modern[name] != want {
			t.Errorf("-courses-file and -collection-source courses=… produced different %s; "+
				"the deprecated flag must be a pure alias", name)
		}
	}
}

// TestDisabledCoursesSectionProducesNoCollectionOutput is the courses half of
// the section-gating proof. Unlike projects it also has to cover DETAIL pages
// and the prune hook: sectionTable's courses entry deletes
// pages.index.courses_section, without which the home page would still render
// an (empty) carousel block for a section that is supposed to be gone.
func TestDisabledCoursesSectionProducesNoCollectionOutput(t *testing.T) {
	tc := goldenCaseByName(t, "pt-real")
	coursesFile, err := filepath.Abs(filepath.Join(goldenSiteDir, tc.coursesRel))
	if err != nil {
		t.Fatalf("resolving courses file: %v", err)
	}

	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, assembleGoldenSite(t, tc.lang),
		flagOut, outDir,
		"-clean-urls",
		"-collection-source", "courses=" + coursesFile,
		"-disable-sections", "courses",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "courses")); !os.IsNotExist(err) {
		t.Errorf("expected no courses/ output when the section is disabled, got err=%v", err)
	}
	sitemapRaw := string(mustReadFile(t, filepath.Join(outDir, "sitemap.xml")))
	if strings.Contains(sitemapRaw, "/courses") {
		t.Errorf("disabled section still has sitemap entries:\n%s", sitemapRaw)
	}
	home := string(mustReadFile(t, filepath.Join(outDir, "index.html")))
	if strings.Contains(home, `class="courses-carousel"`) {
		t.Errorf("the home courses carousel survived -disable-sections courses; " +
			"sectionTable's prune hook must clear pages.index.courses_section")
	}
	// CONTROL: the rest of the build is intact, so "no /courses" is gating and
	// not a failed build silently producing nothing.
	if !strings.Contains(home, `class="site-heading"`) {
		t.Fatal("CONTROL FAILED: the home page did not render at all")
	}
}

// TestUnknownCollectionSourceFailsLoudly pins decision record §7.1: a typo must
// fail the build, not silently skip a whole section's content while every other
// guard still passes.
func TestUnknownCollectionSourceFailsLoudly(t *testing.T) {
	err := Run([]string{
		flagWebsiteRoot, assembleGoldenSite(t, "pt"),
		flagOut, t.TempDir(),
		"-clean-urls",
		"-collection-source", "projekts=/tmp/typo.yaml",
	}, testutil.DiscardLogger())

	if err == nil {
		t.Fatal("expected a build failure for an unknown collection name")
	}
	if !strings.Contains(err.Error(), "unknown collection") {
		t.Errorf("unexpected error: %v", err)
	}
	// The message must list what IS known, so the typo is obvious.
	if !strings.Contains(err.Error(), "projects") {
		t.Errorf("error should name the known collections: %v", err)
	}
}

func TestDeprecatedFlagLogsAWarning(t *testing.T) {
	tc := goldenCaseByName(t, "pt-real")
	projectsFile, err := filepath.Abs(filepath.Join(goldenSiteDir, tc.projectsRel))
	if err != nil {
		t.Fatalf("resolving projects file: %v", err)
	}

	var buf bytes.Buffer
	if err := Run([]string{
		flagWebsiteRoot, assembleGoldenSite(t, "pt"),
		flagOut, t.TempDir(),
		"-clean-urls",
		"-items-per-page", "2",
		"-projects-file", projectsFile,
	}, captureLogger(&buf)); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deprecated flag") || !strings.Contains(out, "-projects-file is deprecated") {
		t.Errorf("expected a slog warning about -projects-file, got:\n%s", out)
	}
}

// TestDisabledProjectsSectionProducesNoCollectionOutput checks the collection
// engine honours -disable-sections through sectionTable rather than around it.
func TestDisabledProjectsSectionProducesNoCollectionOutput(t *testing.T) {
	tc := goldenCaseByName(t, "pt-real")
	projectsFile, err := filepath.Abs(filepath.Join(goldenSiteDir, tc.projectsRel))
	if err != nil {
		t.Fatalf("resolving projects file: %v", err)
	}

	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, assembleGoldenSite(t, tc.lang),
		flagOut, outDir,
		"-clean-urls",
		"-collection-source", "projects=" + projectsFile,
		"-disable-sections", "projects",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "projects")); !os.IsNotExist(err) {
		t.Errorf("expected no projects/ output when the section is disabled, got err=%v", err)
	}
	sitemapRaw := string(mustReadFile(t, filepath.Join(outDir, "sitemap.xml")))
	if strings.Contains(sitemapRaw, "/projects") {
		t.Errorf("disabled section still has sitemap entries:\n%s", sitemapRaw)
	}
}

// TestSiteCollectionsConfigOverridesBuiltin proves §7.3(a)'s override half: a
// website's own collections.yaml replaces the built-in of the same name.
func TestSiteCollectionsConfigOverridesBuiltin(t *testing.T) {
	tc := goldenCaseByName(t, "pt-mock")
	projectsFile, err := filepath.Abs(filepath.Join(goldenSiteDir, tc.projectsRel))
	if err != nil {
		t.Fatalf("resolving projects file: %v", err)
	}
	websiteRoot := assembleGoldenSite(t, tc.lang)

	// Same as the built-in, but with passthrough on — which lights up the
	// available_languages badge filter the typed struct suppressed (Q1).
	writeCollectionsYAML(t, websiteRoot, `
version: 1
collections:
  projects:
    section: projects
    source: {kind: file, shape: list, path_from_flag: projects-file}
    records:
      passthrough: true
      fields:
        - {name: title, type: string, required: true}
        - {name: description, type: string}
        - {name: stack, type: list}
        - {name: href, type: string}
        - {name: date, type: string}
        - {name: order, type: int}
      sort: [{by: order}, {by: title}]
    publish:
      - {key: projects, value: records, limit_from_flag: items-per-page}
    index:
      enabled: true
      template: projects
      base_path: /projects
      paginate: {per_page_from_flag: items-per-page}
      inject: {items: pages.projects.items, pagination: pages.projects.pagination}
`)

	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-clean-urls",
		"-content-source", "mock",
		"-collection-source", "projects=" + projectsFile,
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	page := string(mustReadFile(t, filepath.Join(outDir, "projects", "index.html")))
	cards := strings.Count(page, `<article class="project-card"`)
	badges := strings.Count(page, `class="lang-badge"`)
	if cards == 0 {
		t.Fatal("no cards rendered")
	}
	// Bracket the expected count from BOTH sides. cards*2 is the unfiltered
	// count the built-in produces (asserted by TestProjectsQ1...); zero would
	// mean the badge markup broke entirely rather than that filtering worked.
	if badges >= cards*2 {
		t.Errorf("passthrough: true should let available_languages FILTER the badges "+
			"(%d cards, %d badges — still unfiltered), proving the site config replaced the built-in",
			cards, badges)
	}
	if badges == 0 {
		t.Errorf("CONTROL FAILED: %d cards rendered no badges at all, so this test is not "+
			"measuring filtering — check the fixture's lang_links", cards)
	}
}

func TestMissingCollectionsConfigFlagIsAnError(t *testing.T) {
	err := Run([]string{
		flagWebsiteRoot, assembleGoldenSite(t, "pt"),
		flagOut, t.TempDir(),
		"-clean-urls",
		"-collections-config", filepath.Join(t.TempDir(), "nope.yaml"),
	}, testutil.DiscardLogger())
	if err == nil || !strings.Contains(err.Error(), "collections config not found") {
		t.Errorf("expected a not-found error for -collections-config, got %v", err)
	}
}

func writeCollectionsYAML(t *testing.T, websiteRoot, content string) {
	t.Helper()
	path := filepath.Join(websiteRoot, collections.ConfigFileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
