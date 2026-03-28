package buildcmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ffreis-website-compiler/internal/assetusage"
	"ffreis-website-compiler/internal/sitegen"
	"ffreis-website-compiler/internal/sitemap"
)

var (
	stylesheetTagRE = regexp.MustCompile(`(?is)<link\s+[^>]*rel=["']stylesheet["'][^>]*href=["']([^"']+)["'][^>]*>`)
	preloadTagRE    = regexp.MustCompile(`(?is)<link\s+[^>]*rel=["'][^"']*preload[^"']*["'][^>]*href=["']([^"']+)["'][^>]*>`)
	scriptTagRE     = regexp.MustCompile(`(?is)<script\s+[^>]*src=["']([^"']+)["'][^>]*>\s*</script>`)
	imgTagRE        = regexp.MustCompile(`(?is)<img\s+[^>]*src=["']([^"']+)["'][^>]*>`)
	iconTagRE       = regexp.MustCompile(`(?is)<link\s+[^>]*rel=["'][^"']*icon[^"']*["'][^>]*href=["']([^"']+)["'][^>]*>`)
	cssURLRE        = regexp.MustCompile(`url\(\s*['"]?([^'"\)]+)['"]?\s*\)`)
	cssImportRE     = regexp.MustCompile(`(?is)@import\s+(?:url\(\s*)?['"]?([^'"\)\s;]+)['"]?\s*\)?([^;]*);`)
)

