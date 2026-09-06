package buildcmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"ffreis-website-compiler/internal/cmdutil"
)

type buildOptions struct {
	websiteRoot    string
	assetsDir      string
	templatesDir   string
	sitemapConfig  string
	sitemapBaseURL string
	siteDataSource string
	outDir         string
	postsDir       string
	projectsFile   string
	coursesFile    string
	listingsFile   string
	// contentSource is "prod" (default), "mock", or "dev". When "prod", the
	// build fails if any content path contains /mock/ — prevents mock data
	// reaching prod by accident. Set to "mock" explicitly to opt in to dev
	// content. Set to "dev" for a dev.ffreis.com-style build against real
	// content paths; it implies allowBlanketRobotsDisallow (set in
	// parseBuildOptions) so a dev-only robots.txt block doesn't need a second
	// flag, but it does NOT exempt /mock/ content paths — only "mock" does.
	contentSource string
	// allowBlanketRobotsDisallow opts out of checkRobotsTxtSafety's guard
	// against a committed robots.txt that disallows all crawlers on a non-mock
	// (real content) build. False by default: such a robots.txt fails the
	// build instead of silently shipping to prod. Implied automatically when
	// contentSource is "dev"; only needs setting explicitly for a deliberate
	// full block on a "prod" build.
	allowBlanketRobotsDisallow bool
	itemsPerPage               int
	copyAssets                 bool
	inlineAssets               bool
	jsInlineThreshold          int
	jsSharedInlineThreshold    int             // -1 = disabled; use jsInlineThreshold for all scripts
	sharedScripts              map[string]bool // populated at runtime by collectSharedScripts
	embedFonts                 bool
	inlineBodyCSS              bool
	rasterInlineThreshold      int
	siblingBasePaths           []string
	mirrorExternalAssets       bool
	mirroredAssetsDir          string
	enableSanity               bool
	strictContract             bool
	cleanURLs                  bool
	runOutputTests             bool
	outputTestManifest         string
	// disabledSections lists content sections (see sectionTable in sections.go
	// for the current set) to hide: their pages, nav/footer entries, home-page
	// blocks, and sitemap URLs are omitted. Injected into siteData as
	// sections.<name>: false AFTER contract validation, so it needs no contract
	// entry and cannot dangle.
	disabledSections []string
	// basePath mirrors site_data["base_path"] (e.g. "/en") and is populated by
	// the build command after siteData is loaded. It is prepended to absolute
	// asset references emitted by fingerprintLocalAssets so they remain
	// reachable when the site is deployed under a path prefix.
	basePath string
	// Tracker SDK injection. When trackerEnabled is true the compiler injects a
	// <script> tag pointing at trackerCDNBase plus a Tracker.init(...) snippet
	// before </head> on every rendered page. Sourced from per-site inventory.
	trackerEnabled    bool
	trackerSDKVersion string
	trackerSiteID     string
	trackerEndpoint   string
	// trackerCDNBase has no default. parseBuildOptions requires it to be set
	// whenever trackerEnabled is true; a build fails at flag-parsing time
	// otherwise instead of silently falling back to a hardcoded CDN.
	trackerCDNBase string
	// devData enables injection of window.__devBuild before </head> on every
	// rendered page. devDataJSON holds the pre-serialised JSON payload built from
	// siteData + content after loading; it is populated by buildDevDataPayload and
	// stored here so transformPage can inject it without re-reading siteData.
	devData     bool
	devDataJSON string
	// collectionsConfig overrides the default <website-root>/collections.yaml
	// discovery, mirroring -sitemap-config.
	collectionsConfig string
	// collectionSources maps a collection name to the file path supplying its
	// records (-collection-source <name>=<path>, repeatable). The legacy
	// per-type flags below populate this too, so there is exactly one lookup
	// path downstream.
	collectionSources map[string]string
	// deprecations carries flag-deprecation notices raised during parsing.
	// parseBuildOptions has no logger (and staying pure keeps it easy to test),
	// so prepareBuild emits these through slog once one is available.
	deprecations []string
}

// collectionSourceFlag implements flag.Value for the repeatable
// -collection-source <name>=<path>.
type collectionSourceFlag map[string]string

