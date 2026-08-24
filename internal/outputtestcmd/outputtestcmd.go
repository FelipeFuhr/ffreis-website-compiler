// Package outputtestcmd validates a compiled site's public output against a
// manifest that belongs to the website.  It deliberately contains no product
// rules: each website declares the routes and assertions meaningful to it.
package outputtestcmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultManifest = "tests/output.yaml"

type options struct {
	websiteRoot string
	out         string
	manifest    string
}

type manifest struct {
	Version int        `yaml:"version"`
	Pages   []pageTest `yaml:"pages"`
}

type pageTest struct {
	Name        string   `yaml:"name"`
	Path        string   `yaml:"path"`
	Exists      *bool    `yaml:"exists"`
	Contains    []string `yaml:"contains"`
	NotContains []string `yaml:"not_contains"`
}

// Run executes deterministic assertions on compiled files.  The manifest is
// intentionally declarative: no shell commands or executable snippets are
// accepted, so a website's test fixture cannot execute arbitrary code through
// the compiler process.
func Run(args []string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	manifestPath := opts.manifest
	if manifestPath == "" {
		manifestPath = filepath.Join(opts.websiteRoot, defaultManifest)
	} else if !filepath.IsAbs(manifestPath) {
		manifestPath = filepath.Join(opts.websiteRoot, manifestPath)
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading output test manifest %s: %w", manifestPath, err)
	}
	var tests manifest
	if err := yaml.Unmarshal(data, &tests); err != nil {
		return fmt.Errorf("parsing output test manifest %s: %w", manifestPath, err)
	}
	if err := validateManifest(tests); err != nil {
		return fmt.Errorf("validating output test manifest %s: %w", manifestPath, err)
	}

	outRoot, err := filepath.Abs(opts.out)
	if err != nil {
		return fmt.Errorf("resolving output directory: %w", err)
	}
	info, err := os.Stat(outRoot)
	if err != nil {
		return fmt.Errorf("reading output directory %s: %w", outRoot, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("output path %s is not a directory", outRoot)
	}
	// Canonicalise the root once. Individual test files are then rejected if
	// they are symlinks, preventing a manifest from reading outside compiled
	// output through a surprising filesystem link.
	outRoot, err = filepath.EvalSymlinks(outRoot)
	if err != nil {
		return fmt.Errorf("resolving output directory %s: %w", opts.out, err)
	}

	for _, page := range tests.Pages {
		if err := runPageTest(outRoot, page); err != nil {
			return err
		}
	}
	logger.Info("compiled output tests passed", "manifest", manifestPath, "output", outRoot, "pages", len(tests.Pages))
	return nil
}

func parseOptions(args []string) (options, error) {
	fs := flag.NewFlagSet("test-output", flag.ContinueOnError)
	var opts options
	fs.StringVar(&opts.websiteRoot, "website-root", ".", "website project root; used to resolve the default tests/output.yaml manifest")
	fs.StringVar(&opts.out, "out", "dist", "compiled output directory to test")
	fs.StringVar(&opts.manifest, "manifest", "", "website-owned output test manifest (defaults to tests/output.yaml under website-root)")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	return opts, nil
}

func validateManifest(tests manifest) error {
	if tests.Version != 1 {
		return fmt.Errorf("version must be 1")
	}
	if len(tests.Pages) == 0 {
		return fmt.Errorf("pages must contain at least one test")
	}
	seen := make(map[string]struct{}, len(tests.Pages))
	for i, page := range tests.Pages {
		if strings.TrimSpace(page.Path) == "" {
			return fmt.Errorf("pages[%d].path is required", i)
		}
		filePath, err := outputFilePath(page.Path)
		if err != nil {
			return fmt.Errorf("pages[%d].path: %w", i, err)
		}
		if _, ok := seen[filePath]; ok {
			return fmt.Errorf("pages contains duplicate path %q", page.Path)
		}
		seen[filePath] = struct{}{}
		if page.Exists != nil && !*page.Exists && (len(page.Contains) > 0 || len(page.NotContains) > 0) {
			return fmt.Errorf("pages[%d] cannot assert content when exists is false", i)
		}
	}
	return nil
}

func runPageTest(outRoot string, page pageTest) error {
	rel, err := outputFilePath(page.Path)
	if err != nil {
		return fmt.Errorf("output test %q: %w", displayName(page), err)
	}
	path := filepath.Join(outRoot, rel)
	if !pathWithin(outRoot, path) {
		return fmt.Errorf("output test %q resolves outside output directory", displayName(page))
	}
	// Lstat only identifies a symlink at the final path component. Resolve the
	// complete path too, so a symlinked directory inside output cannot redirect
	// a seemingly-safe route to an arbitrary file outside the build result.
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err == nil && !pathWithin(outRoot, resolvedPath) {
		return fmt.Errorf("output test %q: refusing output path outside compiled output", displayName(page))
	}
	info, err := os.Lstat(path)
	wantExists := page.Exists == nil || *page.Exists
	if os.IsNotExist(err) {
		if wantExists {
			return fmt.Errorf("output test %q: expected file %s to exist", displayName(page), rel)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("output test %q: inspecting %s: %w", displayName(page), rel, err)
	}
	if !wantExists {
		return fmt.Errorf("output test %q: expected file %s not to exist", displayName(page), rel)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("output test %q: refusing symlinked output file %s", displayName(page), rel)
	}
	if info.IsDir() {
		return fmt.Errorf("output test %q: expected file but found directory %s", displayName(page), rel)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("output test %q: reading %s: %w", displayName(page), rel, err)
	}
	text := string(content)
	for _, expected := range page.Contains {
		if !strings.Contains(text, expected) {
			return fmt.Errorf("output test %q: %s does not contain %q", displayName(page), rel, expected)
		}
	}
	for _, forbidden := range page.NotContains {
		if strings.Contains(text, forbidden) {
			return fmt.Errorf("output test %q: %s contains forbidden text %q", displayName(page), rel, forbidden)
		}
	}
	return nil
}

func outputFilePath(route string) (string, error) {
	route = strings.TrimSpace(route)
	if route == "" || filepath.IsAbs(route) && strings.Contains(route, "..") {
		return "", fmt.Errorf("must be a safe route-relative path")
	}
	route = strings.TrimPrefix(route, "/")
	if route == "" || strings.HasSuffix(route, "/") {
		route += "index.html"
	}
	cleaned := filepath.Clean(filepath.FromSlash(route))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("must not traverse outside the compiled output")
	}
	return cleaned, nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func displayName(page pageTest) string {
	if strings.TrimSpace(page.Name) != "" {
		return page.Name
	}
	return page.Path
}

// SortedPaths is retained for tests and future machine-readable reporting.
func SortedPaths(tests manifest) []string {
	paths := make([]string, 0, len(tests.Pages))
	for _, page := range tests.Pages {
		paths = append(paths, page.Path)
	}
	sort.Strings(paths)
	return paths
}