func Run(args []string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	websiteRoot := fs.String("website-root", ".", "website project root; expects <website-root>/src/{assets,templates} (legacy fallback: <website-root>/{site,templates})")
	assetsDirFlag := fs.String("assets-dir", "", "path to source assets folder (defaults to <website-root>/src/assets, then <website-root>/site)")
	siteDirFlag := fs.String("site-dir", "", "legacy alias for -assets-dir")
	templatesDirFlag := fs.String("templates-dir", "", "path to templates root folder (defaults to <website-root>/src/templates, then <website-root>/templates)")
	sitemapConfigFlag := fs.String("sitemap-config", "", "optional path to sitemap YAML config; defaults to <website-root>/sitemap.yaml if present")
	sitemapBaseURL := fs.String("sitemap-base-url", "", "optional base URL for automatic sitemap.xml generation when no sitemap config file is found")
	siteDataSource := fs.String("site-data", "", "optional site data source override; supports file/URL sources or a directory containing YAML layers")
	outDir := fs.String("out", "dist", "output directory for generated static site")
	copyAssets := fs.Bool("copy-assets", true, "copy static assets from assets dir into output")
	inlineAssets := fs.Bool("inline-assets", false, "inline local css/js/images into each html for self-contained pages")
	mirrorExternalAssets := fs.Bool("mirror-external-assets", false, "download external css/js/image/font assets into output and rewrite references to local copies")
	mirroredAssetsDir := fs.String("mirrored-assets-dir", "external", "subdirectory inside output for mirrored external assets")
	enableSanity := fs.Bool("sanity", true, "fail the build if generic sanity checks fail (site contract + invariants + asset reachability)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	assetsDir := *assetsDirFlag
	if assetsDir == "" && *siteDirFlag != "" {
		assetsDir = *siteDirFlag
	}
	templatesDir := *templatesDirFlag

	if assetsDir == "" || templatesDir == "" {
		resolvedAssetsDir, resolvedTemplatesDir, err := resolveWebsitePaths(*websiteRoot)
		if err != nil {
			return err
		}
		if assetsDir == "" {
			assetsDir = resolvedAssetsDir
		}
		if templatesDir == "" {
			templatesDir = resolvedTemplatesDir
		}
	}

	logger.Info(
		"starting website build",
		"website_root", *websiteRoot,
		"assets_dir", assetsDir,
		"templates_dir", templatesDir,
		"out_dir", *outDir,
		"copy_assets", *copyAssets,
		"inline_assets", *inlineAssets,
		"mirror_external_assets", *mirrorExternalAssets,
		"mirrored_assets_dir", *mirroredAssetsDir,
	)

	if _, err := os.Stat(assetsDir); err != nil {
		return fmt.Errorf("assets directory not found: %s (%w)", assetsDir, err)
	}
	if _, err := os.Stat(templatesDir); err != nil {
		return fmt.Errorf("templates directory not found: %s (%w)", templatesDir, err)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	if *copyAssets && !*inlineAssets {
		if err := copyStaticAssets(assetsDir, *outDir); err != nil {
			return fmt.Errorf("copying assets: %w", err)
		}
	}

	var mirrorer *externalAssetMirrorer
	if *mirrorExternalAssets {
		mirrorer = newExternalAssetMirrorer(*outDir, *mirroredAssetsDir)
		if *copyAssets && !*inlineAssets {
			if err := mirrorExternalAssetsInCopiedCSS(filepath.Join(*outDir, "css"), mirrorer); err != nil {
				return fmt.Errorf("mirroring external assets in copied css: %w", err)
			}
		}
	}

	pages, err := sitegen.LoadPageTemplatesFromRoot(templatesDir)
	if err != nil {
		return fmt.Errorf("loading templates: %w", err)
	}
	siteDataResult, err := sitegen.LoadSiteData(templatesDir, *siteDataSource)
	if err != nil {
		return fmt.Errorf("loading site data: %w", err)
	}
	siteDataContractResult, err := sitegen.LoadSiteDataContract(templatesDir)
	if err != nil {
		return fmt.Errorf("loading site data contract: %w", err)
	}
	if siteDataResult.UsedOverride && siteDataResult.DefaultPathFound {
		logger.Warn(
			"site data override supersedes local site data file",
			"override_source", siteDataResult.Source,
			"local_site_data", siteDataResult.DefaultPath,
			"site_data_layers", siteDataResult.Layers,
		)
	}
	if err := sitegen.ValidateSiteData(siteDataResult.Data, siteDataContractResult.Contract); err != nil {
		return fmt.Errorf("validating site data against contract: %w", err)
	}
	if len(siteDataContractResult.Contract.Required) > 0 || len(siteDataContractResult.Contract.Allowed) > 0 {
		usedPaths, err := sitegen.TraceSiteDataUsage(pages, siteDataResult.Data)
		if err != nil {
			return fmt.Errorf("tracing site data usage: %w", err)
		}
		if err := sitegen.ValidateSiteDataContractUsage(siteDataContractResult.Contract, usedPaths); err != nil {
			return fmt.Errorf("validating site data contract usage: %w", err)
		}
	}
	if *enableSanity {
		if err := sitegen.ValidateSiteSanity(siteDataResult.Data, sitegen.DefaultSanityConfig()); err != nil {
			return fmt.Errorf("validating site sanity rules: %w", err)
		}
	}
	logger.Info("loaded templates", "count", len(pages), "templates_dir", templatesDir)

	renderedPages := make(map[string]string, len(pages))
	for _, page := range pages {
		var rendered bytes.Buffer
		if err := page.Tmpl.ExecuteTemplate(&rendered, "layout", sitegen.NewTemplateData(page.Name, siteDataResult.Data)); err != nil {
			return fmt.Errorf("rendering %s.html: %w", page.Name, err)
		}
		renderedPages[page.Name] = rendered.String()
	}

	if _, err := assetusage.Validate(assetsDir, renderedPages); err != nil {
		return fmt.Errorf("validating local css/js asset usage: %w", err)
	}

	for _, page := range pages {
		target := filepath.Join(*outDir, page.Name+".html")
		htmlOut := renderedPages[page.Name]
		if *inlineAssets {
			htmlOut, err = inlineLocalAssets(htmlOut, assetsDir)
			if err != nil {
				return fmt.Errorf("inlining assets in %s: %w", target, err)
			}
		}
		if mirrorer != nil {
			htmlOut, err = mirrorer.rewriteHTML(htmlOut)
			if err != nil {
				return fmt.Errorf("mirroring external assets in %s: %w", target, err)
			}
		}

		if err := os.WriteFile(target, []byte(htmlOut), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", target, err)
		}

		logger.Info("generated page", "page", page.Name, "target", target)
		fmt.Fprintln(os.Stdout, target)
	}

	sitemapConfigPath, err := resolveSitemapConfigPath(*websiteRoot, *sitemapConfigFlag)
	if err != nil {
		return err
	}
	if sitemapConfigPath != "" {
		if err := generateSitemapFromConfig(sitemapConfigPath, *websiteRoot, *outDir); err != nil {
			return err
		}
		logger.Info("generated sitemap from config", "config_path", sitemapConfigPath, "target", filepath.Join(*outDir, "sitemap.xml"))
		fmt.Fprintln(os.Stdout, filepath.Join(*outDir, "sitemap.xml"))
	} else if strings.TrimSpace(*sitemapBaseURL) != "" {
		if err := generateSitemapFromPages(*sitemapBaseURL, templatesDir, pages, *outDir); err != nil {
			return err
		}
		logger.Info("generated sitemap from pages", "base_url", strings.TrimSpace(*sitemapBaseURL), "target", filepath.Join(*outDir, "sitemap.xml"))
		fmt.Fprintln(os.Stdout, filepath.Join(*outDir, "sitemap.xml"))
	}

	logger.Info("build completed", "out_dir", *outDir)

	return nil
}

type externalAssetMirrorer struct {
	client     *http.Client
	outDir     string
	assetsDir  string
	cache      map[string]string
	inProgress map[string]string
}

func newExternalAssetMirrorer(outDir, assetsDir string) *externalAssetMirrorer {
	return &externalAssetMirrorer{
		client:     &http.Client{Timeout: 30 * time.Second},
		outDir:     outDir,
		assetsDir:  strings.Trim(strings.TrimSpace(filepath.ToSlash(assetsDir)), "/"),
		cache:      make(map[string]string),
		inProgress: make(map[string]string),
	}
}

func (m *externalAssetMirrorer) rewriteHTML(doc string) (string, error) {
	var err error

	doc, err = replaceTagWith(doc, stylesheetTagRE, func(tag string, refs []string) (string, error) {
		return m.replaceExternalRef(tag, refs[1], ".css")
	})
	if err != nil {
		return "", err
	}

	doc, err = replaceTagWith(doc, preloadTagRE, func(tag string, refs []string) (string, error) {
		return m.replaceExternalRef(tag, refs[1], hintedExtFromPreload(tag))
	})
	if err != nil {
		return "", err
	}

	doc, err = replaceTagWith(doc, scriptTagRE, func(tag string, refs []string) (string, error) {
		return m.replaceExternalRef(tag, refs[1], ".js")
	})
	if err != nil {
		return "", err
	}

	doc, err = replaceTagWith(doc, iconTagRE, func(tag string, refs []string) (string, error) {
		return m.replaceExternalRef(tag, refs[1], "")
	})
	if err != nil {
		return "", err
	}

	doc, err = replaceTagWith(doc, imgTagRE, func(tag string, refs []string) (string, error) {
		return m.replaceExternalRef(tag, refs[1], "")
	})
	if err != nil {
		return "", err
	}

	return doc, nil
}

func (m *externalAssetMirrorer) replaceExternalRef(tag, ref, hintedExt string) (string, error) {
	absoluteURL, ok := resolveExternalURL(nil, ref)
	if !ok {
		return tag, nil
	}
	localRef, err := m.mirrorURL(absoluteURL, hintedExt)
	if err != nil {
		return "", err
	}
	return strings.Replace(tag, ref, "/"+localRef, 1), nil
}

const maxMirroredAssetBytes = 100 * 1024 * 1024 // 100 MiB

func (m *externalAssetMirrorer) mirrorURL(absoluteURL, hintedExt string) (string, error) {
	if cached, ok := m.cache[absoluteURL]; ok {
		return cached, nil
	}
	if pending, ok := m.inProgress[absoluteURL]; ok {
		return pending, nil
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, absoluteURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request for external asset %s: %w", absoluteURL, err)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading external asset %s: %w", absoluteURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("downloading external asset %s: unexpected status %s", absoluteURL, resp.Status)
	}

	if resp.ContentLength > maxMirroredAssetBytes {
		return "", fmt.Errorf("external asset %s Content-Length (%d) exceeds maximum download size of %d bytes", absoluteURL, resp.ContentLength, maxMirroredAssetBytes)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMirroredAssetBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading external asset %s: %w", absoluteURL, err)
	}
	if int64(len(body)) > maxMirroredAssetBytes {
		return "", fmt.Errorf("external asset %s exceeds maximum download size of %d bytes", absoluteURL, maxMirroredAssetBytes)
	}

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	relPath := mirroredAssetRelPath(absoluteURL, contentType, hintedExt, m.assetsDir)
	m.inProgress[absoluteURL] = relPath
	defer delete(m.inProgress, absoluteURL)

	if isCSSContentType(contentType, relPath, hintedExt) {
		baseURL, parseErr := url.Parse(absoluteURL)
		if parseErr != nil {
			return "", fmt.Errorf("parsing css asset url %s: %w", absoluteURL, parseErr)
		}
		rewritten, rewriteErr := m.rewriteCSS(string(body), baseURL)
		if rewriteErr != nil {
			return "", rewriteErr
		}
		body = []byte(rewritten)
	}

	fullPath := filepath.Join(m.outDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", fmt.Errorf("creating mirrored asset directory for %s: %w", absoluteURL, err)
	}
	if err := os.WriteFile(fullPath, body, 0o644); err != nil {
		return "", fmt.Errorf("writing mirrored asset %s: %w", absoluteURL, err)
	}

	m.cache[absoluteURL] = relPath
	return relPath, nil
}

func (m *externalAssetMirrorer) rewriteCSS(cssText string, baseURL *url.URL) (string, error) {
	rewritten, err := rewriteCSSImports(cssText, func(ref string) (string, bool, error) {
		absoluteURL, ok := resolveExternalURL(baseURL, ref)
		if !ok {
			return ref, false, nil
		}
		localRef, err := m.mirrorURL(absoluteURL, ".css")
		if err != nil {
			return "", false, err
		}
		return "/" + localRef, true, nil
	})
	if err != nil {
		return "", err
	}

	return rewriteCSSURLs(rewritten, func(ref string) (string, bool, error) {
		absoluteURL, ok := resolveExternalURL(baseURL, ref)
		if !ok {
			return ref, false, nil
		}
		localRef, err := m.mirrorURL(absoluteURL, "")
		if err != nil {
			return "", false, err
		}
		return "/" + localRef, true, nil
	})
}

func mirrorExternalAssetsInCopiedCSS(cssRoot string, mirrorer *externalAssetMirrorer) error {
	if mirrorer == nil {
		return nil
	}
	if _, err := os.Stat(cssRoot); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return filepath.WalkDir(cssRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.ToLower(filepath.Ext(path)) != ".css" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rewritten, err := mirrorer.rewriteCSS(string(content), nil)
		if err != nil {
			return err
		}
		return os.WriteFile(path, []byte(rewritten), 0o644)
	})
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

func generateSitemapFromConfig(configPath, websiteRoot, outDir string) error {
	cfg, err := sitemap.LoadConfig(configPath)
	if err != nil {
		return err
	}

	xmlBytes, err := sitemap.GenerateXML(cfg, websiteRoot)
	if err != nil {
		return err
	}

	targetPath := filepath.Join(outDir, "sitemap.xml")
	if err := os.WriteFile(targetPath, xmlBytes, 0o644); err != nil {
		return fmt.Errorf("writing sitemap.xml: %w", err)
	}
	return nil
}

func generateSitemapFromPages(baseURL, templatesDir string, pages []sitegen.PageTemplate, outDir string) error {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return fmt.Errorf("sitemap base URL cannot be empty")
	}

	urls := make([]sitemap.URLItem, 0, len(pages))
	for _, page := range pages {
		path := "/" + page.Name + ".html"
		if page.Name == "index" {
			path = "/"
		}

		item := sitemap.URLItem{Path: path}
		pageTemplatePath := filepath.Join(templatesDir, "pages", page.Name+".gohtml")
		if info, err := os.Stat(pageTemplatePath); err == nil {
			item.Lastmod = info.ModTime().In(time.UTC).Format("2006-01-02")
		}
		urls = append(urls, item)
	}

	cfg := sitemap.Config{
		BaseURL: strings.TrimRight(baseURL, "/"),
		URLs:    urls,
	}

	xmlBytes, err := sitemap.GenerateXML(cfg, "")
	if err != nil {
		return err
	}

	targetPath := filepath.Join(outDir, "sitemap.xml")
	if err := os.WriteFile(targetPath, xmlBytes, 0o644); err != nil {
		return fmt.Errorf("writing sitemap.xml: %w", err)
	}

	return nil
}

func resolveWebsitePaths(websiteRoot string) (string, string, error) {
	newAssets := filepath.Join(websiteRoot, "src", "assets")
	newTemplates := filepath.Join(websiteRoot, "src", "templates")
	if dirExists(newAssets) && dirExists(newTemplates) {
		return newAssets, newTemplates, nil
	}

	legacyAssets := filepath.Join(websiteRoot, "site")
	legacyTemplates := filepath.Join(websiteRoot, "templates")
	if dirExists(legacyAssets) && dirExists(legacyTemplates) {
		return legacyAssets, legacyTemplates, nil
	}

	return "", "", fmt.Errorf(
		"could not resolve website directories under %s; expected src/assets + src/templates (or legacy site + templates)",
		websiteRoot,
	)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func inlineLocalAssets(doc, srcRoot string) (string, error) {
	var err error

	doc, err = replaceTagWith(doc, stylesheetTagRE, func(tag string, refs []string) (string, error) {
		href := refs[1]
		if isExternalRef(href) {
			return tag, nil
		}

		cssBytes, cssPath, err := readAsset(srcRoot, href)
		if err != nil {
			return "", err
		}

		inlinedCSS, err := inlineCSSURLs(string(cssBytes), srcRoot, cssPath)
		if err != nil {
			return "", err
		}

		media := getTagAttr(tag, "media")
		if media != "" {
			return "<style media=\"" + htmlEscape(media) + "\">\n" + inlinedCSS + "\n</style>", nil
		}
		return "<style>\n" + inlinedCSS + "\n</style>", nil
	})
	if err != nil {
		return "", err
	}

	doc, err = replaceTagWith(doc, scriptTagRE, func(tag string, refs []string) (string, error) {
		src := refs[1]
		if isExternalRef(src) {
			return tag, nil
		}
		jsBytes, _, err := readAsset(srcRoot, src)
		if err != nil {
			return "", err
		}
		return "<script>\n" + string(jsBytes) + "\n</script>", nil
	})
	if err != nil {
		return "", err
	}

	doc, err = replaceTagWith(doc, iconTagRE, func(tag string, refs []string) (string, error) {
		href := refs[1]
		if isExternalRef(href) {
			return tag, nil
		}
		dataURL, err := assetToDataURL(srcRoot, href)
		if err != nil {
			return "", err
		}
		return strings.Replace(tag, href, dataURL, 1), nil
	})
	if err != nil {
		return "", err
	}

	doc, err = replaceTagWith(doc, imgTagRE, func(tag string, refs []string) (string, error) {
		src := refs[1]
		if isExternalRef(src) {
			return tag, nil
		}
		dataURL, err := assetToDataURL(srcRoot, src)
		if err != nil {
			return "", err
		}
		return strings.Replace(tag, src, dataURL, 1), nil
	})
	if err != nil {
		return "", err
	}

	return doc, nil
}

func replaceTagWith(doc string, re *regexp.Regexp, replacer func(tag string, refs []string) (string, error)) (string, error) {
	matches := re.FindAllStringSubmatchIndex(doc, -1)
	if len(matches) == 0 {
		return doc, nil
	}

	var out strings.Builder
	last := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		out.WriteString(doc[last:start])
		tag := doc[start:end]

		refs := make([]string, len(m)/2)
		for i := 0; i < len(m); i += 2 {
			if m[i] >= 0 && m[i+1] >= 0 {
				refs[i/2] = doc[m[i]:m[i+1]]
			}
		}

		replacement, err := replacer(tag, refs)
		if err != nil {
			return "", err
		}
		out.WriteString(replacement)
		last = end
	}
	out.WriteString(doc[last:])
	return out.String(), nil
}

func inlineCSSURLs(cssText, srcRoot, cssPath string) (string, error) {
	return rewriteCSSURLs(cssText, func(assetRef string) (string, bool, error) {
		if isExternalRef(assetRef) || strings.HasPrefix(strings.ToLower(strings.TrimSpace(assetRef)), "data:") {
			return assetRef, false, nil
		}

		resolved := resolveCSSAssetRef(cssPath, assetRef)
		dataURL, err := assetToDataURL(srcRoot, resolved)
		if err != nil {
			return "", false, err
		}
		return dataURL, true, nil
	})
}

func rewriteCSSURLs(cssText string, replacer func(ref string) (replacement string, changed bool, err error)) (string, error) {
	matches := cssURLRE.FindAllStringSubmatchIndex(cssText, -1)
	if len(matches) == 0 {
		return cssText, nil
	}

	var out strings.Builder
	last := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		urlStart, urlEnd := m[2], m[3]
		out.WriteString(cssText[last:start])

		assetRef := strings.TrimSpace(cssText[urlStart:urlEnd])
		replacement, changed, err := replacer(assetRef)
		if err != nil {
			return "", err
		}
		if !changed {
			out.WriteString(cssText[start:end])
			last = end
			continue
		}

		out.WriteString("url(\"" + replacement + "\")")
		last = end
	}
	out.WriteString(cssText[last:])
	return out.String(), nil
}