func (f collectionSourceFlag) String() string {
	if len(f) == 0 {
		return ""
	}
	parts := make([]string, 0, len(f))
	for name, path := range f {
		parts = append(parts, name+"="+path)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func (f collectionSourceFlag) Set(v string) error {
	name, path, ok := strings.Cut(v, "=")
	name = strings.TrimSpace(name)
	path = strings.TrimSpace(path)
	if !ok || name == "" || path == "" {
		return fmt.Errorf("expected <name>=<path>, got %q", v)
	}
	if existing, dup := f[name]; dup {
		return fmt.Errorf("collection %q already has a source (%s); pass -collection-source %s= once", name, existing, name)
	}
	f[name] = path
	return nil
}

// disableSectionsHelpText builds the -disable-sections flag's usage string
// from sectionNames() so the list of valid section names can never drift from
// what pageSection/sectionForPath (sections.go) actually recognize.
func disableSectionsHelpText() string {
	return fmt.Sprintf(
		"comma-separated content sections to hide entirely (%s): drops their pages, nav/footer links, home-page blocks and sitemap entries",
		strings.Join(sectionNames(), ","),
	)
}

func parseBuildOptions(args []string) (buildOptions, error) {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)

	var opts buildOptions
	var assetsDirFlag string
	var siteDirFlag string
	var templatesDirFlag string

	opts.collectionSources = make(map[string]string)
	fs.Var(collectionSourceFlag(opts.collectionSources), "collection-source",
		"content source for one collection as <name>=<path>; repeatable. Replaces the per-type -projects-file/-courses-file/-listings-file flags, which are deprecated aliases for it")
	fs.StringVar(&opts.collectionsConfig, "collections-config", "",
		"optional path to a collections YAML config; defaults to <website-root>/collections.yaml if present. Collections not defined there fall back to the compiler's built-in projects/courses/listings definitions")

	fs.StringVar(&opts.websiteRoot, "website-root", ".", "website project root; expects <website-root>/src/{assets,templates} (legacy fallback: <website-root>/{site,templates})")
	fs.StringVar(&assetsDirFlag, "assets-dir", "", "path to source assets folder (defaults to <website-root>/src/assets, then <website-root>/site)")
	fs.StringVar(&siteDirFlag, "site-dir", "", "legacy alias for -assets-dir")
	fs.StringVar(&templatesDirFlag, "templates-dir", "", "path to templates root folder (defaults to <website-root>/src/templates, then <website-root>/templates)")
	fs.StringVar(&opts.sitemapConfig, "sitemap-config", "", "optional path to sitemap YAML config; defaults to <website-root>/sitemap.yaml if present")
	fs.StringVar(&opts.sitemapBaseURL, "sitemap-base-url", "", "base URL for automatic sitemap.xml generation when no sitemap config file is found; overrides site data's base_url. Sitemap generation is default-on: a real (non-mock) build fails if neither this, a sitemap config, nor site data's base_url resolves one")
	fs.StringVar(&opts.siteDataSource, "site-data", "", "optional site data source override; supports file/URL sources or a directory containing YAML layers")
	fs.StringVar(&opts.outDir, "out", "dist", "output directory for generated static site")
	fs.BoolVar(&opts.copyAssets, "copy-assets", true, "copy static assets from assets dir into output")
	fs.BoolVar(&opts.inlineAssets, "inline-assets", false, "inline local css/js/images into each html for self-contained pages")
	fs.IntVar(&opts.jsInlineThreshold, "js-inline-threshold", defaultJSInlineThreshold, "inline local <script src> files smaller than this many bytes as <script> blocks; 0 to disable")
	fs.IntVar(&opts.jsSharedInlineThreshold, "js-shared-inline-threshold", -1, "for JS files referenced by more than one page, only inline if smaller than this many bytes; -1 to disable (all JS uses -js-inline-threshold regardless of page count)")
	fs.BoolVar(&opts.embedFonts, "embed-fonts", false, "embed font files (woff2/woff/ttf/otf/eot) as base64 data URIs in inlined CSS; eliminates font files from dist but increases HTML size")
	fs.BoolVar(&opts.inlineBodyCSS, "inline-body-css", false, "inline body <link rel=stylesheet> as <style> blocks instead of deferred external links; eliminates CSS files from dist but prevents cross-page CSS cache reuse")
	fs.IntVar(&opts.rasterInlineThreshold, "raster-inline-threshold", 0, "inline local raster <img> files smaller than this many bytes as base64 data URIs; 0 to disable; skips LQIP-processed images and SVGs")
	var siblingBasePathsFlag string
	fs.StringVar(&siblingBasePathsFlag, "sibling-base-paths", "", "comma-separated URL prefixes of sibling deployments sharing the same CloudFront distribution (e.g. 'en,jp'); links under these prefixes are skipped by the internal link checker")
	var disableSectionsFlag string
	fs.StringVar(&disableSectionsFlag, "disable-sections", "", disableSectionsHelpText())
	fs.BoolVar(&opts.mirrorExternalAssets, "mirror-external-assets", false, "download external css/js/image/font assets into output and rewrite references to local copies")
	fs.StringVar(&opts.mirroredAssetsDir, "mirrored-assets-dir", "external", "subdirectory inside output for mirrored external assets")
	fs.BoolVar(&opts.enableSanity, "sanity", true, "fail the build if generic sanity checks fail (site contract + invariants + asset reachability)")
	fs.BoolVar(&opts.strictContract, "strict-contract", true, "fail if any allowed contract path is not referenced by any template (disable for local dev with in-progress templates)")
	fs.BoolVar(&opts.cleanURLs, "clean-urls", false, "output each page as <name>/index.html instead of <name>.html for extension-free URLs; updates sitemap accordingly")
	fs.BoolVar(&opts.runOutputTests, "run-output-tests", false, "run the website-owned compiled-output manifest after a successful build")
	fs.StringVar(&opts.outputTestManifest, "output-test-manifest", "", "website-owned output test manifest; defaults to tests/output.yaml under website-root when -run-output-tests is set")
	fs.StringVar(&opts.postsDir, "posts-dir", "", "path to blog posts directory (posts/<slug>/index.md layout); enables Markdown blog post generation and RSS feed when set")
	fs.StringVar(&opts.projectsFile, "projects-file", "", "DEPRECATED, use -collection-source projects=<path>: path to projects.yaml; enables /projects/ paginated page generation when set")
	fs.StringVar(&opts.coursesFile, "courses-file", "", "DEPRECATED, use -collection-source courses=<path>: path to courses.yaml; enables /courses/ paginated page generation when set")
	fs.StringVar(&opts.listingsFile, "listings-file", "", "DEPRECATED, use -collection-source listings=<path>: path to a listings YAML file (shaped as a top-level \"listings:\" key); enables /listings/<id>/ detail-page generation when set")
	fs.StringVar(&opts.contentSource, "content-source", "prod", `content source: "prod" (default), "mock", or "dev". When "prod", any content path containing /mock/ is a fatal error so mock data cannot reach production by accident. "dev" is a dev.ffreis.com-style build against real content paths (not /mock/) and implies -allow-blanket-robots-disallow.`)
	fs.BoolVar(&opts.allowBlanketRobotsDisallow, "allow-blanket-robots-disallow", false, "allow a committed robots.txt that disallows all crawlers under User-agent: * even on a non-mock (real content) build; without this flag, such a robots.txt fails the build so a dev-only block cannot silently ship to prod. Implied automatically by -content-source=dev; only needed explicitly for a deliberate full block on -content-source=prod")
	fs.IntVar(&opts.itemsPerPage, "items-per-page", 12, "number of items per paginated page for projects, courses, and blog")

	fs.BoolVar(&opts.trackerEnabled, "tracker-enabled", false, "inject the Tracker SDK script tag + Tracker.init(...) before </head>; requires -tracker-sdk-version, -tracker-site-id, -tracker-endpoint, -tracker-cdn-base")
	fs.StringVar(&opts.trackerSDKVersion, "tracker-sdk-version", "", "semver of the SDK to load from the CDN (e.g. 1.0.0); pinned per site in inventory.yaml")
	fs.StringVar(&opts.trackerSiteID, "tracker-site-id", "", "site identifier passed to Tracker.init (e.g. flemming, ffreis, petlook)")
	fs.StringVar(&opts.trackerEndpoint, "tracker-endpoint", "", "ingestion endpoint passed to Tracker.init (e.g. https://events.flemming.com.br)")
	fs.StringVar(&opts.trackerCDNBase, "tracker-cdn-base", "", "CDN base URL for the SDK script (e.g. https://cdn.ffreis.com); required when -tracker-enabled is set, no default is assumed")
	fs.BoolVar(&opts.devData, "dev-data", false, "inject window.__devBuild JSON before </head> on every page; for dev.ffreis.com only — never pass on prod builds")

	if err := fs.Parse(args); err != nil {
		return buildOptions{}, err
	}

	switch opts.contentSource {
	case "prod", "mock", "dev":
		// valid
	default:
		return buildOptions{}, fmt.Errorf(
			`invalid --content-source=%q: must be "prod", "mock", or "dev"`,
			opts.contentSource,
		)
	}

	// -content-source=dev implies -allow-blanket-robots-disallow: a
	// dev.ffreis.com build commonly ships a robots.txt that blocks every
	// crawler (correctly — there's no production audience there yet), and
	// checkRobotsTxtSafety's guard against that shape only exempts "mock" by
	// default. Without this, every dev build would need both flags passed
	// separately just to avoid a false-positive build failure. A genuine,
	// deliberate full block on a real --content-source=prod build still needs
	// -allow-blanket-robots-disallow passed explicitly.
	if opts.contentSource == "dev" {
		opts.allowBlanketRobotsDisallow = true
	}

	adoptLegacyContentFlags(&opts)

	// Anti-leak guard: reject /mock/ paths for any content-source other than
	// "mock" itself (i.e. "prod" or "dev"). A dev build is expected to run
	// against real content paths, so it gets no /mock/ exemption — only an
	// explicit --content-source=mock build may reference /mock/ paths.
	if opts.contentSource != "mock" {
		if err := checkNoMockContentPaths(opts); err != nil {
			return buildOptions{}, err
		}
	}

	// Anti-leak guard: -dev-data injects window.__devBuild into every rendered
	// page and is documented as dev.ffreis.com-only. Equality (not != "mock")
	// is deliberate: --content-source=dev (the dev.ffreis.com shorthand this
	// was future-proofed for — see git blame) is exactly the non-prod,
	// non-mock value that must NOT trip this prod-only gate; -dev-data
	// composes cleanly with --content-source=dev.
	if opts.devData && opts.contentSource == "prod" {
		return buildOptions{}, fmt.Errorf(
			"--dev-data cannot be used with --content-source=prod: " +
				"--dev-data injects window.__devBuild and is for dev.ffreis.com only, " +
				"never for a prod build; pass --content-source=mock or drop --dev-data",
		)
	}

	// -tracker-cdn-base has no default: require it explicitly whenever the
	// tracker is enabled instead of silently pointing at a hardcoded CDN.
	if opts.trackerEnabled && opts.trackerCDNBase == "" {
		return buildOptions{}, fmt.Errorf(
			"--tracker-cdn-base is required when --tracker-enabled is set: " +
				"no default CDN base is assumed; pass --tracker-cdn-base explicitly",
		)
	}

	if assetsDirFlag == "" && siteDirFlag != "" {
		assetsDirFlag = siteDirFlag
	}
	opts.assetsDir = assetsDirFlag
	opts.templatesDir = templatesDirFlag

	if siblingBasePathsFlag != "" {
		for _, s := range strings.Split(siblingBasePathsFlag, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				opts.siblingBasePaths = append(opts.siblingBasePaths, s)
			}
		}
	}

	for _, s := range strings.Split(disableSectionsFlag, ",") {
		if s = strings.TrimSpace(s); s != "" {
			opts.disabledSections = append(opts.disabledSections, s)
		}
	}

	return opts, nil
}

