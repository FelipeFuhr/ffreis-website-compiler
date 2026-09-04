package outputtestcmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/outputtestcmd"
)

// writeSite lays out a tiny compiled tree: route -> HTML.
func writeSite(t *testing.T, pages map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for route, html := range pages {
		dir := filepath.Join(root, filepath.FromSlash(strings.Trim(route, "/")))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return root
}

func writeManifest(t *testing.T, body string) (websiteRoot, manifestPath string) {
	t.Helper()
	websiteRoot = t.TempDir()
	manifestPath = filepath.Join(websiteRoot, "output.yaml")
	if err := os.WriteFile(manifestPath, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return websiteRoot, manifestPath
}

func run(t *testing.T, manifestBody string, out string, extra ...string) error {
	t.Helper()
	websiteRoot, manifestPath := writeManifest(t, manifestBody)
	args := append([]string{
		"-website-root", websiteRoot,
		"-manifest", manifestPath,
		"-out", out,
	}, extra...)
	return outputtestcmd.Run(args, nil)
}

func TestRulesOnlyManifestNeedsNoPagesSection(t *testing.T) {
	out := writeSite(t, map[string]string{"/": `<html lang="en"><main id="main">hi</main></html>`})
	err := run(t, `
version: 1
rules:
  - name: main landmark
    routes: ["*"]
    checks:
      - type: matches
        pattern: '<main[\s>]'
`, out)
	if err != nil {
		t.Fatalf("a rules-only manifest must be valid: %v", err)
	}
}

func TestRegexCheckCatchesWhatLiteralContainsCannot(t *testing.T) {
	out := writeSite(t, map[string]string{
		"/checkout/": `<html lang="en"><main id="main"><input name="CardNumber"></main></html>`,
	})
	err := run(t, `
version: 1
rules:
  - name: checkout collects no card data
    routes: ["/checkout/"]
    checks:
      - type: not_matches
        pattern: '(?i)name="(cardnumber|cvv|cvc)"'
`, out)
	if err == nil {
		t.Fatal("case-insensitive card-field markup must fail")
	}
}

func TestLanguageScopingKeepsAnAssertionOffTheOtherTree(t *testing.T) {
	// The regression that mattered: a copy assertion written for one language
	// must not be evaluated against another, which is what previously forced a
	// bilingual site to keep untranslated text to stay green.
	out := writeSite(t, map[string]string{"/": `<html lang="pt"><main id="main">Ola</main></html>`})
	manifest := `
version: 1
rules:
  - name: english-only copy
    langs: ["en"]
    checks:
      - type: contains
        value: "Hello"
`
	if err := run(t, manifest, out, "-lang", "pt"); err != nil {
		t.Fatalf("an en-scoped rule must not run against the pt tree: %v", err)
	}
	if err := run(t, manifest, out, "-lang", "en"); err == nil {
		t.Fatal("the same rule must still run against the en tree")
	}
}

func TestLinkIntegrityCatchesADanglingRoute(t *testing.T) {
	out := writeSite(t, map[string]string{
		"/": `<html lang="en"><main id="main"><a href="/en/ghost/">gone</a></main></html>`,
	})
	err := run(t, `
version: 1
corpus:
  - type: link_integrity
    base_path: /en
`, out, "-lang", "en")
	if err == nil {
		t.Fatal("a link to an uncompiled route must fail")
	}
}

func TestLinkIntegrityPassesWhenEveryRouteExists(t *testing.T) {
	out := writeSite(t, map[string]string{
		"/":       `<html lang="en"><main id="main"><a href="/en/about/">about</a></main></html>`,
		"/about/": `<html lang="en"><main id="main">about</main></html>`,
	})
	if err := run(t, `
version: 1
corpus:
  - type: link_integrity
    base_path: /en
`, out, "-lang", "en"); err != nil {
		t.Fatalf("resolvable links must pass: %v", err)
	}
}

// writeRawFile adds a non-HTML file (e.g. a JSON manifest or an image) to an
// already-compiled output tree, since writeSite only produces index.html
// pages per route.
func writeRawFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestJSONAssetsExistCatchesAMissingManifestIcon(t *testing.T) {
	out := writeSite(t, map[string]string{"/": `<html lang="en"><main id="main">hi</main></html>`})
	writeRawFile(t, out, "site.webmanifest", `{"icons":[{"src":"/images/logo-256.png","sizes":"256x256"}]}`)
	// The icon file itself is never written to out — this is the exact P0-3
	// symptom: a manifest referencing a fingerprinted filename that was never
	// actually copied to the compiled output.
	err := run(t, `
version: 1
corpus:
  - type: json_assets_exist
    path: site.webmanifest
    field: "icons[].src"
`, out)
	if err == nil || !strings.Contains(err.Error(), "compiled output check(s) failed") {
		t.Fatalf("a manifest referencing a missing icon must fail, got %v", err)
	}
}

func TestJSONAssetsExistPassesWhenTheManifestIconResolves(t *testing.T) {
	out := writeSite(t, map[string]string{"/": `<html lang="en"><main id="main">hi</main></html>`})
	writeRawFile(t, out, "site.webmanifest", `{"icons":[{"src":"/images/logo-256.f971395b.png","sizes":"256x256"}]}`)
	writeRawFile(t, out, "images/logo-256.f971395b.png", "pngdata")
	if err := run(t, `
version: 1
corpus:
  - type: json_assets_exist
    path: site.webmanifest
    field: "icons[].src"
`, out); err != nil {
		t.Fatalf("a manifest whose icons all resolve must pass: %v", err)
	}
}

func TestPresetCanBeComposedWithLocalRules(t *testing.T) {
	out := writeSite(t, map[string]string{"/": `<html><body>no landmark</body></html>`})
	err := run(t, `
version: 1
include:
  - a11y-baseline
rules:
  - name: local rule
    checks:
      - type: contains
        value: "no landmark"
`, out)
	if err == nil {
		t.Fatal("the included baseline must flag a page with no main landmark")
	}
}

func TestUnknownCheckTypeIsAnErrorNotASilentSkip(t *testing.T) {
	out := writeSite(t, map[string]string{"/": `<html lang="en"><main>x</main></html>`})
	err := run(t, `
version: 1
rules:
  - name: typo
    checks:
      - type: no_such_check_type
        value: "x"
`, out)
	if err == nil || !strings.Contains(err.Error(), "unknown check type") {
		t.Fatalf("a misspelled check type must be reported, got %v", err)
	}
}

func TestUnknownPresetIsRejected(t *testing.T) {
	out := writeSite(t, map[string]string{"/": `<html lang="en"><main>x</main></html>`})
	err := run(t, `
version: 1
include:
  - nope
`, out)
	if err == nil || !strings.Contains(err.Error(), "unknown preset") {
		t.Fatalf("an unknown preset must be reported, got %v", err)
	}
}

func TestAnInvalidPatternFailsBeforeAnyPageIsRead(t *testing.T) {
	out := writeSite(t, map[string]string{"/": `<html lang="en"><main>x</main></html>`})
	err := run(t, `
version: 1
rules:
  - name: bad regex
    checks:
      - type: matches
        pattern: "([unclosed"
`, out)
	if err == nil || !strings.Contains(err.Error(), "invalid pattern") {
		t.Fatalf("a bad pattern must fail at load time, got %v", err)
	}
}

func TestAnEmptyManifestIsRejected(t *testing.T) {
	out := writeSite(t, map[string]string{"/": `<html lang="en"><main>x</main></html>`})
	err := run(t, "version: 1\n", out)
	if err == nil || !strings.Contains(err.Error(), "at least one of") {
		t.Fatalf("a manifest asserting nothing must be rejected, got %v", err)
	}
}

func TestLegacyPagesFormStillWorksAlongsideRules(t *testing.T) {
	// Backward compatibility: the original manifest form must keep working, and
	// must compose with the new one in a single file.
	out := writeSite(t, map[string]string{"/": `<html lang="en"><main id="main">Hello World</main></html>`})
	if err := run(t, `
version: 1
pages:
  - name: home
    path: /
    contains:
      - "Hello World"
rules:
  - name: landmark
    checks:
      - type: matches
        pattern: '<main[\s>]'
`, out); err != nil {
		t.Fatalf("legacy and composable forms must coexist: %v", err)
	}
}
