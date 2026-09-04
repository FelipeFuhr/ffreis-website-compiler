package buildcmd

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ffreis-website-compiler/internal/assetusage"
	"ffreis-website-compiler/internal/linkcheck"
	"ffreis-website-compiler/internal/listings"
	"ffreis-website-compiler/internal/outputtestcmd"
	"ffreis-website-compiler/internal/posts"
	"ffreis-website-compiler/internal/sitegen"
	"ffreis-website-compiler/internal/sitemap"
)

const (
	sitemapXML          = "sitemap.xml"
	fileManifestJSON    = "manifest.json"
	fileSiteWebmanifest = "site.webmanifest"
	wellKnownDir        = ".well-known"
	mimeTextCSS         = "text/css"
	extWoff2            = ".woff2"

	errFmtWriting = "writing %s: %w"

	// svgInlineSizeLimit is the maximum byte size for an SVG to be inlined as
	// <svg> in HTML. Larger SVGs stay external and are fingerprinted instead.
	svgInlineSizeLimit = 8 * 1024

	// defaultJSInlineThreshold is the default byte limit for inlining <script src>
	// files as <script> blocks. Files at or above this size stay external.
	defaultJSInlineThreshold = 8 * 1024

	// headEndTag is the closing </head> tag with a preceding newline, used as
	// the injection point for head-level transforms. Defined as a constant to
	// avoid duplicate string literals across multiple injection helpers.
	headEndTag = "\n</head>"
)

// Package-level regexes — compiled once, shared across all files in this package.
var (
	stylesheetTagRE = regexp.MustCompile(`(?is)<link\s+[^>]*rel=["']stylesheet["'][^>]*href=["']([^"']+)["'][^>]*>`)
	preloadTagRE    = regexp.MustCompile(`(?is)<link\s+[^>]*rel=["'][^"']*preload[^"']*["'][^>]*href=["']([^"']+)["'][^>]*>`)
	scriptTagRE     = regexp.MustCompile(`(?is)<script\s+[^>]*src=["']([^"']+)["'][^>]*>\s*</script>`)
	imgTagRE        = regexp.MustCompile(`(?is)<img\s+[^>]*src=["']([^"']+)["'][^>]*>`)
	iconTagRE       = regexp.MustCompile(`(?is)<link\s+[^>]*rel=["'][^"']*icon[^"']*["'][^>]*href=["']([^"']+)["'][^>]*>`)
	manifestTagRE   = regexp.MustCompile(`(?is)<link\s+[^>]*rel=["']manifest["'][^>]*href=["']([^"']+)["'][^>]*>`)
	cssURLRE        = regexp.MustCompile(`url\(\s*['"]?([^'"\)]+)['"]?\s*\)`)
	cssImportRE     = regexp.MustCompile(`(?is)@import\s+(?:url\(\s*)?['"]?([^'"\)\s;]+)['"]?\s*\)?([^;]*);`)
	xmlProcInstRE   = regexp.MustCompile(`(?i)<\?xml\b[^?]*\?>`)
	svgRootTagRE    = regexp.MustCompile(`(?i)<svg\b[^>]*>`)

	// CSS minification regexes used by minifyCSS.
	cssPreservedCommentRE = regexp.MustCompile(`/\*![\s\S]*?\*/`)
	cssBlockCommentRE     = regexp.MustCompile(`/\*[\s\S]*?\*/`)
	cssDQStringRE         = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	cssSQStringRE         = regexp.MustCompile(`'(?:[^'\\]|\\.)*'`)
	cssWhitespaceRE       = regexp.MustCompile(`\s+`)
	cssAroundStructRE     = regexp.MustCompile(`\s*([{};:,])\s*`)

	// headEndRE matches </head> for position-based CSS loading split.
	headEndRE = regexp.MustCompile(`(?i)</head>`)

	// baseHrefTagRE extracts the href value from a <base href="..."> tag.
	baseHrefTagRE = regexp.MustCompile(`(?is)<base\s+[^>]*href=["']([^"']+)["'][^>]*>`)

	// Regexes for validateRenderedPageStructure.
	titleTagRE        = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	h1TagRE           = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	metaDescriptionRE = regexp.MustCompile(`(?is)<meta\s+[^>]*name=["']description["'][^>]*content=["']([^"']*)["'][^>]*>|<meta\s+[^>]*content=["']([^"']*)["'][^>]*name=["']description["'][^>]*>`)
	htmlTagsRE        = regexp.MustCompile(`<[^>]+>`)
)

// optionalContent holds the blog posts and the not-yet-migrated typed content
// loaded from optional input flags, plus the page templates needed to render
// them — and the collection engine's per-build state for everything that HAS
// migrated (today: projects and courses).
type optionalContent struct {
	posts           []posts.Post
	listings        []listings.Listing
	postTemplate    *sitegen.PageTemplate
	blogTemplate    *sitegen.PageTemplate
	listingTemplate *sitegen.PageTemplate // the internal "listing" detail template
	// collections carries every collection-engine-managed content set. listings
	// still loads through its typed package above until Phase 5 migrates it.
	collections *collectionSet
}