// legacyContentFlags maps each deprecated per-type content flag to the
// collection name it now feeds. Keep this table and the flag registrations
// above in step: it is the only thing keeping -projects-file working.
var legacyContentFlags = []struct {
	flag       string
	collection string
	value      func(*buildOptions) *string
}{
	{"projects-file", "projects", func(o *buildOptions) *string { return &o.projectsFile }},
	{"courses-file", "courses", func(o *buildOptions) *string { return &o.coursesFile }},
	{"listings-file", "listings", func(o *buildOptions) *string { return &o.listingsFile }},
}

// adoptLegacyContentFlags folds the deprecated per-type flags into
// collectionSources so downstream code has exactly one lookup path. An
// explicit -collection-source for the same collection wins, and the clash is
// reported rather than silently resolved.
func adoptLegacyContentFlags(opts *buildOptions) {
	for _, legacy := range legacyContentFlags {
		path := *legacy.value(opts)
		if path == "" {
			continue
		}
		if existing, ok := opts.collectionSources[legacy.collection]; ok {
			opts.deprecations = append(opts.deprecations, fmt.Sprintf(
				"-%s is deprecated and was ignored because -collection-source %s=%s was also passed; drop -%s",
				legacy.flag, legacy.collection, existing, legacy.flag,
			))
			continue
		}
		opts.collectionSources[legacy.collection] = path
		opts.deprecations = append(opts.deprecations, fmt.Sprintf(
			"-%s is deprecated; use -collection-source %s=%s instead",
			legacy.flag, legacy.collection, path,
		))
	}
}

