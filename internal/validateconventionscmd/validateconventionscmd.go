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
			fmt.Printf("%s\n  %s\n", name, conventions.RuleDoc(name))
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
	for _, f := range res.Warnings {
		logger.Warn("conventions finding (warn)", "finding", f.String())
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
	for _, f := range res.Errors {
		fmt.Fprintf(&b, "  %s\n", f.String())
	}
	b.WriteString("\nFix them, or declare an exception WITH A REASON in " + conventions.ConfigFileName +
		".\nRun with -write-baseline to generate one exception per violating file as a starting point.")
	return fmt.Errorf("%s", b.String())
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
