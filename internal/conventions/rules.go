package conventions

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Rule is one checkable convention.
type Rule struct {
	Name string
	// Doc is shown in `validate-conventions -list` and in the error a site sees,
	// so the fix is readable without opening the compiler source.
	Doc string
	// Scan returns findings for one file's contents. rel is website-root-relative
	// and slash-separated.
	Scan func(rel, contents string) []Finding
	// Selects reports whether this rule looks at a given file at all.
	Selects func(rel string) bool
}

// registry is the single source of truth for which rules exist. A
// conventions.yaml naming anything not in here is a config error rather than a
// silently ignored key — a typo'd rule name that quietly disables the rule is
// the worst possible failure for a gate.
var registry = map[string]Rule{
	ruleNameNoPxLiterals: ruleNoPxLiterals,
	ruleNameNoCSSGrid:    ruleNoCSSGrid,
}

// RuleNames returns every known rule name, sorted.
func RuleNames() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// RuleDoc returns a rule's documentation, or "" if unknown.
func RuleDoc(name string) string { return registry[name].Doc }

// pxLiteralRE finds a CSS px length, including the leading-dot form (".5px").
//
// The per-site tests this replaces use `\b\d+(?:\.\d+)?px\b`, which matches
// ".5px" but reports it as "5px" — sending whoever reads the failure to the
// wrong column — and which false-positives on `url(icon-20px.svg)` and
// `--step-2px`, where "px" is part of a name rather than a length.
var pxLiteralRE = regexp.MustCompile(`(?i)[0-9]*\.?[0-9]+px\b`)

// pxPrecededByIdentChar reports whether the byte before a match is part of an
// identifier, which means the "px" is inside a name (an asset called
// "icon-20px.svg", a custom property "--step-2px") rather than a length.
func pxPrecededByIdentChar(s string, start int) bool {
	if start == 0 {
		return false
	}
	c := s[start-1]
	return c == '_' || c == '-' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

const ruleNameNoPxLiterals = "no-px-literals"

var ruleNoPxLiterals = Rule{
	Name: ruleNameNoPxLiterals,
	Doc: "Lengths are rem/em/%/ch/vw/vh, never px, so the whole layout scales with the " +
		"user's root font size. Declare token and reset files as exceptions if they " +
		"legitimately need a px value (a hairline border, a 1px rule).",
	Selects: func(rel string) bool {
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".css", ".js", ".html":
			return true
		}
		return false
	},
	Scan: func(rel, contents string) []Finding {
		var findings []Finding
		for i, line := range strings.Split(contents, "\n") {
			for _, m := range pxLiteralRE.FindAllStringIndex(line, -1) {
				if pxPrecededByIdentChar(line, m[0]) {
					continue
				}
				findings = append(findings, Finding{
					Rule:   ruleNameNoPxLiterals,
					File:   rel,
					Line:   i + 1,
					Detail: fmt.Sprintf("px literal %q — use a rem/em/%% value, or declare an exception with a reason", line[m[0]:m[1]]),
				})
			}
		}
		return findings
	},
}

const ruleNameNoCSSGrid = "no-css-grid"

// gridPropertyRE matches a CSS Grid PROPERTY declaration, never the substring
// "grid".
//
// That distinction is the whole difficulty, and it is why this is worth having
// in one place: "grid" is a perfectly ordinary word in a class name
// (`.product-grid` is a Flexbox container in ffreis-forma) and in copy. A rule
// that grepped for "grid" would flag both, and the natural fix for a false
// positive is to rename a class or weaken the pattern -- which is how a rule
// stops meaning anything.
var gridPropertyRE = regexp.MustCompile(
	`(?i)(?:display\s*:\s*(?:inline-)?grid\b|grid-template[a-z-]*\s*:|grid-column[a-z-]*\s*:|grid-row[a-z-]*\s*:|grid-auto-[a-z-]*\s*:|grid-area\s*:|grid-gap\s*:|\bgrid\s*:)`)

var ruleNoCSSGrid = Rule{
	Name: ruleNameNoCSSGrid,
	Doc: "No CSS Grid property declarations: layout primitives stay Flexbox. This is " +
		"OPT-IN per site and not a fleet default -- current guidance is Grid for 2D " +
		"page structure and Flexbox for 1D components. Enable it only where a site " +
		"has a recorded reason, as ffreis-forma does (AGENTS.md / VISUAL_REGRESSION.md: " +
		"preserve the v7 wide-screen composition). The substring \"grid\" in a class " +
		"name or copy is never flagged.",
	Selects: func(rel string) bool {
		return strings.EqualFold(filepath.Ext(rel), ".css")
	},
	Scan: func(rel, contents string) []Finding {
		var findings []Finding
		for i, line := range strings.Split(contents, "\n") {
			for _, m := range gridPropertyRE.FindAllString(line, -1) {
				findings = append(findings, Finding{
					Rule:   ruleNameNoCSSGrid,
					File:   rel,
					Line:   i + 1,
					Detail: fmt.Sprintf("CSS Grid declaration %q — use Flexbox, or declare an exception with a reason", strings.TrimSpace(m)),
				})
			}
		}
		return findings
	},
}