func rewriteCSSImports(cssText string, replacer func(ref string) (replacement string, changed bool, err error)) (string, error) {
	matches := cssImportRE.FindAllStringSubmatchIndex(cssText, -1)
	if len(matches) == 0 {
		return cssText, nil
	}

	var out strings.Builder
	last := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		refStart, refEnd := m[2], m[3]
		suffixStart, suffixEnd := m[4], m[5]
		out.WriteString(cssText[last:start])

		ref := strings.TrimSpace(cssText[refStart:refEnd])
		replacement, changed, err := replacer(ref)
		if err != nil {
			return "", err
		}
		if !changed {
			out.WriteString(cssText[start:end])
			last = end
			continue
		}

		suffix := ""
		if suffixStart >= 0 && suffixEnd >= 0 {
			suffix = cssText[suffixStart:suffixEnd]
		}
		out.WriteString("@import url(\"" + replacement + "\")" + suffix + ";")
		last = end
	}
	out.WriteString(cssText[last:])
	return out.String(), nil
}

func resolveCSSAssetRef(cssPath, ref string) string {
	if strings.HasPrefix(ref, "/") {
		return strings.TrimPrefix(ref, "/")
	}
	base := filepath.Dir(cssPath)
	return filepath.ToSlash(filepath.Clean(filepath.Join(base, ref)))
}

