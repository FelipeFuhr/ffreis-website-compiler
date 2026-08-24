package outputtestcmd

import (
	"fmt"
	"io/fs"
	"os"
	gopath "path"
	"strings"

	"ffreis-website-compiler/internal/linkcheck"
	"ffreis-website-compiler/internal/outputcheck"
)

// ruleSpec is the composable form of a manifest entry: a selector plus the
// checks that apply there. It carries no assertion logic itself — every check
// is resolved through the registry, so a website can use any registered type
// without this file changing.
type ruleSpec struct {
	Name          string           `yaml:"name"`
	Routes        []string         `yaml:"routes"`
	ExcludeRoutes []string         `yaml:"exclude_routes"`
	Langs         []string         `yaml:"langs"`
	Checks        []map[string]any `yaml:"checks"`
}

// buildSuite assembles presets, rules and corpus checks into one suite.
//
// Presets are composed first so a website's own rules are reported after the
// baseline it opted into, which keeps output readable.
func buildSuite(tests manifest, reg *outputcheck.Registry) (outputcheck.Suite, error) {
	var suite outputcheck.Suite

	for _, name := range tests.Include {
		rules, err := outputcheck.Preset(name, reg)
		if err != nil {
			return suite, err
		}
		suite.Rules = append(suite.Rules, rules...)
	}

	for i, spec := range tests.Rules {
		checks := make([]outputcheck.Check, 0, len(spec.Checks))
		for j, raw := range spec.Checks {
			typeName, config, err := splitCheckSpec(raw)
			if err != nil {
				return suite, fmt.Errorf("rules[%d].checks[%d]: %w", i, j, err)
			}
			check, err := reg.Build(typeName, config)
			if err != nil {
				return suite, fmt.Errorf("rules[%d].checks[%d]: %w", i, j, err)
			}
			checks = append(checks, check)
		}
		suite.Rules = append(suite.Rules, outputcheck.Rule{
			Name: ruleName(spec, i),
			Selector: outputcheck.Selector{
				Routes:        spec.Routes,
				ExcludeRoutes: spec.ExcludeRoutes,
				Langs:         spec.Langs,
			},
			Checks: checks,
		})
	}

	for i, raw := range tests.Corpus {
		typeName, config, err := splitCheckSpec(raw)
		if err != nil {
			return suite, fmt.Errorf("corpus[%d]: %w", i, err)
		}
		check, err := reg.BuildCorpus(typeName, config)
		if err != nil {
			return suite, fmt.Errorf("corpus[%d]: %w", i, err)
		}
		suite.Corpus = append(suite.Corpus, check)
	}

	return suite, nil
}

func ruleName(spec ruleSpec, index int) string {
	if strings.TrimSpace(spec.Name) != "" {
		return spec.Name
	}
	return fmt.Sprintf("rules[%d]", index)
}

// splitCheckSpec separates the dispatch key from the check's own configuration,
// so a check type defines its own fields without the manifest schema listing
// them.
func splitCheckSpec(raw map[string]any) (string, map[string]any, error) {
	typeRaw, ok := raw["type"]
	if !ok {
		return "", nil, fmt.Errorf("missing required field \"type\"")
	}
	typeName, ok := typeRaw.(string)
	if !ok || strings.TrimSpace(typeName) == "" {
		return "", nil, fmt.Errorf("field \"type\" must be a non-empty string")
	}
	config := make(map[string]any, len(raw))
	for key, value := range raw {
		if key != "type" {
			config[key] = value
		}
	}
	return typeName, config, nil
}

// loadTargets walks compiled output and returns one target per emitted page.
//
// scan-fix(gosec:G122): reads go through os.Root rather than a bare
// filepath.WalkDir + os.ReadFile pair. A root-scoped handle resolves every path
// inside the compiled output, so a symlink swapped in between the walk and the
// read cannot redirect a read outside the build result.
func loadTargets(outRoot, lang string) ([]outputcheck.Target, error) {
	root, err := os.OpenRoot(outRoot)
	if err != nil {
		return nil, fmt.Errorf("opening compiled output %s: %w", outRoot, err)
	}
	defer func() { _ = root.Close() }()

	var targets []outputcheck.Target
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "index.html" {
			return nil
		}
		content, readErr := fs.ReadFile(root.FS(), path)
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", path, readErr)
		}
		route := "/"
		if dir := gopath.Dir(path); dir != "." {
			route = "/" + dir + "/"
		}
		targets = append(targets, outputcheck.Target{
			Route: route,
			Lang:  lang,
			HTML:  string(content),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return targets, nil
}

// registry returns the engine's built-in checks plus the IO-backed ones this
// command can supply.
//
// This is where the extension point earns its keep: `link_integrity` needs the
// filesystem and the canonical implementation in internal/linkcheck, neither of
// which the pure engine should know about. Registering it here keeps the engine
// free of IO while still letting a manifest declare it like any other check.
func registry(outRoot string) *outputcheck.Registry {
	reg := outputcheck.DefaultRegistry()
	reg.RegisterCorpus("link_integrity", func(config map[string]any) (outputcheck.CorpusCheck, error) {
		basePath, _ := config["base_path"].(string)
		var siblings []string
		if raw, ok := config["sibling_base_paths"].([]any); ok {
			for _, item := range raw {
				if value, ok := item.(string); ok {
					siblings = append(siblings, value)
				}
			}
		}
		return linkIntegrity{outRoot: outRoot, basePath: basePath, siblings: siblings}, nil
	})
	return reg
}

// linkIntegrity adapts the canonical link validator to the check contract. It
// deliberately delegates rather than reimplementing: internal/linkcheck already
// handles clean URLs, non-navigational schemes and cross-deployment sibling
// prefixes, and a second implementation would drift from it.
type linkIntegrity struct {
	outRoot  string
	basePath string
	siblings []string
}

func (linkIntegrity) Name() string { return "link_integrity" }

func (l linkIntegrity) CheckAll(_ []outputcheck.Target) []outputcheck.Finding {
	result, err := linkcheck.Validate(l.outRoot, l.basePath, l.siblings)
	if err != nil {
		return []outputcheck.Finding{{
			Rule: "link_integrity", Check: "link_integrity",
			Message: fmt.Sprintf("link validation could not run: %v", err),
		}}
	}
	findings := make([]outputcheck.Finding, 0, len(result.BrokenLinks))
	for _, broken := range result.BrokenLinks {
		findings = append(findings, outputcheck.Finding{
			Rule:    "link_integrity",
			Check:   "link_integrity",
			Route:   broken.SourceFile,
			Message: fmt.Sprintf("links to %s, which was not compiled", broken.Href),
		})
	}
	return findings
}
