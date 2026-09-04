package buildcmd

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ffreis-website-compiler/internal/collections"
	"ffreis-website-compiler/internal/sitegen"
	"ffreis-website-compiler/internal/sitemap"
)

// The buildcmd half of the collection engine (decision record §3.2-§3.3):
// resolve the config, load + project each collection's records, publish them
// into site data, and — later in the build — write the paginated index pages
// and per-record detail pages.
//
// Everything about a collection's SHAPE lives in internal/collections; this
// file only orchestrates, so the sitemap, section-gating and pagination
// machinery it composes with stays exactly where it already is.

// migratedCollections is the set of collections the engine actually drives.
//
// The built-in definitions cover projects, courses and listings, but courses
// and listings still load through internal/courses and internal/listings until
// decision-record Phases 3 and 5 migrate them. Running the engine for those two
// now would double-publish their site-data keys and double-write their pages,
// so the engine is gated on this list and each later phase adds one name.
//
// A collection defined by a site's own collections.yaml is always driven: it
// has no typed loader to conflict with.
var migratedCollections = map[string]bool{
	"projects": true,
}

// collectionSet is the resolved, per-build view of every collection: the merged
// definitions plus the records projected for this build.
type collectionSet struct {
	// defs is every known collection (built-ins merged with collections.yaml),
	// keyed by name.
	defs map[string]collections.Collection
	// projected holds the projection for each collection that actually
	// contributed records this build. A collection whose section is disabled,
	// or whose source yielded nothing, is absent.
	projected map[string]collections.Projection
	// detailTemplates holds each enabled detail spec's page template, captured
	// before filterInternalPages removes it. See captureDetailTemplates.
	detailTemplates map[string]*sitegen.PageTemplate
}

// captureDetailTemplates stashes every page template an enabled detail spec
// names, and must run BEFORE filterInternalPages.
//
// Every detail template is marked pages.<name>.internal so its no-record render
// (decision record Q8) stays off disk — which means by the time the detail
// writer runs, filterInternalPages has already dropped it from the page slice.
// The typed course/listing writers work around this via findPaginationTemplates'
// hardcoded four names; a collection's detail template is named by config, so
// this walks the resolved definitions instead of a fixed list.
//
// Index templates deliberately keep resolving against the FILTERED slice: an
// index page is an ordinary, non-internal page, and it must still disappear
// when its section is disabled.
func (s *collectionSet) captureDetailTemplates(pages []sitegen.PageTemplate) {
	if s == nil {
		return
	}
	s.detailTemplates = make(map[string]*sitegen.PageTemplate)
	for _, def := range s.defs {
		if def.Detail == nil || !def.Detail.Enabled {
			continue
		}
		if tpl := findTemplate(pages, def.Detail.Template); tpl != nil {
			s.detailTemplates[def.Detail.Template] = tpl
		}
	}
}

// detailTemplate returns the captured detail template for a collection, or nil
// when the site ships none.
func (s *collectionSet) detailTemplate(def collections.Collection) *sitegen.PageTemplate {
	if s == nil || def.Detail == nil {
		return nil
	}
	return s.detailTemplates[def.Detail.Template]
}

// collectionRecords returns the projected records for one collection, or nil
// when it produced none this build. Nil-safe so callers need no guard.
func collectionRecords(set *collectionSet, name string) []any {
	if set == nil {
		return nil
	}
	return set.projected[name].Records
}