// checkNoMockContentPaths is the /mock/ anti-leak guard's path enumeration.
//
// Decision record Q9 / §6.1: this list used to be hand-maintained, so any new
// content-path mechanism silently stopped being covered. -collection-source is
// enumerated here in the same change that introduces it, and postsDir is still
// listed because blog posts are deliberately out of the collection migration.
// The legacy per-type flags are covered transitively: adoptLegacyContentFlags
// has already folded them into collectionSources by the time this runs.
func checkNoMockContentPaths(opts buildOptions) error {
	paths := make([]string, 0, len(opts.collectionSources)+1)
	paths = append(paths, opts.postsDir)
	for _, name := range sortedSourceNames(opts.collectionSources) {
		paths = append(paths, opts.collectionSources[name])
	}

	for _, p := range paths {
		if strings.Contains(p, "/mock/") || strings.HasSuffix(p, "/mock") {
			return fmt.Errorf(
				"content path %q contains /mock/ but --content-source=%q: "+
					"mock content cannot be used outside --content-source=mock; "+
					"pass --content-source=mock to enable mock content",
				p, opts.contentSource,
			)
		}
	}
	return nil
}

func sortedSourceNames(sources map[string]string) []string {
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func resolveBuildPaths(opts buildOptions) (assetsDir string, templatesDir string, err error) {
	assetsDir = opts.assetsDir
	templatesDir = opts.templatesDir
	if assetsDir != "" && templatesDir != "" {
		return assetsDir, templatesDir, nil
	}

	resolvedAssetsDir, resolvedTemplatesDir, err := cmdutil.ResolveWebsitePaths(opts.websiteRoot)
	if err != nil {
		return "", "", err
	}

	if assetsDir == "" {
		assetsDir = resolvedAssetsDir
	}
	if templatesDir == "" {
		templatesDir = resolvedTemplatesDir
	}

	return assetsDir, templatesDir, nil
}

func logBuildStart(logger *slog.Logger, opts buildOptions, assetsDir, templatesDir string) {
	logger.Info(
		"starting website build",
		"website_root", opts.websiteRoot,
		"assets_dir", assetsDir,
		"templates_dir", templatesDir,
		"out_dir", opts.outDir,
		"copy_assets", opts.copyAssets,
		"inline_assets", opts.inlineAssets,
		"mirror_external_assets", opts.mirrorExternalAssets,
		"mirrored_assets_dir", opts.mirroredAssetsDir,
	)
}

func validateBuildDirs(assetsDir, templatesDir string) error {
	if _, err := os.Stat(assetsDir); err != nil {
		return fmt.Errorf("assets directory not found: %s (%w)", assetsDir, err)
	}
	if _, err := os.Stat(templatesDir); err != nil {
		return fmt.Errorf("templates directory not found: %s (%w)", templatesDir, err)
	}
	return nil
}

func ensureOutDir(outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	return nil
}

func maybeCopyAssets(opts buildOptions, assetsDir string) error {
	if !opts.copyAssets || opts.inlineAssets {
		return nil
	}
	if err := checkRobotsTxtSafety(assetsDir, opts); err != nil {
		return err
	}
	if err := copyStaticAssets(assetsDir, opts.outDir); err != nil {
		return fmt.Errorf("copying assets: %w", err)
	}
	return nil
}

func maybeInitMirrorer(opts buildOptions) (*externalAssetMirrorer, error) {
	if !opts.mirrorExternalAssets {
		return nil, nil
	}
	// External url() refs inside inline <style> blocks are handled by mirrorer.rewriteHTML,
	// which runs per-page in transformPage. No pre-pass over copied CSS files is needed
	// because css/ originals are no longer copied to the output directory.
	return newExternalAssetMirrorer(opts.outDir, opts.mirroredAssetsDir), nil
}
