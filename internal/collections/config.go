// Package collections is the generic content-collection engine described in
// docs/decisions/0001-generic-content-collections.md.
//
// A collection is "a named set of records, loaded from somewhere, optionally
// sorted, optionally projected/derived, then published to (a) site-data keys
// for templates, (b) zero or more paginated index pages, (c) zero or one
// detail page per record, with (d) sitemap attributes and (e) section gating."
// It replaces the near-identical typed loaders in internal/projects,
// internal/courses and internal/listings with one configurable path.
//
// Deliberate non-goals (decision record §3.5, §3.6, §3.7): JSON-LD emission,
// scheduling/date math, language redirect stubs, RSS, sitemap XML generation,
// and section-gating policy. The engine returns []sitemap.URLItem and registers
// into buildcmd's sectionTable rather than reimplementing either.
package collections

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Source kinds.
const (
	SourceFile     = "file"
	SourceSiteData = "site_data"
)

// Source shapes.
const (
	ShapeList        = "list"
	ShapeWrappedList = "wrapped_list"
	ShapeKeyedMap    = "keyed_map"
)

// Field types.
const (
	TypeString = "string"
	TypeInt    = "int"
	TypeBool   = "bool"
	TypeList   = "list"
	TypeMap    = "map"
	TypeAny    = "any"
)

// ValueRecords is the only supported publish/inject value today: the
// collection's projected records.
const ValueRecords = "records"

// Config is the parsed <website-root>/collections.yaml.
type Config struct {
	Version     int                   `yaml:"version"`
	Collections map[string]Collection `yaml:"collections"`
}

// Collection is one named set of records and everything done with it.
type Collection struct {
	// Name is the collection's key in Config.Collections. It is filled in by
	// LoadConfig/Builtins rather than read from YAML.
	Name string `yaml:"-"`
	// Section is the sectionTable key that gates this collection
	// (-disable-sections, sitemap pruning, dev-data payload).
	Section string      `yaml:"section"`
	Source  Source      `yaml:"source"`
	Records RecordSpec  `yaml:"records"`
	Publish []Publish   `yaml:"publish"`
	Index   *IndexSpec  `yaml:"index"`
	Detail  *DetailSpec `yaml:"detail"`
}

// Source says where a collection's records come from.
type Source struct {
	Kind    string `yaml:"kind"`     // file | site_data
	Shape   string `yaml:"shape"`    // list | wrapped_list | keyed_map
	Key     string `yaml:"key"`      // wrapped_list / site_data key
	IDField string `yaml:"id_field"` // keyed_map: where the map key is copied
	Path    string `yaml:"path"`     // literal path, rarely used
	// PathFromFlag is the -collection-source name (and the legacy flag name)
	// that supplies this collection's file path.
	PathFromFlag string  `yaml:"path_from_flag"`
	Fallback     *Source `yaml:"fallback"`
}

// RecordSpec is the projection applied to each raw record.
type RecordSpec struct {
	Fields []Field `yaml:"fields"`
	// Passthrough keeps YAML keys that Fields does not list. False (the
	// default) reproduces today's typed-struct behaviour, where unlisted keys
	// are silently dropped — see decision record Q1, which is why flipping this
	// is a deliberate per-site change and not the engine's default.
	Passthrough bool     `yaml:"passthrough"`
	Require     []string `yaml:"require"`
	Sort        []SortBy `yaml:"sort"`
	Derive      []Derive `yaml:"derive"`
}

// Field is one projected key in the emitted record map.
type Field struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	Default  any    `yaml:"default"`
	// DefaultFrom derives the value from another field when it is absent or
	// empty (e.g. slug from title).
	DefaultFrom *DefaultFrom `yaml:"default_from"`
}

// DefaultFrom names a field to derive a missing value from.
type DefaultFrom struct {
	Slugify string `yaml:"slugify"`
}

// SortBy is one sort key. Dir defaults to ascending.
type SortBy struct {
	By  string `yaml:"by"`
	Dir string `yaml:"dir"`
}

