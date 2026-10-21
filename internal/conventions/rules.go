package conventions

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Domain is the kind of tree a rule is valid against.
//
// The split exists because the structural rules must see a whole page. A
// template partial legitimately has no <main> and no <title>, so running a
// landmark rule over src/ would report a violation for every correctly-built
// site; and a px literal in generated dist/ CSS is the same literal already
// reported from its source file, so running a style rule over dist/ would
// double every finding.
type Domain int

const (
	// DomainSource is the hand-written tree (src/assets/css, src/assets/js).
	DomainSource Domain = iota
	// DomainOutput is a built site, passed with -dist.
	DomainOutput
)

func (d Domain) String() string {
	if d == DomainOutput {
		return "output"
	}
	return "source"
}

// Tree reads a file from the same scan root as the file being scanned, by a
// path relative to that root. It is how a rule follows a reference out of the
// file it was given — the CSS reset lives in whatever stylesheet the page loads
// first, which the page itself only names.
type Tree interface {
	ReadFile(relToRoot string) (string, bool)
}

// Rule is one checkable convention.
type Rule struct {
	Name string
	// Doc is shown in `validate-conventions -list` and in the error a site sees,
	// so the fix is readable without opening the compiler source.
	Doc string
	// Domain restricts the rule to source or built-output trees.
	Domain Domain
	// Scan returns findings for one file's contents. rel is website-root-relative
	// and slash-separated. Exactly one of Scan and ScanTree is set.
	Scan func(rel, contents string) []Finding
	// ScanTree is Scan for rules that must also read sibling files.
	ScanTree func(rel, contents string, tree Tree) []Finding
	// Selects reports whether this rule looks at a given file at all.
	Selects func(rel string) bool
}

// scan dispatches to whichever of the two scan functions this rule defines.
func (r Rule) scan(rel, contents string, tree Tree) []Finding {
	if r.ScanTree != nil {
		return r.ScanTree(rel, contents, tree)
	}
	return r.Scan(rel, contents)
}

