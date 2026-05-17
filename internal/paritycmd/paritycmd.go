package paritycmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const siteDirName = "site.d"

// Options holds parsed CLI flags.
type Options struct {
	DataRoot  string
	Langs     []string
	SkipFiles []string
	SkipKeys  []string
	SkipFile  string
	MaxDepth  int
}

// LangData holds the loaded data for one language directory.
type LangData struct {
	Lang   string
	Files  []string
	KeySet map[string]struct{}
}

// SkipConfig holds intentional parity exceptions read from .lang-parity-skip.yaml.
// The outer key is the language code; the inner key is the site.d basename.
type SkipConfig struct {
	Skip map[string]map[string]struct{}
}

// ParityReport is the result of comparing all language data sets.
type ParityReport struct {
	FilesOnlyInLang []FileDiff
	KeyMismatches   []KeyDiff
}

// FileDiff describes a file present in some languages but absent in others.
type FileDiff struct {
	Filename  string
	PresentIn []string
	AbsentIn  []string
}

// KeyDiff describes a top-level key set mismatch between two languages for the same file.
type KeyDiff struct {
	Filename  string
	LangA     string
	KeysOnlyA []string
	LangB     string
	KeysOnlyB []string
}

func Run(args []string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 3
	}

	langs := opts.Langs
	if len(langs) == 0 {
		langs, err = detectLangs(opts.DataRoot)
		if err != nil {
			return fmt.Errorf("detecting language directories: %w", err)
		}
	}
	if len(langs) == 0 {
		logger.Info("no language directories found; nothing to check", "data_root", opts.DataRoot)
		return nil
	}

	skipFile := opts.SkipFile
	if skipFile == "" {
		skipFile = filepath.Join(opts.DataRoot, "..", ".lang-parity-skip.yaml")
	}
	skipConfig, err := loadSkipConfig(skipFile)
	if err != nil {
		return fmt.Errorf("loading skip config: %w", err)
	}

	skipKeySet := toSet(opts.SkipKeys)
	var langDataList []LangData
	for _, lang := range langs {
		ld, err := loadLangData(opts.DataRoot, lang, opts.MaxDepth)
		if err != nil {
			return fmt.Errorf("loading lang %q: %w", lang, err)
		}
		for k := range skipKeySet {
			delete(ld.KeySet, k)
		}
		langDataList = append(langDataList, ld)
	}

	report := compareAllLangs(langDataList, skipConfig, opts)
	return reportAndCheck(report, logger)
}

func parseOptions(args []string) (Options, error) {
	fs := flag.NewFlagSet("check-lang-parity", flag.ContinueOnError)
	var opts Options
	var langs, skipFiles, skipKeys string

	fs.StringVar(&opts.DataRoot, "data-root", "", "path to the data/ directory (required)")
	fs.StringVar(&langs, "langs", "", "comma-separated language codes to check; auto-detects subdirs if empty")
	fs.StringVar(&skipFiles, "skip-files", "", "comma-separated site.d filenames to skip (basename match)")
	fs.StringVar(&skipKeys, "skip-keys", "", "comma-separated dotted key paths to exclude from key comparison")
	fs.StringVar(&opts.SkipFile, "skip-config", "", "path to .lang-parity-skip.yaml; default: <data-root>/../.lang-parity-skip.yaml")
	fs.IntVar(&opts.MaxDepth, "max-depth", 3, "key flattening depth (default 3)")

	if err := fs.Parse(args); err != nil {
		return Options{}, err
	}
	if opts.DataRoot == "" {
		return Options{}, fmt.Errorf("-data-root is required")
	}
	opts.Langs = splitCSV(langs)
	opts.SkipFiles = splitCSV(skipFiles)
	opts.SkipKeys = splitCSV(skipKeys)
	return opts, nil
}

// detectLangs returns all non-"shared" immediate subdirectories of dataRoot.
func detectLangs(dataRoot string) ([]string, error) {
	entries, err := os.ReadDir(dataRoot)
	if err != nil {
		return nil, err
	}
	var langs []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "shared" {
			langs = append(langs, e.Name())
		}
	}
	sort.Strings(langs)
	return langs, nil
}

// loadLangData reads <dataRoot>/<lang>/site.d/*.yaml, merges them additively,
// and returns the flattened dotted key paths up to maxDepth.
func loadLangData(dataRoot, lang string, maxDepth int) (LangData, error) {
	siteDDir := filepath.Join(dataRoot, lang, siteDirName)
	merged, files, err := loadSiteD(siteDDir)
	if err != nil {
		return LangData{}, err
	}
	keys := flattenKeys(merged, "", maxDepth)
	keySet := toSet(keys)
	return LangData{Lang: lang, Files: files, KeySet: keySet}, nil
}