// Derive computes a field from the already-projected record. Exactly one of
// Template or Money must be set (decision record §3.4 — three forms, no more;
// a fourth is the signal to let the template compute it instead).
type Derive struct {
	Name     string     `yaml:"name"`
	Template string     `yaml:"template"`
	Money    *MoneySpec `yaml:"money"`
}

// MoneySpec renders an integer cents field as a currency string.
type MoneySpec struct {
	Field  string `yaml:"field"`
	Symbol string `yaml:"symbol"`
}

// Publish writes the projected records to a dotted site-data path.
type Publish struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
	Limit int    `yaml:"limit"`
	// LimitFromFlag names a CLI flag supplying the limit (today: items-per-page).
	LimitFromFlag string `yaml:"limit_from_flag"`
}

// IndexSpec describes the paginated index pages.
type IndexSpec struct {
	Enabled  bool              `yaml:"enabled"`
	Template string            `yaml:"template"`
	BasePath string            `yaml:"base_path"`
	Paginate PaginateSpec      `yaml:"paginate"`
	Inject   map[string]string `yaml:"inject"`
	Sitemap  SitemapAttrs      `yaml:"sitemap"`
}

// PaginateSpec is the per-page item count, literal or from a flag.
type PaginateSpec struct {
	PerPage         int    `yaml:"per_page"`
	PerPageFromFlag string `yaml:"per_page_from_flag"`
}

// DetailSpec describes the per-record detail page.
type DetailSpec struct {
	Enabled  bool   `yaml:"enabled"`
	Template string `yaml:"template"`
	Path     string `yaml:"path"`
	DataKey  string `yaml:"data_key"`
	// ExtraFields are projected onto the detail record only, on top of the
	// index projection — courses' long_description / what_you_learn /
	// curriculum, which ToCurrentCourse adds and ToSiteDataList omits.
	//
	// DEVIATION from decision record §3.2, which sketches this as a bare list
	// of names (`extra_fields: [what_you_learn, curriculum]`). Names alone
	// cannot reproduce the zero values: ToCurrentCourse emits []any{} for an
	// absent what_you_learn, and the record itself flags "nil vs \"\" from dig
	// on absent-vs-zero-value fields" as a silent-breakage risk (§6.1). Full
	// Field entries carry the type, so the same projection code — and the same
	// zeroValue rules — apply here as to records.fields.
	ExtraFields []Field `yaml:"extra_fields"`
	// PageDataFrom is a dotted site-data path template (e.g.
	// "pages.product-{{.slug}}") resolved once per record and merged into the
	// detail template's data under PageDataKey, alongside the DataKey record.
	//
	// It exists because page COPY and record STRUCTURE live in two different
	// site-data namespaces: flemming's course pages read both courses.<slug>
	// (structure) and pages.<slug> (title, meta_description, body HTML), and
	// forma's products are the mirror image — products.<slug> plus
	// pages.product-<slug> (decision record §5.1(b), §5.3).
	//
	// Deliberately NOT a general "merge arbitrary site data" feature: it is one
	// dotted path, resolved per record, exposed under one fixed key. A second
	// path, or a configurable key, is the signal to stop and reconsider rather
	// than to widen this.
	//
	// It is also what lets a site migrate its templates onto a collection
	// BEFORE folding its per-page copy into the records — forma keeps its nine
	// pages.product-* entries and folds them later, as a separate diff.
	PageDataFrom string       `yaml:"page_data_from"`
	Sitemap      SitemapAttrs `yaml:"sitemap"`
}

// PageDataKey is the fixed template-data key DetailSpec.PageDataFrom resolves
// into. Fixed rather than configurable on purpose: DataKey varies per
// collection because .CurrentCourse / .CurrentListing / .CurrentProduct are
// each a different domain noun, whereas "the page copy for this record" is one
// concept with one name across every collection.
const PageDataKey = "CurrentPageData"