func Run(args []string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	opts, assetsDir, templatesDir, mirrorer, err := prepareBuild(args, logger)
	if err != nil {
		return err
	}

	pages, siteDataResult, _, err := loadAndValidateSiteInputs(logger, opts, templatesDir)
	if err != nil {
		return err
	}

	// Cache base_path on opts so downstream transforms (e.g. fingerprintLocalAssets)
	// can prepend it to root-absolute asset references for deployments served under
	// a path prefix like "/en".
	opts.basePath, _ = siteDataResult.Data["base_path"].(string)

	// Load optional content (posts, projects, courses) and inject into site data.
	// This runs after contract validation so compiler-managed injected data does
	// not need contract entries.
	content, err := loadOptionalContent(logger, opts, siteDataResult.Data)
	if err != nil {
		return err
	}

	// Build dev-data JSON payload once from siteData + content so transformPage
	// can inject it without accessing siteData directly.
	opts.devDataJSON = buildDevDataPayload(opts, siteDataResult.Data, content)

	// Save references to templates needed for paginated page generation,
	// before filterInternalPages removes them.
	findPaginationTemplates(pages, content)

	// Render all pages (including internal ones) so their CSS/JS assets are
	// seen as "used" by the asset validator. Internal pages are filtered out
	// from disk output and sitemap after validation.
	renderedPages, err := sitegen.RenderPages(pages, siteDataResult.Data)
	if err != nil {
		return err
	}
	if err := validateAssetUsage(logger, opts, assetsDir, renderedPages); err != nil {
		return err
	}

	// Cross-page sharing analysis: identify which JS files appear on more than one page.
	// These are candidates for caching rather than per-page inlining when
	// -js-shared-inline-threshold is set. Runs after rendering so the full set of
	// script references (including those injected by partials and the base layout) is visible.
	if opts.jsSharedInlineThreshold >= 0 {
		opts.sharedScripts = collectSharedScripts(renderedPages)
	}

	pages = filterInternalPages(pages, siteDataResult.Data)

	if err := writePages(logger, opts, pages, assetsDir, siteDataResult.Data, renderedPages, mirrorer); err != nil {
		return err
	}

	extraSitemapURLs, err := writeAllPaginatedContent(logger, opts, pages, content, siteDataResult.Data, assetsDir, mirrorer)
	if err != nil {
		return err
	}

	// Validate that every internal <a href> link in the compiled output points
	// to a page that was actually generated. This catches broken navigation links
	// (the main cause of "access denied" in production) before S3 promotion.
	// siblingBasePaths lists other deployments sharing the same bucket (e.g. "/en",
	// "/jp") so cross-deployment links in the language switcher are not false-positives.
	// Merge explicitly declared siblings (from -sibling-base-paths flag) with
	// any auto-detected ones from ui.nav.lang_links in the site data.
	siblingBasePaths := append(opts.siblingBasePaths, resolveSiblingBasePaths(siteDataResult.Data)...)
	if err := linkcheck.ValidateAndReport(opts.outDir, opts.basePath, siblingBasePaths); err != nil {
		return fmt.Errorf("link validation: %w", err)
	}
	logger.Info("internal link check passed", "out_dir", opts.outDir)

	if err := maybeGenerateSitemap(logger, opts, assetsDir, templatesDir, pages, extraSitemapURLs, siteDataResult.Data); err != nil {
		return err
	}
	if opts.runOutputTests {
		args := []string{"-website-root", opts.websiteRoot, "-out", opts.outDir}
		if opts.outputTestManifest != "" {
			args = append(args, "-manifest", opts.outputTestManifest)
		}
		if err := outputtestcmd.Run(args, logger); err != nil {
			return fmt.Errorf("testing compiled output: %w", err)
		}
	}

	logger.Info("build completed", "out_dir", opts.outDir)
	return nil
}

// prepareBuild parses flags, resolves paths, validates directories, and
// initialises the optional asset mirrorer. Extracted from Run to keep Run's
// cognitive complexity within the allowed threshold.
func prepareBuild(args []string, logger *slog.Logger) (opts buildOptions, assetsDir, templatesDir string, mirrorer *externalAssetMirrorer, err error) {
	opts, err = parseBuildOptions(args)
	if err != nil {
		return
	}

	assetsDir, templatesDir, err = resolveBuildPaths(opts)
	if err != nil {
		return
	}

	logBuildStart(logger, opts, assetsDir, templatesDir)
	for _, notice := range opts.deprecations {
		logger.Warn("deprecated flag", "detail", notice)
	}

	if err = validateBuildDirs(assetsDir, templatesDir); err != nil {
		return
	}
	if err = ensureOutDir(opts.outDir); err != nil {
		return
	}
	if err = maybeCopyAssets(opts, assetsDir); err != nil {
		return
	}

	mirrorer, err = maybeInitMirrorer(opts)
	return
}

