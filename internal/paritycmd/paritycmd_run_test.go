package paritycmd

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── fixtures ────────────────────────────────────────────────────────────────

// newDataRoot builds a data/ tree from {lang: {filename: yaml}} and returns its
// path. Mirrors the layout the real content repos use: data/<lang>/site.d/*.yaml.
func newDataRoot(t *testing.T, langs map[string]map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "data")
	for lang, files := range langs {
		siteD := filepath.Join(root, lang, "site.d")
		if err := os.MkdirAll(siteD, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", siteD, err)
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(siteD, name), []byte(body), 0o600); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
	}
	return root
}

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
}

// capturing returns a logger and the buffer it writes to, so a test can assert
// on what an operator would actually see.
func capturing() (*slog.Logger, *bytes.Buffer) {
	buf := bytes.NewBuffer(nil)
	return slog.New(slog.NewTextHandler(buf, nil)), buf
}

// ── options ─────────────────────────────────────────────────────────────────

func TestParseOptions_RequiresADataRoot(t *testing.T) {
	_, err := parseOptions(nil)
	if err == nil || !strings.Contains(err.Error(), "-data-root is required") {
		t.Fatalf("err = %v, want a required-flag error", err)
	}
}

func TestParseOptions_ParsesCSVFlagsAndDefaults(t *testing.T) {
	opts, err := parseOptions([]string{
		"-data-root", "/d",
		"-langs", "en, pt ,,jp",
		"-skip-files", "90-local.yaml",
		"-skip-keys", "ui.debug , ui.beta",
	})
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	if opts.MaxDepth != 3 {
		t.Errorf("MaxDepth = %d, want the default 3", opts.MaxDepth)
	}
	if strings.Join(opts.Langs, "|") != "en|pt|jp" {
		t.Errorf("Langs = %v, want empty entries dropped and values trimmed", opts.Langs)
	}
	if strings.Join(opts.SkipKeys, "|") != "ui.debug|ui.beta" {
		t.Errorf("SkipKeys = %v, want trimmed values", opts.SkipKeys)
	}
	if strings.Join(opts.SkipFiles, "|") != "90-local.yaml" {
		t.Errorf("SkipFiles = %v", opts.SkipFiles)
	}
}

func TestParseOptions_RejectsAnUnknownFlag(t *testing.T) {
	if _, err := parseOptions([]string{"-data-root", "/d", "-nope"}); err == nil {
		t.Fatal("an unknown flag must be an error")
	}
}

