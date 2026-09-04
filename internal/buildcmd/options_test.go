package buildcmd

import (
	"strings"
	"testing"
)

func TestParseBuildOptions_ContentSourceGuard(t *testing.T) {
	t.Run("default prod rejects mock courses path", func(t *testing.T) {
		_, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-courses-file", "/some/repo/mock/courses.yaml",
		})
		if err == nil {
			t.Fatal("expected error for mock path with prod source, got nil")
		}
		if !strings.Contains(err.Error(), "/mock/") {
			t.Errorf("error should mention /mock/, got: %v", err)
		}
	})

	t.Run("explicit prod rejects mock posts path", func(t *testing.T) {
		_, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-content-source", "prod",
			"-posts-dir", "/some/repo/mock/posts",
		})
		if err == nil {
			t.Fatal("expected error for mock path with prod source, got nil")
		}
	})

	t.Run("mock source allows mock paths", func(t *testing.T) {
		_, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-content-source", "mock",
			"-courses-file", "/some/repo/mock/courses.yaml",
			"-posts-dir", "/some/repo/mock/posts",
		})
		if err != nil {
			t.Fatalf("expected no error for mock paths with mock source, got: %v", err)
		}
	})

	t.Run("prod source allows non-mock paths", func(t *testing.T) {
		_, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-courses-file", "/some/repo/courses.yaml",
			"-posts-dir", "/some/repo/posts",
		})
		if err != nil {
			t.Fatalf("expected no error for real paths with prod source, got: %v", err)
		}
	})

	t.Run("dev source rejects mock courses path", func(t *testing.T) {
		// dev is for real content paths against dev.ffreis.com; it gets no
		// /mock/ exemption — only content-source=mock does.
		_, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-content-source", "dev",
			"-courses-file", "/some/repo/mock/courses.yaml",
		})
		if err == nil {
			t.Fatal("expected error for mock path with dev source, got nil")
		}
		if !strings.Contains(err.Error(), "/mock/") {
			t.Errorf("error should mention /mock/, got: %v", err)
		}
	})

	t.Run("dev source allows non-mock paths", func(t *testing.T) {
		_, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-content-source", "dev",
			"-courses-file", "/some/repo/courses.yaml",
		})
		if err != nil {
			t.Fatalf("expected no error for real paths with dev source, got: %v", err)
		}
	})

	t.Run("invalid content-source value is rejected", func(t *testing.T) {
		_, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-content-source", "staging",
		})
		if err == nil {
			t.Fatal("expected error for invalid --content-source value, got nil")
		}
		if !strings.Contains(err.Error(), "content-source") {
			t.Errorf("error should mention content-source, got: %v", err)
		}
	})
}

func TestParseBuildOptions_DevContentSourceImpliesAllowBlanketRobotsDisallow(t *testing.T) {
	t.Run("content-source=dev implies allow-blanket-robots-disallow", func(t *testing.T) {
		opts, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-content-source", "dev",
		})
		if err != nil {
			t.Fatalf("expected no error for content-source=dev, got: %v", err)
		}
		if opts.contentSource != "dev" {
			t.Errorf("contentSource = %q, want %q", opts.contentSource, "dev")
		}
		if !opts.allowBlanketRobotsDisallow {
			t.Error("expected allowBlanketRobotsDisallow to be implied true for content-source=dev")
		}
	})

	t.Run("content-source=prod (default) does not imply allow-blanket-robots-disallow", func(t *testing.T) {
		opts, err := parseBuildOptions([]string{
			"-website-root", ".",
		})
		if err != nil {
			t.Fatalf("expected no error for default build, got: %v", err)
		}
		if opts.allowBlanketRobotsDisallow {
			t.Error("expected allowBlanketRobotsDisallow to stay false for content-source=prod")
		}
	})

	t.Run("content-source=mock does not imply allow-blanket-robots-disallow", func(t *testing.T) {
		opts, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-content-source", "mock",
		})
		if err != nil {
			t.Fatalf("expected no error for content-source=mock, got: %v", err)
		}
		if opts.allowBlanketRobotsDisallow {
			t.Error("expected allowBlanketRobotsDisallow to stay false for content-source=mock")
		}
	})

	t.Run("explicit allow-blanket-robots-disallow=false still overridden true by content-source=dev", func(t *testing.T) {
		// -allow-blanket-robots-disallow=false is also the flag's zero value, so
		// this is really the same case as the implicit-default case above, but
		// spelled out explicitly to document that dev's implication always wins.
		opts, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-content-source", "dev",
			"-allow-blanket-robots-disallow=false",
		})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !opts.allowBlanketRobotsDisallow {
			t.Error("expected content-source=dev to imply allowBlanketRobotsDisallow=true regardless of an explicit =false")
		}
	})
}

