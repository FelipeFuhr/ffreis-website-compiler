package exportsitedatacmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"ffreis-website-compiler/internal/cmdutil"
	"ffreis-website-compiler/internal/sitegen"
)

func Run(args []string, _ *slog.Logger) error {
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
		resolvedTemplatesRoot, err := cmdutil.ResolveTemplatesRoot(*websiteRoot)
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
