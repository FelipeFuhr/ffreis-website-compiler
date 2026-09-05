package bundlecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig_PetlookReference(t *testing.T) {
	cfg, err := LoadConfig("testdata/petlook.bundle-config.yaml")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.CDNBase != "https://petlook.app" {
		t.Errorf("cdn_base = %q, want https://petlook.app", cfg.CDNBase)
	}
	if cfg.LangAliases["jp"] != "ja" || cfg.LangAliases["pt"] != "pt-BR" {
		t.Errorf("lang_aliases = %v, want jp:ja pt:pt-BR", cfg.LangAliases)
	}
	if len(cfg.UISections) != 3 {
		t.Fatalf("expected 3 ui_sections (nav, generator, errors), got %d", len(cfg.UISections))
	}
	names := map[string]bool{}
	for _, sec := range cfg.UISections {
		names[sec.Name] = true
	}
	for _, want := range []string{"nav", "generator", "errors"} {
		if !names[want] {
			t.Errorf("missing ui_sections entry %q", want)
		}
	}
	if len(cfg.LangLinkKeys) == 0 || len(cfg.RecommendationKeys) == 0 || len(cfg.BrandKeys) == 0 {
		t.Error("expected non-empty lang_link_keys, recommendation_keys, brand_keys")
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := LoadConfig(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestLoadConfig_RequiresUISections(t *testing.T) {
	path := writeConfig(t, "cdn_base: https://x\n")
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "ui_sections") {
		t.Fatalf("expected ui_sections validation error, got: %v", err)
	}
}

func TestLoadConfig_RejectsSectionMissingName(t *testing.T) {
	path := writeConfig(t, "ui_sections:\n  - keys: [a]\n")
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("expected missing-name validation error, got: %v", err)
	}
}

func TestLoadConfig_RejectsSectionMissingKeys(t *testing.T) {
	path := writeConfig(t, "ui_sections:\n  - name: nav\n")
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "nav") {
		t.Fatalf("expected missing-keys validation error naming the section, got: %v", err)
	}
}

func TestRun_RequiresBundleConfig(t *testing.T) {
	err := Run([]string{"-data-root", "testdata/data", "-out", t.TempDir()}, nil)
	if err == nil || !strings.Contains(err.Error(), "-bundle-config") {
		t.Fatalf("expected -bundle-config required error, got: %v", err)
	}
}

func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bundle-config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
