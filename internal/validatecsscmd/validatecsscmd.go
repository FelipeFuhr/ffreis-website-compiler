// Package validatecsscmd implements the "validate-css" command: a per-page
// CSS usage check that warns when a page's own linked stylesheets carry
// rules that page's own rendered output never uses. It is advisory, not a
// hard gate -- see internal/cssusage's package doc for why (it cannot see
// classes added by client-side JavaScript after load, so it is written to
// never fail the build, only to surface what it can prove with confidence).
package validatecsscmd

import (
	"flag"
	"log/slog"
	"os"

	"ffreis-website-compiler/internal/cmdutil"
	"ffreis-website-compiler/internal/cssusage"
	"ffreis-website-compiler/internal/sitegen"
)

func Run(args []string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	opts, err := parseValidateCSSOptions(args)
	if err != nil {
		return err
	}

	assetsDir, templatesDir, err := resolveValidateCSSPaths(opts)
	if err != nil {
		return err
	}

	inputs, err := sitegen.LoadSiteInputs(templatesDir, opts.siteDataSource)
	if err != nil {
		return err
	}
	cmdutil.LogSiteDataOverride(logger, inputs.SiteDataResult)

	renderedPages, err := sitegen.RenderPages(inputs.Pages, inputs.SiteDataResult.Data)
	if err != nil {
		return err
	}

	result, err := cssusage.Validate(assetsDir, renderedPages)
	if err != nil {
		return err
	}

	for _, finding := range result.Findings {
		logger.Warn(
			"css rule unused by its own page",
			"page", finding.Page,
			"css_file", finding.CSSFile,
			"selector", finding.Selector,
		)
	}
	logger.Info(
		"css usage validation complete",
		"website_root", opts.websiteRoot,
		"assets_dir", assetsDir,
		"templates_dir", templatesDir,
		"pages_checked", len(renderedPages),
		"findings", len(result.Findings),
	)
	return nil
}

type validateCSSOptions struct {
	websiteRoot    string
	assetsDir      string
	templatesDir   string
	siteDataSource string
}

func parseValidateCSSOptions(args []string) (validateCSSOptions, error) {
	fs := flag.NewFlagSet("validate-css", flag.ContinueOnError)

	var opts validateCSSOptions
	fs.StringVar(&opts.websiteRoot, "website-root", ".", "website project root; expects <website-root>/src/{assets,templates} (legacy fallback: <website-root>/{site,templates})")
	fs.StringVar(&opts.assetsDir, "assets-dir", "", "path to source assets folder (defaults to <website-root>/src/assets, then <website-root>/site)")
	fs.StringVar(&opts.templatesDir, "templates-dir", "", "path to templates root folder (defaults to <website-root>/src/templates, then <website-root>/templates)")
	fs.StringVar(&opts.siteDataSource, "site-data", "", "optional site data source override; supports file/URL sources or a directory containing YAML layers")

	if err := fs.Parse(args); err != nil {
		return validateCSSOptions{}, err
	}
	return opts, nil
}

func resolveValidateCSSPaths(opts validateCSSOptions) (assetsDir, templatesDir string, err error) {
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