// registry is the single source of truth for which rules exist. A
// conventions.yaml naming anything not in here is a config error rather than a
// silently ignored key — a typo'd rule name that quietly disables the rule is
// the worst possible failure for a gate.
var registry = map[string]Rule{
	ruleNameNoPxLiterals:           ruleNoPxLiterals,
	ruleNameNoCSSGrid:              ruleNoCSSGrid,
	ruleNameSingleMainLandmark:     ruleSingleMainLandmark,
	ruleNameChromeLandmarks:        ruleChromeLandmarks,
	ruleNameLabeledFormControls:    ruleLabeledFormControls,
	ruleNameFormAutocomplete:       ruleFormAutocomplete,
	ruleNameFormErrorWiring:        ruleFormErrorWiring,
	ruleNameLCPImageNotLazy:        ruleLCPImageNotLazy,
	ruleNameCSSResetPresent:        ruleCSSResetPresent,
	ruleNameFontFamilyDeclared:     ruleFontFamilyDeclared,
	ruleNameSEOMetaPresent:         ruleSEOMetaPresent,
	ruleNameSocialPreviewImage:     ruleSocialPreviewImage,
	ruleNameHiddenIsEffective:      ruleHiddenIsEffective,
	ruleNameNoSelfReferentialLinks: ruleNoSelfReferentialLinks,
	ruleNameNoMixedAuthStates:      ruleNoMixedAuthStates,
	ruleNameNoHTMLExtensionURLs:    ruleNoHTMLExtensionURLs,
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

// RuleDomain reports which tree a rule reads: source files, or a built site
// that the caller must pass with -dist.
func RuleDomain(name string) Domain { return registry[name].Domain }

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
	Name:   ruleNameNoPxLiterals,
	Domain: DomainSource,
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
	Name:   ruleNameNoCSSGrid,
	Domain: DomainSource,
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

// scanTarget is one directory to walk, and which rules may look at it.
type scanTarget struct {
	path   string
	domain Domain
}

// Run applies every enabled rule across the site's source tree and any built
// output directories given in outputRoots (the -dist flag).
func Run(websiteRoot string, cfg Config, outputRoots []string) (Result, error) {
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

	var targets []scanTarget
	for _, r := range ScanRoots {
		targets = append(targets, scanTarget{path: r, domain: DomainSource})
	}
	for _, r := range outputRoots {
		targets = append(targets, scanTarget{path: r, domain: DomainOutput})
	}

	for _, t := range targets {
		abs := t.path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(websiteRoot, t.path)
		}
		if _, err := os.Stat(abs); err != nil {
			continue // a site need not have every scan root
		}
		// relPrefix is how paths under this root are reported, so a finding
		// names the file the way the repo does.
		relPrefix := t.path
		if filepath.IsAbs(t.path) {
			if r, err := filepath.Rel(websiteRoot, t.path); err == nil && !strings.HasPrefix(r, "..") {
				relPrefix = r
			}
		}
		if err := scanRoot(abs, relPrefix, t.domain, enabled, cfg, &res, exceptionHits); err != nil {
			return Result{}, err
		}
	}

	// Property 2: an enabled rule that scanned nothing is a misconfiguration
	// reported as an error, not a silent pass. This is the failure mode that has
	// repeatedly made a green gate in this fleet mean "nothing ran".
	for _, name := range enabled {
		if res.ScannedFiles[name] == 0 {
			res.Errors = append(res.Errors, Finding{
				Rule:   name,
				File:   ConfigFileName,
				Detail: noFilesScannedDetail(name),
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
	res.Errors = sortAndDedupe(res.Errors)
	res.Warnings = sortAndDedupe(res.Warnings)
	return res, nil
}

// noFilesScannedDetail explains property 2's error in terms of the rule's own
// domain, because the two cases have completely different fixes: a source rule
// that scanned nothing means the site's CSS is somewhere unexpected, while an
// output rule that scanned nothing almost always means the caller forgot
// -dist — and a gate reporting "pass" because nobody built the site first is
// the exact failure this fleet keeps rediscovering.
func noFilesScannedDetail(name string) string {
	if registry[name].Domain == DomainOutput {
		return "rule is enabled but matched no files — it is reporting a pass while checking " +
			"nothing. It inspects built pages, so the run needs -dist pointing at a built site " +
			"(build first); check too that an exception is not swallowing the whole tree"
	}
	return "rule is enabled but matched no files — it is reporting a pass while checking " +
		"nothing. Check the site actually has " + strings.Join(ScanRoots, " / ") +
		", and that an exception is not swallowing the whole tree"
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

// sortAndDedupe orders findings by file then line, and collapses exact
// duplicates — the same file reachable through two scan roots (a -dist nested
// inside the source tree, or the same root passed twice).
//
// It deliberately does NOT collapse the same problem reported across many
// pages: a missing reset really does affect each of ffreis-forma's 56 pages,
// and the Result should say so. Making that readable is the command's job, not
// a thing to lose here (see groupForOutput in validateconventionscmd).
func sortAndDedupe(f []Finding) []Finding {
	sort.Slice(f, func(i, j int) bool {
		if f[i].File != f[j].File {
			return f[i].File < f[j].File
		}
		if f[i].Line != f[j].Line {
			return f[i].Line < f[j].Line
		}
		if f[i].Rule != f[j].Rule {
			return f[i].Rule < f[j].Rule
		}
		return f[i].Detail < f[j].Detail
	})
	out := f[:0]
	var prev Finding
	for i, cur := range f {
		if i > 0 && cur == prev {
			continue
		}
		out = append(out, cur)
		prev = cur
	}
	return out
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
func scanRoot(
	abs, relPrefix string, domain Domain,
	enabled []string, cfg Config, res *Result, exceptionHits map[string]bool,
) error {
	root, err := os.OpenRoot(abs)
	if err != nil {
		return fmt.Errorf("opening %s: %w", abs, err)
	}
	defer func() { _ = root.Close() }()
	tree := rootTree{root: root}

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
			if rule.Domain != domain || !rule.Selects(rel) {
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
			found := rule.scan(rel, string(contents), tree.relativeTo(p))
			if severityOf(rc) == SeverityWarn {
				res.Warnings = append(res.Warnings, found...)
			} else {
				res.Errors = append(res.Errors, found...)
			}
		}
		return nil
	})
}

// rootTree is a Tree over one scan root. Reads stay inside the root handle for
// the same reason the walk does (see scanRoot's scan-fix note).
type rootTree struct {
	root *os.Root
	// dir is the directory the scanned file sits in, so a rule can resolve a
	// document-relative href ("css/app.css") the way a browser would.
	dir string
}

func (t rootTree) relativeTo(filePath string) rootTree {
	t.dir = path.Dir(filePath)
	return t
}

// ReadFile resolves rel against the scanned file's directory, or against the
// root when rel is root-absolute ("/css/app.css", as emitted for a site
// deployed at its domain root).
func (t rootTree) ReadFile(rel string) (string, bool) {
	rel = strings.SplitN(rel, "?", 2)[0]
	rel = strings.SplitN(rel, "#", 2)[0]
	if rel == "" {
		return "", false
	}

	var target string
	if strings.HasPrefix(rel, "/") {
		target = path.Clean(strings.TrimPrefix(rel, "/"))
	} else {
		target = path.Clean(path.Join(t.dir, rel))
	}
	if !fs.ValidPath(target) {
		// Escapes the root (a "../.." href, or an absolute href under a deploy
		// base path we are not holding). Not readable, not an error: the rule
		// reports what it could not check.
		return "", false
	}
	b, err := fs.ReadFile(t.root.FS(), target)
	if err != nil {
		return "", false
	}
	return string(b), true
}
