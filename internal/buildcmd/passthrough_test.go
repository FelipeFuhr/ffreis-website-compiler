package buildcmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyStaticAssets_PassthroughCopiesExtraFile(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcRoot, "embed.html"), []byte("<p>demo</p>"), 0o644); err != nil {
		t.Fatalf("writing fixture file: %v", err)
	}

	if err := copyStaticAssets(srcRoot, dstRoot, []string{"embed.html"}); err != nil {
		t.Fatalf("copyStaticAssets: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dstRoot, "embed.html"))
	if err != nil {
		t.Fatalf("expected embed.html copied to output, got error: %v", err)
	}
	if string(got) != "<p>demo</p>" {
		t.Errorf("content = %q, want <p>demo</p>", got)
	}
}

func TestCopyStaticAssets_PassthroughCopiesExtraDirectory(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcRoot, "extra", "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcRoot, "extra", "nested", "file.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("writing fixture file: %v", err)
	}

	if err := copyStaticAssets(srcRoot, dstRoot, []string{"extra"}); err != nil {
		t.Fatalf("copyStaticAssets: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dstRoot, "extra", "nested", "file.txt")); err != nil {
		t.Errorf("expected extra/nested/file.txt copied to output, got: %v", err)
	}
}

func TestCopyStaticAssets_PassthroughMissingPathIsNotAnError(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	if err := copyStaticAssets(srcRoot, dstRoot, []string{"does-not-exist.html"}); err != nil {
		t.Fatalf("expected a missing passthrough path to be silently skipped, got: %v", err)
	}
}

func TestClassifyPassthroughPaths(t *testing.T) {
	srcRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcRoot, "a-file.html"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srcRoot, "a-dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	dirs, files := classifyPassthroughPaths(srcRoot, []string{"a-file.html", "a-dir", "missing"})
	if len(dirs) != 1 || dirs[0] != "a-dir" {
		t.Errorf("dirs = %v, want [a-dir]", dirs)
	}
	if len(files) != 1 || files[0] != "a-file.html" {
		t.Errorf("files = %v, want [a-file.html]", files)
	}
}
