package bundlecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// detectLangs decides which language bundles get emitted. A directory it fails
// to see is a language that silently ships nothing.

func TestDetectLangs_ReturnsSortedLanguageDirsExcludingShared(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"pt", "en", "jp", "shared"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	langs, err := detectLangs(root)
	if err != nil {
		t.Fatalf("detectLangs: %v", err)
	}
	if strings.Join(langs, "|") != "en|jp|pt" {
		t.Errorf("langs = %v, want [en jp pt]: sorted, no 'shared', no plain files", langs)
	}
}

func TestDetectLangs_EmptyRootYieldsNoLanguages(t *testing.T) {
	langs, err := detectLangs(t.TempDir())
	if err != nil {
		t.Fatalf("detectLangs: %v", err)
	}
	if len(langs) != 0 {
		t.Errorf("langs = %v, want none", langs)
	}
}

// Only "shared" is special; a directory that merely contains the word is a
// language like any other.
func TestDetectLangs_OnlyTheExactSharedNameIsExcluded(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"shared", "shared-extra", "not-shared"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	langs, err := detectLangs(root)
	if err != nil {
		t.Fatalf("detectLangs: %v", err)
	}
	if strings.Join(langs, "|") != "not-shared|shared-extra" {
		t.Errorf("langs = %v, want only the exact 'shared' name excluded", langs)
	}
}

func TestDetectLangs_MissingRootIsAnError(t *testing.T) {
	if _, err := detectLangs(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a missing data root must be an error, not an empty language list")
	}
}
