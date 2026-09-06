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
// Every built-in is now on the engine: projects (Phase 2), courses (Phase 3)
// and listings (Phase 5). The map is kept rather than folded away because the
// gate is what let each phase land independently — a collection still holding a
// typed loader would double-publish its site-data key and double-write its
// pages if the engine also drove it — and because it is the seam a future
// built-in (decision record Phase 6, flemming) is added behind.
//
// A collection defined by a site's own collections.yaml is always driven: it
// has no typed loader to conflict with.
var migratedCollections = map[string]bool{
	"projects": true,
	"courses":  true,
	"listings": true,
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
	// detailTemplates holds each collection's detail page template, captured
	// BEFORE filterInternalPages removes it. A detail template is marked
	// pages.<name>.internal so its .CurrentX-less validation render never
	// reaches disk (decision record Q8) — which also means it is gone from the
	// page slice by the time the writers run, exactly like content.courseTemplate
	// had to be captured early before this collection existed.
	detailTemplates map[string]sitegen.PageTemplate
}

// collectionRecords returns the projected records for one collection, or nil
// when it produced none this build. Nil-safe so callers need no guard.
func collectionRecords(set *collectionSet, name string) []any {
	if set == nil {
		return nil
	}
	return set.projected[name].Records
}

// names returns the collections that produced records, in the engine's
// processing order (collections.Order — declaration order, not alphabetical).
// Nil-safe so a caller with no collection state needs no guard.
func (s *collectionSet) names() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.projected))
	for _, name := range collections.Order(s.defs) {
		if _, ok := s.projected[name]; ok {
			out = append(out, name)
		}
	}
	return out
}

// captureDetailTemplates records every driven collection's detail template
// while the page slice still contains internal pages. Called from
// findPaginationTemplates, which runs before filterInternalPages for exactly
// this reason.
func (s *collectionSet) captureDetailTemplates(pages []sitegen.PageTemplate) {
	if s == nil {
		return
	}
	s.detailTemplates = make(map[string]sitegen.PageTemplate)
	for name, def := range s.defs {
		if def.Detail == nil || !def.Detail.Enabled || def.Detail.Template == "" {
			continue
		}
		if tpl := findTemplate(pages, def.Detail.Template); tpl != nil {
			s.detailTemplates[name] = *tpl
		}
	}
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
	for _, name := range collections.Order(set.defs) {
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

// The index and detail writers are two separate passes, called from two
// different points in writeAllPaginatedContent rather than one loop that does
// both per collection.
//
// That is not stylistic. Before the migration every collection had its own
// hand-written call, and all the DETAIL writers ran ahead of all the paginated
// INDEX writers (maybeWriteCourseLandingPages and maybeWriteListingDetailPages,
// then the blog/projects/courses listings). Since extraSitemapURLs is emitted
// in call order and generateSitemapFromPages does not re-sort it, folding the
// two into one per-collection loop would reshuffle sitemap.xml on every site
// that has a detail page. Keeping the two passes — and iterating each in
// collections.Order — reproduces the legacy sequence exactly, which is what
// lets the Phase 3 golden files be byte-identical rather than "same set,
// different order".

// writeAllCollectionDetailPages writes the per-record detail pages for every
// collection that produced records, returning their sitemap entries.
func writeAllCollectionDetailPages(
	logger *slog.Logger,
	opts buildOptions,
	set *collectionSet,
	siteData map[string]any,
	assetsDir string,
	mirrorer *externalAssetMirrorer,
) ([]sitemap.URLItem, error) {
	var urls []sitemap.URLItem
	for _, name := range set.names() {
		detailURLs, err := writeCollectionDetailPages(
			logger, opts, set, set.defs[name], set.projected[name], siteData, assetsDir, mirrorer)
		if err != nil {
			return nil, err
		}
		urls = append(urls, detailURLs...)
	}
	return urls, nil
}

// writeAllCollectionIndexPages writes the paginated index pages for every
// collection that produced records, returning their sitemap entries.
func writeAllCollectionIndexPages(
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
		indexURLs, err := writeCollectionIndexPages(
			logger, opts, pages, set.defs[name], set.projected[name], siteData, assetsDir, mirrorer)
		if err != nil {
			return nil, err
		}
		urls = append(urls, indexURLs...)
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
// Not reachable for `projects` (detail.enabled is false); it is the courses
// (Phase 3) and listings (Phase 5) writer.
//
// The template comes from set.detailTemplates rather than the page slice: a
// detail template is pages.<name>.internal, so filterInternalPages has already
// removed it by the time any writer runs (decision record Q8).
func writeCollectionDetailPages(
	logger *slog.Logger,
	opts buildOptions,
	set *collectionSet,
	def collections.Collection,
	projection collections.Projection,
	siteData map[string]any,
	assetsDir string,
	mirrorer *externalAssetMirrorer,
) ([]sitemap.URLItem, error) {
	if def.Detail == nil || !def.Detail.Enabled || len(projection.Details) == 0 {
		return nil, nil
	}
	tpl, ok := set.detailTemplates[def.Name]
	if !ok {
		// Matches maybeWriteCourseLandingPages: a site without the detail page
		// template simply gets no detail pages, rather than a build failure.
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
			tmpl:     tpl,
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
			Path:        urlPath,
			Changefreq:  def.Detail.Sitemap.Changefreq,
			Priority:    def.Detail.Sitemap.Priority,
			Lastmod:     def.Detail.Sitemap.Lastmod,
			LastmodFrom: def.Detail.Sitemap.LastmodFrom,
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
	// Page identity: the shared template's own name unless the collection
	// declares a per-record one, which also opts these pages into the per-page
	// transforms below (see DetailSpec.PageNameFrom).
	pageName := p.def.Detail.Template
	if p.def.Detail.PageNameFrom != "" {
		resolved, err := collections.RenderPageName(p.def.Detail.PageNameFrom, p.record, p.opts.basePath)
		if err != nil {
			return nil, fmt.Errorf("collection %q detail %s: %w", p.def.Name, p.urlPath, err)
		}
		pageName = resolved
	}

	templateData := map[string]any{
		"PageName":           pageName,
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

	// The two per-page transforms writePages applies and this writer historically
	// did not (decision record Q2). Gated on a declared per-record page identity
	// so no existing collection's output changes; for a site whose detail pages
	// were ordinary pages until the migration, skipping them would silently drop
	// the hreflang alternates and language-switcher hrefs those pages ship today.
	if p.def.Detail.PageNameFrom != "" {
		htmlOut = injectHreflangAlternates(htmlOut, p.siteData, pageName, p.opts.cleanURLs)
		htmlOut = injectLangSwitcherHrefs(htmlOut, p.siteData, pageName, p.opts.cleanURLs)
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
		if attrs.LastmodFrom != "" {
			urls[i].LastmodFrom = attrs.LastmodFrom
		}
	}
	return urls
}
