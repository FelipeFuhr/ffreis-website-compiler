package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProjects(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "projects.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

const showcaseYAML = `
- title: "Flemming"
  description: "A Portuguese-language course platform."
  order: 1
  stack: [Go, Rust, Terraform]
  links:
    - label: "Visit the site"
      href: "https://flemming.com.br"
      external: true
    - label: "Read the write-up"
      href: "/blog/building-flemming/"
- title: "Older thing"
  description: "Single-link legacy entry."
  order: 2
  href: "/contact/"
`

func TestLoadProjectsFile_ParsesMultipleLinks(t *testing.T) {
	list, err := LoadProjectsFile(writeProjects(t, showcaseYAML))
	if err != nil {
		t.Fatalf("LoadProjectsFile: %v", err)
	}
	if len(list[0].Links) != 2 {
		t.Fatalf("links = %d, want 2", len(list[0].Links))
	}
	if got := list[0].Links[0]; got.Label != "Visit the site" || !got.External {
		t.Errorf("first link = %+v, want the external site link", got)
	}
	if got := list[0].Links[1]; got.Href != "/blog/building-flemming/" || got.External {
		t.Errorf("second link = %+v, want the internal write-up link", got)
	}
}

// The single-href form predates links and still has to work unchanged.
func TestLoadProjectsFile_LegacyHrefStillWorks(t *testing.T) {
	list, err := LoadProjectsFile(writeProjects(t, showcaseYAML))
	if err != nil {
		t.Fatalf("LoadProjectsFile: %v", err)
	}
	if list[1].Href != "/contact/" || len(list[1].Links) != 0 {
		t.Fatalf("legacy entry = %+v, want href set and no links", list[1])
	}
}

func TestLoadProjectsFile_RejectsIncompleteLink(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"no href", "- title: t\n  links:\n    - label: L\n", "'href'"},
		{"no label", "- title: t\n  links:\n    - href: /x/\n", "'label'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadProjectsFile(writeProjects(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestToSiteDataList_ExposesLinksToTemplates(t *testing.T) {
	list, err := LoadProjectsFile(writeProjects(t, showcaseYAML))
	if err != nil {
		t.Fatalf("LoadProjectsFile: %v", err)
	}
	item, ok := ToSiteDataList(list, "")[0].(map[string]any)
	if !ok {
		t.Fatal("site data item is not a map")
	}
	links, ok := item["links"].([]any)
	if !ok || len(links) != 2 {
		t.Fatalf("links = %v, want two template-visible entries", item["links"])
	}
	first, ok := links[0].(map[string]any)
	if !ok || first["label"] != "Visit the site" || first["external"] != true {
		t.Fatalf("first link = %v, want label+external exposed", links[0])
	}
}

func TestWithoutDrafts_DropsDraftEntries(t *testing.T) {
	list, err := LoadProjectsFile(writeProjects(t, `
- title: "Published"
  description: d
  order: 1
- title: "In progress"
  description: d
  order: 2
  draft: true
`))
	if err != nil {
		t.Fatalf("LoadProjectsFile: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("loaded %d entries, want both (drafts are filtered by the build, not the loader)", len(list))
	}
	kept := WithoutDrafts(list)
	if len(kept) != 1 || kept[0].Title != "Published" {
		t.Fatalf("kept = %+v, want only the published entry", kept)
	}
}

func TestToSiteDataList_PrefixesInternalLinksWithBasePath(t *testing.T) {
	list, err := LoadProjectsFile(writeProjects(t, showcaseYAML))
	if err != nil {
		t.Fatalf("LoadProjectsFile: %v", err)
	}
	item := ToSiteDataList(list, "/en")[0].(map[string]any)
	links := item["links"].([]any)

	external := links[0].(map[string]any)
	if external["href"] != "https://flemming.com.br" {
		t.Errorf("external href = %v, want it untouched", external["href"])
	}
	internal := links[1].(map[string]any)
	if internal["href"] != "/en/blog/building-flemming/" {
		t.Errorf("internal href = %v, want the base path prefixed", internal["href"])
	}
}

func TestResolveHref(t *testing.T) {
	for _, tc := range []struct {
		name     string
		href     string
		external bool
		basePath string
		want     string
	}{
		{"internal gains the prefix", "/blog/x/", false, "/en", "/en/blog/x/"},
		{"external is untouched", "https://example.com", true, "/en", "https://example.com"},
		{"no base path is a no-op", "/blog/x/", false, "", "/blog/x/"},
		{"relative href is untouched", "blog/x/", false, "/en", "blog/x/"},
		{"already prefixed is not doubled", "/en/blog/x/", false, "/en", "/en/blog/x/"},
		{"prefix match must be on a boundary", "/english/x/", false, "/en", "/en/english/x/"},
		{"base path itself is not doubled", "/en", false, "/en", "/en"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveHref(tc.href, tc.external, tc.basePath); got != tc.want {
				t.Errorf("resolveHref(%q, %v, %q) = %q, want %q", tc.href, tc.external, tc.basePath, got, tc.want)
			}
		})
	}
}

// The invariant that matters is not "non-booleans are rejected" — YAML is more
// forgiving than that, and `draft: yes` is a perfectly ordinary way to write
// true. It is that no value a person could plausibly type MEANING "hold this
// back" may result in the entry shipping. Either the load fails, or Draft is
// true; silently false is the one outcome that is never acceptable.
func TestLoadProjectsFile_DraftIsNeverSilentlyFalse(t *testing.T) {
	for _, value := range []string{`yes`, `"yes"`, `on`, `"true"`, `1`, `True`, `TRUE`, `true`} {
		t.Run(value, func(t *testing.T) {
			list, err := LoadProjectsFile(writeProjects(t,
				"- title: \"P\"\n  description: d\n  order: 1\n  draft: "+value+"\n"))
			if err != nil {
				return // rejected outright — also safe
			}
			if !list[0].Draft {
				t.Fatalf("draft: %s parsed as NOT a draft — the entry would publish", value)
			}
		})
	}
}

// The mirror image: values meaning "publish this" must not accidentally withhold.
func TestLoadProjectsFile_ExplicitNonDraftPublishes(t *testing.T) {
	for _, value := range []string{`false`, `no`, `"no"`, `off`} {
		t.Run(value, func(t *testing.T) {
			list, err := LoadProjectsFile(writeProjects(t,
				"- title: \"P\"\n  description: d\n  order: 1\n  draft: "+value+"\n"))
			if err != nil {
				return
			}
			if list[0].Draft {
				t.Fatalf("draft: %s parsed as a draft — the entry would be withheld unexpectedly", value)
			}
		})
	}
}
