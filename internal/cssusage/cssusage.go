// Package cssusage finds CSS rules a page's own linked stylesheets carry but
// that page's own rendered HTML never uses -- the per-page counterpart to
// internal/assetusage's file-level "is this CSS file reachable from any
// page" check.
//
// This is a conservative, token-based heuristic, not a DOM/CSS engine: a
// selector is flagged only when a class or ID it requires is PROVABLY absent
// from the page (the token appears nowhere in the page's rendered class/id
// attributes), which is a sound necessary-condition check even though it
// doesn't verify the selector's full structural match. False negatives
// (missing a genuinely unused rule) are expected; false positives should not
// occur from static markup alone. The one known source of false positives is
// markup a page's own client-side JavaScript adds after load -- this package
// only ever sees the server-rendered HTML, so a selector that exclusively
// targets a JS-added class will always look unused here. That is why this
// check is advisory (it warns, it never fails validation) rather than a hard
// gate like assetusage.Validate.
package cssusage

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"ffreis-website-compiler/internal/assetusage"
)

// Finding is one CSS selector, inside one stylesheet a page links, whose
// required classes/IDs are absent from that page's own rendered HTML.
type Finding struct {
	Page     string
	CSSFile  string
	Selector string
}

type Result struct {
	Findings []Finding
}

// Validate scopes to each rendered page's own <link rel="stylesheet"> tags
// (via assetusage.LocalStylesheetHrefs) and checks every rule in those local
// sheets against that same page's own class/id attributes.
func Validate(assetsRoot string, renderedPages map[string]string) (Result, error) {
	var result Result
	for _, pageName := range sortedKeys(renderedPages) {
		html := renderedPages[pageName]
		tokens := extractDOMTokens(html)

		hrefs, err := assetusage.LocalStylesheetHrefs(html)
		if err != nil {
			return Result{}, fmt.Errorf("collecting stylesheet hrefs for page %s: %w", pageName, err)
		}
		for _, href := range hrefs {
			selectors, err := findingsForStylesheet(assetsRoot, href, tokens)
			if err != nil {
				return Result{}, err
			}
			for _, selector := range selectors {
				result.Findings = append(result.Findings, Finding{
					Page:     pageName,
					CSSFile:  href,
					Selector: selector,
				})
			}
		}
	}
	return result, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func findingsForStylesheet(assetsRoot, href string, tokens domTokens) ([]string, error) {
	fullPath := filepath.Join(assetsRoot, filepath.FromSlash(href))
	raw, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			// assetusage.Validate already reports a missing local asset
			// reference; this package only judges rules in sheets that exist.
			return nil, nil
		}
		return nil, fmt.Errorf("reading css asset %s: %w", fullPath, err)
	}
	return unusedSelectorsInCSS(string(raw), tokens), nil
}

type domTokens struct {
	classes map[string]struct{}
	ids     map[string]struct{}
}

var (
	classAttrRE = regexp.MustCompile(`(?is)\sclass\s*=\s*["']([^"']*)["']`)
	idAttrRE    = regexp.MustCompile(`(?is)\sid\s*=\s*["']([^"']*)["']`)
)

func extractDOMTokens(html string) domTokens {
	tokens := domTokens{classes: map[string]struct{}{}, ids: map[string]struct{}{}}
	for _, match := range classAttrRE.FindAllStringSubmatch(html, -1) {
		for _, class := range strings.Fields(match[1]) {
			tokens.classes[class] = struct{}{}
		}
	}
	for _, match := range idAttrRE.FindAllStringSubmatch(html, -1) {
		if id := strings.TrimSpace(match[1]); id != "" {
			tokens.ids[id] = struct{}{}
		}
	}
	return tokens
}
