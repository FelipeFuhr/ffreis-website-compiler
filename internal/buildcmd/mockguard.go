package buildcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// mockMarkerFile is committed at the root of a content repo's mock/ tree. It
// travels with the content, so a mock corpus stays identifiable after it is
// copied, renamed, or checked out somewhere the word "mock" never appears in
// the path. The path check below catches the common case; the marker catches
// the case where the path was the only evidence and someone changed it.
const mockMarkerFile = ".mock-content"

// mockMarkerSearchDepth bounds the walk up from a content path toward the repo
// root. Content lives at most a couple of levels under the mock root
// (mock/posts/<slug>/, mock/projects.yaml), so three levels covers every
// layout the deployer produces without escaping into the workspace.
const mockMarkerSearchDepth = 3

// assertNoMockContent fails a non-mock build that was handed mock content.
//
// Two independent signals, because either alone is defeatable: a /mock/ path
// segment (defeated by renaming the directory) and a committed marker file
// (defeated by deleting it from the content repo, which is a visible diff).
// Mock content reaching production is the failure this guards, so it is a build
// error, never a warning.
func assertNoMockContent(contentSource string, paths ...string) error {
	if contentSource == contentSourceMock {
		return nil
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if strings.Contains(p, "/mock/") || strings.HasSuffix(p, "/mock") {
			return mockLeakError(p, contentSource, "its path contains a /mock/ segment")
		}
		if marker, found := findMockMarker(p); found {
			return mockLeakError(p, contentSource, fmt.Sprintf("%s marks it as mock content", marker))
		}
	}
	return nil
}

// findMockMarker looks for the mock marker beside a content path and in its
// nearest ancestors, returning the marker path when one is found.
func findMockMarker(contentPath string) (string, bool) {
	dir := contentPath
	if info, err := os.Stat(contentPath); err != nil || !info.IsDir() {
		dir = filepath.Dir(contentPath)
	}
	for range mockMarkerSearchDepth + 1 {
		marker := filepath.Join(dir, mockMarkerFile)
		if _, err := os.Stat(marker); err == nil {
			return marker, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

func mockLeakError(path, contentSource, reason string) error {
	return fmt.Errorf(
		"content path %q cannot be used with -content-source=%q because %s; "+
			"mock content must never reach a production build — pass -content-source=%s "+
			"to build a dev site from mock content, or point this build at the real content",
		path, contentSource, reason, contentSourceMock,
	)
}