// SitemapAttrs mirrors sitemap.URLItem field-for-field so composing with
// internal/sitemap is by construction — the engine returns []sitemap.URLItem
// and never generates XML itself.
type SitemapAttrs struct {
	Changefreq  string `yaml:"changefreq"`
	Priority    string `yaml:"priority"`
	Lastmod     string `yaml:"lastmod"`
	LastmodFrom string `yaml:"lastmod_from"`
}

// ConfigFileName is the website-owned config file, discovered next to
// sitemap.yaml under the website root.
const ConfigFileName = "collections.yaml"

// ResolveConfigPath mirrors buildcmd.resolveSitemapConfigPath exactly: an
// explicit flag path must exist, otherwise <websiteRoot>/collections.yaml is
// used when present, otherwise "" (meaning: built-in defaults only).
func ResolveConfigPath(websiteRoot, flagPath string) (string, error) {
	if strings.TrimSpace(flagPath) != "" {
		if _, err := os.Stat(flagPath); err != nil {
			return "", fmt.Errorf("collections config not found: %s (%w)", flagPath, err)
		}
		return flagPath, nil
	}

	defaultPath := filepath.Join(websiteRoot, ConfigFileName)
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath, nil
	}

	return "", nil
}

// LoadConfig reads and validates a collections.yaml.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("reading collections config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing collections config %s: %w", path, err)
	}
	for name, c := range cfg.Collections {
		c.Name = name
		cfg.Collections[name] = c
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validating collections config %s: %w", path, err)
	}
	return cfg, nil
}

// Validate rejects a config the engine cannot honour, so a typo fails at load
// time rather than silently emitting nothing.
func (c Config) Validate() error {
	if c.Version != 0 && c.Version != 1 {
		return fmt.Errorf("unsupported version %d (only version 1 is understood)", c.Version)
	}
	for _, name := range sortedCollectionNames(c.Collections) {
		if err := c.Collections[name].Validate(); err != nil {
			return fmt.Errorf("collection %q: %w", name, err)
		}
	}
	return nil
}

// Validate checks one collection's internal consistency.
func (c Collection) Validate() error {
	if err := c.Source.validate(); err != nil {
		return err
	}
	if err := c.Records.validate(); err != nil {
		return err
	}
	for _, p := range c.Publish {
		if strings.TrimSpace(p.Key) == "" {
			return fmt.Errorf("publish entry has an empty key")
		}
		if p.Value != ValueRecords {
			return fmt.Errorf("publish %q: unsupported value %q (only %q is understood)", p.Key, p.Value, ValueRecords)
		}
	}
	if c.Index != nil && c.Index.Enabled {
		if strings.TrimSpace(c.Index.Template) == "" {
			return fmt.Errorf("index.enabled is true but index.template is empty")
		}
		if strings.TrimSpace(c.Index.BasePath) == "" {
			return fmt.Errorf("index.enabled is true but index.base_path is empty")
		}
		if _, err := c.Index.PageKey(); err != nil {
			return err
		}
	}
	if c.Detail != nil && c.Detail.Enabled {
		if strings.TrimSpace(c.Detail.Template) == "" {
			return fmt.Errorf("detail.enabled is true but detail.template is empty")
		}
		if strings.TrimSpace(c.Detail.Path) == "" {
			return fmt.Errorf("detail.enabled is true but detail.path is empty")
		}
		if strings.TrimSpace(c.Detail.DataKey) == "" {
			return fmt.Errorf("detail.enabled is true but detail.data_key is empty")
		}
	}
	return nil
}

func (s Source) validate() error {
	switch s.Kind {
	case SourceFile, SourceSiteData:
	case "":
		return fmt.Errorf("source.kind is required (%s or %s)", SourceFile, SourceSiteData)
	default:
		return fmt.Errorf("source.kind %q is not understood (want %s or %s)", s.Kind, SourceFile, SourceSiteData)
	}

	switch s.Shape {
	case ShapeList, ShapeWrappedList, ShapeKeyedMap:
	case "":
		return fmt.Errorf("source.shape is required (%s, %s or %s)", ShapeList, ShapeWrappedList, ShapeKeyedMap)
	default:
		return fmt.Errorf("source.shape %q is not understood (want %s, %s or %s)",
			s.Shape, ShapeList, ShapeWrappedList, ShapeKeyedMap)
	}

	if s.Shape == ShapeWrappedList && strings.TrimSpace(s.Key) == "" {
		return fmt.Errorf("source.shape %s requires source.key", ShapeWrappedList)
	}
	if s.Kind == SourceSiteData && strings.TrimSpace(s.Key) == "" {
		return fmt.Errorf("source.kind %s requires source.key", SourceSiteData)
	}
	if s.Fallback != nil {
		if err := s.Fallback.validate(); err != nil {
			return fmt.Errorf("source.fallback: %w", err)
		}
	}
	return nil
}

