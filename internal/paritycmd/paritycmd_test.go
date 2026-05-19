package paritycmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFlattenKeys_depth1(t *testing.T) {
	m := map[string]any{"a": map[string]any{"b": "c"}}
	got := flattenKeys(m, "", 1)
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("depth 1: got %v, want [a]", got)
	}
}

func TestFlattenKeys_depth3(t *testing.T) {
	m := map[string]any{"ui": map[string]any{"nav": map[string]any{"home": "x"}}}
	got := flattenKeys(m, "", 3)
	want := []string{"ui", "ui.nav", "ui.nav.home"}
	if len(got) != len(want) {
		t.Fatalf("depth 3: got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("depth 3 [%d]: got %q, want %q", i, got[i], w)
		}
	}
}

func TestFlattenKeys_sliceIsLeaf(t *testing.T) {
	m := map[string]any{"ui": map[string]any{"list": []any{1, 2}}}
	got := flattenKeys(m, "", 3)
	want := []string{"ui", "ui.list"}
	if len(got) != len(want) {
		t.Fatalf("slice leaf: got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("slice leaf [%d]: got %q, want %q", i, got[i], w)
		}
	}
}

func TestFlattenKeys_empty(t *testing.T) {
	got := flattenKeys(nil, "", 3)
	if len(got) != 0 {
		t.Errorf("empty map: got %v, want []", got)
	}
}

func TestLoadSkipConfig_absent(t *testing.T) {
	sc, err := loadSkipConfig("/nonexistent/.lang-parity-skip.yaml")
	if err != nil {
		t.Fatalf("absent file should not error: %v", err)
	}
	if len(sc.Skip) != 0 {
		t.Errorf("expected empty skip config, got %v", sc.Skip)
	}
}

func TestLoadSkipConfig_valid(t *testing.T) {
	dir := t.TempDir()
	content := "skip:\n  - en/site.d/10-courses.yaml\n  - en/site.d/20-calendar.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, ".lang-parity-skip.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	sc, err := loadSkipConfig(filepath.Join(dir, ".lang-parity-skip.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sc.isFileSkipped("en", "10-courses.yaml") {
		t.Error("10-courses.yaml should be skipped for en")
	}
	if !sc.isFileSkipped("en", "20-calendar.yaml") {
		t.Error("20-calendar.yaml should be skipped for en")
	}
	if sc.isFileSkipped("pt", "10-courses.yaml") {
		t.Error("10-courses.yaml should not be skipped for pt")
	}
}

func writeYAML(t *testing.T, dir, lang, filename, content string) {
	t.Helper()
	siteDDir := filepath.Join(dir, lang, "site.d")
	if err := os.MkdirAll(siteDDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(siteDDir, filename), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCompareAllLangs_clean(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "en", "50-ui.yaml", "nav:\n  home: Home\nfooter:\n  text: Footer\n")
	writeYAML(t, dir, "pt", "50-ui.yaml", "nav:\n  home: Início\nfooter:\n  text: Rodapé\n")

	enLD, _ := loadLangData(dir, "en", 3)
	ptLD, _ := loadLangData(dir, "pt", 3)

	report := compareAllLangs([]LangData{enLD, ptLD}, SkipConfig{Skip: map[string]map[string]struct{}{}}, Options{DataRoot: dir, MaxDepth: 3})
	if len(report.FilesOnlyInLang) != 0 {
		t.Errorf("expected no FileDiffs, got %v", report.FilesOnlyInLang)
	}
	if len(report.KeyMismatches) != 0 {
		t.Errorf("expected no KeyDiffs, got %v", report.KeyMismatches)
	}
}

func TestCompareAllLangs_keyMismatch(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "en", "50-ui.yaml", "nav:\n  home: Home\nextra_section:\n  key: val\n")
	writeYAML(t, dir, "pt", "50-ui.yaml", "nav:\n  home: Início\n")

	enLD, _ := loadLangData(dir, "en", 3)
	ptLD, _ := loadLangData(dir, "pt", 3)

	report := compareAllLangs([]LangData{enLD, ptLD}, SkipConfig{Skip: map[string]map[string]struct{}{}}, Options{DataRoot: dir, MaxDepth: 3})
	if len(report.KeyMismatches) == 0 {
		t.Error("expected KeyDiff for extra_section, got none")
	}
	kd := report.KeyMismatches[0]
	if kd.Filename != "50-ui.yaml" {
		t.Errorf("expected filename 50-ui.yaml, got %s", kd.Filename)
	}
}

func TestCompareAllLangs_fileMissing(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "en", "99-new.yaml", "key: value\n")
	// pt does not have 99-new.yaml

	enLD, _ := loadLangData(dir, "en", 3)
	ptLD, _ := loadLangData(dir, "pt", 3)

	report := compareAllLangs([]LangData{enLD, ptLD}, SkipConfig{Skip: map[string]map[string]struct{}{}}, Options{DataRoot: dir, MaxDepth: 3})
	if len(report.FilesOnlyInLang) == 0 {
		t.Error("expected FileDiff for 99-new.yaml, got none")
	}
	if len(report.KeyMismatches) != 0 {
		t.Errorf("expected no KeyDiff for a file missing in one lang, got %v", report.KeyMismatches)
	}
}

func TestCompareAllLangs_skipApplied(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "en", "10-courses.yaml", "courses:\n  - name: Course A\n")
	// pt does not have 10-courses.yaml

	sc := SkipConfig{Skip: map[string]map[string]struct{}{
		"en": {"10-courses.yaml": {}},
	}}

	enLD, _ := loadLangData(dir, "en", 3)
	ptLD, _ := loadLangData(dir, "pt", 3)

	report := compareAllLangs([]LangData{enLD, ptLD}, sc, Options{DataRoot: dir})
	if len(report.FilesOnlyInLang) != 0 {
		t.Errorf("expected no FileDiff (skipped), got %v", report.FilesOnlyInLang)
	}
	if len(report.KeyMismatches) != 0 {
		t.Errorf("expected no KeyDiff (skipped), got %v", report.KeyMismatches)
	}
}