func assetToDataURL(srcRoot, ref string) (string, error) {
	content, path, err := readAsset(srcRoot, ref)
	if err != nil {
		return "", err
	}
	mimeType := detectMimeType(path, content)
	return "data:" + mimeType + ";base64," + encodeBase64(content), nil
}

func readAsset(srcRoot, ref string) ([]byte, string, error) {
	cleanRef := strings.TrimPrefix(ref, "/")
	fullPath := filepath.Clean(filepath.Join(srcRoot, filepath.FromSlash(cleanRef)))
	if !strings.HasPrefix(fullPath, filepath.Clean(srcRoot)+string(filepath.Separator)) && fullPath != filepath.Clean(srcRoot) {
		return nil, "", fmt.Errorf("asset path escapes source root: %s", ref)
	}
	realPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return nil, "", fmt.Errorf("resolving asset path %s: %w", ref, err)
	}
	realRoot, err := filepath.EvalSymlinks(filepath.Clean(srcRoot))
	if err != nil {
		return nil, "", fmt.Errorf("resolving source root: %w", err)
	}
	if !strings.HasPrefix(realPath, realRoot+string(filepath.Separator)) && realPath != realRoot {
		return nil, "", fmt.Errorf("asset path escapes source root via symlink: %s", ref)
	}
	content, err := os.ReadFile(realPath)
	if err != nil {
		return nil, "", fmt.Errorf("reading asset %s: %w", ref, err)
	}
	return content, cleanRef, nil
}

