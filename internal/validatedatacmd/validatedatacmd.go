package validatedatacmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"ffreis-website-compiler/internal/sitegen"
)

func Run(args []string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	opts, err := parseValidateDataOptions(args)
	if err != nil {
		return err
	}

	templatesRoot, err := resolveTemplatesRootFlag(opts.websiteRoot, opts.templatesDir)
	if err != nil {
		return err
	}

	siteDataResult, siteDataContractResult, err := loadAndValidateSiteData(logger, templatesRoot, opts.siteDataSource)
	if err != nil {
		return err
	}

	logger.Info(
		"site data validation passed",
		"website_root", opts.websiteRoot,
		"templates_dir", templatesRoot,
		"site_data_source", firstNonEmpty(siteDataResult.Source, siteDataResult.DefaultPath),
		"site_data_layers", siteDataResult.Layers,
		"site_data_contract_source", firstNonEmpty(siteDataContractResult.Source, siteDataContractResult.DefaultPath),
	)
	return nil
}

type validateDataOptions struct {
	websiteRoot    string
	templatesDir   string
	siteDataSource string
}

func parseValidateDataOptions(args []string) (validateDataOptions, error) {
	fs := flag.NewFlagSet("validate-site-data", flag.ContinueOnError)
	var opts validateDataOptions
	fs.StringVar(&opts.websiteRoot, "website-root", ".", "website project root; expects <website-root>/src/{assets,templates} (legacy fallback: <website-root>/{site,templates})")
	fs.StringVar(&opts.templatesDir, "templates-dir", "", "path to templates root folder (defaults to <website-root>/src/templates, then <website-root>/templates)")
	fs.StringVar(&opts.siteDataSource, "site-data", "", "optional site data source override; supports file/URL sources or a directory containing YAML layers")
	if err := fs.Parse(args); err != nil {
		return validateDataOptions{}, err
	}
	return opts, nil
}

func resolveTemplatesRootFlag(websiteRoot, templatesDirFlag string) (string, error) {
	templatesRoot := strings.TrimSpace(templatesDirFlag)
	if templatesRoot != "" {
		return templatesRoot, nil
	}
	return resolveTemplatesRoot(websiteRoot)
}

func loadAndValidateSiteData(logger *slog.Logger, templatesRoot, siteDataSource string) (sitegen.SiteDataLoadResult, sitegen.SiteDataContractLoadResult, error) {
	siteDataResult, err := sitegen.LoadSiteData(templatesRoot, siteDataSource)
	if err != nil {
		return sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("loading site data: %w", err)
	}
	siteDataContractResult, err := sitegen.LoadSiteDataContract(templatesRoot)
	if err != nil {
		return sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("loading site data contract: %w", err)
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
		return sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("validating site data against contract: %w", err)
	}

	if err := validateSiteDataContractUsage(templatesRoot, siteDataResult, siteDataContractResult); err != nil {
		return sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, err
	}

	return siteDataResult, siteDataContractResult, nil
}

func validateSiteDataContractUsage(templatesRoot string, siteDataResult sitegen.SiteDataLoadResult, siteDataContractResult sitegen.SiteDataContractLoadResult) error {
	contract := siteDataContractResult.Contract
	if len(contract.Required) == 0 && len(contract.Allowed) == 0 {
		return nil
	}

	pages, err := sitegen.LoadPageTemplatesFromRoot(templatesRoot)
	if err != nil {
		return fmt.Errorf("loading templates for site data usage validation: %w", err)
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

func resolveTemplatesRoot(websiteRoot string) (string, error) {
	newTemplates := filepath.Join(websiteRoot, "src", "templates")
	if dirExists(newTemplates) {
		return newTemplates, nil
	}

	legacyTemplates := filepath.Join(websiteRoot, "templates")
	if dirExists(legacyTemplates) {
		return legacyTemplates, nil
	}

	return "", fmt.Errorf(
		"could not resolve templates directory under %s; expected src/templates (or legacy templates)",
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