// names returns the collections that produced records, in stable order.
func (s *collectionSet) names() []string {
	out := make([]string, 0, len(s.projected))
	for name := range s.projected {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// loadCollections resolves the collections config, validates the requested
// sources against it, then loads, projects and publishes every enabled
// collection's records into siteData.
//
// Runs after contract validation (like loadOptionalContent), so published keys
// need no contract entry and cannot dangle.
func loadCollections(logger *slog.Logger, opts buildOptions, siteData map[string]any) (*collectionSet, error) {
	configPath, err := collections.ResolveConfigPath(opts.websiteRoot, opts.collectionsConfig)
	if err != nil {
		return nil, err
	}

	var cfg collections.Config
	if configPath != "" {
		cfg, err = collections.LoadConfig(configPath)
		if err != nil {
			return nil, err
		}
		logger.Info("loaded collections config", "config_path", configPath, "collections", cfg.Names())
	}

	set := &collectionSet{
		defs:      collections.Resolve(cfg),
		projected: make(map[string]collections.Projection),
	}

	if err := validateCollectionSources(opts, set.defs); err != nil {
		return nil, err
	}

	basePath, _ := siteData["base_path"].(string)
	for _, name := range sortedDefNames(set.defs) {
		def := set.defs[name]

		if !migratedCollections[name] && !siteDefined(cfg, name) {
			continue
		}

		// A disabled section loads no content, so no index/detail pages, no
		// carousel and no sitemap entries are produced for it — matching how
		// loadOptionalContent gates every typed loader today.
		if def.Section != "" && !sectionEnabled(siteData, def.Section) {
			continue
		}

		raw, err := collections.LoadRecords(def.Source, collections.LoadOptions{
			Path:     opts.collectionSources[name],
			SiteData: siteData,
		})
		if err != nil {
			return nil, fmt.Errorf("loading collection %q: %w", name, err)
		}
		if len(raw) == 0 {
			continue
		}

		projection, err := def.Project(raw, collections.ProjectOptions{BasePath: basePath})
		if err != nil {
			return nil, fmt.Errorf("loading collection %q: %w", name, err)
		}

		def.ApplyPublish(siteData, projection.Records, collections.PublishOptions{
			ItemsPerPage: opts.itemsPerPage,
		})
		set.projected[name] = projection
		logger.Info("loaded collection", "collection", name, "records", len(projection.Records))
	}

	return set, nil
}

// validateCollectionSources rejects a -collection-source (or a legacy
// -projects-file/-courses-file/-listings-file that adoptLegacyContentFlags
// folded into one) naming a collection nothing defines.
//
// Decision record §7.1 is explicit that this must fail loudly: a typo that
// silently skipped the content would ship a site with a whole section quietly
// missing, and every other guard in the build would still pass.
func validateCollectionSources(opts buildOptions, defs map[string]collections.Collection) error {
	for _, name := range sortedSourceNames(opts.collectionSources) {
		if _, ok := defs[name]; ok {
			continue
		}
		return fmt.Errorf(
			"-collection-source %s=%s names an unknown collection: no collections.yaml entry and no built-in definition for %q. Known collections: %s",
			name, opts.collectionSources[name], name, strings.Join(sortedDefNames(defs), ", "),
		)
	}
	return nil
}

// siteDefined reports whether a site's own collections.yaml declares this
// collection, in which case it is driven regardless of migratedCollections —
// a site-defined collection has no typed loader to conflict with.
func siteDefined(cfg collections.Config, name string) bool {
	_, ok := cfg.Collections[name]
	return ok
}

func sortedDefNames(defs map[string]collections.Collection) []string {
	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// writeCollectionPages writes the index and detail pages for every collection
// that produced records, returning their sitemap entries.
func writeCollectionPages(
	logger *slog.Logger,
	opts buildOptions,
	pages []sitegen.PageTemplate,
	set *collectionSet,
	siteData map[string]any,
	assetsDir string,
	mirrorer *externalAssetMirrorer,
) ([]sitemap.URLItem, error) {
	var urls []sitemap.URLItem

	for _, name := range set.names() {
		def := set.defs[name]
		projection := set.projected[name]

		indexURLs, err := writeCollectionIndexPages(logger, opts, pages, def, projection, siteData, assetsDir, mirrorer)
		if err != nil {
			return nil, err
		}
		urls = append(urls, indexURLs...)

		detailURLs, err := writeCollectionDetailPages(
			logger, opts, set.detailTemplate(def), def, projection, siteData, assetsDir, mirrorer)
		if err != nil {
			return nil, err
		}
		urls = append(urls, detailURLs...)
	}

	return urls, nil
}

// writeCollectionIndexPages generates the paginated index pages by delegating
// to writePaginatedPages — the same writer blog/projects/courses already share
// — so the emitted bytes are identical by construction rather than by
// reimplementation.
func writeCollectionIndexPages(
	logger *slog.Logger,
	opts buildOptions,
	pages []sitegen.PageTemplate,
	def collections.Collection,
	projection collections.Projection,
	siteData map[string]any,
	assetsDir string,
	mirrorer *externalAssetMirrorer,
) ([]sitemap.URLItem, error) {
	if def.Index == nil || !def.Index.Enabled {
		return nil, nil
	}
	tpl := findTemplate(pages, def.Index.Template)
	if tpl == nil {
		// Matches maybeWriteProjectListings: a site without the page template
		// simply gets no index pages, rather than a build failure.
		return nil, nil
	}

	pageKey, err := def.Index.PageKey()
	if err != nil {
		return nil, fmt.Errorf("collection %q: %w", def.Name, err)
	}

	urls, err := writePaginatedPages(paginatedPagesParams{
		logger:      logger,
		opts:        opts,
		tmpl:        *tpl,
		items:       projection.Records,
		sectionName: pageKey,
		basePath:    def.Index.BasePath,
		siteData:    siteData,
		assetsDir:   assetsDir,
		mirrorer:    mirrorer,
	})
	if err != nil {
		return nil, fmt.Errorf("writing %s pages: %w", def.Name, err)
	}
	return applySitemapAttrs(urls, def.Index.Sitemap), nil
}

// writeCollectionDetailPages renders one page per record using the detail
// template, generalising writeCourseLandingPages and writeListingDetailPages.
//
// tpl comes from collectionSet.detailTemplates rather than the page slice: by
// this point filterInternalPages has removed every internal template, and a
// detail template is always internal (decision record Q8).
//
// Not reachable for `projects` (detail.enabled is false) — it is the Phase 3
// (courses) and Phase 5 (listings) writer, landed here so the engine is whole
// and unit-testable before either migration starts.
func writeCollectionDetailPages(
	logger *slog.Logger,
	opts buildOptions,
	tpl *sitegen.PageTemplate,
	def collections.Collection,
	projection collections.Projection,
	siteData map[string]any,
	assetsDir string,
	mirrorer *externalAssetMirrorer,
) ([]sitemap.URLItem, error) {
	if def.Detail == nil || !def.Detail.Enabled || len(projection.Details) == 0 {
		return nil, nil
	}
	if tpl == nil {
		return nil, nil
	}

	allToCopy := make(map[string]string)
	urls := make([]sitemap.URLItem, 0, len(projection.Details))

	for _, record := range projection.Details {
		urlPath, err := collections.RenderPath(def.Detail.Path, record, opts.basePath)
		if err != nil {
			return nil, fmt.Errorf("collection %q: %w", def.Name, err)
		}

		toCopy, err := renderAndWriteCollectionDetail(detailPageParams{
			logger:   logger,
			opts:     opts,
			tmpl:     *tpl,
			def:      def,
			record:   record,
			urlPath:  urlPath,
			siteData: siteData,
			assets:   assetsDir,
			mirrorer: mirrorer,
		})
		if err != nil {
			return nil, err
		}
		for k, v := range toCopy {
			allToCopy[k] = v
		}

		urls = append(urls, sitemap.URLItem{
			Path:       urlPath,
			Changefreq: def.Detail.Sitemap.Changefreq,
			Priority:   def.Detail.Sitemap.Priority,
			Lastmod:    def.Detail.Sitemap.Lastmod,
		})
	}

	if err := writeHashedAssets(opts.outDir, assetsDir, allToCopy); err != nil {
		return nil, err
	}
	return urls, nil
}

// detailPageParams bundles the arguments for renderAndWriteCollectionDetail to
// keep the parameter count within the allowed threshold.
type detailPageParams struct {
	logger   *slog.Logger
	opts     buildOptions
	tmpl     sitegen.PageTemplate
	def      collections.Collection
	record   map[string]any
	urlPath  string
	siteData map[string]any
	assets   string
	mirrorer *externalAssetMirrorer
}

// renderAndWriteCollectionDetail renders one record's detail page and writes it
// to the on-disk target derived from its URL path.
//
// The two-phase render contract (decision record Q8) is preserved: the detail
// template is also executed once by sitegen.RenderPages with no .CurrentX, for
// asset-usage validation, and pages.<name>.internal keeps that render off disk.
// Detail templates must therefore still guard on {{if .CurrentX}}.
func renderAndWriteCollectionDetail(p detailPageParams) (map[string]string, error) {
	templateData := map[string]any{
		"PageName":           p.def.Detail.Template,
		"SiteData":           p.siteData,
		p.def.Detail.DataKey: p.record,
	}

	// One shared detail template means .PageName is the template name for every
	// record, so a template can no longer reach its own page copy via
	// pages.<PageName>. page_data_from resolves that copy per record instead.
	if p.def.Detail.PageDataFrom != "" {
		pageData, err := collections.ResolvePageData(
			p.def.Detail.PageDataFrom, p.record, p.siteData, p.opts.basePath)
		if err != nil {
			return nil, fmt.Errorf("collection %q detail %s: %w", p.def.Name, p.urlPath, err)
		}
		templateData[collections.PageDataKey] = pageData
	}

	var rendered bytes.Buffer
	if err := p.tmpl.Tmpl.ExecuteTemplate(&rendered, "layout", templateData); err != nil {
		return nil, fmt.Errorf("rendering %s detail %s: %w", p.def.Name, p.urlPath, err)
	}

	htmlOut, toCopy, err := transformPage(rendered.String(), p.opts, p.assets, p.mirrorer)
	if err != nil {
		return nil, fmt.Errorf("transforming %s detail %s: %w", p.def.Name, p.urlPath, err)
	}

	target := resolveDetailTarget(p.opts.outDir, p.urlPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, fmt.Errorf("creating directory for %s detail %s: %w", p.def.Name, p.urlPath, err)
	}
	if err := os.WriteFile(target, []byte(htmlOut), 0o644); err != nil { //nolint:gosec
		return nil, fmt.Errorf(errFmtWriting, target, err)
	}
	p.logger.Info("generated collection detail page", "collection", p.def.Name, "path", p.urlPath, "target", target)
	return toCopy, nil
}

// resolveDetailTarget maps a detail page's URL path to its on-disk file.
//
// A path ending in "/" becomes <path>index.html (courses, listings); anything
// else is written literally, which is what forma's flat
// /product-<slug>.html URLs need (decision record §5.3 flags this as genuinely
// new behaviour, since both existing writers hardcode the directory form).
func resolveDetailTarget(outDir, urlPath string) string {
	trimmed := strings.TrimPrefix(urlPath, "/")
	if strings.HasSuffix(urlPath, "/") {
		return filepath.Join(outDir, filepath.FromSlash(trimmed), "index.html")
	}
	return filepath.Join(outDir, filepath.FromSlash(trimmed))
}

// applySitemapAttrs stamps a collection's declared sitemap attributes onto the
// URL items a writer produced. Empty attrs leave the item as the writer built
// it — which for the index writer is Path only, matching today.
func applySitemapAttrs(urls []sitemap.URLItem, attrs collections.SitemapAttrs) []sitemap.URLItem {
	if attrs == (collections.SitemapAttrs{}) {
		return urls
	}
	for i := range urls {
		if attrs.Changefreq != "" {
			urls[i].Changefreq = attrs.Changefreq
		}
		if attrs.Priority != "" {
			urls[i].Priority = attrs.Priority
		}
		if attrs.Lastmod != "" {
			urls[i].Lastmod = attrs.Lastmod
		}
	}
	return urls
}