func detectMimeType(path string, content []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".ico":
		return "image/x-icon"
	default:
		return http.DetectContentType(content)
	}
}

func isExternalRef(ref string) bool {
	lower := strings.ToLower(strings.TrimSpace(ref))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "//")
}

func resolveExternalURL(baseURL *url.URL, ref string) (string, bool) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", false
	}

	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "data:"),
		strings.HasPrefix(lower, "mailto:"),
		strings.HasPrefix(lower, "tel:"),
		strings.HasPrefix(lower, "javascript:"),
		strings.HasPrefix(lower, "#"):
		return "", false
	}

	if strings.HasPrefix(trimmed, "//") {
		scheme := "https"
		if baseURL != nil && baseURL.Scheme != "" {
			scheme = baseURL.Scheme
		}
		return scheme + ":" + trimmed, true
	}

	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.IsAbs() && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return parsed.String(), true
	}

	if baseURL == nil {
		return "", false
	}

	resolved := baseURL.ResolveReference(&url.URL{Path: trimmed})
	if strings.Contains(trimmed, "?") || strings.Contains(trimmed, "#") {
		if parsed, err := url.Parse(trimmed); err == nil {
			resolved = baseURL.ResolveReference(parsed)
		}
	}
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", false
	}
	if resolved.Host == "" {
		return "", false
	}
	return resolved.String(), true
}

