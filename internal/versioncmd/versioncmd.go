// Package versioncmd implements the `version` subcommand: it prints the
// compiler's build version and, with --json, a machine-readable capability
// manifest. The manifest exists so consumer CI (this compiler's site repos)
// can feature-detect what a specific compiler build supports — e.g. does it
// accept -content-source=dev, does it generate listing detail pages — instead
// of grepping compiler source for strings that happen to indicate a feature
// today but are fragile to internal refactors (renamed packages, moved
// functions, reworded comments all break a source grep without changing
// behavior at all).
package versioncmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"

	"ffreis-website-compiler/internal/buildcmd"
	"ffreis-website-compiler/internal/sitegen"
)

// Version is the compiler's semver build version. It is deliberately left
// empty in source rather than set to a plausible-looking string: this
// package cannot know the real release version at edit time, and a
// hand-typed value would silently drift the moment an actual release is
// cut. A release build sets it via:
//
//	-ldflags "-X ffreis-website-compiler/internal/versioncmd.Version=<semver>"
//
// where <semver> comes from the git tag release-please cuts for this repo
// (see .release-please-config.json / .release-please-manifest.json). The
// Makefile's `install` target wires this up automatically from `git
// describe`. When Version is left unset (e.g. a plain `go run`), resolvedVersion
// falls back to the Go toolchain's own VCS-derived pseudo-version instead —
// see resolvedVersion for why -buildvcs=true matters for that path.
var Version string

// resolvedVersion returns Version if a release build set it via -ldflags,
// otherwise falls back to the module pseudo-version the Go toolchain
// computes from VCS info (e.g. "v0.0.0-20260904114111-88ea00041a6d+dirty"),
// which is accurate to the exact commit that was built. That fallback
// requires the build to have been invoked with -buildvcs=true: it is the
// default for `go build`/`go install`, but `go run` needs it passed
// explicitly (confirmed empirically — `go run`'s default "auto" mode does
// not stamp VCS info the way `go build`'s does), which is why every `go run
// ./cmd/website-compiler ... version --json` call site in this repo's own
// Makefile and in consumer CI passes -buildvcs=true. Falls back to
// "0.0.0-dev" only when neither is available, e.g. containers/Dockerfile.cli
// builds outside a git checkout (it copies go.mod/cmd/internal, not .git).
//
// Known gap: Go's own -buildvcs stamping (confirmed empirically) can report
// a stale commit when built from a linked git worktree rather than a plain
// checkout — `git describe` itself stays correct there, which is exactly why
// the Makefile's VERSION uses `git describe` for -ldflags instead of relying
// on this fallback. A plain `actions/checkout` in CI, or a normal (non
// -worktree) local clone, is unaffected.
func resolvedVersion() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "0.0.0-dev"
}

// Manifest is the machine-readable shape emitted by `version --json`. Every
// field beyond Version is sourced from a real, current registry in this
// codebase (buildcmd.SectionNames, sitegen.TemplateFunctionNames, and the
// commands/capabilities lists below, each tied by comment to the
// internal/cli/cli.go switch or internal/buildcmd/options.go flag it
// mirrors) rather than being invented or hand-guessed.
type Manifest struct {
	// Version is the compiler build's version — see the Version var and
	// resolvedVersion.
	Version string `json:"version"`
	// Commands lists every top-level CLI verb internal/cli/cli.go dispatches
	// to (canonical names only, not aliases like "compile"/"web").
	Commands []string `json:"commands"`
	// Capabilities lists optional/opt-in build features and flags a consumer
	// CI script might need to feature-detect before passing a flag or
	// relying on a behavior existing in a given compiler build. See
	// internal/buildcmd/options.go for the flags these correspond to.
	Capabilities []string `json:"capabilities"`
	// Sections lists the content sections the -disable-sections flag
	// currently recognizes (buildcmd.SectionNames()).
	Sections []string `json:"sections"`
	// TemplateFunctions lists the Go template helper functions available to
	// page templates (sitegen.TemplateFunctionNames()) — e.g. "required",
	// "dig", "trimSuffix". A website's templates can only use a helper if
	// it appears here for the compiler build in use.
	TemplateFunctions []string `json:"template_functions"`
}

