// Package outputcheck is the composable assertion engine behind compiled-output
// tests.
//
// The design separates three concerns that the first implementation combined
// into one struct:
//
//   - a [Check] asserts one property of one page and knows nothing about any
//     other check, the manifest format, or the filesystem;
//   - a [Selector] decides *where* a check applies and knows nothing about what
//     it asserts;
//   - a [Registry] maps a manifest's `type:` onto a check constructor, so a new
//     assertion is registered rather than wired into the runner.
//
// The consequence is that adding an assertion type touches exactly one new
// file, and a website composes selectors and checks freely without the compiler
// learning any product rule.
package outputcheck

import (
	"fmt"
	"sort"
	"strings"
)

// Target is one compiled page. It is deliberately a value, not a file handle:
// the engine performs no IO, which keeps it trivially testable and prevents a
// manifest from reaching the filesystem through a check.
type Target struct {
	// Route is the public path within a language tree, e.g. "/checkout/".
	Route string
	// Lang is the language tree this page was compiled into, e.g. "pt".
	Lang string
	HTML string
	// Root is the absolute path to the compiled output directory this target
	// was loaded from. It is empty when a Target is built directly (e.g. most
	// unit tests) rather than via the real loader. Every check in this package
	// stays IO-free by relying on HTML alone, except the json_assets_exist
	// built-in: verifying a referenced asset exists on disk is inherently a
	// question about the wider output tree, not this one Target's HTML, so it
	// needs a root to resolve paths against. All Targets loaded for one run
	// carry the same Root.
	Root string
}

// Finding is one failed assertion, carrying enough context to act on without
// re-running anything.
type Finding struct {
	Rule    string
	Route   string
	Lang    string
	Check   string
	Message string
}

func (f Finding) String() string {
	return fmt.Sprintf("[%s] %s%s: %s (%s)", f.Check, f.Lang, f.Route, f.Message, f.Rule)
}

// Check asserts a property of a single page. It returns one message per
// failure and an empty slice when the page satisfies it.
type Check interface {
	Name() string
	Check(Target) []string
}

// CorpusCheck asserts a property that only the whole compiled site can answer,
// such as whether every internal link resolves to a page that was emitted.
type CorpusCheck interface {
	Name() string
	CheckAll([]Target) []Finding
}

// CheckFunc adapts a plain function into a [Check], so a one-off assertion does
// not need its own type.
type CheckFunc struct {
	CheckName string
	Fn        func(Target) []string
}

func (c CheckFunc) Name() string { return c.CheckName }

func (c CheckFunc) Check(t Target) []string { return c.Fn(t) }

// Selector decides which targets a rule applies to.
//
// The language dimension is what stops an assertion written against one
// language tree from being silently evaluated against another — the failure
// mode that previously forced a bilingual site to keep untranslated copy in
// order to stay green.
type Selector struct {
	// Routes limits the rule to these routes. Empty, or the single entry "*",
	// means every route.
	Routes []string
	// ExcludeRoutes removes routes from an otherwise matching set.
	ExcludeRoutes []string
	// Langs limits the rule to these language trees. Empty means every language.
	Langs []string
}

// Matches reports whether the rule applies to this target.
func (s Selector) Matches(t Target) bool {
	for _, excluded := range s.ExcludeRoutes {
		if routeEqual(excluded, t.Route) {
			return false
		}
	}
	if len(s.Langs) > 0 && !contains(s.Langs, t.Lang) {
		return false
	}
	if len(s.Routes) == 0 {
		return true
	}
	for _, route := range s.Routes {
		if route == "*" || routeEqual(route, t.Route) {
			return true
		}
	}
	return false
}

// Rule binds a set of checks to the places they apply.
type Rule struct {
	Name     string
	Selector Selector
	Checks   []Check
}

// Suite is a composed set of rules plus whole-site checks.
type Suite struct {
	Rules  []Rule
	Corpus []CorpusCheck
}

// Run evaluates every rule against every matching target and returns all
// findings.
//
// It deliberately collects rather than failing fast: one run should report
// everything that is wrong, so a fix cycle is not one defect long.
func (s Suite) Run(targets []Target) []Finding {
	var findings []Finding
	for _, rule := range s.Rules {
		for _, target := range targets {
			if !rule.Selector.Matches(target) {
				continue
			}
			for _, check := range rule.Checks {
				for _, message := range check.Check(target) {
					findings = append(findings, Finding{
						Rule:    rule.Name,
						Route:   target.Route,
						Lang:    target.Lang,
						Check:   check.Name(),
						Message: message,
					})
				}
			}
		}
	}
	for _, corpus := range s.Corpus {
		findings = append(findings, corpus.CheckAll(targets)...)
	}
	return findings
}

// Factory builds a [Check] from its manifest configuration.
type Factory func(config map[string]any) (Check, error)

// CorpusFactory builds a [CorpusCheck] from its manifest configuration.
type CorpusFactory func(config map[string]any) (CorpusCheck, error)

// Registry maps manifest type names onto constructors. It is the extension
// point: registering a new type never modifies the runner or the manifest
// schema.
type Registry struct {
	factories       map[string]Factory
	corpusFactories map[string]CorpusFactory
}

// NewRegistry returns an empty registry with no built-in checks.
func NewRegistry() *Registry {
	return &Registry{
		factories:       map[string]Factory{},
		corpusFactories: map[string]CorpusFactory{},
	}
}

// Register adds a per-page check type.
func (r *Registry) Register(typeName string, factory Factory) {
	r.factories[typeName] = factory
}

// RegisterCorpus adds a whole-site check type.
func (r *Registry) RegisterCorpus(typeName string, factory CorpusFactory) {
	r.corpusFactories[typeName] = factory
}

// Build constructs a registered per-page check.
//
// An unknown type is an error rather than a silent skip: a manifest that names
// a check the runner does not understand is a mistake worth surfacing, not an
// assertion to quietly drop.
func (r *Registry) Build(typeName string, config map[string]any) (Check, error) {
	factory, ok := r.factories[typeName]
	if !ok {
		return nil, fmt.Errorf("unknown check type %q (known: %s)", typeName, strings.Join(r.Types(), ", "))
	}
	return factory(config)
}

// BuildCorpus constructs a registered whole-site check.
func (r *Registry) BuildCorpus(typeName string, config map[string]any) (CorpusCheck, error) {
	factory, ok := r.corpusFactories[typeName]
	if !ok {
		return nil, fmt.Errorf("unknown corpus check type %q (known: %s)", typeName, strings.Join(r.CorpusTypes(), ", "))
	}
	return factory(config)
}

// Types lists the registered per-page check names, sorted.
func (r *Registry) Types() []string { return sortedKeys(r.factories) }

// CorpusTypes lists the registered whole-site check names, sorted.
func (r *Registry) CorpusTypes() []string { return sortedKeysCorpus(r.corpusFactories) }

func sortedKeys(m map[string]Factory) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysCorpus(m map[string]CorpusFactory) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

// routeEqual compares routes ignoring surrounding slashes, so a manifest may
// write "checkout", "/checkout" or "/checkout/" interchangeably.
func routeEqual(a, b string) bool {
	return strings.Trim(a, "/") == strings.Trim(b, "/")
}

// stringField reads a required string from a check's configuration.
func stringField(config map[string]any, key string) (string, error) {
	raw, ok := config[key]
	if !ok {
		return "", fmt.Errorf("missing required field %q", key)
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("field %q must be a string", key)
	}
	if value == "" {
		return "", fmt.Errorf("field %q must not be empty", key)
	}
	return value, nil
}
