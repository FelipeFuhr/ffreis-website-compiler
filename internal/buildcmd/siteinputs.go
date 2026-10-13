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
// templates. The build command treats pages.<name>.<key> for each key in
// compilerManagedPageKeys (e.g. internal, fragment) as compiler-managed
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
	contract := contractWithoutPageManagedPatterns(siteDataContractResult.Contract)
	siteDataForValidation := siteDataWithoutPageManagedFlags(siteDataResult.Data)
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

// compilerManagedPageKeys lists the pages.<name>.<key> keys the compiler
// itself reads as build configuration rather than template content: a site
// never declares these in site.contract.yaml, and setting one in site data
// never trips the dangling-leaf check. Adding a new compiler-read page flag
// is a one-line change here; both exemption mechanisms below pick it up
// automatically.
var compilerManagedPageKeys = []string{"internal", "fragment"}

// siteDataWithoutPageManagedFlags returns a shallow copy of siteData where
// every key in compilerManagedPageKeys has been removed from every page
// entry. Only the two affected levels (top-level map and the pages map) are
// copied; deeper values are shared and never mutated.
func siteDataWithoutPageManagedFlags(siteData map[string]any) map[string]any {
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
		for _, key := range compilerManagedPageKeys {
			delete(copied, key)
		}
		newPages[name] = copied
	}

	result := make(map[string]any, len(siteData))
	for k, v := range siteData {
		result[k] = v
	}
	result["pages"] = newPages
	return result
}

func contractWithoutPageManagedPatterns(contract sitegen.SiteDataContract) sitegen.SiteDataContract {
	contract.Required = contractPatternsWithoutPageManaged(contract.Required)
	contract.Allowed = contractPatternsWithoutPageManaged(contract.Allowed)
	contract.CompilerConsumed = contractPatternsWithoutPageManaged(contract.CompilerConsumed)
	return contract
}

func contractPatternsWithoutPageManaged(patterns []string) []string {
	filtered := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if !isPageManagedPattern(pattern) {
			filtered = append(filtered, pattern)
		}
	}
	return filtered
}

func isPageManagedPattern(pattern string) bool {
	parts := strings.Split(strings.TrimSpace(pattern), ".")
	if len(parts) != 3 || parts[0] != "pages" {
		return false
	}
	segment, key := parts[1], parts[2]
	managed := false
	for _, k := range compilerManagedPageKeys {
		if key == k {
			managed = true
			break
		}
	}
	if !managed {
		return false
	}
	if segment == "*" {
		return true
	}
	return segment != "" && !strings.Contains(segment, "*")
}

// resolveSiblingBasePaths returns the URL prefixes of sibling deployments by
// inspecting ui.nav.lang_links in the site data. Non-active language links
// point to sibling deployments that share the same CloudFront distribution;
// their href values become the sibling base paths for the internal link checker.
//
// Example (PT deployment, active="pt-BR"):
//   - href="/en/" active=false → sibling "/en"
//   - href="/jp/" active=false → sibling "/jp"
//   - href="/"   active=true  → current deployment, skip
//
// Returns nil for single-language sites that have no lang_links.
func resolveSiblingBasePaths(siteData map[string]any) []string {
	ui, _ := siteData["ui"].(map[string]any)
	nav, _ := ui["nav"].(map[string]any)
	langLinks, _ := nav["lang_links"].([]any)
	if len(langLinks) == 0 {
		return nil
	}
	var siblings []string
	for _, item := range langLinks {
		link, _ := item.(map[string]any)
		if active, _ := link["active"].(bool); active {
			continue // this is the current deployment
		}
		href, _ := link["href"].(string)
		href = strings.TrimRight(href, "/")
		if href == "" {
			continue // PT root "/" trims to "" — basePath logic already handles cross-root links
		}
		siblings = append(siblings, href)
	}
	return siblings
}

// resolvePageSlug returns the URL slug for a page. It reads pages.<name>.slug
// from site data and falls back to pageName when absent.
func resolvePageSlug(siteData map[string]any, pageName string) string {
	pagesData, _ := siteData["pages"].(map[string]any)
	pageData, _ := pagesData[pageName].(map[string]any)
	slug, _ := pageData["slug"].(string)
	if slug == "" {
		return pageName
	}
	return slug
}

// sectionEnabled reports whether a content section (e.g. "blog", "courses",
// "projects") is enabled for this build. Sections default to ENABLED; a site
// disables one by setting sections.<name>: false in its site data. An absent
// sections map or key means enabled, so sites without a sections block are
// completely unaffected.
func sectionEnabled(siteData map[string]any, name string) bool {
	sections, ok := siteData["sections"].(map[string]any)
	if !ok {
		return true
	}
	enabled, ok := sections[name].(bool)
	if !ok {
		return true
	}
	return enabled
}

// filterInternalPages removes pages marked internal: true in the site data
// (pages.<name>.internal) and pages belonging to a disabled content section
// (sections.<name>: false). Internal pages are still rendered (for asset usage
// validation) but excluded from HTML output and sitemap generation; disabled
// sections are dropped entirely so no listing/detail page is emitted.
func filterInternalPages(pages []sitegen.PageTemplate, siteData map[string]any) []sitegen.PageTemplate {
	pagesData, _ := siteData["pages"].(map[string]any)
	result := pages[:0:0]
	for _, p := range pages {
		pd, _ := pagesData[p.Name].(map[string]any)
		if internal, _ := pd["internal"].(bool); internal {
			continue
		}
		if sec := pageSection(p.Name); sec != "" && !sectionEnabled(siteData, sec) {
			continue
		}
		result = append(result, p)
	}
	return result
}