// commands mirrors the subcommand set internal/cli/cli.go's Run switch
// dispatches to (excluding the "help"/"-h"/"--help" pseudo-command and
// aliases). Kept as an explicit list rather than introspected: cli.go
// imports this package to wire up "version" itself, so the reverse import
// would cycle. Update this list whenever a case is added to or removed from
// that switch.
var commands = []string{
	"build",
	"check-lang-parity",
	"export-site-data",
	"serve",
	"test-output",
	"validate-assets",
	"validate-sanity",
	"validate-site-data",
	"version",
}

// capabilities lists optional/opt-in build features exposed by
// internal/buildcmd/options.go's current flag set (see the Manifest.Capabilities
// doc comment for what these are for). Each entry corresponds to a real flag
// or flag-gated behavior on the base branch this package was added on:
//
//   - asset-inlining: -inline-assets, -js-inline-threshold,
//     -js-shared-inline-threshold, -embed-fonts, -inline-body-css,
//     -raster-inline-threshold
//   - blog-posts: -posts-dir (Markdown blog post generation + RSS feed)
//   - clean-urls: -clean-urls
//   - content-source-dev: -content-source=dev (dev.ffreis.com-style builds
//     against real content paths; implies -allow-blanket-robots-disallow)
//   - content-source-mock: -content-source=mock (exempts /mock/ content
//     paths from the anti-leak guard)
//   - courses-pagination: -courses-file
//   - dev-data-injection: -dev-data (window.__devBuild; content-source!=prod only)
//   - external-asset-mirroring: -mirror-external-assets, -mirrored-assets-dir
//   - listings-detail-pages: -listings-file (/listings/<id>/ detail pages)
//   - output-tests: -run-output-tests, -output-test-manifest, the
//     test-output command
//   - projects-pagination: -projects-file
//   - robots-safety-guard: -allow-blanket-robots-disallow /
//     checkRobotsTxtSafety's blanket-Disallow guard
//   - sanity-checks: -sanity, the validate-sanity command
//   - section-registry: -disable-sections, backed by buildcmd.SectionNames()
//     (see the Sections manifest field for the current names)
//   - sibling-base-paths: -sibling-base-paths
//   - sitemap-default-on: automatic sitemap.xml generation via
//     -sitemap-base-url / site data base_url fallback, with no sitemap
//     config file required
//   - strict-contract: -strict-contract
//   - tracker-sdk-injection: -tracker-enabled (+ -tracker-sdk-version,
//     -tracker-site-id, -tracker-endpoint, -tracker-cdn-base)
var capabilities = []string{
	"asset-inlining",
	"blog-posts",
	"clean-urls",
	"content-source-dev",
	"content-source-mock",
	"courses-pagination",
	"dev-data-injection",
	"external-asset-mirroring",
	"listings-detail-pages",
	"output-tests",
	"projects-pagination",
	"robots-safety-guard",
	"sanity-checks",
	"section-registry",
	"sibling-base-paths",
	"sitemap-default-on",
	"strict-contract",
	"tracker-sdk-injection",
}

// NewManifest builds the current capability manifest.
func NewManifest() Manifest {
	return Manifest{
		Version:           resolvedVersion(),
		Commands:          commands,
		Capabilities:      capabilities,
		Sections:          buildcmd.SectionNames(),
		TemplateFunctions: sitegen.TemplateFunctionNames(),
	}
}

// Run implements the `version` subcommand: prints the build version, or with
// --json the full machine-readable capability manifest.
func Run(args []string, _ *slog.Logger) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print the full capability manifest (version, commands, capabilities, sections, template_functions) as JSON instead of just the version string")
	if err := fs.Parse(args); err != nil {
		return err
	}

	manifest := NewManifest()

	if !*jsonOut {
		fmt.Println(manifest.Version)
		return nil
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(manifest)
}
