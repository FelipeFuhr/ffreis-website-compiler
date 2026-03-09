package buildcmd

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_GeneratesHelloWorldOutput(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot, err := filepath.Abs(filepath.Join("..", "..", "examples", "hello-world"))
	if err != nil {
		t.Fatalf("resolving website root: %v", err)
	}
	outDir := t.TempDir()

	args := []string{
		"-website-root", websiteRoot,
		"-out", outDir,
		"-sitemap-base-url", "https://example.com",
	}
	if err := Run(args, logger); err != nil {
		t.Fatalf("build run failed: %v", err)
	}

	indexPath := filepath.Join(outDir, "index.html")
	content, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("reading %s: %v", indexPath, err)
	}
	html := string(content)
	for _, expected := range []string{
		"<title>Hello World</title>",
		"<p>Hello, world.</p>",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("expected %q in rendered html", expected)
		}
	}

	cssPath := filepath.Join(outDir, "css", "main.css")
	if _, err := os.Stat(cssPath); err != nil {
		t.Fatalf("expected copied css asset %s: %v", cssPath, err)
	}

	sitemapPath := filepath.Join(outDir, "sitemap.xml")
	sitemapRaw, err := os.ReadFile(sitemapPath)
	if err != nil {
		t.Fatalf("expected generated sitemap: %v", err)
	}
	if !strings.Contains(string(sitemapRaw), "<urlset") {
		t.Fatalf("expected sitemap.xml to contain urlset")
	}
}
