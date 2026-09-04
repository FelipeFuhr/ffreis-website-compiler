package buildcmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"sort"
	"strings"
	"testing"
	"time"

	"ffreis-website-compiler/internal/testutil"
)

// Phase 0 of docs/decisions/0001-generic-content-collections.md: byte-for-byte
// baselines for everything the `projects` content collection emits, captured
// from the UNMODIFIED engine (internal/projects + injectProjectsHomeCarousel +
// writeProjectPages) and re-asserted unchanged against the collection engine
// that replaces it in Phase 2.
//
// The decision record is explicit that three of these baselines pin current
// BUGS (Q1, Q2, Q3). They are asserted by name below, with the reason, so that
// a well-meaning "fix" during the migration fails loudly instead of quietly
// rewriting a baseline. Fixing them is deliberate follow-up work, not a
// side effect of the refactor.
//
// See testdata/goldensite/README.md for why the fixture is vendored rather
// than driving the live ffreis-website checkout.

var updateGolden = flag.Bool("update-golden", false,
	"rewrite internal/buildcmd/testdata/goldensite/golden from the current build output")

const goldenSiteDir = "testdata/goldensite"

// goldenCase is one cell of the language × content matrix.
type goldenCase struct {
	name string
	lang string // pt | en — selects the data/<lang> overlay

	// projectsRel is the -projects-file path, relative to testdata/goldensite.
	projectsRel string
	// contentSource is the -content-source value. The mock cells MUST pass
	// "mock": their content path contains a /mock/ segment and the anti-leak
	// guard in parseBuildOptions (decision record Q9) rejects it otherwise.
	contentSource string
	// itemsPerPage is -items-per-page.
	itemsPerPage int
}

// goldenCases is the matrix. Real content is 3 records and mock content is 100.
//
// The two real-content cells run at -items-per-page 2 rather than the
// deployer's default of 12: 3 records at 12/page is a single page, and the
// baseline has to cover /projects/page/2/index.html. The deployer's actual
// production shape (real content, 12/page, exactly one page and no page/2) is
// pinned separately by the pt-real-default cell, whose golden tree asserts the
// absence of page/2 simply by not containing it.
var goldenCases = []goldenCase{
	{name: "pt-real", lang: "pt", projectsRel: "content/projects.yaml", contentSource: "prod", itemsPerPage: 2},
	{name: "en-real", lang: "en", projectsRel: "content/projects.yaml", contentSource: "prod", itemsPerPage: 2},
	{name: "pt-mock", lang: "pt", projectsRel: "content/mock/projects.yaml", contentSource: "mock", itemsPerPage: 12},
	{name: "en-mock", lang: "en", projectsRel: "content/mock/projects.yaml", contentSource: "mock", itemsPerPage: 12},
	{name: "pt-real-default", lang: "pt", projectsRel: "content/projects.yaml", contentSource: "prod", itemsPerPage: 12},
}

func TestGoldenProjects(t *testing.T) {
	for _, tc := range goldenCases {
		t.Run(tc.name, func(t *testing.T) {
			out := buildGoldenCase(t, tc)
			got := snapshotGoldenOutput(t, out)
			compareGolden(t, tc.name, got)
		})
	}
}

// ── the build ────────────────────────────────────────────────────────────────

// buildGoldenCase assembles the fixture website for tc.lang, runs a build with
// the same flag shape the deployer uses, and returns the output directory.
func buildGoldenCase(t *testing.T, tc goldenCase) string {
	t.Helper()

	websiteRoot := assembleGoldenSite(t, tc.lang)
	outDir := t.TempDir()

	projectsFile, err := filepath.Abs(filepath.Join(goldenSiteDir, tc.projectsRel))
	if err != nil {
		t.Fatalf("resolving projects file: %v", err)
	}

	args := []string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		// -clean-urls is what the deployer always passes (deploy.yml), and it is
		// what makes /projects/index.html the double-written path behind Q2.
		"-clean-urls",
		"-projects-file", projectsFile,
		"-content-source", tc.contentSource,
		"-items-per-page", fmt.Sprint(tc.itemsPerPage),
	}
	if err := Run(args, testutil.DiscardLogger()); err != nil {
		t.Fatalf("golden build %s failed: %v", tc.name, err)
	}
	return outDir
}

// assembleGoldenSite merges the fixture's website half and its per-language
// data half exactly the way ffreis-website-deployer's deploy.yml "Inject data
// into website" step does: shared site.d, then <lang>/site.d over it (same-named
// files replace), then <lang>/site.yaml.
func assembleGoldenSite(t *testing.T, lang string) string {
	t.Helper()

	websiteRoot := t.TempDir()
	copyTree(t, filepath.Join(goldenSiteDir, "site"), websiteRoot)

	dataDir := filepath.Join(goldenSiteDir, "data", lang)
	copyTree(t, filepath.Join(dataDir, "site.d"), filepath.Join(websiteRoot, "src", "data", "site.d"))
	copyGoldenFile(t,
		filepath.Join(dataDir, "site.yaml"),
		filepath.Join(websiteRoot, "src", "data", "site.yaml"),
	)
	return websiteRoot
}