// ScanRoots are the directories a conventions run walks, in order. They are
// fixed rather than configurable on purpose: a per-site "which directories do
// we check" knob is exactly how forma's own test ended up looking at one CSS
// file while 60+ others under css/pages/ went unchecked.
var ScanRoots = []string{
	filepath.Join("src", "assets", "css"),
	filepath.Join("src", "assets", "js"),
}

// Run applies every enabled rule across the site and any extra directories
// (e.g. a built dist/ tree) given in extraRoots.
func Run(websiteRoot string, cfg Config, extraRoots []string) (Result, error) {
	res := Result{ScannedFiles: map[string]int{}}

	enabled := make([]string, 0, len(cfg.Rules))
	for name, rc := range cfg.Rules {
		if rc.Enabled {
			enabled = append(enabled, name)
		}
	}
	if len(enabled) == 0 {
		return res, nil
	}
	sort.Strings(enabled)

	// exceptionHits tracks which declared exceptions actually matched something,
	// so stale ones can be reported (package doc, property 3).
	exceptionHits := map[string]bool{}

	roots := append(append([]string{}, ScanRoots...), extraRoots...)
	for _, root := range roots {
		abs := root
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(websiteRoot, root)
		}
		if _, err := os.Stat(abs); err != nil {
			continue // a site need not have every scan root
		}
		// relPrefix is how paths under this root are reported, so a finding
		// names the file the way the repo does.
		relPrefix := root
		if filepath.IsAbs(root) {
			if r, err := filepath.Rel(websiteRoot, root); err == nil && !strings.HasPrefix(r, "..") {
				relPrefix = r
			}
		}
		if err := scanRoot(abs, relPrefix, enabled, cfg, &res, exceptionHits); err != nil {
			return Result{}, err
		}
	}

	// Property 2: an enabled rule that scanned nothing is a misconfiguration
	// reported as an error, not a silent pass. This is the failure mode that has
	// repeatedly made a green gate in this fleet mean "nothing ran".
	for _, name := range enabled {
		if res.ScannedFiles[name] == 0 {
			res.Errors = append(res.Errors, Finding{
				Rule: name,
				File: ConfigFileName,
				Detail: "rule is enabled but matched no files — it is reporting a pass while checking " +
					"nothing. Check the site actually has " + strings.Join(ScanRoots, " / ") +
					", and that an exception is not swallowing the whole tree",
			})
		}
	}

	// Property 3: surface exemptions that no longer apply.
	for name, rc := range cfg.Rules {
		if !rc.Enabled {
			continue
		}
		for _, ex := range rc.Exceptions {
			if !exceptionHits[exceptionKey(name, ex)] {
				res.StaleExceptions = append(res.StaleExceptions, fmt.Sprintf(
					"%s: exception %q matched no file — delete it, or fix the path", name, ex.Path))
			}
		}
	}
	sort.Strings(res.StaleExceptions)
	sortFindings(res.Errors)
	sortFindings(res.Warnings)
	return res, nil
}

func exceptionFor(rc RuleConfig, rel string) (Exception, bool) {
	for _, ex := range rc.Exceptions {
		if matchPath(ex.Path, rel) {
			return ex, true
		}
	}
	return Exception{}, false
}

func exceptionKey(rule string, ex Exception) string { return rule + "\x00" + ex.Path }

func sortFindings(f []Finding) {
	sort.Slice(f, func(i, j int) bool {
		if f[i].File != f[j].File {
			return f[i].File < f[j].File
		}
		if f[i].Line != f[j].Line {
			return f[i].Line < f[j].Line
		}
		return f[i].Detail < f[j].Detail
	})
}

// scanRoot walks one directory and applies every enabled rule to the files in
// it.
//
// scan-fix(gosec:G122): reads go through os.Root rather than a bare
// filepath.WalkDir + os.ReadFile pair, matching internal/outputtestcmd's own
// fix. A root-scoped handle resolves every path inside the scanned tree, so a
// symlink swapped in between the walk and the read cannot redirect a read
// outside it. In-tree symlinks are still followed, which this fleet needs:
// ffreis-forma aliases shared per-page stylesheets as symlinks into
// css/pages/_shared/, and a scanner that skipped them would silently stop
// checking exactly the files the per-site test it replaces never checked either.
func scanRoot(abs, relPrefix string, enabled []string, cfg Config, res *Result, exceptionHits map[string]bool) error {
	root, err := os.OpenRoot(abs)
	if err != nil {
		return fmt.Errorf("opening %s: %w", abs, err)
	}
	defer func() { _ = root.Close() }()

	return fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel := filepath.ToSlash(filepath.Join(relPrefix, p))

		for _, name := range enabled {
			rule := registry[name]
			if !rule.Selects(rel) {
				continue
			}
			rc := cfg.Rules[name]
			if ex, ok := exceptionFor(rc, rel); ok {
				exceptionHits[exceptionKey(name, ex)] = true
				continue
			}
			contents, readErr := fs.ReadFile(root.FS(), p)
			if readErr != nil {
				return fmt.Errorf("reading %s: %w", rel, readErr)
			}
			res.ScannedFiles[name]++
			found := rule.Scan(rel, string(contents))
			if severityOf(rc) == SeverityWarn {
				res.Warnings = append(res.Warnings, found...)
			} else {
				res.Errors = append(res.Errors, found...)
			}
		}
		return nil
	})
}
