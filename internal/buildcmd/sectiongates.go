package buildcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ffreis-website-compiler/internal/flagregistry"
)

// registryRelPath is where a website declares its flag registry, relative to
// -website-root. Discovered automatically, like sitemap.yaml.
const registryRelPath = "flags/flags.json"

// knownSections is the closed set of content sections the compiler can gate.
// Adding a section here without declaring it in a site's registry makes that
// site's build fail until the gate is declared — which is the point: an
// undeclared point of variation is a defect, not a default.
var knownSections = []string{"blog", "courses", "projects"}

// findFlagRegistry returns the path to the website's flag registry, or "" when
// the site does not declare one (sites that have not adopted the registry yet).
func findFlagRegistry(websiteRoot string) string {
	path := filepath.Join(websiteRoot, registryRelPath)
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// resolveSectionGates computes the list of content sections to disable for this
// build, from the site's declared registry defaults plus the per-environment
// overrides the deployer passes.
//
// Two modes, and the difference is the whole point of the registry:
//
//   - No registry (legacy): sections are ON unless -disable-sections names them.
//     The safe state requires configuration, so dropping the config re-enables
//     everything. Kept only for sites that have not declared a registry yet.
//
//   - Registry present (fail-closed): every section's default comes from its
//     declared section_<name> gate. Losing the per-environment override falls
//     back to the declared default — for a not-ready section that default is
//     off, so a config that goes missing cannot publish it.
//
// disable always beats enable: between two contradictory instructions, the one
// that ships less is the safe one.
func resolveSectionGates(registryPath string, enable, disable []string) ([]string, error) {
	if registryPath == "" {
		if len(enable) > 0 {
			return nil, fmt.Errorf(
				"-enable-sections %q requires a flag registry at <website-root>/%s; "+
					"without one, sections default to enabled and -enable-sections is meaningless",
				strings.Join(enable, ","), registryRelPath,
			)
		}
		return append([]string(nil), disable...), nil
	}

	reg, err := flagregistry.Load(registryPath)
	if err != nil {
		return nil, err
	}
	gates, err := reg.SectionGates()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", registryPath, err)
	}

	if err := validateGateCoverage(registryPath, gates); err != nil {
		return nil, err
	}
	// enable first, then disable, so disable wins on conflict.
	if err := applyGateOverrides(gates, enable, true, "-enable-sections", registryPath); err != nil {
		return nil, err
	}
	if err := applyGateOverrides(gates, disable, false, "-disable-sections", registryPath); err != nil {
		return nil, err
	}

	var disabled []string
	for name, enabled := range gates {
		if !enabled {
			disabled = append(disabled, name)
		}
	}
	sort.Strings(disabled)
	return disabled, nil
}

// validateGateCoverage requires the registry to declare exactly the known
// sections: no missing gate (which would silently re-enable a section) and no
// gate for a section the compiler does not know (a dangling declaration that
// would never be read).
func validateGateCoverage(registryPath string, gates map[string]bool) error {
	var missing []string
	for _, name := range knownSections {
		if _, ok := gates[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"%s declares a flag registry but is missing gate flags for section(s) %s; "+
				"declare %s%s so the shipped state is explicit",
			registryPath, strings.Join(missing, ", "),
			flagregistry.SectionGatePrefix, missing[0],
		)
	}

	known := make(map[string]bool, len(knownSections))
	for _, name := range knownSections {
		known[name] = true
	}
	var dangling []string
	for name := range gates {
		if !known[name] {
			dangling = append(dangling, name)
		}
	}
	if len(dangling) > 0 {
		sort.Strings(dangling)
		return fmt.Errorf(
			"%s declares gate flag(s) for unknown section(s) %s; known sections are %s",
			registryPath, strings.Join(dangling, ", "), strings.Join(knownSections, ", "),
		)
	}
	return nil
}

// applyGateOverrides sets the named sections to value, rejecting any name the
// registry does not declare so a typo fails the build instead of silently
// leaving the declared default in place.
func applyGateOverrides(gates map[string]bool, names []string, value bool, flagName, registryPath string) error {
	for _, name := range names {
		if _, ok := gates[name]; !ok {
			return fmt.Errorf(
				"%s names section %q, which is not declared in %s; declare %s%s or fix the name",
				flagName, name, registryPath, flagregistry.SectionGatePrefix, name,
			)
		}
		gates[name] = value
	}
	return nil
}
