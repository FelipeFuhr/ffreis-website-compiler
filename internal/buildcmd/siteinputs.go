package buildcmd

import (
	"fmt"
	"log/slog"
	"strings"

	"ffreis-website-compiler/internal/cmdutil"
	"ffreis-website-compiler/internal/sitegen"
)

func loadAndValidateSiteInputs(logger *slog.Logger, opts buildOptions, templatesDir string) ([]sitegen.PageTemplate, sitegen.SiteDataLoadResult, sitegen.SiteDataContractLoadResult, error) {
	var (
		pages                  []sitegen.PageTemplate
		siteDataResult         sitegen.SiteDataLoadResult
		siteDataContractResult sitegen.SiteDataContractLoadResult
		err                    error
	)

	pages, siteDataResult, siteDataContractResult, err = loadSiteDataWithOptionalUsageCheck(logger, templatesDir, opts.siteDataSource, opts.strictContract)
	if err != nil {
		return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, err
	}

	if opts.enableSanity {
		if err := sitegen.ValidateSiteSanity(siteDataResult.Data, sitegen.DefaultSanityConfig()); err != nil {
			return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("validating site sanity rules: %w", err)
		}
	}
	logger.Info("loaded templates", "count", len(pages), "templates_dir", templatesDir)
	return pages, siteDataResult, siteDataContractResult, nil
}

// loadSiteDataWithOptionalUsageCheck loads templates and data, validates site
// data against the contract, and optionally validates contract usage by
// templates. The build command treats pages.<name>.internal as compiler-managed
// metadata and excludes it from contract checks.
func loadSiteDataWithOptionalUsageCheck(logger *slog.Logger, templatesDir, siteDataSource string, validateUsage bool) ([]sitegen.PageTemplate, sitegen.SiteDataLoadResult, sitegen.SiteDataContractLoadResult, error) {
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
	contract := contractWithoutPageInternalPatterns(siteDataContractResult.Contract)
	siteDataForValidation := siteDataWithoutPageInternalFlags(siteDataResult.Data)
	if err := sitegen.ValidateSiteData(siteDataForValidation, contract); err != nil {
		return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("validating site data against contract: %w", err)
	}
	if validateUsage {
		usedPaths, err := sitegen.TraceSiteDataUsage(pages, siteDataResult.Data)
		if err != nil {
			return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("tracing site data usage: %w", err)
		}
		if err := sitegen.ValidateSiteDataContractUsage(contract, usedPaths); err != nil {
			return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("validating site data contract usage: %w", err)
		}
	}
	return pages, siteDataResult, siteDataContractResult, nil
}

// siteDataWithoutPageInternalFlags returns a shallow copy of siteData where
// the "internal" key has been removed from every page entry. Only the two
// affected levels (top-level map and the pages map) are copied; deeper values
// are shared and never mutated.
func siteDataWithoutPageInternalFlags(siteData map[string]any) map[string]any {
	pagesData, ok := siteData["pages"].(map[string]any)
	if !ok {
		return siteData
	}

	newPages := make(map[string]any, len(pagesData))
	for name, pageData := range pagesData {
		pageMap, ok := pageData.(map[string]any)
		if !ok {
			newPages[name] = pageData
			continue
		}
		copied := make(map[string]any, len(pageMap))
		for k, v := range pageMap {
			copied[k] = v
		}
		delete(copied, "internal")
		newPages[name] = copied
	}

	result := make(map[string]any, len(siteData))
	for k, v := range siteData {
		result[k] = v
	}
	result["pages"] = newPages
	return result
}

func contractWithoutPageInternalPatterns(contract sitegen.SiteDataContract) sitegen.SiteDataContract {
	contract.Required = contractPatternsWithoutPageInternal(contract.Required)
	contract.Allowed = contractPatternsWithoutPageInternal(contract.Allowed)
	return contract
}

func contractPatternsWithoutPageInternal(patterns []string) []string {
	filtered := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if !isPageInternalPattern(pattern) {
			filtered = append(filtered, pattern)
		}
	}
	return filtered
}

func isPageInternalPattern(pattern string) bool {
	parts := strings.Split(strings.TrimSpace(pattern), ".")
	if len(parts) != 3 || parts[0] != "pages" || parts[2] != "internal" {
		return false
	}
	segment := parts[1]
	if segment == "*" {
		return true
	}
	return segment != "" && !strings.Contains(segment, "*")
}

// filterInternalPages removes pages marked internal: true in the site data
// (pages.<name>.internal). Such pages are still rendered (for asset usage
// validation) but excluded from HTML output and sitemap generation.
func filterInternalPages(pages []sitegen.PageTemplate, siteData map[string]any) []sitegen.PageTemplate {
	pagesData, _ := siteData["pages"].(map[string]any)
	result := pages[:0:0]
	for _, p := range pages {
		pd, _ := pagesData[p.Name].(map[string]any)
		if internal, _ := pd["internal"].(bool); !internal {
			result = append(result, p)
		}
	}
	return result
}
