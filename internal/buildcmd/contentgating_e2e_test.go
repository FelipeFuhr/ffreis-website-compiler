package buildcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

// These tests build a whole site and then read what actually landed on disk.
//
// They exist because the unit tests one layer down prove that a gated item is
// filtered out of the item list, which is NOT the same claim as "it does not
// ship". A section or a draft leaves many traces — listing pages, per-item
// pages, the home-page block, RSS, sitemap.xml — and each is written by
// different code. This repo has already shipped a production incident of
// precisely that shape: a placeholder blog entry that survived in the listing
// data and rendered live. Filtering at load is the fix; these tests are the
// evidence that nothing downstream reintroduces it.

// Where a section's real output lands. The bare blog.html / projects.html are
// the un-paginated base-page renders and carry no items.
const (
	blogListing     = "blog/index.html"
	projectsListing = "projects/index.html"
	rssFeed         = "blog/feed.xml"
)

const (
	gatingRegistryAllOff = `{
  "version": 1,
  "project": "gating-test",
  "flags": [
    {"name": "section_blog",     "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false},
    {"name": "section_courses",  "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false},
    {"name": "section_projects", "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false}
  ]
}`

	gatingSiteYAML = `base_path: ""
title: Gating Test
pages:
  blog:
    title: Writing
    posts: []
  post:
    title: Post
    internal: true
  projects:
    title: Projects
    items: []
`

	gatingContract = `allowed:
  - title
  - base_path
  - pages.*.title
  - pages.*.posts
  - pages.*.items
`

	gatingBlogTmpl = `{{define "page"}}<h1>{{dig .SiteData "pages" "blog" "title"}}</h1>
{{range dig .SiteData "pages" "blog" "items"}}<article class="post-card"><a href="{{dig . "url"}}">{{dig . "title"}}</a></article>{{end}}{{end}}`

	gatingPostTmpl = `{{define "page"}}<article><h1>{{dig .SiteData "post" "title"}}</h1></article>{{end}}`

	gatingProjectsTmpl = `{{define "page"}}<h1>{{dig .SiteData "pages" "projects" "title"}}</h1>
{{range dig .SiteData "pages" "projects" "items"}}<article class="project-card">{{dig . "title"}}</article>{{end}}{{end}}`

	gatingIndexTmpl = `{{define "page"}}<h1>{{dig .SiteData "title"}}</h1>
{{range dig .SiteData "pages" "blog" "posts"}}<a class="home-post" href="{{dig . "url"}}">{{dig . "title"}}</a>{{end}}
{{range dig .SiteData "projects"}}<span class="home-project">{{dig . "title"}}</span>{{end}}{{end}}`
)

// gatingSite builds a website root with blog, post, projects and index templates.
func gatingSite(t *testing.T) string {
	t.Helper()
	root := newTestWebsiteRoot(t)
	testutil.MustMkdirAll(t, filepath.Join(root, "flags"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(root, "src", "assets", "css", fileMainCSS):            mainCSSContent,
		filepath.Join(root, "src", "data", fileSiteYAML):                    gatingSiteYAML,
		filepath.Join(root, "src", "data", fileSiteContractYAML):            gatingContract,
		filepath.Join(root, "flags", "flags.json"):                          gatingRegistryAllOff,
		filepath.Join(root, "src", "templates", "pages", "blog.gohtml"):     gatingBlogTmpl,
		filepath.Join(root, "src", "templates", "pages", "post.gohtml"):     gatingPostTmpl,
		filepath.Join(root, "src", "templates", "pages", "projects.gohtml"): gatingProjectsTmpl,
		filepath.Join(root, "src", "templates", "pages", "index.gohtml"):    gatingIndexTmpl,
	})
	return root
}

// gatingContent writes one published and one draft item of each content type.
func gatingContent(t *testing.T) (postsDir, projectsFile string) {
	t.Helper()
	dir := t.TempDir()

	postsDir = filepath.Join(dir, "posts")
	for _, p := range []struct{ slug, title, draft string }{
		{"published-post", "Published Post", ""},
		{"secret-draft-post", "Secret Draft Post", "draft: true\n"},
	} {
		postDir := filepath.Join(postsDir, p.slug)
		testutil.MustMkdirAll(t, postDir)
		body := "---\ntitle: \"" + p.title + "\"\ndate: \"2026-08-01\"\nslug: \"" + p.slug + "\"\n" +
			"summary: \"s\"\n" + p.draft + "---\n\nBody text.\n"
		testutil.WriteFiles(t, map[string]string{filepath.Join(postDir, "index.md"): body})
	}

	projectsFile = filepath.Join(dir, "projects.yaml")
	testutil.WriteFiles(t, map[string]string{projectsFile: `
- title: "Published Project"
  description: "d"
  order: 1
- title: "Secret Draft Project"
  description: "d"
  order: 2
  draft: true
`})
	return postsDir, projectsFile
}

