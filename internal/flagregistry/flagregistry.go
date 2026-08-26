// Package flagregistry loads a website's declared flag registry — the single
// file that enumerates every deliberate point of variation in a site build.
//
// The workspace rule is that variation is DECLARED, never implicit: a build
// input that changes what ships must exist as a registry entry, so that the
// absence of a decision is a detectable defect rather than a silent default.
// This package reads the registry; internal/buildcmd resolves the compile-bound
// gates in it against the per-environment flags the deployer passes.
//
// Schema: flags/flag-registry.schema.json in the website repo (fleet reference
// copy: heavy-heater/flags/flag-registry.schema.json).
package flagregistry

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SectionGatePrefix marks a gate flag that governs a content section. The
// section name is whatever follows the prefix: section_blog gates "blog".
const SectionGatePrefix = "section_"

// Flag is one declared point of variation. Only the fields the compiler acts on
// are typed; the rest of the schema (options, value_type, cost_guard, …) is
// carried for the toolbar and other projections and is ignored here.
type Flag struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Category    string `json:"category"`
	Binding     string `json:"binding"`
	Effect      string `json:"effect"`
	Default     any    `json:"default"`
	Seam        string `json:"seam"`
	Scope       string `json:"scope"`
	Description string `json:"description"`
}

// Registry is the parsed contents of flags/flags.json.
type Registry struct {
	Version int    `json:"version"`
	Project string `json:"project"`
	Flags   []Flag `json:"flags"`
}

// Load reads and validates the registry at path.
func Load(path string) (*Registry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading flag registry %s: %w", path, err)
	}

	var reg Registry
	if err := json.Unmarshal(raw, &reg); err != nil {
		return nil, fmt.Errorf("parsing flag registry %s: %w", path, err)
	}
	if reg.Version != 1 {
		return nil, fmt.Errorf("flag registry %s: unsupported version %d (want 1)", path, reg.Version)
	}
	for i, f := range reg.Flags {
		if f.Name == "" {
			return nil, fmt.Errorf("flag registry %s: flags[%d] missing required field 'name'", path, i)
		}
	}
	return &reg, nil
}

// SectionGates returns the declared default for every section_* gate flag,
// keyed by section name ("blog", "courses", "projects").
//
// A gate whose default is not a bool is an error rather than a silent false:
// a mistyped default in the registry must not decide what ships.
func (r *Registry) SectionGates() (map[string]bool, error) {
	gates := map[string]bool{}
	for _, f := range r.Flags {
		if !strings.HasPrefix(f.Name, SectionGatePrefix) {
			continue
		}
		name := strings.TrimPrefix(f.Name, SectionGatePrefix)
		if name == "" {
			return nil, fmt.Errorf("flag registry: %q has an empty section name", f.Name)
		}
		enabled, ok := f.Default.(bool)
		if !ok {
			return nil, fmt.Errorf(
				"flag registry: gate %q has non-boolean default %v (%T); section gates are toggles",
				f.Name, f.Default, f.Default,
			)
		}
		gates[name] = enabled
	}
	return gates, nil
}
