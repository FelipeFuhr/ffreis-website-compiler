package collections

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

const sampleConfig = `
version: 1
collections:
  articles:
    section: articles
    source:
      kind: file
      shape: list
      path_from_flag: articles-file
    records:
      fields:
        - {name: title, type: string, required: true}
        - {name: slug, type: string, default_from: {slugify: title}}
        - {name: order, type: int}
      derive:
        - {name: href, template: "/articles/{{.slug}}/"}
      sort:
        - {by: order}
        - {by: title}
    publish:
      - {key: articles, value: records, limit_from_flag: items-per-page}
    index:
      enabled: true
      template: articles
      base_path: /articles
      paginate: {per_page_from_flag: items-per-page}
      inject:
        items: pages.articles.items
        pagination: pages.articles.pagination
    detail:
      enabled: true
      template: article
      path: "/articles/{{.slug}}/"
      data_key: CurrentArticle
      sitemap: {changefreq: monthly, priority: "0.6"}
`

func TestLoadConfigParsesEveryAxis(t *testing.T) {
	cfg, err := LoadConfig(writeTemp(t, ConfigFileName, sampleConfig))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	c, ok := cfg.Collections["articles"]
	if !ok {
		t.Fatalf("no articles collection; got %v", cfg.Names())
	}
	if c.Name != "articles" {
		t.Errorf("Name = %q, want articles (LoadConfig must fill it from the map key)", c.Name)
	}
	if c.Section != "articles" {
		t.Errorf("Section = %q", c.Section)
	}
	if c.Source.Kind != SourceFile || c.Source.Shape != ShapeList || c.Source.PathFromFlag != "articles-file" {
		t.Errorf("source = %+v", c.Source)
	}
	if len(c.Records.Fields) != 3 || !c.Records.Fields[0].Required {
		t.Errorf("fields = %+v", c.Records.Fields)
	}
	if c.Records.Fields[1].DefaultFrom == nil || c.Records.Fields[1].DefaultFrom.Slugify != "title" {
		t.Errorf("default_from = %+v", c.Records.Fields[1].DefaultFrom)
	}
	if len(c.Records.Derive) != 1 || c.Records.Derive[0].Template != "/articles/{{.slug}}/" {
		t.Errorf("derive = %+v", c.Records.Derive)
	}
	if len(c.Records.Sort) != 2 {
		t.Errorf("sort = %+v", c.Records.Sort)
	}
	if len(c.Publish) != 1 || c.Publish[0].LimitFromFlag != "items-per-page" {
		t.Errorf("publish = %+v", c.Publish)
	}
	if c.Index == nil || !c.Index.Enabled || c.Index.Inject["items"] != "pages.articles.items" {
		t.Errorf("index = %+v", c.Index)
	}
	if c.Detail == nil || c.Detail.DataKey != "CurrentArticle" || c.Detail.Sitemap.Priority != "0.6" {
		t.Errorf("detail = %+v", c.Detail)
	}
}

