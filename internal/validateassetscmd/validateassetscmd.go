package validateassetscmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"ffreis-website-compiler/internal/assetusage"
	"ffreis-website-compiler/internal/cmdutil"
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

	renderedPages, err := cmdutil.RenderPages(pages, siteDataResult.Data)
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
		"site_data_source", cmdutil.FirstNonEmpty(siteDataResult.Source, siteDataResult.DefaultPath),
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
	cmdutil.LogSiteDataOverride(logger, siteDataResult)

	if err := cmdutil.ValidateSiteDataAndUsage(pages, siteDataResult, siteDataContractResult); err != nil {
		return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, err
	}
	return pages, siteDataResult, siteDataContractResult, nil
}