func hintedExtFromPreload(tag string) string {
	switch strings.ToLower(strings.TrimSpace(getTagAttr(tag, "as"))) {
	case "style":
		return ".css"
	case "script":
		return ".js"
	case "font":
		if hrefType := strings.ToLower(strings.TrimSpace(getTagAttr(tag, "type"))); hrefType != "" {
			return extensionFromContentType(hrefType)
		}
		return ".woff2"
	case "image":
		return ""
	default:
		return ""
	}
}

func mirroredAssetRelPath(absoluteURL, contentType, hintedExt, assetsDir string) string {
	parsed, err := url.Parse(absoluteURL)
	if err != nil {
		sum := sha256.Sum256([]byte(absoluteURL))
		return path.Join(assetsDir, "unknown", hex.EncodeToString(sum[:8])+normalizeExt(hintedExt))
	}

	host := sanitizePathSegment(parsed.Host)
	segments := []string{}
	cleanPath := strings.Trim(parsed.Path, "/")
	if cleanPath != "" {
		for _, segment := range strings.Split(cleanPath, "/") {
			sanitized := sanitizePathSegment(segment)
			if sanitized != "" {
				segments = append(segments, sanitized)
			}
		}
	}
	if len(segments) == 0 {
		segments = []string{"index"}
	}

	fileName := segments[len(segments)-1]
	dirParts := segments[:len(segments)-1]
	ext := normalizeExt(filepath.Ext(fileName))
	if ext == "" {
		ext = normalizeExt(hintedExt)
	}
	if ext == "" {
		ext = extensionFromContentType(contentType)
	}
	if ext == "" {
		ext = ".bin"
	}

	fileStem := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	if fileStem == "" {
		fileStem = "index"
	}
	if parsed.RawQuery != "" {
		fileStem += "--" + shortHash(parsed.RawQuery)
	}

	parts := []string{assetsDir, host}
	parts = append(parts, dirParts...)
	parts = append(parts, fileStem+ext)
	return path.Join(parts...)
}