func TestLoadConfigRejectsInvalidConfigs(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			"unsupported version",
			"version: 2\ncollections: {}\n",
			"unsupported version",
		},
		{
			"unknown source kind",
			"collections:\n  a:\n    source: {kind: carrier_pigeon, shape: list}\n",
			"source.kind",
		},
		{
			"unknown shape",
			"collections:\n  a:\n    source: {kind: file, shape: blob}\n",
			"source.shape",
		},
		{
			"wrapped_list without key",
			"collections:\n  a:\n    source: {kind: file, shape: wrapped_list}\n",
			"requires source.key",
		},
		{
			"site_data without key",
			"collections:\n  a:\n    source: {kind: site_data, shape: list}\n",
			"requires source.key",
		},
		{
			"duplicate field",
			"collections:\n  a:\n    source: {kind: file, shape: list}\n    records:\n      fields:\n        - {name: t, type: string}\n        - {name: t, type: int}\n",
			"declares \"t\" twice",
		},
		{
			"unknown field type",
			"collections:\n  a:\n    source: {kind: file, shape: list}\n    records:\n      fields:\n        - {name: t, type: colour}\n",
			"unknown type",
		},
		{
			"derive with neither form",
			"collections:\n  a:\n    source: {kind: file, shape: list}\n    records:\n      derive:\n        - {name: href}\n",
			"exactly one of template or money",
		},
		{
			"derive with both forms",
			"collections:\n  a:\n    source: {kind: file, shape: list}\n    records:\n      derive:\n        - {name: h, template: \"x\", money: {field: c, symbol: $}}\n",
			"exactly one of template or money",
		},
		{
			"bad sort dir",
			"collections:\n  a:\n    source: {kind: file, shape: list}\n    records:\n      sort:\n        - {by: order, dir: sideways}\n",
			"unknown dir",
		},
		{
			"publish with unsupported value",
			"collections:\n  a:\n    source: {kind: file, shape: list}\n    publish:\n      - {key: a, value: totals}\n",
			"unsupported value",
		},
		{
			"index enabled without template",
			"collections:\n  a:\n    source: {kind: file, shape: list}\n    index: {enabled: true, base_path: /a}\n",
			"index.template is empty",
		},
		{
			"detail enabled without data_key",
			"collections:\n  a:\n    source: {kind: file, shape: list}\n    detail: {enabled: true, template: a, path: \"/a/\"}\n",
			"data_key is empty",
		},
		{
			"index inject items in an unsupported shape",
			"collections:\n  a:\n    source: {kind: file, shape: list}\n    index:\n      enabled: true\n      template: a\n      base_path: /a\n      inject: {items: sidebar.a}\n",
			"must be of the form pages.<key>.items",
		},
		{
			"index inject items and pagination disagree",
			"collections:\n  a:\n    source: {kind: file, shape: list}\n    index:\n      enabled: true\n      template: a\n      base_path: /a\n      inject: {items: pages.a.items, pagination: pages.b.pagination}\n",
			"must target the same pages.<key> subtree",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(writeTemp(t, ConfigFileName, tc.yaml))
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestResolveConfigPath mirrors buildcmd.resolveSitemapConfigPath's contract:
// an explicit flag must exist, the default is used when present, and absence
// is not an error.
func TestResolveConfigPath(t *testing.T) {
	root := t.TempDir()

	got, err := ResolveConfigPath(root, "")
	if err != nil || got != "" {
		t.Errorf("no config: got (%q, %v), want (\"\", nil)", got, err)
	}

	defaultPath := filepath.Join(root, ConfigFileName)
	if err := os.WriteFile(defaultPath, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatalf("writing default config: %v", err)
	}
	got, err = ResolveConfigPath(root, "")
	if err != nil || got != defaultPath {
		t.Errorf("default config: got (%q, %v), want (%q, nil)", got, err, defaultPath)
	}

	explicit := writeTemp(t, "other.yaml", "version: 1\n")
	got, err = ResolveConfigPath(root, explicit)
	if err != nil || got != explicit {
		t.Errorf("explicit config: got (%q, %v), want (%q, nil)", got, err, explicit)
	}

	if _, err := ResolveConfigPath(root, filepath.Join(root, "missing.yaml")); err == nil {
		t.Error("an explicit -collections-config that does not exist must be an error, not a silent fallback")
	}
}

// TestResolveMergesOverBuiltins pins decision record §7.3(a): the built-ins are
// the default, a site's config overrides by name, and new names are added.
func TestResolveMergesOverBuiltins(t *testing.T) {
	base := Resolve(Config{})
	for _, name := range []string{"projects", "courses", "listings"} {
		if _, ok := base[name]; !ok {
			t.Errorf("built-in %q missing from an empty config resolve", name)
		}
	}

	cfg, err := LoadConfig(writeTemp(t, ConfigFileName, sampleConfig))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Collections["projects"] = Collection{
		Section: "projects",
		Source:  Source{Kind: SourceFile, Shape: ShapeList},
		Records: RecordSpec{Passthrough: true},
	}

	merged := Resolve(cfg)
	if _, ok := merged["articles"]; !ok {
		t.Error("a site-defined collection must be added")
	}
	if !merged["projects"].Records.Passthrough {
		t.Error("a site-defined collection must fully replace the built-in of the same name")
	}
	if _, ok := merged["listings"]; !ok {
		t.Error("built-ins the site does not override must survive")
	}

	// Builtins must hand out a fresh copy each call, so one build's mutation
	// cannot leak into another in the same process.
	a, b := Builtins(), Builtins()
	a["projects"] = Collection{Name: "mutated"}
	if b["projects"].Name != "projects" {
		t.Error("Builtins() returned a shared map; mutating one caller's copy affected another")
	}
}

// TestOrderIsDeclarationThenAlphabetical pins the processing order that decides
// which collection's pages are written — and therefore which sitemap entries are
// emitted — first. See builtinOrder: it is DECLARATION order, because the
// hand-written writers it replaced ran projects before courses, and sorting the
// names would rewrite sitemap.xml on every site running both.
func TestOrderIsDeclarationThenAlphabetical(t *testing.T) {
	t.Run("built-ins keep declaration order", func(t *testing.T) {
		got := Order(Builtins())
		want := []string{"projects", "courses", "listings"}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("Order = %v, want %v (alphabetical would start with courses)", got, want)
		}
	})

	t.Run("site-defined collections follow, alphabetically", func(t *testing.T) {
		defs := Builtins()
		for _, name := range []string{"zebras", "articles"} {
			defs[name] = Collection{Name: name}
		}
		got := Order(defs)
		want := []string{"projects", "courses", "listings", "articles", "zebras"}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("Order = %v, want %v: built-ins first in declaration order, then "+
				"site-defined names sorted so the result is deterministic", got, want)
		}
	})

	t.Run("a deleted built-in is skipped, not emitted empty", func(t *testing.T) {
		defs := Builtins()
		delete(defs, "courses")
		got := Order(defs)
		want := []string{"projects", "listings"}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("Order = %v, want %v; builtinOrder names a collection that is no "+
				"longer defined, and Order must drop it rather than return a phantom", got, want)
		}
	})
}

