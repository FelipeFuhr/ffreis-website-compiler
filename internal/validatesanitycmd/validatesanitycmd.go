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

	fs := flag.NewFlagSet("validate-sanity", flag.ContinueOnError)
	websiteRoot := fs.String("website-root", ".", "website project root; expects <website-root>/src/{assets,templates} (legacy fallback: <website-root>/{site,templates})")
	assetsDirFlag := fs.String("assets-dir", "", "path to source assets folder (defaults to <website-root>/src/assets, then <website-root>/site)")
	templatesDirFlag := fs.String("templates-dir", "", "path to templates root folder (defaults to <website-root>/src/templates, then <website-root>/templates)")
	siteDataSource := fs.String("site-data", "", "optional site data source override; supports file/URL sources or a directory containing YAML layers")
	sanityDirFlag := fs.String("sanity-dir", "", "optional sanity folder (defaults to <website-root>/sanity if it exists); can contain sanity.yaml to enable/disable checks")
	runSanityDirChecks := fs.Bool("run-sanity-dir-checks", true, "run executable checks from <sanity-dir>/checks.d/ (if present)")
	sanityChecksDirName := fs.String("sanity-checks-dir", "checks.d", "relative directory name under sanity dir that contains executable sanity checks")
	checkAssets := fs.Bool("check-assets", true, "also validate rendered pages only reference local css/js assets (same behavior as validate-assets)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	assetsDir := strings.TrimSpace(*assetsDirFlag)
	templatesRoot := strings.TrimSpace(*templatesDirFlag)
	if assetsDir == "" || templatesRoot == "" {
		resolvedAssetsDir, resolvedTemplatesDir, err := resolveWebsitePaths(*websiteRoot)
		if err != nil {
			return err
		}
		if assetsDir == "" {
			assetsDir = resolvedAssetsDir
		}
		if templatesRoot == "" {
			templatesRoot = resolvedTemplatesDir
		}
	}

	sanityDir := strings.TrimSpace(*sanityDirFlag)
	if sanityDir == "" {
		defaultSanityDir := filepath.Join(*websiteRoot, "sanity")
		if dirExists(defaultSanityDir) {
			sanityDir = defaultSanityDir
		}
	}
	sanityConfig, sanityConfigSource, err := loadSanityConfig(sanityDir)
	if err != nil {
		return fmt.Errorf("loading sanity config: %w", err)
	}

	pages, err := sitegen.LoadPageTemplatesFromRoot(templatesRoot)
	if err != nil {
		return fmt.Errorf("loading templates: %w", err)
	}
	siteDataResult, err := sitegen.LoadSiteData(templatesRoot, *siteDataSource)
	if err != nil {
		return fmt.Errorf("loading site data: %w", err)
	}
	siteDataContractResult, err := sitegen.LoadSiteDataContract(templatesRoot)
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

	if err := sitegen.ValidateSiteSanity(siteDataResult.Data, sanityConfig); err != nil {
		return fmt.Errorf("validating site sanity rules: %w", err)
	}

	if *runSanityDirChecks {
		executable, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolving compiler executable: %w", err)
		}
		if err := runSanityChecksFromDir(*websiteRoot, sanityDir, *sanityChecksDirName, executable, logger); err != nil {
			return err
		}
	}

	if *checkAssets {
		renderedPages := make(map[string]string, len(pages))
		for _, page := range pages {
			var rendered bytes.Buffer
			if err := page.Tmpl.ExecuteTemplate(&rendered, "layout", sitegen.NewTemplateData(page.Name, siteDataResult.Data)); err != nil {
				return fmt.Errorf("rendering %s.html for asset validation: %w", page.Name, err)
			}
			renderedPages[page.Name] = rendered.String()
		}

		if _, err := assetusage.Validate(assetsDir, renderedPages); err != nil {
			return fmt.Errorf("validating local css/js asset usage: %w", err)
		}
	}

	logger.Info(
		"sanity validation passed",
		"website_root", *websiteRoot,
		"assets_dir", assetsDir,
		"templates_dir", templatesRoot,
		"site_data_source", firstNonEmpty(siteDataResult.Source, siteDataResult.DefaultPath),
		"site_data_layers", siteDataResult.Layers,
		"site_data_contract_source", firstNonEmpty(siteDataContractResult.Source, siteDataContractResult.DefaultPath),
		"sanity_dir", sanityDir,
		"sanity_config_source", sanityConfigSource,
		"ran_sanity_dir_checks", *runSanityDirChecks,
		"checked_assets", *checkAssets,
	)
	return nil
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

	entries, err := os.ReadDir(checksRoot)
	if err != nil {
		return fmt.Errorf("reading sanity checks directory %s: %w", checksRoot, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var ran int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		checkPath := filepath.Join(checksRoot, entry.Name())
		info, err := os.Stat(checkPath)
		if err != nil {
			return fmt.Errorf("stat sanity check %s: %w", checkPath, err)
		}
		if !isRunnableSanityCheckFile(entry.Name(), info.Mode()) {
			continue
		}

		ran++
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
	}

	if ran == 0 {
		logger.Info("no executable sanity checks found", "checks_dir", checksRoot)
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
