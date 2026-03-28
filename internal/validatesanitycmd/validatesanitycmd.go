package validatesanitycmd

import (
	"bytes"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"ffreis-website-compiler/internal/assetusage"
	"ffreis-website-compiler/internal/sitegen"
	"gopkg.in/yaml.v3"
)

func Run(args []string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	opts, err := parseValidateSanityOptions(args)
	if err != nil {
		return err
	}

	assetsDir, templatesRoot, sanityDir, sanityConfig, sanityConfigSource, err := resolveValidateSanityPaths(opts)
	if err != nil {
		return err
	}

	pages, siteDataResult, siteDataContractResult, err := loadAndValidateSiteData(logger, templatesRoot, opts.siteDataSource, sanityConfig)
	if err != nil {
		return err
	}

	if err := maybeRunSanityDirChecks(opts, sanityDir, logger); err != nil {
		return err
	}
	if err := maybeValidateAssets(opts, assetsDir, pages, siteDataResult.Data); err != nil {
		return err
	}

	logSanitySuccess(logger, opts, assetsDir, templatesRoot, siteDataResult, siteDataContractResult, sanityDir, sanityConfigSource)
	return nil
}

type validateSanityOptions struct {
	websiteRoot         string
	assetsDir           string
	templatesDir        string
	siteDataSource      string
	sanityDir           string
	runSanityDirChecks  bool
	sanityChecksDirName string
	checkAssets         bool
}

func parseValidateSanityOptions(args []string) (validateSanityOptions, error) {
	fs := flag.NewFlagSet("validate-sanity", flag.ContinueOnError)

	var opts validateSanityOptions
	fs.StringVar(&opts.websiteRoot, "website-root", ".", "website project root; expects <website-root>/src/{assets,templates} (legacy fallback: <website-root>/{site,templates})")
	fs.StringVar(&opts.assetsDir, "assets-dir", "", "path to source assets folder (defaults to <website-root>/src/assets, then <website-root>/site)")
	fs.StringVar(&opts.templatesDir, "templates-dir", "", "path to templates root folder (defaults to <website-root>/src/templates, then <website-root>/templates)")
	fs.StringVar(&opts.siteDataSource, "site-data", "", "optional site data source override; supports file/URL sources or a directory containing YAML layers")
	fs.StringVar(&opts.sanityDir, "sanity-dir", "", "optional sanity folder (defaults to <website-root>/sanity if it exists); can contain sanity.yaml to enable/disable checks")
	fs.BoolVar(&opts.runSanityDirChecks, "run-sanity-dir-checks", true, "run executable checks from <sanity-dir>/checks.d/ (if present)")
	fs.StringVar(&opts.sanityChecksDirName, "sanity-checks-dir", "checks.d", "relative directory name under sanity dir that contains executable sanity checks")
	fs.BoolVar(&opts.checkAssets, "check-assets", true, "also validate rendered pages only reference local css/js assets (same behavior as validate-assets)")

	if err := fs.Parse(args); err != nil {
		return validateSanityOptions{}, err
	}

	opts.assetsDir = strings.TrimSpace(opts.assetsDir)
	opts.templatesDir = strings.TrimSpace(opts.templatesDir)
	opts.sanityDir = strings.TrimSpace(opts.sanityDir)
	return opts, nil
}

func resolveValidateSanityPaths(opts validateSanityOptions) (assetsDir, templatesRoot, sanityDir string, sanityConfig sitegen.SanityConfig, sanityConfigSource string, err error) {
	assetsDir = opts.assetsDir
	templatesRoot = opts.templatesDir
	if assetsDir == "" || templatesRoot == "" {
		resolvedAssetsDir, resolvedTemplatesDir, err := resolveWebsitePaths(opts.websiteRoot)
		if err != nil {
			return "", "", "", sitegen.SanityConfig{}, "", err
		}
		if assetsDir == "" {
			assetsDir = resolvedAssetsDir
		}
		if templatesRoot == "" {
			templatesRoot = resolvedTemplatesDir
		}
	}

	sanityDir = opts.sanityDir
	if sanityDir == "" {
		defaultSanityDir := filepath.Join(opts.websiteRoot, "sanity")
		if dirExists(defaultSanityDir) {
			sanityDir = defaultSanityDir
		}
	}
	sanityConfig, sanityConfigSource, err = loadSanityConfig(sanityDir)
	if err != nil {
		return "", "", "", sitegen.SanityConfig{}, "", fmt.Errorf("loading sanity config: %w", err)
	}
	return assetsDir, templatesRoot, sanityDir, sanityConfig, sanityConfigSource, nil
}

