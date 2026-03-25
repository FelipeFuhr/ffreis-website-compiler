package validateassetscmd

import (
	"bytes"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"ffreis-website-compiler/internal/assetusage"
	"ffreis-website-compiler/internal/sitegen"
)

func Run(args []string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	fs := flag.NewFlagSet("validate-assets", flag.ContinueOnError)
	websiteRoot := fs.String("website-root", ".", "website project root; expects <website-root>/src/{assets,templates} (legacy fallback: <website-root>/{site,templates})")
	assetsDirFlag := fs.String("assets-dir", "", "path to source assets folder (defaults to <website-root>/src/assets, then <website-root>/site)")
	siteDirFlag := fs.String("site-dir", "", "legacy alias for -assets-dir")
	templatesDirFlag := fs.String("templates-dir", "", "path to templates root folder (defaults to <website-root>/src/templates, then <website-root>/templates)")
	siteDataSource := fs.String("site-data", "", "optional site data source override; supports file/URL sources or a directory containing YAML layers")
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

	renderedPages := make(map[string]string, len(pages))
	for _, page := range pages {
		var rendered bytes.Buffer
		if err := page.Tmpl.ExecuteTemplate(&rendered, "layout", sitegen.NewTemplateData(page.Name, siteDataResult.Data)); err != nil {
			return fmt.Errorf("rendering %s.html for asset validation: %w", page.Name, err)
		}
		renderedPages[page.Name] = rendered.String()
	}

	result, err := assetusage.Validate(assetsDir, renderedPages)
	if err != nil {
		return fmt.Errorf("validating local css/js asset usage: %w", err)
	}

	logger.Info(
		"asset usage validation passed",
		"website_root", *websiteRoot,
		"assets_dir", assetsDir,
		"templates_dir", templatesDir,
		"site_data_source", firstNonEmpty(siteDataResult.Source, siteDataResult.DefaultPath),
		"site_data_layers", siteDataResult.Layers,
		"used_css", len(result.UsedCSS),
		"used_js", len(result.UsedJS),
	)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
