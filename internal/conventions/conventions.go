// Package conventions implements the fleet's shared style/structure rules as
// compiler gates, replacing the hand-written per-site test files that had been
// accumulating to enforce the same things one repo at a time.
//
// The problem it solves is not "we have no checks" — several sites had them.
// It is that every site had its OWN, so each one drifted: ffreis-forma and
// ffreis-editify both carried a `no-px-literals.test.js`, and forma's scanned
// only `src/assets/css/styles.css` while the site had since grown 60+ files
// under `css/pages/`, none of them checked. A rule that lives in the tool is
// one implementation, applied to the whole tree, versioned with the compiler.
//
// # Why a config file rather than always-on
//
// Rules are opt-in per site via conventions.yaml, because a fleet-wide gate
// that lands fatal breaks every consumer at once and the repo's own
// versioning policy asks for an opt-in or warning period. The pin IS the
// adoption event: a site adopts a rule when someone moves its compiler pin and
// writes the rule into its conventions.yaml, in a reviewable PR.
//
// # Three properties that keep this from becoming a suppression file
//
// A declared-exceptions mechanism usually decays into a dumping ground. Three
// rules prevent that, and each is enforced here rather than documented:
//
//  1. An exception without a `reason` is rejected. Not warned about —
//     rejected. The reason is the whole value of a visible exception over a
//     silently weakened regex.
//  2. A rule that is enabled but scanned nothing is an error. A gate that
//     matched no files reports "pass", which is the single most common way a
//     check in this fleet has turned out to be measuring nothing.
//  3. An exception that matches nothing is reported. Stale entries are how a
//     baseline becomes permanent; surfacing them makes burn-down possible
//     instead of aspirational.
package conventions

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFileName is the per-site rule declaration, read from the website root.
const ConfigFileName = "conventions.yaml"

// Severity decides whether a finding fails the command or only reports.
type Severity string

const (
	// SeverityError makes any finding fail the command.
	SeverityError Severity = "error"
	// SeverityWarn reports findings and exits successfully — the grace period a
	// newly-adopted rule gets before it is turned fatal.
	SeverityWarn Severity = "warn"
)

// Exception exempts paths from one rule. Reason is mandatory: see the package
// doc's property 1.
type Exception struct {
	// Path is a slash-separated glob relative to the website root, matched with
	// path.Match per segment plus a "**" prefix/suffix form for whole subtrees.
	Path string `yaml:"path"`
	// Reason is why this exemption is legitimate. An empty reason is a config
	// error, not a warning.
	Reason string `yaml:"reason"`
	// Expires is an optional YYYY-MM-DD note used only by reporting; it is
	// deliberately not enforced here, so a date cannot silently start failing
	// a build on a day nobody touched the repo.
	Expires string `yaml:"expires,omitempty"`
}

// RuleConfig is one rule's per-site settings.
type RuleConfig struct {
	Enabled    bool        `yaml:"enabled"`
	Severity   Severity    `yaml:"severity,omitempty"`
	Exceptions []Exception `yaml:"exceptions,omitempty"`
}

// Config is the parsed conventions.yaml.
type Config struct {
	Rules map[string]RuleConfig `yaml:"rules"`
}

// Finding is one rule violation at one place.
type Finding struct {
	Rule   string
	File   string // slash-separated, relative to the website root
	Line   int    // 1-indexed; 0 when the finding is about a file as a whole
	Detail string
}

func (f Finding) String() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d: [%s] %s", f.File, f.Line, f.Rule, f.Detail)
	}
	return fmt.Sprintf("%s: [%s] %s", f.File, f.Rule, f.Detail)
}

// Result is everything one run learned, separated by what the caller must do
// about it.
type Result struct {
	// Errors are findings from rules configured at severity error.
	Errors []Finding
	// Warnings are findings from rules configured at severity warn.
	Warnings []Finding
	// StaleExceptions are declared exemptions that matched no scanned file.
	StaleExceptions []string
	// ScannedFiles counts what each enabled rule actually looked at, so a rule
	// that scanned nothing can be caught (package doc, property 2).
	ScannedFiles map[string]int
}

// LoadConfig reads conventions.yaml from websiteRoot. A missing file is not an
// error — it means the site has adopted no rules yet, and every rule is off.
// Returns the path it read so callers can report provenance rather than
// leaving "which config applied?" ambiguous.
func LoadConfig(websiteRoot string) (Config, string, error) {
	p := filepath.Join(websiteRoot, ConfigFileName)
	raw, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{Rules: map[string]RuleConfig{}}, "", nil
	}
	if err != nil {
		return Config{}, "", fmt.Errorf("reading %s: %w", ConfigFileName, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, p, fmt.Errorf("parsing %s: %w", p, err)
	}
	if cfg.Rules == nil {
		cfg.Rules = map[string]RuleConfig{}
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, p, fmt.Errorf("%s: %w", p, err)
	}
	return cfg, p, nil
}