func loadAndValidateSiteData(logger *slog.Logger, templatesRoot, siteDataSource string, sanityConfig sitegen.SanityConfig) ([]sitegen.PageTemplate, sitegen.SiteDataLoadResult, sitegen.SiteDataContractLoadResult, error) {
	pages, err := sitegen.LoadPageTemplatesFromRoot(templatesRoot)
	if err != nil {
		return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("loading templates: %w", err)
	}
	siteDataResult, err := sitegen.LoadSiteData(templatesRoot, siteDataSource)
	if err != nil {
		return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("loading site data: %w", err)
	}
	siteDataContractResult, err := sitegen.LoadSiteDataContract(templatesRoot)
	if err != nil {
		return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("loading site data contract: %w", err)
	}

	logSiteDataOverride(logger, siteDataResult)
	if err := validateSiteDataAndUsage(pages, siteDataResult, siteDataContractResult); err != nil {
		return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, err
	}

	if err := sitegen.ValidateSiteSanity(siteDataResult.Data, sanityConfig); err != nil {
		return nil, sitegen.SiteDataLoadResult{}, sitegen.SiteDataContractLoadResult{}, fmt.Errorf("validating site sanity rules: %w", err)
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

func maybeRunSanityDirChecks(opts validateSanityOptions, sanityDir string, logger *slog.Logger) error {
	if !opts.runSanityDirChecks {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving compiler executable: %w", err)
	}
	return runSanityChecksFromDir(opts.websiteRoot, sanityDir, opts.sanityChecksDirName, executable, logger)
}

func maybeValidateAssets(opts validateSanityOptions, assetsDir string, pages []sitegen.PageTemplate, siteData map[string]any) error {
	if !opts.checkAssets {
		return nil
	}

	renderedPages, err := renderPagesForAssetValidation(pages, siteData)
	if err != nil {
		return err
	}
	if _, err := assetusage.Validate(assetsDir, renderedPages); err != nil {
		return fmt.Errorf("validating local css/js asset usage: %w", err)
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

func logSanitySuccess(logger *slog.Logger, opts validateSanityOptions, assetsDir, templatesRoot string, siteDataResult sitegen.SiteDataLoadResult, siteDataContractResult sitegen.SiteDataContractLoadResult, sanityDir, sanityConfigSource string) {
	logger.Info(
		"sanity validation passed",
		"website_root", opts.websiteRoot,
		"assets_dir", assetsDir,
		"templates_dir", templatesRoot,
		"site_data_source", firstNonEmpty(siteDataResult.Source, siteDataResult.DefaultPath),
		"site_data_layers", siteDataResult.Layers,
		"site_data_contract_source", firstNonEmpty(siteDataContractResult.Source, siteDataContractResult.DefaultPath),
		"sanity_dir", sanityDir,
		"sanity_config_source", sanityConfigSource,
		"ran_sanity_dir_checks", opts.runSanityDirChecks,
		"checked_assets", opts.checkAssets,
	)
}

type sanityConfigFile struct {
	Version int `yaml:"version"`
	Checks  struct {
		CourseStartMatchesFirstSession         *bool `yaml:"course_start_matches_first_session"`
		CourseDurationHoursMatchesSessionHours *bool `yaml:"course_duration_hours_matches_session_hours"`
	} `yaml:"checks"`
}

func loadSanityConfig(sanityDir string) (sitegen.SanityConfig, string, error) {
	config := sitegen.DefaultSanityConfig()
	if strings.TrimSpace(sanityDir) == "" {
		return config, "", nil
	}

	candidates := []string{
		filepath.Join(sanityDir, "sanity.yaml"),
		filepath.Join(sanityDir, "sanity.yml"),
	}
	var path string
	for _, candidate := range candidates {
		if fileExists(candidate) {
			path = candidate
			break
		}
	}
	if path == "" {
		return config, "", nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return sitegen.SanityConfig{}, "", err
	}

	var parsed sanityConfigFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return sitegen.SanityConfig{}, "", err
	}

	if parsed.Checks.CourseStartMatchesFirstSession != nil {
		config.CourseStartMatchesFirstSession = *parsed.Checks.CourseStartMatchesFirstSession
	}
	if parsed.Checks.CourseDurationHoursMatchesSessionHours != nil {
		config.CourseDurationHoursMatchesSessionHours = *parsed.Checks.CourseDurationHoursMatchesSessionHours
	}
	return config, path, nil
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

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func runSanityChecksFromDir(websiteRoot, sanityDir, sanityChecksDirName, compilerExe string, logger *slog.Logger) error {
	if strings.TrimSpace(sanityDir) == "" {
		return nil
	}
	checksRoot := filepath.Join(sanityDir, sanityChecksDirName)
	if !dirExists(checksRoot) {
		return nil
	}

	entries, err := readSortedDirEntries(checksRoot)
	if err != nil {
		return err
	}

	ran := 0
	for _, entry := range entries {
		checkPath, runnable, err := resolveRunnableSanityCheck(checksRoot, entry)
		if err != nil {
			return err
		}
		if !runnable {
			continue
		}

		ran++
		if err := runSanityCheck(websiteRoot, sanityDir, checksRoot, compilerExe, checkPath, logger); err != nil {
			return err
		}
	}

	if ran == 0 {
		logger.Info("no executable sanity checks found", "checks_dir", checksRoot)
	}
	return nil
}

func readSortedDirEntries(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading sanity checks directory %s: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func resolveRunnableSanityCheck(checksRoot string, entry os.DirEntry) (string, bool, error) {
	if entry.IsDir() {
		return "", false, nil
	}

	checkPath := filepath.Join(checksRoot, entry.Name())
	info, err := os.Stat(checkPath)
	if err != nil {
		return "", false, fmt.Errorf("stat sanity check %s: %w", checkPath, err)
	}
	if !isRunnableSanityCheckFile(entry.Name(), info.Mode()) {
		return "", false, nil
	}
	return checkPath, true, nil
}

func runSanityCheck(websiteRoot, sanityDir, checksRoot, compilerExe, checkPath string, logger *slog.Logger) error {
	logger.Info("running sanity check", "check", checkPath)

	cmd, err := sanityCheckCommand(checkPath)
	if err != nil {
		return err
	}
	cmd.Dir = websiteRoot
	cmd.Env = append(os.Environ(),
		"WEBSITE_ROOT="+websiteRoot,
		"SANITY_DIR="+sanityDir,
		"SANITY_CHECKS_DIR="+checksRoot,
		"WEBSITE_COMPILER_EXE="+compilerExe,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sanity check failed (%s): %w\n%s", checkPath, err, strings.TrimSpace(string(out)))
	}
	if len(out) > 0 {
		logger.Info("sanity check output", "check", checkPath, "output", strings.TrimSpace(string(out)))
	}
	return nil
}

func isRunnableSanityCheckFile(name string, mode os.FileMode) bool {
	if mode&0o111 != 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".sh", ".bash", ".py", ".rb":
		return true
	default:
		return false
	}
}

func sanityCheckCommand(path string) (*exec.Cmd, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".sh" || ext == ".bash" {
		return exec.Command("/bin/bash", path), nil
	}
	if ext == ".py" {
		return exec.Command("python3", path), nil
	}
	if ext == ".rb" {
		return exec.Command("ruby", path), nil
	}
	if runtime.GOOS == "windows" && ext == ".cmd" {
		return exec.Command("cmd.exe", "/c", path), nil
	}
	return exec.Command(path), nil
}