// buildGated runs a full build and returns every generated file's contents keyed
// by its path relative to the output directory.
func buildGated(t *testing.T, extraArgs ...string) map[string]string {
	t.Helper()
	root := gatingSite(t)
	postsDir, projectsFile := gatingContent(t)
	outDir := t.TempDir()

	args := append([]string{
		flagWebsiteRoot, root,
		flagOut, outDir,
		"-posts-dir", postsDir,
		"-projects-file", projectsFile,
		"-sitemap-base-url", "https://example.com",
		"-strict-contract=false",
	}, extraArgs...)

	if err := Run(args, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	out := map[string]string{}
	err := filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(outDir, path)
		if relErr != nil {
			return relErr
		}
		out[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatalf("walking output: %v", err)
	}
	return out
}

// assertAbsentEverywhere fails naming the file that leaked, so a failure says
// which surface reintroduced the item rather than merely that one did.
func assertAbsentEverywhere(t *testing.T, out map[string]string, needle string) {
	t.Helper()
	for path, body := range out {
		if strings.Contains(body, needle) {
			t.Errorf("%q leaked into %s", needle, path)
		}
	}
}

func assertPresentIn(t *testing.T, out map[string]string, path, needle string) {
	t.Helper()
	body, ok := out[path]
	if !ok {
		t.Fatalf("expected %s to be generated; got files: %v", path, sortedKeys(out))
	}
	if !strings.Contains(body, needle) {
		t.Errorf("expected %q in %s", needle, path)
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ── Wanted scenario: dev ─────────────────────────────────────────────────────

// The dev shape — sections opted in, drafts admitted — must actually publish
// everything, or the "prod ships nothing" tests below would pass vacuously.
func TestGating_DevBuildPublishesEverythingIncludingDrafts(t *testing.T) {
	out := buildGated(t, "-enable-sections", "blog,courses,projects", "-include-drafts")

	assertPresentIn(t, out, blogListing, "Published Post")
	assertPresentIn(t, out, blogListing, "Secret Draft Post")
	assertPresentIn(t, out, projectsListing, "Published Project")
	assertPresentIn(t, out, projectsListing, "Secret Draft Project")

	if _, ok := out["blog/secret-draft-post/index.html"]; !ok {
		t.Errorf("expected the draft post's own page to be generated on dev")
	}
	assertPresentIn(t, out, rssFeed, "Secret Draft Post")
}

// ── Unwanted scenario: the prod shape ────────────────────────────────────────

// The landmine, at build level: a prod-shaped build passes NO section
// configuration at all. Nothing not-ready may appear on any surface.
func TestGating_ProdBuildWithNoSectionConfigShipsNothing(t *testing.T) {
	out := buildGated(t)

	for _, gone := range []string{
		"Published Post", "Secret Draft Post",
		"Published Project", "Secret Draft Project",
	} {
		assertAbsentEverywhere(t, out, gone)
	}
	for _, page := range []string{blogListing, projectsListing, rssFeed, "blog.html", "projects.html"} {
		if _, ok := out[page]; ok {
			t.Errorf("expected %s not to be generated when its section is gated off", page)
		}
	}
	// The home page must still build — gating a section is not breaking the site.
	assertPresentIn(t, out, fileIndexHTML, "Gating Test")
}

// ── Unwanted scenario: the draft gate, alone ─────────────────────────────────

// The two gates are independent. With every section fully enabled and the
// published items shipping normally, a draft must still leave no trace — not in
// the listing, not as its own page, not in RSS, not in sitemap.xml, not in the
// home-page block.
func TestGating_DraftLeavesNoTraceWhenSectionsAreEnabled(t *testing.T) {
	out := buildGated(t, "-enable-sections", "blog,courses,projects")

	// Control: the published items must be present, or "absent" proves nothing.
	assertPresentIn(t, out, blogListing, "Published Post")
	assertPresentIn(t, out, projectsListing, "Published Project")
	assertPresentIn(t, out, rssFeed, "Published Post")

	assertAbsentEverywhere(t, out, "Secret Draft Post")
	assertAbsentEverywhere(t, out, "Secret Draft Project")
	assertAbsentEverywhere(t, out, "secret-draft-post")

	if _, ok := out["blog/secret-draft-post/index.html"]; ok {
		t.Errorf("draft post generated its own page despite -include-drafts being absent")
	}
}

// Each surface is written by different code, so name them individually: a
// failure should say "sitemap" or "RSS", not just "something leaked".
func TestGating_DraftAbsentFromEachIndividualSurface(t *testing.T) {
	out := buildGated(t, "-enable-sections", "blog,courses,projects")

	for _, tc := range []struct{ surface, path string }{
		{"blog listing", blogListing},
		{"RSS feed", rssFeed},
		{"sitemap.xml", "sitemap.xml"},
		{"home page", fileIndexHTML},
		{"projects listing", projectsListing},
	} {
		t.Run(tc.surface, func(t *testing.T) {
			body, ok := out[tc.path]
			if !ok {
				t.Skipf("%s not generated in this configuration", tc.path)
			}
			for _, needle := range []string{"Secret Draft Post", "Secret Draft Project", "secret-draft-post"} {
				if strings.Contains(body, needle) {
					t.Errorf("%s contains %q", tc.surface, needle)
				}
			}
		})
	}
}

// ── Unwanted scenario: a gate flipped on deliberately ────────────────────────

// A registry default of true is honoured — the mechanism is a declaration, not a
// hardcoded "everything off". This is the test that would catch the gate being
// welded shut and the whole registry becoming decorative.
func TestGating_RegistryDefaultTrueShipsTheSection(t *testing.T) {
	root := gatingSite(t)
	postsDir, projectsFile := gatingContent(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(root, "flags", "flags.json"): strings.Replace(
			gatingRegistryAllOff,
			`{"name": "section_projects", "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false}`,
			`{"name": "section_projects", "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": true}`,
			1),
	})

	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, root, flagOut, outDir,
		"-posts-dir", postsDir, "-projects-file", projectsFile,
		"-sitemap-base-url", "https://example.com",
		"-strict-contract=false",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	raw, err := os.ReadFile(filepath.Join(outDir, projectsListing))
	if err != nil {
		t.Fatalf("expected projects page when its gate defaults to true: %v", err)
	}
	if !strings.Contains(string(raw), "Published Project") {
		t.Errorf("expected the published project to render")
	}
	// Blog still defaults false, so enabling one gate must not enable its neighbours.
	if _, err := os.Stat(filepath.Join(outDir, blogListing)); !os.IsNotExist(err) {
		t.Errorf("blog gate defaults false; enabling projects must not enable blog")
	}
}

// ── Unwanted scenario: misconfiguration must fail the build ──────────────────

func TestGating_BuildFailsOnMisconfiguredRegistry(t *testing.T) {
	postsDir, projectsFile := gatingContent(t)

	for _, tc := range []struct {
		name     string
		registry string
		args     []string
		wantErr  string
	}{
		{
			name:     "malformed JSON",
			registry: `{"version": 1, "flags": [`,
			wantErr:  "parsing flag registry",
		},
		{
			name:     "gate missing for a known section",
			registry: `{"version": 1, "flags": [{"name": "section_blog", "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false}]}`,
			wantErr:  "missing gate flags",
		},
		{
			name:     "enable names an undeclared section",
			registry: gatingRegistryAllOff,
			args:     []string{"-enable-sections", "blogs"},
			wantErr:  `not declared in`,
		},
		{
			name:     "gate declared for an unknown section",
			registry: strings.Replace(gatingRegistryAllOff, `"flags": [`, `"flags": [{"name": "section_podcast", "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false},`, 1),
			wantErr:  "podcast",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := gatingSite(t)
			testutil.WriteFiles(t, map[string]string{
				filepath.Join(root, "flags", "flags.json"): tc.registry,
			})
			args := append([]string{
				flagWebsiteRoot, root, flagOut, t.TempDir(),
				"-posts-dir", postsDir, "-projects-file", projectsFile,
				"-strict-contract=false",
			}, tc.args...)

			err := Run(args, testutil.DiscardLogger())
			if err == nil {
				t.Fatalf("expected the build to fail; a misconfigured gate must never resolve to a default")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// ── Unwanted scenario: mock content reaching a prod build ────────────────────

// The 2026-07 incident in build form: a corpus marked as mock must stop a
// non-mock build even when nothing in the path says "mock".
func TestGating_MarkedMockCorpusFailsAProdBuild(t *testing.T) {
	root := gatingSite(t)
	postsDir, projectsFile := gatingContent(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(filepath.Dir(postsDir), mockMarkerFile): "generated corpus\n",
	})

	err := Run([]string{
		flagWebsiteRoot, root, flagOut, t.TempDir(),
		"-posts-dir", postsDir, "-projects-file", projectsFile,
		"-enable-sections", "blog,courses,projects",
		"-strict-contract=false",
	}, testutil.DiscardLogger())

	if err == nil || !strings.Contains(err.Error(), mockMarkerFile) {
		t.Fatalf("err = %v, want the build to refuse marked mock content", err)
	}

	// Control: the same corpus is fine in an explicitly-mock build.
	if err := Run([]string{
		flagWebsiteRoot, root, flagOut, t.TempDir(),
		"-posts-dir", postsDir, "-projects-file", projectsFile,
		"-enable-sections", "blog,courses,projects",
		"-content-source", contentSourceMock,
		"-sitemap-base-url", "https://example.com",
		"-strict-contract=false",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("a mock build must accept mock content: %v", err)
	}
}
