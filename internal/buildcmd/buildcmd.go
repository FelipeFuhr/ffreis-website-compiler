package buildcmd

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ffreis-website-compiler/internal/sitegen"
	"ffreis-website-compiler/internal/sitemap"
)

var (
	stylesheetTagRE = regexp.MustCompile(`(?is)<link\s+[^>]*rel=["']stylesheet["'][^>]*href=["']([^"']+)["'][^>]*>`)
	scriptTagRE     = regexp.MustCompile(`(?is)<script\s+[^>]*src=["']([^"']+)["'][^>]*>\s*</script>`)
	imgTagRE        = regexp.MustCompile(`(?is)<img\s+[^>]*src=["']([^"']+)["'][^>]*>`)
	iconTagRE       = regexp.MustCompile(`(?is)<link\s+[^>]*rel=["'][^"']*icon[^"']*["'][^>]*href=["']([^"']+)["'][^>]*>`)
	cssURLRE        = regexp.MustCompile(`url\(\s*['"]?([^'"\)]+)['"]?\s*\)`)
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
	outDir := fs.String("out", "dist", "output directory for generated static site")
	copyAssets := fs.Bool("copy-assets", true, "copy static assets from assets dir into output")
	inlineAssets := fs.Bool("inline-assets", false, "inline local css/js/images into each html for self-contained pages")
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

	pages, err := sitegen.LoadPageTemplatesFromRoot(templatesDir)
	if err != nil {
		return fmt.Errorf("loading templates: %w", err)
	}
	logger.Info("loaded templates", "count", len(pages), "templates_dir", templatesDir)

	for _, page := range pages {
		target := filepath.Join(*outDir, page.Name+".html")

		var rendered bytes.Buffer
		if err := page.Tmpl.ExecuteTemplate(&rendered, "layout", nil); err != nil {
			return fmt.Errorf("rendering %s: %w", target, err)
		}

		htmlOut := rendered.String()
		if *inlineAssets {
			htmlOut, err = inlineLocalAssets(htmlOut, assetsDir)
			if err != nil {
				return fmt.Errorf("inlining assets in %s: %w", target, err)
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
		if isExternalRef(assetRef) || strings.HasPrefix(assetRef, "data:") {
			out.WriteString(cssText[start:end])
			last = end
			continue
		}

		resolved := resolveCSSAssetRef(cssPath, assetRef)
		dataURL, err := assetToDataURL(srcRoot, resolved)
		if err != nil {
			return "", err
		}
		out.WriteString("url(\"" + dataURL + "\")")
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
	content, err := os.ReadFile(fullPath)
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
	dirs := []string{"css", "fonts", "images", "js"}
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
