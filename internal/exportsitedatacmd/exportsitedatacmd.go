package exportsitedatacmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"ffreis-website-compiler/internal/sitegen"
	"gopkg.in/yaml.v3"
)

func Run(args []string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	fs := flag.NewFlagSet("export-site-data", flag.ContinueOnError)
	websiteRoot := fs.String("website-root", ".", "website project root; expects <website-root>/src/{assets,templates} (legacy fallback: <website-root>/{site,templates})")
	templatesDirFlag := fs.String("templates-dir", "", "path to templates root folder (defaults to <website-root>/src/templates, then <website-root>/templates)")
	siteDataSource := fs.String("site-data", "", "optional site data source override; supports file/URL sources or a directory containing YAML layers")
	format := fs.String("format", "json", "output format: json|yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}

	templatesRoot := strings.TrimSpace(*templatesDirFlag)
	if templatesRoot == "" {
		resolvedTemplatesRoot, err := resolveTemplatesRoot(*websiteRoot)
		if err != nil {
			return err
		}
		templatesRoot = resolvedTemplatesRoot
	}

	siteDataResult, err := sitegen.LoadSiteData(templatesRoot, *siteDataSource)
	if err != nil {
		return fmt.Errorf("loading site data: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(siteDataResult.Data)
	case "yaml", "yml":
		out, err := yaml.Marshal(siteDataResult.Data)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(out)
		return err
	default:
		return fmt.Errorf("unsupported format %q (expected json|yaml)", *format)
	}
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

