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

	opts, err := parseValidateAssetsOptions(args)
	if err != nil {
		return err
	}

	assetsDir, templatesDir, err := resolveValidateAssetsPaths(opts)
	if err != nil {
		return err
	}

	pages, siteDataResult, siteDataContractResult, err := loadAndValidateSiteData(logger, templatesDir, opts.siteDataSource)
	if err != nil {
		return err
	}

	renderedPages, err := renderPagesForAssetValidation(pages, siteDataResult.Data)
	if err != nil {
		return err
	}

	result, err := assetusage.Validate(assetsDir, renderedPages)
	if err != nil {
		return fmt.Errorf("validating local css/js asset usage: %w", err)
	}

	logger.Info(
		"asset usage validation passed",
		"website_root", opts.websiteRoot,
		"assets_dir", assetsDir,
		"templates_dir", templatesDir,
		"site_data_source", firstNonEmpty(siteDataResult.Source, siteDataResult.DefaultPath),
		"site_data_layers", siteDataResult.Layers,
		"used_css", len(result.UsedCSS),
		"used_js", len(result.UsedJS),
	)
	_ = siteDataContractResult
	return nil
}

type validateAssetsOptions struct {
	websiteRoot    string
	assetsDir      string
	siteDir        string
	templatesDir   string
	siteDataSource string
}

func parseValidateAssetsOptions(args []string) (validateAssetsOptions, error) {
	fs := flag.NewFlagSet("validate-assets", flag.ContinueOnError)

	var opts validateAssetsOptions
	fs.StringVar(&opts.websiteRoot, "website-root", ".", "website project root; expects <website-root>/src/{assets,templates} (legacy fallback: <website-root>/{site,templates})")
	fs.StringVar(&opts.assetsDir, "assets-dir", "", "path to source assets folder (defaults to <website-root>/src/assets, then <website-root>/site)")
	fs.StringVar(&opts.siteDir, "site-dir", "", "legacy alias for -assets-dir")
	fs.StringVar(&opts.templatesDir, "templates-dir", "", "path to templates root folder (defaults to <website-root>/src/templates, then <website-root>/templates)")
	fs.StringVar(&opts.siteDataSource, "site-data", "", "optional site data source override; supports file/URL sources or a directory containing YAML layers")

	if err := fs.Parse(args); err != nil {
		return validateAssetsOptions{}, err
	}
	return opts, nil
}

func resolveValidateAssetsPaths(opts validateAssetsOptions) (assetsDir, templatesDir string, err error) {
	assetsDir = opts.assetsDir
	if assetsDir == "" && opts.siteDir != "" {
		assetsDir = opts.siteDir
	}
	templatesDir = opts.templatesDir

	if assetsDir != "" && templatesDir != "" {
		return assetsDir, templatesDir, nil
	}

	resolvedAssetsDir, resolvedTemplatesDir, err := resolveWebsitePaths(opts.websiteRoot)
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

func loadAndValidateSiteData(logger *slog.Logger, templatesDir, siteDataSource string) ([]sitegen.PageTemplate, sitegen.SiteDataLoadResult, sitegen.SiteDataContractLoadResult, error) {
	pages, err := sitegen.LoadPageTemplatesFromRoot(templatesDir)
	if err != nil {
		return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("loading templates: %w", err)
	}
	siteDataResult, err := sitegen.LoadSiteData(templatesDir, siteDataSource)
	if err != nil {
		return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("loading site data: %w", err)
	}
	siteDataContractResult, err := sitegen.LoadSiteDataContract(templatesDir)
	if err != nil {
		return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("loading site data contract: %w", err)
	}
	logSiteDataOverride(logger, siteDataResult)

	if err := validateSiteDataAndUsage(pages, siteDataResult, siteDataContractResult); err != nil {
		return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, err
	}
	return pages, siteDataResult, siteDataContractResult, nil
}

func logSiteDataOverride(logger *slog.Logger, siteDataResult sitegen.SiteDataLoadResult) {
	if !siteDataResult.UsedOverride || !siteDataResult.DefaultPathFound {
		return
	}
	logger.Warn(
		"site data override supersedes local site data file",
		"override_source", siteDataResult.Source,
		"local_site_data", siteDataResult.DefaultPath,
		"site_data_layers", siteDataResult.Layers,
	)
}

func validateSiteDataAndUsage(pages []sitegen.PageTemplate, siteDataResult sitegen.SiteDataLoadResult, siteDataContractResult sitegen.SiteDataContractLoadResult) error {
	if err := sitegen.ValidateSiteData(siteDataResult.Data, siteDataContractResult.Contract); err != nil {
		return fmt.Errorf("validating site data against contract: %w", err)
	}
	contract := siteDataContractResult.Contract
	if len(contract.Required) == 0 && len(contract.Allowed) == 0 {
		return nil
	}
	usedPaths, err := sitegen.TraceSiteDataUsage(pages, siteDataResult.Data)
	if err != nil {
		return fmt.Errorf("tracing site data usage: %w", err)
	}
	if err := sitegen.ValidateSiteDataContractUsage(contract, usedPaths); err != nil {
		return fmt.Errorf("validating site data contract usage: %w", err)
	}
	return nil
}

func renderPagesForAssetValidation(pages []sitegen.PageTemplate, siteData map[string]any) (map[string]string, error) {
	renderedPages := make(map[string]string, len(pages))
	for _, page := range pages {
		var rendered bytes.Buffer
		if err := page.Tmpl.ExecuteTemplate(&rendered, "layout", sitegen.NewTemplateData(page.Name, siteData)); err != nil {
			return nil, fmt.Errorf("rendering %s.html for asset validation: %w", page.Name, err)
		}
		renderedPages[page.Name] = rendered.String()
	}
	return renderedPages, nil
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