// loadOptionalContent loads blog posts and courses/listings from the paths
// specified in opts, runs the collection engine, and injects the data into
// siteData for template rendering.
func loadOptionalContent(logger *slog.Logger, opts buildOptions, siteData map[string]any) (*optionalContent, error) {
	content := &optionalContent{}

	// Inject section flags AFTER contract validation (this function runs post-
	// validation), so sections.<name> needs no contract entry and cannot dangle.
	// Templates + filterInternalPages + the sitemap read these via sectionEnabled.
	injectSectionFlags(siteData, opts.disabledSections)

	// A disabled section (sections.<name>: false) loads no content, so no
	// listing/detail pages, RSS, or home carousel are produced for it. Combined
	// with filterInternalPages dropping its base page, the section is fully absent.
	if opts.postsDir != "" && sectionEnabled(siteData, "blog") {
		loaded, err := posts.LoadPostsDir(opts.postsDir)
		if err != nil {
			return nil, fmt.Errorf("loading blog posts: %w", err)
		}
		if err := posts.CopyPostImages(loaded, opts.outDir); err != nil {
			return nil, fmt.Errorf("copying post images: %w", err)
		}
		content.posts = loaded
		injectPostsBlogList(siteData, loaded, opts.itemsPerPage)
	}

	// The collection engine runs between injectSectionFlags and
	// pruneDisabledSectionData, exactly like the typed loaders around it: after
	// the section flags exist so sectionEnabled can gate a collection, and
	// before pruning so a disabled section's stale site data is still scrubbed.
	collectionSet, err := loadCollections(logger, opts, siteData)
	if err != nil {
		return nil, err
	}
	content.collections = collectionSet

	if opts.listingsFile != "" && sectionEnabled(siteData, "listings") {
		loaded, err := listings.LoadListingsFile(opts.listingsFile)
		if err != nil {
			return nil, fmt.Errorf("loading listings: %w", err)
		}
		content.listings = loaded
		injectListingsData(siteData, loaded)
	}

	pruneDisabledSectionData(siteData, opts.disabledSections)

	return content, nil
}

// findPaginationTemplates records the page templates that later writers need
// but filterInternalPages is about to remove from the slice — the "post"/"blog"
// pair, the still-typed "listing" detail template, and (through the collection
// set) every migrated collection's detail template.
func findPaginationTemplates(pages []sitegen.PageTemplate, content *optionalContent) {
	for i := range pages {
		switch pages[i].Name {
		case "post":
			tmp := pages[i]
			content.postTemplate = &tmp
		case "blog":
			tmp := pages[i]
			content.blogTemplate = &tmp
		case "listing":
			tmp := pages[i]
			content.listingTemplate = &tmp
		}
	}
	content.collections.captureDetailTemplates(pages)
}

// writeAllPaginatedContent writes individual post pages, the RSS feed, every
// collection's detail and paginated index pages, and the paginated blog
// listings. Returns the extra sitemap URL items they produce.
//
// The call ORDER here is part of the output contract: extraSitemapURLs is
// emitted in this sequence and generateSitemapFromPages does not re-sort it. It
// is preserved from before the collection engine existed — all detail writers,
// then the blog listings, then the paginated collection indexes — so migrating a
// collection onto the engine reshuffles nothing. See the comment above
// writeAllCollectionDetailPages.
func writeAllPaginatedContent(
	logger *slog.Logger,
	opts buildOptions,
	pages []sitegen.PageTemplate,
	content *optionalContent,
	siteData map[string]any,
	assetsDir string,
	mirrorer *externalAssetMirrorer,
) ([]sitemap.URLItem, error) {
	var extraSitemapURLs []sitemap.URLItem

	if err := maybeWritePostContent(logger, opts, content, siteData, assetsDir, mirrorer); err != nil {
		return nil, err
	}

	collectionDetailURLs, err := writeAllCollectionDetailPages(logger, opts, content.collections, siteData, assetsDir, mirrorer)
	if err != nil {
		return nil, err
	}
	extraSitemapURLs = append(extraSitemapURLs, collectionDetailURLs...)

	listingDetailURLs, err := maybeWriteListingDetailPages(logger, opts, content, siteData, assetsDir, mirrorer)
	if err != nil {
		return nil, err
	}
	extraSitemapURLs = append(extraSitemapURLs, listingDetailURLs...)

	blogURLs, err := maybeWriteBlogListings(logger, opts, content, siteData, assetsDir, mirrorer)
	if err != nil {
		return nil, err
	}
	extraSitemapURLs = append(extraSitemapURLs, blogURLs...)

	collectionIndexURLs, err := writeAllCollectionIndexPages(logger, opts, pages, content.collections, siteData, assetsDir, mirrorer)
	if err != nil {
		return nil, err
	}
	extraSitemapURLs = append(extraSitemapURLs, collectionIndexURLs...)

	return extraSitemapURLs, nil
}