// goldenMtime is stamped onto every file copied into the assembled fixture.
//
// generateSitemapFromPages derives each static page's <lastmod> from the mtime
// of its page template file (buildcmd.go). A fresh `git clone` or `git
// checkout` sets mtimes to the time of the checkout, so without pinning them
// the sitemap golden would encode "the day CI ran" and fail on every machine
// but the one that generated it. Pinning here keeps the sitemap comparison
// byte-exact rather than forcing a fuzzy match that would stop noticing real
// lastmod regressions.
var goldenMtime = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if mkErr := os.MkdirAll(filepath.Dir(target), 0o755); mkErr != nil {
			return mkErr
		}
		if writeErr := os.WriteFile(target, raw, 0o644); writeErr != nil {
			return writeErr
		}
		return os.Chtimes(target, goldenMtime, goldenMtime)
	})
	if err != nil {
		t.Fatalf("copying %s -> %s: %v", src, dst, err)
	}
}

func copyGoldenFile(t *testing.T, src, dst string) {
	t.Helper()
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("creating dir for %s: %v", dst, err)
	}
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		t.Fatalf("writing %s: %v", dst, err)
	}
	if err := os.Chtimes(dst, goldenMtime, goldenMtime); err != nil {
		t.Fatalf("stamping mtime on %s: %v", dst, err)
	}
}

// ── snapshot + compare ───────────────────────────────────────────────────────

// snapshotGoldenOutput collects the build artefacts the projects collection is
// responsible for, keyed by slash-separated dist-relative path:
//
//   - projects/**              every generated index / paged page
//   - sitemap.xml              Q3's duplicate /projects + /projects/ entries
//   - index.html               the home carousel (the `publish:` half) AND the
//     hreflang control for Q2
//
// Collecting the whole projects/ subtree rather than two named files means a
// page that appears or disappears fails the diff on its own.
func snapshotGoldenOutput(t *testing.T, outDir string) map[string]string {
	t.Helper()

	got := make(map[string]string)
	err := filepath.WalkDir(outDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(outDir, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel != "sitemap.xml" && rel != "index.html" && !strings.HasPrefix(rel, "projects/") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		got[rel] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", outDir, err)
	}
	if len(got) == 0 {
		t.Fatalf("snapshot of %s is empty — the build produced no projects output at all", outDir)
	}
	return got
}

// compareGolden diffs the snapshot against the committed baseline, or rewrites
// the baseline under -update-golden.
func compareGolden(t *testing.T, caseName string, got map[string]string) {
	t.Helper()

	goldenDir := filepath.Join(goldenSiteDir, "golden", caseName)
	if *updateGolden {
		writeGolden(t, goldenDir, got)
		return
	}

	want := readGolden(t, goldenDir)
	assertSameFileSet(t, want, got)

	for _, name := range sortedKeys(got) {
		if w, ok := want[name]; ok && w != got[name] {
			t.Errorf("%s/%s differs from golden (%d bytes want, %d got).\n"+
				"If this change is INTENDED and reviewed, regenerate with:\n"+
				"  go test ./internal/buildcmd -run TestGoldenProjects -update-golden\n"+
				"first differing line: %s",
				caseName, name, len(w), len(got[name]), firstDiffLine(w, got[name]))
		}
	}
}

func assertSameFileSet(t *testing.T, want, got map[string]string) {
	t.Helper()
	for _, name := range sortedKeys(want) {
		if _, ok := got[name]; !ok {
			t.Errorf("golden file %s was not produced by this build", name)
		}
	}
	for _, name := range sortedKeys(got) {
		if _, ok := want[name]; !ok {
			t.Errorf("build produced %s, which has no golden baseline", name)
		}
	}
}

func writeGolden(t *testing.T, goldenDir string, got map[string]string) {
	t.Helper()
	if err := os.RemoveAll(goldenDir); err != nil {
		t.Fatalf("clearing %s: %v", goldenDir, err)
	}
	for name, content := range got {
		target := filepath.Join(goldenDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("creating dir for %s: %v", target, err)
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", target, err)
		}
	}
	t.Logf("updated golden baseline %s (%d files)", goldenDir, len(got))
}

func readGolden(t *testing.T, goldenDir string) map[string]string {
	t.Helper()
	want := make(map[string]string)
	err := filepath.WalkDir(goldenDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(goldenDir, path)
		if relErr != nil {
			return relErr
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		want[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatalf("reading golden baseline %s: %v "+
			"(generate it with: go test ./internal/buildcmd -run TestGoldenProjects -update-golden)",
			goldenDir, err)
	}
	return want
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// firstDiffLine returns a short description of where two documents diverge, so
// a failure message points at the change instead of dumping two whole pages.
func firstDiffLine(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	for i := 0; i < len(wantLines) && i < len(gotLines); i++ {
		if wantLines[i] != gotLines[i] {
			return fmt.Sprintf("line %d:\n  want: %s\n  got:  %s", i+1, wantLines[i], gotLines[i])
		}
	}
	return fmt.Sprintf("line %d: one document is a prefix of the other (want %d lines, got %d)",
		minInt(len(wantLines), len(gotLines))+1, len(wantLines), len(gotLines))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