func TestParseBuildOptions_TrackerCDNBaseGuard(t *testing.T) {
	t.Run("tracker enabled without cdn base fails", func(t *testing.T) {
		_, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-tracker-enabled",
			"-tracker-sdk-version", "1.0.0",
			"-tracker-site-id", "flemming",
			"-tracker-endpoint", "https://events.flemming.com.br",
		})
		if err == nil {
			t.Fatal("expected error for tracker-enabled with no tracker-cdn-base, got nil")
		}
		if !strings.Contains(err.Error(), "tracker-cdn-base") {
			t.Errorf("error should mention tracker-cdn-base, got: %v", err)
		}
	})

	t.Run("tracker enabled with explicit cdn base succeeds", func(t *testing.T) {
		opts, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-tracker-enabled",
			"-tracker-sdk-version", "1.0.0",
			"-tracker-site-id", "flemming",
			"-tracker-endpoint", "https://events.flemming.com.br",
			"-tracker-cdn-base", "https://cdn.ffreis.com",
		})
		if err != nil {
			t.Fatalf("expected no error for tracker-enabled with explicit tracker-cdn-base, got: %v", err)
		}
		if opts.trackerCDNBase != "https://cdn.ffreis.com" {
			t.Errorf("trackerCDNBase = %q, want https://cdn.ffreis.com", opts.trackerCDNBase)
		}
	})

	t.Run("tracker disabled without cdn base is fine", func(t *testing.T) {
		_, err := parseBuildOptions([]string{
			"-website-root", ".",
		})
		if err != nil {
			t.Fatalf("expected no error when tracker is disabled, got: %v", err)
		}
	})
}

func TestParseBuildOptions_DevDataProdGuard(t *testing.T) {
	t.Run("dev-data with default (prod) content-source fails", func(t *testing.T) {
		_, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-dev-data",
		})
		if err == nil {
			t.Fatal("expected error for dev-data with prod content-source, got nil")
		}
		if !strings.Contains(err.Error(), "dev-data") || !strings.Contains(err.Error(), "content-source") {
			t.Errorf("error should mention both dev-data and content-source, got: %v", err)
		}
	})

	t.Run("dev-data with explicit prod content-source fails", func(t *testing.T) {
		_, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-dev-data",
			"-content-source", "prod",
		})
		if err == nil {
			t.Fatal("expected error for dev-data with content-source=prod, got nil")
		}
	})

	t.Run("dev-data with mock content-source succeeds", func(t *testing.T) {
		opts, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-dev-data",
			"-content-source", "mock",
		})
		if err != nil {
			t.Fatalf("expected no error for dev-data with content-source=mock, got: %v", err)
		}
		if !opts.devData {
			t.Error("expected devData to be true")
		}
	})

	t.Run("dev-data with dev content-source succeeds", func(t *testing.T) {
		// PR #103's prod-only gate uses strict equality (== "prod") specifically
		// so a future non-prod, non-mock content-source composes correctly with
		// it. content-source=dev is that value: confirm it actually does.
		opts, err := parseBuildOptions([]string{
			"-website-root", ".",
			"-dev-data",
			"-content-source", "dev",
		})
		if err != nil {
			t.Fatalf("expected no error for dev-data with content-source=dev, got: %v", err)
		}
		if !opts.devData {
			t.Error("expected devData to be true")
		}
		if opts.contentSource != "dev" {
			t.Errorf("contentSource = %q, want %q", opts.contentSource, "dev")
		}
	})

	t.Run("no dev-data with prod content-source succeeds", func(t *testing.T) {
		opts, err := parseBuildOptions([]string{
			"-website-root", ".",
		})
		if err != nil {
			t.Fatalf("expected no error for default prod build without dev-data, got: %v", err)
		}
		if opts.devData {
			t.Error("expected devData to be false by default")
		}
	})
}

func TestDisableSectionsHelpText_ListsEverySectionTableName(t *testing.T) {
	help := disableSectionsHelpText()
	for _, name := range sectionNames() {
		if !strings.Contains(help, name) {
			t.Errorf("disable-sections help text missing section %q: %s", name, help)
		}
	}
}

func TestParseBuildOptions_OutputTests(t *testing.T) {
	t.Parallel()
	opts, err := parseBuildOptions([]string{
		"-website-root", "site",
		"-run-output-tests",
		"-output-test-manifest", "tests/release.yaml",
	})
	if err != nil {
		t.Fatalf("parseBuildOptions() error = %v", err)
	}
	if !opts.runOutputTests || opts.outputTestManifest != "tests/release.yaml" {
		t.Fatalf("output test options = %+v, want enabled custom manifest", opts)
	}
}
