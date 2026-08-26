package buildcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func markedMockDir(t *testing.T, rel string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), rel)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, mockMarkerFile), []byte("mock corpus\n"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	return dir
}

func TestAssertNoMockContent_RejectsMockPathSegment(t *testing.T) {
	err := assertNoMockContent(contentSourceProd, "/w/checkout/projects/mock/projects.yaml")
	if err == nil || !strings.Contains(err.Error(), "/mock/ segment") {
		t.Fatalf("err = %v, want a path-segment rejection", err)
	}
}

// The marker is what survives a rename: the corpus is still mock even when the
// path no longer says so.
func TestAssertNoMockContent_RejectsRenamedDirCarryingTheMarker(t *testing.T) {
	dir := markedMockDir(t, "fixtures/posts")
	err := assertNoMockContent(contentSourceProd, dir)
	if err == nil || !strings.Contains(err.Error(), mockMarkerFile) {
		t.Fatalf("err = %v, want the marker file to be cited", err)
	}
}

func TestAssertNoMockContent_FindsMarkerAboveTheContentPath(t *testing.T) {
	root := markedMockDir(t, "fixtures")
	nested := filepath.Join(root, "posts", "some-slug")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := assertNoMockContent(contentSourceProd, nested); err == nil {
		t.Fatal("a marker at the corpus root must reject content nested beneath it")
	}
}

func TestAssertNoMockContent_AllowsMockContentInAMockBuild(t *testing.T) {
	dir := markedMockDir(t, "fixtures/posts")
	if err := assertNoMockContent(contentSourceMock, dir, "/w/checkout/projects/mock/projects.yaml"); err != nil {
		t.Fatalf("a mock build must accept mock content: %v", err)
	}
}

func TestAssertNoMockContent_AllowsRealContent(t *testing.T) {
	real := filepath.Join(t.TempDir(), "checkout", "projects")
	if err := os.MkdirAll(real, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := assertNoMockContent(contentSourceProd, filepath.Join(real, "projects.yaml"), ""); err != nil {
		t.Fatalf("real content must pass: %v", err)
	}
}

// The walk must not escape the content repo and pick up an unrelated marker
// from somewhere high in the workspace.
func TestAssertNoMockContent_MarkerSearchIsDepthBounded(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, mockMarkerFile), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	deep := filepath.Join(root, "a", "b", "c", "d", "e")
	if err := os.MkdirAll(deep, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := assertNoMockContent(contentSourceProd, deep); err != nil {
		t.Fatalf("marker beyond the search depth must not reject: %v", err)
	}
}
