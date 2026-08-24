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