// loadSiteD reads all *.yaml files from siteDDir and merges them into a single map.
func loadSiteD(siteDDir string) (map[string]any, []string, error) {
	entries, err := os.ReadDir(siteDDir)
	if os.IsNotExist(err) {
		return map[string]any{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	merged := map[string]any{}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !isYAMLFile(e.Name()) {
			continue
		}
		m, err := readYAMLFile(filepath.Join(siteDDir, e.Name()))
		if err != nil {
			return nil, nil, err
		}
		for k, v := range m {
			merged[k] = v
		}
		files = append(files, e.Name())
	}
	return merged, files, nil
}

// flattenKeys traverses m and returns sorted dotted key paths up to depth.
// Slices are treated as leaves at whatever depth they appear.
func flattenKeys(m map[string]any, prefix string, depth int) []string {
	if depth <= 0 || len(m) == 0 {
		return nil
	}
	var keys []string
	for k, v := range m {
		full := joinKey(prefix, k)
		keys = append(keys, full)
		if depth > 1 {
			if child, ok := v.(map[string]any); ok {
				keys = append(keys, flattenKeys(child, full, depth-1)...)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// loadSkipConfig reads .lang-parity-skip.yaml. Returns an empty SkipConfig if absent.
func loadSkipConfig(path string) (SkipConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return SkipConfig{Skip: map[string]map[string]struct{}{}}, nil
	}
	if err != nil {
		return SkipConfig{}, err
	}
	var raw struct {
		Skip []string `yaml:"skip"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return SkipConfig{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return parseSkipEntries(raw.Skip), nil
}

func parseSkipEntries(entries []string) SkipConfig {
	sc := SkipConfig{Skip: map[string]map[string]struct{}{}}
	for _, entry := range entries {
		// format: "<lang>/site.d/<filename>"
		parts := strings.SplitN(entry, "/", 3)
		if len(parts) != 3 || parts[1] != siteDirName {
			continue
		}
		lang, filename := parts[0], parts[2]
		if sc.Skip[lang] == nil {
			sc.Skip[lang] = map[string]struct{}{}
		}
		sc.Skip[lang][filename] = struct{}{}
	}
	return sc
}

func (sc SkipConfig) isFileSkipped(lang, filename string) bool {
	files, ok := sc.Skip[lang]
	if !ok {
		return false
	}
	_, skipped := files[filename]
	return skipped
}

// isFilenameSkipped returns true if filename appears in the skip list for any language.
// A skip entry for any lang means the whole file is an intentional exception.
func (sc SkipConfig) isFilenameSkipped(filename string) bool {
	for _, files := range sc.Skip {
		if _, ok := files[filename]; ok {
			return true
		}
	}
	return false
}

// compareAllLangs cross-compares all LangData entries and produces a ParityReport.
func compareAllLangs(langs []LangData, skipConfig SkipConfig, opts Options) ParityReport {
	fileUnion := buildFileUnion(langs)
	skipFileSet := toSet(opts.SkipFiles)
	var report ParityReport

	for filename := range fileUnion {
		if _, skip := skipFileSet[filename]; skip {
			continue
		}
		if skipConfig.isFilenameSkipped(filename) {
			continue
		}
		if fd, ok := checkPresence(langs, filename, skipConfig); ok {
			report.FilesOnlyInLang = append(report.FilesOnlyInLang, fd)
		}
		diffs := checkKeyParity(langs, filename, skipConfig, opts.DataRoot, opts.MaxDepth)
		report.KeyMismatches = append(report.KeyMismatches, diffs...)
	}

	sortReport(&report)
	return report
}

// checkPresence reports whether a file is missing in any language. Returns (FileDiff, true) if so.
func checkPresence(langs []LangData, filename string, skipConfig SkipConfig) (FileDiff, bool) {
	var present, absent []string
	for _, ld := range langs {
		if skipConfig.isFileSkipped(ld.Lang, filename) {
			continue
		}
		if langHasFile(ld, filename) {
			present = append(present, ld.Lang)
		} else {
			absent = append(absent, ld.Lang)
		}
	}
	sort.Strings(present)
	sort.Strings(absent)
	if len(absent) == 0 {
		return FileDiff{}, false
	}
	return FileDiff{Filename: filename, PresentIn: present, AbsentIn: absent}, true
}

// checkKeyParity compares key sets for a file across all langs that have it.
func checkKeyParity(langs []LangData, filename string, skipConfig SkipConfig, dataRoot string, maxDepth int) []KeyDiff {
	sets := buildPerFileSets(langs, filename, skipConfig, dataRoot, maxDepth)
	if len(sets) < 2 {
		return nil
	}
	return pairwiseDiff(sets, filename)
}

// buildPerFileSets loads per-file key sets for all langs that have the file and aren't skipped.
func buildPerFileSets(langs []LangData, filename string, skipConfig SkipConfig, dataRoot string, maxDepth int) map[string]map[string]struct{} {
	sets := map[string]map[string]struct{}{}
	for _, ld := range langs {
		if skipConfig.isFileSkipped(ld.Lang, filename) || !langHasFile(ld, filename) {
			continue
		}
		sets[ld.Lang] = perFileKeys(dataRoot, ld.Lang, filename, maxDepth)
	}
	return sets
}

// pairwiseDiff returns a KeyDiff for each pair of langs with differing key sets.
func pairwiseDiff(sets map[string]map[string]struct{}, filename string) []KeyDiff {
	langList := sortedMapKeys(sets)
	var diffs []KeyDiff
	for i := 0; i < len(langList)-1; i++ {
		for j := i + 1; j < len(langList); j++ {
			la, lb := langList[i], langList[j]
			onlyA := setDiff(sets[la], sets[lb])
			onlyB := setDiff(sets[lb], sets[la])
			if len(onlyA) > 0 || len(onlyB) > 0 {
				diffs = append(diffs, KeyDiff{
					Filename: filename, LangA: la, KeysOnlyA: onlyA,
					LangB: lb, KeysOnlyB: onlyB,
				})
			}
		}
	}
	return diffs
}

// perFileKeys loads top-level keys from a single site.d/<filename> for a given lang.
func perFileKeys(dataRoot, lang, filename string, maxDepth int) map[string]struct{} {
	path := filepath.Join(dataRoot, lang, siteDirName, filename)
	m, err := readYAMLFile(path)
	if err != nil {
		return map[string]struct{}{}
	}
	return toSet(flattenKeys(m, "", maxDepth))
}

func sortReport(report *ParityReport) {
	sort.Slice(report.FilesOnlyInLang, func(i, j int) bool {
		return report.FilesOnlyInLang[i].Filename < report.FilesOnlyInLang[j].Filename
	})
	sort.Slice(report.KeyMismatches, func(i, j int) bool {
		a, b := report.KeyMismatches[i], report.KeyMismatches[j]
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.LangA < b.LangA
	})
}

func reportAndCheck(report ParityReport, logger *slog.Logger) error {
	for _, fd := range report.FilesOnlyInLang {
		logger.Warn("file missing in some languages",
			"file", fd.Filename,
			"present_in", strings.Join(fd.PresentIn, ","),
			"absent_in", strings.Join(fd.AbsentIn, ","),
		)
	}
	for _, kd := range report.KeyMismatches {
		logger.Error("key mismatch between languages",
			"file", kd.Filename,
			"lang_a", kd.LangA, "keys_only_in_a", strings.Join(kd.KeysOnlyA, ", "),
			"lang_b", kd.LangB, "keys_only_in_b", strings.Join(kd.KeysOnlyB, ", "),
		)
	}
	if len(report.KeyMismatches) > 0 {
		return fmt.Errorf("%d key mismatch(es) found across language versions", len(report.KeyMismatches))
	}
	if len(report.FilesOnlyInLang) > 0 {
		logger.Info("parity check complete: file-presence warnings above; no key mismatches")
		return nil
	}
	logger.Info("parity check passed: all language versions are structurally in sync")
	return nil
}

// --- small helpers ---

func buildFileUnion(langs []LangData) map[string]struct{} {
	union := map[string]struct{}{}
	for _, ld := range langs {
		for _, f := range ld.Files {
			union[f] = struct{}{}
		}
	}
	return union
}

func langHasFile(ld LangData, filename string) bool {
	for _, f := range ld.Files {
		if f == filename {
			return true
		}
	}
	return false
}

func setDiff(a, b map[string]struct{}) []string {
	var diff []string
	for k := range a {
		if _, ok := b[k]; !ok {
			diff = append(diff, k)
		}
	}
	sort.Strings(diff)
	return diff
}

func toSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func joinKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func isYAMLFile(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

func readYAMLFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return m, nil
}