func sanitizePathSegment(v string) string {
	if v == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"?", "_",
		"&", "_",
		"=", "_",
		"%", "_",
		"+", "_",
	)
	v = replacer.Replace(v)
	v = strings.Trim(v, "._-")
	if v == "" {
		return "asset"
	}
	return v
}

func shortHash(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:6])
}

func normalizeExt(v string) string {
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, ".") {
		return strings.ToLower(v)
	}
	return "." + strings.ToLower(v)
}

func extensionFromContentType(contentType string) string {
	trimmed := strings.TrimSpace(strings.Split(contentType, ";")[0])
	if trimmed == "" {
		return ""
	}
	if exts, err := mime.ExtensionsByType(trimmed); err == nil {
		for _, ext := range exts {
			normalized := normalizeExt(ext)
			if normalized != "" {
				return normalized
			}
		}
	}
	switch trimmed {
	case "text/css":
		return ".css"
	case "application/javascript", "text/javascript":
		return ".js"
	case "font/woff2":
		return ".woff2"
	case "font/woff":
		return ".woff"
	case "font/ttf", "application/x-font-ttf":
		return ".ttf"
	case "image/svg+xml":
		return ".svg"
	case "image/x-icon":
		return ".ico"
	default:
		return ""
	}
}

func isCSSContentType(contentType, relPath, hintedExt string) bool {
	if strings.EqualFold(strings.TrimSpace(strings.Split(contentType, ";")[0]), "text/css") {
		return true
	}
	switch normalizeExt(filepath.Ext(relPath)) {
	case ".css":
		return true
	}
	return normalizeExt(hintedExt) == ".css"
}

func htmlEscape(v string) string {
	v = strings.ReplaceAll(v, "&", "&amp;")
	v = strings.ReplaceAll(v, "\"", "&quot;")
	v = strings.ReplaceAll(v, "<", "&lt;")
	v = strings.ReplaceAll(v, ">", "&gt;")
	return v
}

func encodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func getTagAttr(tag, attr string) string {
	re := regexp.MustCompile(`(?i)` + attr + `\s*=\s*["']([^"']+)["']`)
	m := re.FindStringSubmatch(tag)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func copyStaticAssets(srcRoot, dstRoot string) error {
	dirs := []string{"css", "fonts", "images", "js", "ld"}
	files := []string{"send.js", "contactScript.js", "robots.txt", "sitemap.xml"}

	for _, dir := range dirs {
		src := filepath.Join(srcRoot, dir)
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		dst := filepath.Join(dstRoot, dir)
		if err := copyDir(src, dst); err != nil {
			return err
		}
	}

	for _, file := range files {
		src := filepath.Join(srcRoot, file)
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		dst := filepath.Join(dstRoot, file)
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}

	return nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}

	return out.Close()
}
