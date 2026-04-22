package testutil

import (
	"io"
	"log/slog"
	"os"
	"testing"
)

func DiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func MustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func WriteFiles(t *testing.T, files map[string]string) {
	t.Helper()
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec
			t.Fatalf("write %s: %v", path, err)
		}
	}
}