// maybeWritePostContent writes individual blog post pages and the RSS feed
// when a post template and loaded posts are available.
func maybeWritePostContent(logger *slog.Logger, opts buildOptions, content *optionalContent, siteData map[string]any, assetsDir string, mirrorer *externalAssetMirrorer) error {
	if content.postTemplate == nil || len(content.posts) == 0 {
		return nil
	}
	if err := ValidatePostLangs(logger, content.posts, siteData); err != nil {
		return fmt.Errorf("validating post languages: %w", err)
	}
	if err := writePostPages(logger, opts, *content.postTemplate, content.posts, siteData, assetsDir, mirrorer); err != nil {
		return fmt.Errorf("writing post pages: %w", err)
	}
	if err := writeRSSFeed(opts.outDir, siteData, content.posts); err != nil {
		return fmt.Errorf("writing rss feed: %w", err)
	}
	return nil
}

// maybeWriteBlogListings generates paginated /blog/ listing pages when a blog
// template and loaded posts are available.
func maybeWriteBlogListings(logger *slog.Logger, opts buildOptions, content *optionalContent, siteData map[string]any, assetsDir string, mirrorer *externalAssetMirrorer) ([]sitemap.URLItem, error) {
	if content.blogTemplate == nil || len(content.posts) == 0 {
		return nil, nil
	}
	urls, err := writeBlogPaginatedPages(logger, opts, *content.blogTemplate, content.posts, siteData, assetsDir, mirrorer)
	if err != nil {
		return nil, fmt.Errorf("writing blog listing pages: %w", err)
	}
	return urls, nil
}

func writePages(logger *slog.Logger, opts buildOptions, pages []sitegen.PageTemplate, assetsDir string, siteData map[string]any, renderedPages map[string]string, mirrorer *externalAssetMirrorer) error {
	allToCopy := make(map[string]string) // hashedRelPath → originalRelPath

	for _, page := range pages {
		slug := resolvePageSlug(siteData, page.Name)
		target, err := resolvePageTarget(opts.outDir, slug, opts.cleanURLs)
		if err != nil {
			return err
		}
		// Runs on the pre-transform HTML so it still sees <img> tags that
		// applyPositionBasedTransforms is about to inline into <svg> (icons
		// under svgInlineSizeLimit lose their <img> tag entirely).
		validateImageAltText(logger, page.Name, renderedPages[page.Name])
		htmlOut, pageCopy, err := transformPage(renderedPages[page.Name], opts, assetsDir, mirrorer)
		if err != nil {
			return fmt.Errorf("transforming %s: %w", target, err)
		}
		if err := validatePageBaseHref(htmlOut, opts.basePath, slug, page.Name); err != nil {
			return err
		}
		if err := validateRenderedPageStructure(page.Name, htmlOut); err != nil {
			return err
		}
		htmlOut = injectHreflangAlternates(htmlOut, siteData, page.Name, opts.cleanURLs)
		htmlOut = injectLangSwitcherHrefs(htmlOut, siteData, page.Name, opts.cleanURLs)
		for k, v := range pageCopy {
			allToCopy[k] = v
		}
		if err := os.WriteFile(target, []byte(htmlOut), 0o644); err != nil { //nolint:gosec
			return fmt.Errorf(errFmtWriting, target, err)
		}
		logger.Info("generated page", "page", page.Name, "target", target)
		fmt.Fprintln(os.Stdout, target)
	}

	// Manifest icons are fingerprinted here (rather than in maybeCopyAssets,
	// which runs before siteData/basePath are known) so the rewrite can prepend
	// basePath to root-absolute icon refs exactly like fingerprintLocalAssets
	// does for HTML, and so the resulting toCopy entries are covered by the same
	// writeHashedAssets pass below. Gated the same way maybeCopyAssets is: no
	// static assets at all means no manifest processing either.
	if opts.copyAssets && !opts.inlineAssets {
		if err := writeManifestFiles(assetsDir, opts.outDir, opts.basePath, allToCopy); err != nil {
			return fmt.Errorf("writing manifest files: %w", err)
		}
	}

	return writeHashedAssets(opts.outDir, assetsDir, allToCopy)
}