// TestRenderPath covers the detail-page path template, which decides both the
// on-disk target and the sitemap entry — so a divergence between the two is
// impossible by construction only if this one function is right.
func TestRenderPath(t *testing.T) {
	record := map[string]any{"slug": "mlops-in-production", "id": "l-42"}

	cases := []struct {
		name     string
		tmpl     string
		basePath string
		want     string
	}{
		{"courses directory form", "/courses/{{.slug}}/", "/pt", "/courses/mlops-in-production/"},
		{"listings directory form", "/listings/{{.id}}/", "", "/listings/l-42/"},
		// forma's flat URLs — decision record §5.3 flags this as genuinely new.
		{"flat html form", "/product-{{.slug}}.html", "", "/product-mlops-in-production.html"},
		// base_path is exposed but deliberately NOT prepended by default: courses
		// and listings compensate on the consuming side (§3.4).
		{"base path is available, not implicit", "{{.__base_path}}/blog/{{.slug}}/", "/en", "/en/blog/mlops-in-production/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RenderPath(tc.tmpl, record, tc.basePath)
			if err != nil {
				t.Fatalf("RenderPath(%q): %v", tc.tmpl, err)
			}
			if got != tc.want {
				t.Errorf("RenderPath(%q) = %q, want %q", tc.tmpl, got, tc.want)
			}
		})
	}

	if _, err := RenderPath("/courses/{{.slug", record, ""); err == nil {
		t.Error("expected a parse error for a malformed path template")
	}
	if _, err := RenderPath(`/courses/{{call .slug}}/`, record, ""); err == nil {
		t.Error("expected an execution error for a path template that cannot run")
	}
}

func TestBuiltinsAreInternallyValid(t *testing.T) {
	for name, c := range Builtins() {
		if err := c.Validate(); err != nil {
			t.Errorf("built-in %q is not valid: %v", name, err)
		}
	}
}

func TestLoadConfigReportsMissingFile(t *testing.T) {
	if _, err := LoadConfig(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Error("expected an error for a missing config file")
	}
}

func TestLoadConfigReportsMalformedYAML(t *testing.T) {
	if _, err := LoadConfig(writeTemp(t, ConfigFileName, "collections: [oops\n")); err == nil {
		t.Error("expected a parse error for malformed YAML")
	}
}
