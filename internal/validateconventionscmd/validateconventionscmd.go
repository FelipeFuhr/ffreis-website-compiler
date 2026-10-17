// Package validateconventionscmd implements the "validate-conventions"
// command: the fleet's shared style/structure rules, run against a site's own
// source tree, replacing the hand-written per-site test files that enforced the
// same conventions one repo at a time.
//
// See internal/conventions for the rule engine and why exceptions must carry a
// reason.
package validateconventionscmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"ffreis-website-compiler/internal/conventions"
)

type options struct {
	websiteRoot   string
	distDir       string
	writeBaseline bool
	list          bool
}

func parseOptions(args []string) (options, error) {
	var opts options
	fs := flag.NewFlagSet("validate-conventions", flag.ContinueOnError)
	fs.StringVar(&opts.websiteRoot, "website-root", ".", "site root containing "+conventions.ConfigFileName)
	fs.StringVar(&opts.distDir, "dist", "", "optional compiled output dir to scan as well (e.g. dist)")
	fs.BoolVar(&opts.writeBaseline, "write-baseline", false,
		"instead of failing, rewrite "+conventions.ConfigFileName+" with an exception per violating file")
	fs.BoolVar(&opts.list, "list", false, "list the known rules and exit")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	return opts, nil
}

func Run(args []string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	if opts.list {
		for _, name := range conventions.RuleNames() {
			where := "reads the site's source tree"
			if conventions.RuleDomain(name) == conventions.DomainOutput {
				where = "reads built pages — the run needs -dist"
			}
			fmt.Printf("%s  (%s)\n  %s\n", name, where, conventions.RuleDoc(name))
		}
		return nil
	}

	cfg, cfgPath, err := conventions.LoadConfig(opts.websiteRoot)
	if err != nil {
		return err
	}
	if cfgPath == "" {
		// Not an error: a site with no conventions.yaml has adopted no rules.
		// Said out loud rather than exiting 0 silently, because "the gate ran
		// and found nothing" and "the gate had nothing to run" look identical
		// in a CI log otherwise.
		logger.Info("no conventions config; nothing to enforce",
			"expected", conventions.ConfigFileName, "website_root", opts.websiteRoot)
		return nil
	}

	var extra []string
	if opts.distDir != "" {
		extra = append(extra, opts.distDir)
	}

	res, err := conventions.Run(opts.websiteRoot, cfg, extra)
	if err != nil {
		return err
	}

	if opts.writeBaseline {
		return writeBaseline(opts.websiteRoot, cfg, res, logger)
	}

	for _, s := range res.StaleExceptions {
		logger.Warn("stale conventions exception", "detail", s)
	}
	for _, g := range groupForOutput(res.Warnings) {
		logger.Warn("conventions finding (warn)", "finding", g.String())
	}

	scanned := 0
	for _, n := range res.ScannedFiles {
		scanned += n
	}
	if len(res.Errors) == 0 {
		logger.Info("conventions check passed", "config", cfgPath, "files_scanned", scanned,
			"warnings", len(res.Warnings))
		return nil
	}

	// Report every violation, not the first: a site adopting a rule needs the
	// whole list to decide between fixing and declaring exceptions, and one
	// error at a time turns adoption into a guessing loop.
	var b strings.Builder
	fmt.Fprintf(&b, "%d conventions violation(s) across %d scanned file(s):\n", len(res.Errors), scanned)
	for _, g := range groupForOutput(res.Errors) {
		fmt.Fprintf(&b, "  %s\n", g.String())
	}
	b.WriteString("\nFix them, or declare an exception WITH A REASON in " + conventions.ConfigFileName +
		".\nRun with -write-baseline to generate one exception per violating file as a starting point.")
	return fmt.Errorf("%s", b.String())
}

// maxPlacesShown caps how many files one grouped finding lists before
// summarising the rest.
const maxPlacesShown = 6

// findingGroup is one message and every place it was found.
type findingGroup struct {
	rule   string
	detail string
	places []string
}

func (g findingGroup) String() string {
	shown := g.places
	suffix := ""
	if len(shown) > maxPlacesShown {
		shown = shown[:maxPlacesShown]
		suffix = fmt.Sprintf(" … and %d more", len(g.places)-maxPlacesShown)
	}
	return fmt.Sprintf("[%s] %s\n      %s%s", g.rule, g.detail, strings.Join(shown, ", "), suffix)
}

// groupForOutput collapses findings that share a rule and a message into one
// block listing where they were found.
//
// Several of these rules describe a property of the whole site rather than of
// one page: a missing reset, or a missing og:image, reports once per page, and
// ffreis-forma has 56 of them. Fifty-six identical lines say nothing the first
// one did not and bury the findings that ARE page-specific. The Result keeps
// every finding — this only decides how they are printed.
func groupForOutput(findings []conventions.Finding) []findingGroup {
	var groups []findingGroup
	index := map[string]int{}
	for _, f := range findings {
		key := f.Rule + "\x00" + f.Detail
		place := f.File
		if f.Line > 0 {
			place = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		if i, ok := index[key]; ok {
			groups[i].places = append(groups[i].places, place)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, findingGroup{rule: f.Rule, detail: f.Detail, places: []string{place}})
	}
	return groups
}

// writeBaseline rewrites conventions.yaml adding one exception per violating
// file, with a placeholder reason.
//
// The placeholder is deliberately NOT a valid reason -- conventions.LoadConfig
// rejects it by name -- so a generated baseline cannot be merged without
// someone reading each line and saying why. A baseline that is pleasant to
// ignore is how an exceptions file becomes permanent.
func writeBaseline(websiteRoot string, cfg conventions.Config, res conventions.Result, logger *slog.Logger) error {
	perRule := map[string]map[string]bool{}
	for _, f := range append(append([]conventions.Finding{}, res.Errors...), res.Warnings...) {
		if perRule[f.Rule] == nil {
			perRule[f.Rule] = map[string]bool{}
		}
		perRule[f.Rule][f.File] = true
	}
	if len(perRule) == 0 {
		logger.Info("no violations; baseline unchanged")
		return nil
	}

	added := 0
	for rule, files := range perRule {
		rc := cfg.Rules[rule]
		paths := make([]string, 0, len(files))
		for p := range files {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			rc.Exceptions = append(rc.Exceptions, conventions.Exception{
				Path:   p,
				Reason: conventions.PlaceholderReason(),
			})
			added++
		}
		cfg.Rules[rule] = rc
	}

	if err := conventions.SaveConfig(websiteRoot, cfg); err != nil {
		return err
	}
	logger.Info("baseline written", "exceptions_added", added,
		"next", "replace every placeholder reason — the config loader rejects the placeholder by name, "+
			"so the gate stays red until each line has a real reason or the file is fixed")
	return nil
}