func writeHashedAssets(outDir, assetsDir string, assets map[string]string) error {
	for hashedRel, origRel := range assets {
		src := filepath.Join(assetsDir, filepath.FromSlash(origRel))
		dst := filepath.Join(outDir, filepath.FromSlash(hashedRel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("creating dir for %s: %w", hashedRel, err)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("reading %s for hashed copy: %w", origRel, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil { //nolint:gosec
			return fmt.Errorf("writing hashed asset %s: %w", hashedRel, err)
		}
	}
	return nil
}

func resolvePageTarget(outDir, pageName string, cleanURLs bool) (string, error) {
	if cleanURLs && pageName != "index" {
		dir := filepath.Join(outDir, pageName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("creating directory for %s: %w", pageName, err)
		}
		return filepath.Join(dir, "index.html"), nil
	}
	return filepath.Join(outDir, pageName+".html"), nil
}

// validatePageBaseHref checks that the <base href> in the rendered HTML matches
// the URL where the page will actually be served. A mismatch — typically caused
// by a template using .PageName instead of pageSlug — breaks anchor links and
// relative navigations on pages whose slug differs from the internal page key
// (e.g. the EN "contato" page served at /en/contact/).
func validatePageBaseHref(html, basePath, slug, pageName string) error {
	m := baseHrefTagRE.FindStringSubmatch(html)
	if m == nil {
		return nil // no <base href>; nothing to validate
	}
	got := strings.TrimRight(m[1], "/")
	var want string
	if slug == "index" {
		want = basePath + "/"
		if m[1] == want {
			return nil
		}
	} else {
		want = basePath + "/" + slug
	}
	if got != strings.TrimRight(want, "/") {
		return fmt.Errorf(
			"page %q: <base href=%q> but page is served at %q — template must use pageSlug, not .PageName",
			pageName, m[1], want,
		)
	}
	return nil
}

// validateRenderedPageStructure catches a class of silent data-missing bugs:
// templates that use bare {{dig}} for content fields get an empty string when
// the data key is absent, and the build succeeds with invisible broken content.
// This check fails the build when the resulting HTML has empty structural
// elements that must always carry content.
//
// What it checks (defence-in-depth; templates should use {{required (dig ...)}}
// as the first line of defence):
//   - <title> must be non-empty
//   - <h1> must be non-empty
//   - <meta name="description"> must have a non-empty content attribute
func validateRenderedPageStructure(pageName, html string) error {
	var errs []string

	if m := titleTagRE.FindStringSubmatch(html); m != nil {
		if strings.TrimSpace(htmlTagsRE.ReplaceAllString(m[1], "")) == "" {
			errs = append(errs, "<title> is empty")
		}
	}

	if m := h1TagRE.FindStringSubmatch(html); m != nil {
		if strings.TrimSpace(htmlTagsRE.ReplaceAllString(m[1], "")) == "" {
			errs = append(errs, "<h1> is empty")
		}
	}

	if m := metaDescriptionRE.FindStringSubmatch(html); m != nil {
		content := m[1]
		if content == "" {
			content = m[2]
		}
		if strings.TrimSpace(content) == "" {
			errs = append(errs, `<meta name="description"> content is empty`)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf(
			"page %q has empty structural content (%s) — replace bare {{dig}} calls with {{required (dig ...) \"...\"}} for non-optional fields",
			pageName, strings.Join(errs, "; "),
		)
	}
	return nil
}

// findTemplate returns a pointer to the first PageTemplate with the given name,
// or nil if not found.
func findTemplate(pages []sitegen.PageTemplate, name string) *sitegen.PageTemplate {
	for i := range pages {
		if pages[i].Name == name {
			return &pages[i]
		}
	}
	return nil
}

// maybeGenerateSitemap generates sitemap.xml, preferring (in priority order)
// a sitemap.yaml config, then an explicit -sitemap-base-url, then a base URL
// derived from site data (see resolveSitemapBaseURL) so a site that already
// declares base_url needs no redundant sitemap-specific flag. This last
// fallback makes sitemap generation default-on: previously, a build with
// neither a config nor -sitemap-base-url silently shipped with no sitemap.xml
// at all and no warning.
//
// If no base URL can be determined by any of these, a real (non-mock) build
// now fails outright rather than silently shipping without a sitemap — see
// the error below. A mock/dev build (opts.contentSource == "mock", the same
// signal checkRobotsTxtSafety uses) is exempt, matching that a legitimate
// no-sitemap local/dev build should not be blocked.
//
// Whenever a sitemap is actually generated (any of the three paths),
// robots.txt is updated to declare it — see writeRobotsTxtOutput.
func maybeGenerateSitemap(logger *slog.Logger, opts buildOptions, assetsDir, templatesDir string, pages []sitegen.PageTemplate, extraURLs []sitemap.URLItem, siteData map[string]any) error {
	sitemapConfigPath, err := resolveSitemapConfigPath(opts.websiteRoot, opts.sitemapConfig)
	if err != nil {
		return err
	}

	if sitemapConfigPath != "" {
		baseURL, err := generateSitemapFromConfig(sitemapConfigPath, opts.websiteRoot, opts.outDir, extraURLs, siteData)
		if err != nil {
			return err
		}
		logger.Info("generated sitemap from config", "config_path", sitemapConfigPath, "target", filepath.Join(opts.outDir, sitemapXML))
		fmt.Fprintln(os.Stdout, filepath.Join(opts.outDir, sitemapXML))
		return finalizeRobotsTxt(opts, assetsDir, baseURL)
	}

	baseURL := resolveSitemapBaseURL(opts, siteData)
	if baseURL == "" {
		if opts.contentSource != "mock" {
			return fmt.Errorf(
				"no sitemap.xml could be generated for this build (-content-source=%s): "+
					"no sitemap.yaml config found (or via -sitemap-config), no -sitemap-base-url "+
					"flag, and no base_url in site data; shipping a real build with no sitemap at "+
					"all is not allowed. Add a sitemap.yaml, pass -sitemap-base-url, set base_url "+
					"in site data, or pass -content-source=mock if this is a legitimate "+
					"no-sitemap dev build",
				opts.contentSource,
			)
		}
		return finalizeRobotsTxt(opts, assetsDir, "")
	}

	if err := generateSitemapFromPages(baseURL, templatesDir, pages, opts.outDir, opts.cleanURLs, extraURLs); err != nil {
		return err
	}
	logger.Info("generated sitemap from pages", "base_url", baseURL, "target", filepath.Join(opts.outDir, sitemapXML))
	fmt.Fprintln(os.Stdout, filepath.Join(opts.outDir, sitemapXML))
	return finalizeRobotsTxt(opts, assetsDir, baseURL)
}

// resolveSitemapBaseURL resolves the base URL for default-on sitemap
// generation once no sitemap.yaml config was found: an explicit
// -sitemap-base-url always wins (the pre-existing opt-in behavior), falling
// back to siteData["base_url"] — the same site-data field already read
// elsewhere in this package for RSS feed links (posts.go) and hreflang
// canonical URLs (hreflang.go) — so sites that already declare it for those
// features get a sitemap for free. Returns "" when neither is set.
func resolveSitemapBaseURL(opts buildOptions, siteData map[string]any) string {
	if baseURL := strings.TrimSpace(opts.sitemapBaseURL); baseURL != "" {
		return baseURL
	}
	baseURL, _ := siteData["base_url"].(string)
	return strings.TrimSpace(baseURL)
}

// finalizeRobotsTxt writes robots.txt to the output directory, gated the same
// way maybeCopyAssets gates copyStaticAssets (no static assets at all means
// no robots.txt either, matching pre-existing behavior). baseURL is the base
// URL a sitemap was actually generated with this build, or "" when sitemap
// generation was legitimately skipped (a mock/dev build with no derivable
// base URL) — see writeRobotsTxtOutput for what each case does.
func finalizeRobotsTxt(opts buildOptions, assetsDir, baseURL string) error {
	if !opts.copyAssets || opts.inlineAssets {
		return nil
	}
	return writeRobotsTxtOutput(assetsDir, opts.outDir, baseURL)
}

func resolveSitemapConfigPath(websiteRoot, flagPath string) (string, error) {
	if strings.TrimSpace(flagPath) != "" {
		if _, err := os.Stat(flagPath); err != nil {
			return "", fmt.Errorf("sitemap config not found: %s (%w)", flagPath, err)
		}
		return flagPath, nil
	}

	defaultPath := filepath.Join(websiteRoot, "sitemap.yaml")
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath, nil
	}

	return "", nil
}

// validateAssetUsage checks that every local CSS/JS asset is referenced by a
// rendered page. Disabling a section legitimately orphans its assets (e.g.
// carousel.js once every carousel is hidden), so when sections are disabled an
// unused asset is downgraded to a warning — but a missing reference (a genuinely
// broken asset link) still fails the build.
func validateAssetUsage(logger *slog.Logger, opts buildOptions, assetsDir string, renderedPages map[string]string) error {
	result, err := assetusage.Validate(assetsDir, renderedPages)
	if err == nil {
		return nil
	}
	if len(opts.disabledSections) > 0 && len(result.MissingRef) == 0 {
		for _, a := range append(result.UnusedCSS, result.UnusedJS...) {
			logger.Warn("unused asset tolerated because a content section is disabled", "asset", a)
		}
		return nil
	}
	return fmt.Errorf("validating local css/js asset usage: %w", err)
}

// injectSectionFlags writes sections.<name>: false into siteData for each
// disabled section. Called after contract validation so the flag needs no
// contract entry; sectionEnabled and the templates read it back.
func injectSectionFlags(siteData map[string]any, disabled []string) {
	if len(disabled) == 0 {
		return
	}
	sections := make(map[string]any, len(disabled))
	for _, name := range disabled {
		sections[name] = false
	}
	siteData["sections"] = sections
}

// pruneDisabledSectionData removes references to disabled sections from the site
// data that templates render unconditionally: the nav/footer list, the human
// sitemap page's link + post lists, and each disabled section's table-declared
// prune hook (e.g. the home page's blog/courses blocks, which gate on data
// presence). Runs after section flags are injected — i.e. after contract
// validation — so the tracer still sees the full data. Together with
// filterInternalPages (drops the pages) and filterDisabledSectionURLs
// (sitemap.xml) this ensures a disabled section leaves no reachable link, so the
// internal link checker stays green.
func pruneDisabledSectionData(siteData map[string]any, disabled []string) {
	if len(disabled) == 0 {
		return
	}
	off := make(map[string]bool, len(disabled))
	for _, s := range disabled {
		off[s] = true
	}
	// nav + footer both range over siteData["nav"].
	siteData["nav"] = filterLinksBySection(siteData["nav"], off)
	if pages, ok := siteData["pages"].(map[string]any); ok {
		if sm, ok := pages["sitemap"].(map[string]any); ok {
			sm["links"] = filterLinksBySection(sm["links"], off)
		}
	}
	for _, s := range sectionTable {
		if off[s.name] && s.prune != nil {
			s.prune(siteData)
		}
	}
}

// filterLinksBySection drops list entries whose "href" points at a disabled section.
func filterLinksBySection(v any, off map[string]bool) any {
	items, ok := v.([]any)
	if !ok {
		return v
	}
	result := make([]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			if href, ok := m["href"].(string); ok {
				if sec := sectionForPath(href); sec != "" && off[sec] {
					continue
				}
			}
		}
		result = append(result, it)
	}
	return result
}

// filterDisabledSectionURLs drops static sitemap entries belonging to a disabled
// content section so a disabled section leaves no trace in sitemap.xml.
func filterDisabledSectionURLs(urls []sitemap.URLItem, siteData map[string]any) []sitemap.URLItem {
	result := urls[:0:0]
	for _, u := range urls {
		if sec := sectionForPath(u.Path); sec != "" && !sectionEnabled(siteData, sec) {
			continue
		}
		result = append(result, u)
	}
	return result
}

// generateSitemapFromConfig returns the config's resolved base_url on success
// (already trimmed of a trailing slash by sitemap.LoadConfig) so the caller
// can pass it to finalizeRobotsTxt.
func generateSitemapFromConfig(configPath, websiteRoot, outDir string, extraURLs []sitemap.URLItem, siteData map[string]any) (string, error) {
	cfg, err := sitemap.LoadConfig(configPath)
	if err != nil {
		return "", err
	}
	cfg.URLs = filterDisabledSectionURLs(cfg.URLs, siteData)
	cfg.URLs = append(cfg.URLs, extraURLs...)

	xmlBytes, err := sitemap.GenerateXML(cfg, websiteRoot)
	if err != nil {
		return "", err
	}

	targetPath := filepath.Join(outDir, sitemapXML)
	if err := os.WriteFile(targetPath, xmlBytes, 0o644); err != nil { //nolint:gosec
		return "", fmt.Errorf(errFmtWriting, sitemapXML, err)
	}
	return cfg.BaseURL, nil
}

func generateSitemapFromPages(baseURL, templatesDir string, pages []sitegen.PageTemplate, outDir string, cleanURLs bool, extraURLs []sitemap.URLItem) error {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return fmt.Errorf("sitemap base URL cannot be empty")
	}

	urls := make([]sitemap.URLItem, 0, len(pages)+len(extraURLs))
	for _, page := range pages {
		var path string
		if page.Name == "index" {
			path = "/"
		} else if cleanURLs {
			path = "/" + page.Name
		} else {
			path = "/" + page.Name + ".html"
		}

		item := sitemap.URLItem{Path: path}
		pageTemplatePath := filepath.Join(templatesDir, "pages", page.Name+".gohtml")
		if info, err := os.Stat(pageTemplatePath); err == nil {
			item.Lastmod = info.ModTime().In(time.UTC).Format("2006-01-02")
		}
		urls = append(urls, item)
	}
	urls = append(urls, extraURLs...)

	cfg := sitemap.Config{
		BaseURL: strings.TrimRight(baseURL, "/"),
		URLs:    urls,
	}

	xmlBytes, err := sitemap.GenerateXML(cfg, "")
	if err != nil {
		return err
	}

	targetPath := filepath.Join(outDir, sitemapXML)
	if err := os.WriteFile(targetPath, xmlBytes, 0o644); err != nil { //nolint:gosec
		return fmt.Errorf(errFmtWriting, sitemapXML, err)
	}

	return nil
}

// collectSharedScripts scans every rendered page for local <script src="..."> references
// and returns the set of asset paths (without leading "/") that appear on more than one page.
// These scripts benefit from being cached externally rather than inlined into every HTML file.
func collectSharedScripts(renderedPages map[string]string) map[string]bool {
	usageCount := make(map[string]int, 16)
	for _, html := range renderedPages {
		matches := scriptTagRE.FindAllStringSubmatch(html, -1)
		for _, m := range matches {
			src := m[1]
			if isExternalRef(src) {
				continue
			}
			usageCount[strings.TrimPrefix(src, "/")]++
		}
	}
	shared := make(map[string]bool, len(usageCount))
	for path, n := range usageCount {
		if n > 1 {
			shared[path] = true
		}
	}
	return shared
}

// devBuildPayload is the shape of window.__devBuild injected by -dev-data.
type devBuildPayload struct {
	ContentSource string         `json:"contentSource"`
	ContentLangs  []string       `json:"contentLangs"`
	Posts         []devItemEntry `json:"posts"`
	Courses       []devItemEntry `json:"courses"`
	Projects      []devItemEntry `json:"projects"`
	Listings      []devItemEntry `json:"listings"`
}

// devItemEntry carries the title/slug and declared language codes for one
// content item so the dev panel's dynamic tab can filter without a rebuild.
type devItemEntry struct {
	ID    string   `json:"id"`    // slug for posts, title for courses/projects
	Langs []string `json:"langs"` // nil → available in all content languages
}

// buildDevDataPayload serialises the current build's content-source metadata
// and per-item language declarations into a JSON string for window.__devBuild.
// Returns "" when -dev-data is not set.
func buildDevDataPayload(opts buildOptions, siteData map[string]any, content *optionalContent) string {
	if !opts.devData {
		return ""
	}

	// Read content_languages from siteData (set by Phase 4).
	var contentLangs []string
	if cl, ok := siteData["content_languages"].([]any); ok {
		for _, v := range cl {
			if s, ok := v.(string); ok {
				contentLangs = append(contentLangs, s)
			}
		}
	}

	payload := devBuildPayload{
		ContentSource: opts.contentSource,
		ContentLangs:  contentLangs,
	}

	// Each field below mirrors a table-declared section (sections.go) and is
	// only populated while sectionEnabled agrees that section is on, so the
	// dev payload never advertises content a disabled section has hidden
	// everywhere else in the build (pages, sitemap, nav) — even if content.*
	// were ever populated ahead of a section being toggled off.
	if sectionEnabled(siteData, "blog") {
		payload.Posts = make([]devItemEntry, 0, len(content.posts))
		for _, p := range content.posts {
			payload.Posts = append(payload.Posts, devItemEntry{ID: p.Meta.Slug, Langs: p.Meta.AvailableLanguages})
		}
	}
	if sectionEnabled(siteData, "courses") {
		// Langs comes from the record's supported_languages, which
		// courses.SupportedLanguages used to supply directly. recordLangs
		// deliberately returns nil rather than an empty slice for an absent
		// list: the field's documented meaning is "nil → available in all
		// content languages", so emitting [] would tell the dev panel the
		// opposite of what the typed loader did.
		payload.Courses = devEntriesFromCollection(content.collections, "courses", "supported_languages")
	}
	if sectionEnabled(siteData, "projects") {
		// Built even when the collection produced nothing, so the JSON keeps
		// emitting [] rather than null — window.__devBuild's shape is read by
		// ffreis-website's and casaboa's dev panels. Per-item IDs stay
		// title-based, matching what this payload carried before projects
		// moved onto the collection engine. projects has never declared
		// per-item languages, hence the empty langsKey.
		payload.Projects = devEntriesFromCollection(content.collections, "projects", "")
	}
	if sectionEnabled(siteData, "listings") {
		payload.Listings = make([]devItemEntry, 0, len(content.listings))
		for _, l := range content.listings {
			payload.Listings = append(payload.Listings, devItemEntry{ID: l.ID, Langs: nil})
		}
	}

	b, err := json.Marshal(payload)
	if err != nil {
		// json.Marshal cannot fail for these types; return empty to skip injection.
		return ""
	}
	return string(b)
}

// devEntriesFromCollection builds one dev-panel entry per projected record.
//
// The ID is the record's title, which is what the typed loaders supplied for
// both projects and courses. langsKey names the record field holding the
// per-item language codes, or "" for a collection that declares none.
//
// The slice is always non-nil so the JSON keeps emitting [] rather than null:
// window.__devBuild's shape is read by ffreis-website's and casaboa's dev
// panels, and decision record §7.4 defers any change to it to its own PR.
func devEntriesFromCollection(set *collectionSet, name, langsKey string) []devItemEntry {
	records := collectionRecords(set, name)
	entries := make([]devItemEntry, 0, len(records))
	for _, r := range records {
		rec, ok := r.(map[string]any)
		if !ok {
			continue
		}
		title, _ := rec["title"].(string)
		entries = append(entries, devItemEntry{ID: title, Langs: recordLangs(rec, langsKey)})
	}
	return entries
}

// recordLangs reads a record's language-code list.
//
// It returns nil — not an empty slice — when the key is absent or the list is
// empty, because devItemEntry documents nil as "available in all content
// languages". The typed courses.Course produced exactly that: an omitted
// supported_languages left the []string field nil, which marshalled to null.
// The projected record instead carries a non-nil empty []any, so this
// conversion is what keeps the emitted JSON identical.
func recordLangs(rec map[string]any, key string) []string {
	if key == "" {
		return nil
	}
	raw, _ := rec[key].([]any)
	if len(raw) == 0 {
		return nil
	}
	langs := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			langs = append(langs, s)
		}
	}
	if len(langs) == 0 {
		return nil
	}
	return langs
}