func TestSplitCSV(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []string
	}{
		{"empty yields nil", "", nil},
		{"single value", "en", []string{"en"}},
		{"values are trimmed", " en , pt ", []string{"en", "pt"}},
		{"empty entries are dropped", "en,,pt,", []string{"en", "pt"}},
		{"only separators yields nil", ",,,", nil},
		{"only whitespace yields nil", " , ", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := splitCSV(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("splitCSV(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("splitCSV(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// ── language detection ──────────────────────────────────────────────────────

func TestDetectLangs_ReturnsSortedLanguageDirsExcludingShared(t *testing.T) {
	root := newDataRoot(t, map[string]map[string]string{
		"pt": {"10-a.yaml": "a: 1\n"},
		"en": {"10-a.yaml": "a: 1\n"},
		"jp": {"10-a.yaml": "a: 1\n"},
		// "shared" holds cross-language data and is not a language of its own.
		"shared": {"10-a.yaml": "a: 1\n"},
	})
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o600); err != nil {
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

func TestDetectLangs_MissingRootIsAnError(t *testing.T) {
	if _, err := detectLangs(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a missing data root must be an error")
	}
}

// ── Run: the public surface ─────────────────────────────────────────────────

func TestRun_PassesWhenLanguagesAreInSync(t *testing.T) {
	root := newDataRoot(t, map[string]map[string]string{
		"en": {"10-ui.yaml": "ui:\n  title: Hello\n  cta: Go\n"},
		"pt": {"10-ui.yaml": "ui:\n  title: Ola\n  cta: Ir\n"},
	})

	logger, buf := capturing()
	if err := Run([]string{"-data-root", root}, logger); err != nil {
		t.Fatalf("structurally identical languages must pass: %v", err)
	}
	if !strings.Contains(buf.String(), "parity check passed") {
		t.Errorf("expected a pass message; log was:\n%s", buf.String())
	}
}

func TestRun_FailsOnAKeyMismatch(t *testing.T) {
	root := newDataRoot(t, map[string]map[string]string{
		"en": {"10-ui.yaml": "ui:\n  title: Hello\n  subtitle: Extra\n"},
		"pt": {"10-ui.yaml": "ui:\n  title: Ola\n"},
	})

	logger, buf := capturing()
	err := Run([]string{"-data-root", root}, logger)
	if err == nil {
		t.Fatal("a key present in one language but not another must fail")
	}
	if !strings.Contains(err.Error(), "key mismatch") {
		t.Errorf("err = %v, want it to name the mismatch", err)
	}
	if !strings.Contains(buf.String(), "subtitle") {
		t.Errorf("the offending key should be logged; log was:\n%s", buf.String())
	}
}

// A file missing from one language is a warning, not a failure: languages
// legitimately roll out at different times.
func TestRun_WarnsButPassesWhenAFileIsMissingFromOneLanguage(t *testing.T) {
	root := newDataRoot(t, map[string]map[string]string{
		"en": {"10-ui.yaml": "ui:\n  title: Hello\n", "20-extra.yaml": "extra:\n  a: 1\n"},
		"pt": {"10-ui.yaml": "ui:\n  title: Ola\n"},
	})

	logger, buf := capturing()
	if err := Run([]string{"-data-root", root}, logger); err != nil {
		t.Fatalf("a missing file must warn, not fail: %v", err)
	}
	log := buf.String()
	if !strings.Contains(log, "file missing in some languages") || !strings.Contains(log, "20-extra.yaml") {
		t.Errorf("expected a warning naming the missing file; log was:\n%s", log)
	}
	if !strings.Contains(log, "file-presence warnings above") {
		t.Errorf("expected the summary to mention the warnings; log was:\n%s", log)
	}
}

func TestRun_SkipKeysExcludesATopLevelKeyFromComparison(t *testing.T) {
	root := newDataRoot(t, map[string]map[string]string{
		"en": {"10-ui.yaml": "ui:\n  title: Hello\ndebug_flag: true\n"},
		"pt": {"10-ui.yaml": "ui:\n  title: Ola\n"},
	})

	if err := Run([]string{"-data-root", root}, discard()); err == nil {
		t.Fatal("control: the mismatch should fail without -skip-keys")
	}
	if err := Run([]string{"-data-root", root, "-skip-keys", "debug_flag"}, discard()); err != nil {
		t.Fatalf("-skip-keys should exclude the key: %v", err)
	}
}

// A sharp edge worth pinning: flattening emits BOTH the parent and each dotted
// leaf ("debug" and "debug.on"), so skipping the parent alone leaves the leaves
// still compared. Every key the skip is meant to cover has to be listed.
func TestRun_SkipKeysOnANestedBranchNeedsEveryDottedKey(t *testing.T) {
	root := newDataRoot(t, map[string]map[string]string{
		"en": {"10-ui.yaml": "ui:\n  title: Hello\ndebug:\n  on: true\n"},
		"pt": {"10-ui.yaml": "ui:\n  title: Ola\n"},
	})

	if err := Run([]string{"-data-root", root, "-skip-keys", "debug"}, discard()); err == nil {
		t.Fatal("skipping only the parent should still fail on debug.on — if this now passes, the flattening changed")
	}
	if err := Run([]string{"-data-root", root, "-skip-keys", "debug,debug.on"}, discard()); err != nil {
		t.Fatalf("listing the parent and its leaf should pass: %v", err)
	}
}

func TestRun_ExplicitLangsOverrideDetection(t *testing.T) {
	root := newDataRoot(t, map[string]map[string]string{
		"en": {"10-ui.yaml": "ui:\n  title: Hello\n"},
		"pt": {"10-ui.yaml": "ui:\n  title: Ola\n"},
		"jp": {"10-ui.yaml": "ui:\n  title: Konnichiwa\n  extra: x\n"},
	})

	if err := Run([]string{"-data-root", root}, discard()); err == nil {
		t.Fatal("control: jp's extra key should fail when every language is checked")
	}
	if err := Run([]string{"-data-root", root, "-langs", "en,pt"}, discard()); err != nil {
		t.Fatalf("restricting to en,pt should pass: %v", err)
	}
}

func TestRun_NoLanguageDirectoriesIsANoOp(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	logger, buf := capturing()
	if err := Run([]string{"-data-root", root}, logger); err != nil {
		t.Fatalf("an empty data root must be a no-op, not an error: %v", err)
	}
	if !strings.Contains(buf.String(), "nothing to check") {
		t.Errorf("expected a 'nothing to check' message; log was:\n%s", buf.String())
	}
}

func TestRun_ReportsAMissingDataRoot(t *testing.T) {
	err := Run([]string{"-data-root", filepath.Join(t.TempDir(), "absent")}, discard())
	if err == nil || !strings.Contains(err.Error(), "detecting language directories") {
		t.Fatalf("err = %v, want a contextual detection error", err)
	}
}

func TestRun_ReportsAMalformedYAMLFile(t *testing.T) {
	root := newDataRoot(t, map[string]map[string]string{
		"en": {"10-ui.yaml": "ui: [unclosed\n"},
		"pt": {"10-ui.yaml": "ui:\n  title: Ola\n"},
	})

	err := Run([]string{"-data-root", root}, discard())
	if err == nil || !strings.Contains(err.Error(), "loading lang") {
		t.Fatalf("err = %v, want the failing language named", err)
	}
}

func TestRun_ReportsAMalformedSkipConfig(t *testing.T) {
	root := newDataRoot(t, map[string]map[string]string{
		"en": {"10-ui.yaml": "ui:\n  title: Hello\n"},
	})
	skip := filepath.Join(t.TempDir(), "skip.yaml")
	if err := os.WriteFile(skip, []byte("skip: [unclosed\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := Run([]string{"-data-root", root, "-skip-config", skip}, discard())
	if err == nil || !strings.Contains(err.Error(), "loading skip config") {
		t.Fatalf("err = %v, want the skip-config failure surfaced", err)
	}
}

func TestRun_NonPositiveMaxDepthFallsBackToTheDefault(t *testing.T) {
	root := newDataRoot(t, map[string]map[string]string{
		"en": {"10-ui.yaml": "ui:\n  nav:\n    title: Hello\n"},
		"pt": {"10-ui.yaml": "ui:\n  nav:\n    title: Ola\n"},
	})

	if err := Run([]string{"-data-root", root, "-max-depth", "0"}, discard()); err != nil {
		t.Fatalf("max-depth 0 should fall back to the default, not flatten nothing: %v", err)
	}
}

func TestRun_ToleratesANilLogger(t *testing.T) {
	root := newDataRoot(t, map[string]map[string]string{
		"en": {"10-ui.yaml": "ui:\n  title: Hello\n"},
		"pt": {"10-ui.yaml": "ui:\n  title: Ola\n"},
	})
	if err := Run([]string{"-data-root", root}, nil); err != nil {
		t.Fatalf("Run with a nil logger: %v", err)
	}
}

// ── reporting ───────────────────────────────────────────────────────────────

func TestSortReport_OrdersFilesAndMismatchesDeterministically(t *testing.T) {
	report := &ParityReport{
		FilesOnlyInLang: []FileDiff{
			{Filename: "30-c.yaml"}, {Filename: "10-a.yaml"}, {Filename: "20-b.yaml"},
		},
		KeyMismatches: []KeyDiff{
			{Filename: "20-b.yaml", LangA: "en"},
			{Filename: "10-a.yaml", LangA: "pt"},
			{Filename: "10-a.yaml", LangA: "en"},
		},
	}

	sortReport(report)

	var files []string
	for _, f := range report.FilesOnlyInLang {
		files = append(files, f.Filename)
	}
	if strings.Join(files, "|") != "10-a.yaml|20-b.yaml|30-c.yaml" {
		t.Errorf("files = %v, want name order", files)
	}

	var mismatches []string
	for _, k := range report.KeyMismatches {
		mismatches = append(mismatches, k.Filename+":"+k.LangA)
	}
	want := "10-a.yaml:en|10-a.yaml:pt|20-b.yaml:en"
	if strings.Join(mismatches, "|") != want {
		t.Errorf("mismatches = %v, want %q — by filename then language", mismatches, want)
	}
}

func TestReportAndCheck_PassesOnAnEmptyReport(t *testing.T) {
	logger, buf := capturing()
	if err := reportAndCheck(ParityReport{}, logger); err != nil {
		t.Fatalf("an empty report must pass: %v", err)
	}
	if !strings.Contains(buf.String(), "structurally in sync") {
		t.Errorf("expected the success message; log was:\n%s", buf.String())
	}
}

func TestReportAndCheck_KeyMismatchesFailEvenAlongsideFileWarnings(t *testing.T) {
	report := ParityReport{
		FilesOnlyInLang: []FileDiff{{Filename: "20-b.yaml", PresentIn: []string{"en"}, AbsentIn: []string{"pt"}}},
		KeyMismatches:   []KeyDiff{{Filename: "10-a.yaml", LangA: "en", KeysOnlyA: []string{"x"}, LangB: "pt"}},
	}

	err := reportAndCheck(report, discard())
	if err == nil || !strings.Contains(err.Error(), "1 key mismatch") {
		t.Fatalf("err = %v, want the mismatch count reported", err)
	}
}
