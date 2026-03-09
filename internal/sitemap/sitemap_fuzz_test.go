package sitemap

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzLoadConfigAndGenerateXML(f *testing.F) {
	f.Add("base_url: https://example.com\nurls:\n  - path: /\n")
	f.Add("base_url: https://example.com\ndefault_lastmod: 2026-01-01\nurls:\n  - path: /about.html\n")
	f.Add("invalid: [")

	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 4096 {
			t.Skip()
		}

		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "sitemap.yaml")
		if err := os.WriteFile(cfgPath, []byte(raw), 0o644); err != nil {
			t.Fatalf("writing fuzz config: %v", err)
		}

		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			return
		}

		_, _ = GenerateXML(cfg, tmpDir)
	})
}