func (r RecordSpec) validate() error {
	seen := make(map[string]bool, len(r.Fields))
	for _, f := range r.Fields {
		if strings.TrimSpace(f.Name) == "" {
			return fmt.Errorf("records.fields has an entry with no name")
		}
		if seen[f.Name] {
			return fmt.Errorf("records.fields declares %q twice", f.Name)
		}
		seen[f.Name] = true
		switch f.Type {
		case TypeString, TypeInt, TypeBool, TypeList, TypeMap, TypeAny, "":
		default:
			return fmt.Errorf("records.fields[%s]: unknown type %q", f.Name, f.Type)
		}
	}
	for _, d := range r.Derive {
		if strings.TrimSpace(d.Name) == "" {
			return fmt.Errorf("records.derive has an entry with no name")
		}
		hasTemplate := strings.TrimSpace(d.Template) != ""
		hasMoney := d.Money != nil
		if hasTemplate == hasMoney {
			return fmt.Errorf("records.derive[%s]: set exactly one of template or money", d.Name)
		}
		if hasMoney && strings.TrimSpace(d.Money.Field) == "" {
			return fmt.Errorf("records.derive[%s]: money.field is required", d.Name)
		}
	}
	for _, s := range r.Sort {
		if strings.TrimSpace(s.By) == "" {
			return fmt.Errorf("records.sort has an entry with no 'by' field")
		}
		switch s.Dir {
		case "", "asc", "desc":
		default:
			return fmt.Errorf("records.sort[%s]: unknown dir %q (want asc or desc)", s.By, s.Dir)
		}
	}
	return nil
}

func sortedCollectionNames(m map[string]Collection) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Names returns this config's collection names in stable order.
func (c Config) Names() []string {
	return sortedCollectionNames(c.Collections)
}

// PageKey returns the site-data page key an index writes its items and
// pagination into — the "<key>" in inject.items: pages.<key>.items.
//
// The paginated writer isolates and injects one pages.<key> subtree per
// rendered page (shallowCopySiteDataForSection / injectPaginatedSection), so
// today only that shape is supported. Rather than silently mis-injecting an
// inject path it cannot honour, an unsupported one is rejected at config-load
// time. Generalising to arbitrary dotted paths means generalising that
// isolation too, and is deliberately left until a site needs it.
func (s IndexSpec) PageKey() (string, error) {
	items := s.Inject["items"]
	pagination := s.Inject["pagination"]

	itemsKey, err := pageSubtreeKey(items, "items")
	if err != nil {
		return "", err
	}
	if pagination != "" {
		paginationKey, err := pageSubtreeKey(pagination, "pagination")
		if err != nil {
			return "", err
		}
		if paginationKey != itemsKey {
			return "", fmt.Errorf(
				"index.inject.items (%s) and index.inject.pagination (%s) must target the same pages.<key> subtree",
				items, pagination)
		}
	}
	return itemsKey, nil
}

// pageSubtreeKey validates a "pages.<key>.<leaf>" dotted path and returns <key>.
func pageSubtreeKey(path, leaf string) (string, error) {
	parts := strings.Split(path, ".")
	if len(parts) != 3 || parts[0] != "pages" || parts[2] != leaf || parts[1] == "" {
		return "", fmt.Errorf(
			"index.inject.%s must be of the form pages.<key>.%s, got %q", leaf, leaf, path)
	}
	return parts[1], nil
}