// validateConfig rejects a config that cannot mean what it says.
func validateConfig(cfg Config) error {
	var problems []string
	for name, rc := range cfg.Rules {
		if _, ok := registry[name]; !ok {
			problems = append(problems, fmt.Sprintf(
				"unknown rule %q (known rules: %s)", name, strings.Join(RuleNames(), ", ")))
			continue
		}
		switch rc.Severity {
		case "", SeverityError, SeverityWarn:
		default:
			problems = append(problems, fmt.Sprintf(
				"rule %q: severity must be %q or %q, got %q", name, SeverityError, SeverityWarn, rc.Severity))
		}
		for i, ex := range rc.Exceptions {
			if strings.TrimSpace(ex.Path) == "" {
				problems = append(problems, fmt.Sprintf("rule %q exception %d: path is empty", name, i))
			}
			// Property 1: a reason is mandatory. An exemption whose
			// justification nobody wrote down is indistinguishable from one
			// added to make a gate go quiet.
			if strings.TrimSpace(ex.Reason) == "" {
				problems = append(problems, fmt.Sprintf(
					"rule %q exception %q: reason is required — an exception without a recorded reason is "+
						"just a silently weakened rule", name, ex.Path))
			} else if isPlaceholderReason(ex.Reason) {
				// -write-baseline deliberately writes a reason that does not
				// pass this check, so a generated baseline cannot be merged
				// without someone reading each line. A baseline that is
				// pleasant to ignore is how an exceptions file becomes
				// permanent.
				problems = append(problems, fmt.Sprintf(
					"rule %q exception %q: reason is still the placeholder written by -write-baseline — "+
						"replace it with the actual reason, or fix the file and delete the exception",
					name, ex.Path))
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return errors.New(strings.Join(problems, "; "))
}

// placeholderReasonPrefix is what -write-baseline writes. Kept here, next to
// the check that rejects it, so the generator and the validator cannot drift
// into a state where the generated baseline quietly passes.
const placeholderReasonPrefix = "TODO"

// isPlaceholderReason reports whether a reason is still the generated stub.
func isPlaceholderReason(reason string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(reason)), placeholderReasonPrefix)
}

// severityOf defaults an unset severity to error. A rule someone bothered to
// enable should bite; the warn period is something you ask for explicitly.
func severityOf(rc RuleConfig) Severity {
	if rc.Severity == SeverityWarn {
		return SeverityWarn
	}
	return SeverityError
}

// matchPath reports whether rel (slash-separated, website-root-relative)
// matches an exception glob. Supported forms, deliberately few:
//
//	src/assets/css/tokens.css   exact
//	src/assets/css/*.css        path.Match, one segment
//	src/assets/css/**           whole subtree
//	**/vendor/**                subtree anywhere
func matchPath(pattern, rel string) bool {
	pattern = strings.TrimPrefix(pattern, "./")
	rel = strings.TrimPrefix(rel, "./")

	if strings.Contains(pattern, "**") {
		parts := strings.SplitN(pattern, "**", 2)
		prefix := strings.TrimSuffix(parts[0], "/")
		suffix := strings.TrimPrefix(parts[1], "/")
		if prefix != "" && !strings.HasPrefix(rel, prefix+"/") && rel != prefix {
			return false
		}
		if suffix == "" {
			return true
		}
		// Match the suffix against any trailing portion of the path so
		// "**/vendor/**" catches a vendor dir at any depth.
		segs := strings.Split(rel, "/")
		for i := range segs {
			if ok, _ := path.Match(suffix, strings.Join(segs[i:], "/")); ok {
				return true
			}
			if strings.HasPrefix(strings.Join(segs[i:], "/"), strings.TrimSuffix(suffix, "/**")+"/") {
				return true
			}
		}
		return false
	}

	ok, err := path.Match(pattern, rel)
	return err == nil && ok
}

// SaveConfig writes cfg back to websiteRoot/conventions.yaml. Used by
// -write-baseline; the file is small and hand-edited afterwards, so a plain
// marshal is right here rather than a comment-preserving round-trip.
func SaveConfig(websiteRoot string, cfg Config) error {
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", ConfigFileName, err)
	}
	header := "# Fleet style/structure conventions enforced by the website-compiler's\n" +
		"# `validate-conventions` command. These rules used to live as hand-written test\n" +
		"# files in each site; they are now one implementation in the tool, and this file\n" +
		"# records only what THIS site deliberately does differently.\n" +
		"#\n" +
		"# Every exception needs a real `reason` — the loader rejects an empty one, and a\n" +
		"# `TODO` left by -write-baseline is not a reason. Run\n" +
		"# `website-compiler validate-conventions -list` for what each rule means.\n"
	p := filepath.Join(websiteRoot, ConfigFileName)
	// 0o644, not 0o600: conventions.yaml is a committed, human-read config file
	// that every contributor and CI job must be able to open. Same reasoning as
	// internal/posts and internal/testutil, which carry the same exemption.
	if err := os.WriteFile(p, []byte(header+string(out)), 0o644); err != nil { //nolint:gosec
		return fmt.Errorf("writing %s: %w", p, err)
	}
	return nil
}

// PlaceholderReason is the stub -write-baseline writes into each generated
// exception. It is intentionally rejected by LoadConfig: generating a baseline
// must not produce a config that can be merged unread.
func PlaceholderReason() string {
	return placeholderReasonPrefix + ": say why this file may keep its violations, or fix them and delete this line"
}
