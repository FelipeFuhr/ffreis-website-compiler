package conventions

import (
	"regexp"
	"strings"
)

// CSS scanning for the two rules that ask "does the first-loaded stylesheet
// declare X?" (css-reset-present, font-family-declared).
//
// This is a presence check on declaration blocks, not a CSS parser and not a
// cascade resolver. Deciding what actually applies to `body` on a rendered page
// needs computed styles, which is the rendered-checks tier (R11) and not
// something a static pass can honestly claim. What it can say with confidence
// is "no rule in this stylesheet sets box-sizing: border-box", and that is the
// failure these two rules exist to catch.

// cssRuleRE matches one `selectors { declarations }` pair. Because neither
// capture may contain a brace, an at-rule wrapper (`@media (...) { p { ... } }`)
// never matches as a unit: its inner rules are matched individually and the
// prelude is simply skipped.
//
// The consequence, stated rather than hidden: nesting context is not tracked,
// so a declaration inside a media query counts as present. That is the right
// trade for a presence check — the question these rules answer is "is the
// reset here at all", and whether it actually wins the cascade on a given
// element is a computed-style question, i.e. the rendered tier.
var cssRuleRE = regexp.MustCompile(`(?s)([^{}]+)\{([^{}]*)\}`)

// cssRule is one declaration block.
type cssRule struct {
	// Selector is the raw selector list, lowercased and whitespace-collapsed.
	Selector string
	// Decls is the declaration text, lowercased with all whitespace removed, so
	// `max-width: 100%` and the minified `max-width:100%` compare equal. Every
	// contains-check in the rules relies on this normalization.
	Decls string
}

var wsRE = regexp.MustCompile(`\s+`)

// parseCSSRules returns every declaration block in source order.
func parseCSSRules(css string) []cssRule {
	css = stripCSSComments(css)
	matches := cssRuleRE.FindAllStringSubmatch(css, -1)
	rules := make([]cssRule, 0, len(matches))
	for _, m := range matches {
		rules = append(rules, cssRule{
			Selector: strings.ToLower(strings.TrimSpace(wsRE.ReplaceAllString(m[1], " "))),
			Decls:    strings.ToLower(wsRE.ReplaceAllString(m[2], "")),
		})
	}
	return rules
}

var cssCommentRE = regexp.MustCompile(`(?s)/\*.*?\*/`)

func stripCSSComments(css string) string { return cssCommentRE.ReplaceAllString(css, "") }

// selectorMentions reports whether a selector list targets the given element
// name, as a whole word. Matching "body" as a substring would be satisfied by
// `.body-copy`, which is exactly the kind of near-miss that makes a gate
// report a pass it did not earn.
func selectorMentions(selector string, names ...string) bool {
	for _, name := range names {
		for _, m := range elementWordRE(name).FindAllStringIndex(selector, -1) {
			if !precededByIdentPunct(selector, m[0]) {
				return true
			}
		}
	}
	return false
}

var elementWordCache = map[string]*regexp.Regexp{}

func elementWordRE(name string) *regexp.Regexp {
	if re, ok := elementWordCache[name]; ok {
		return re
	}
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	elementWordCache[name] = re
	return re
}

// precededByIdentPunct reports whether a selector word is part of a class, id,
// attribute or custom-property name rather than an element selector.
func precededByIdentPunct(s string, start int) bool {
	if start == 0 {
		return false
	}
	switch s[start-1] {
	case '.', '#', '-', '_', '[', '"', '\'', '$':
		return true
	}
	return false
}

// declaresAny reports whether any of the given normalized declaration prefixes
// appears in the block. Callers pass already-normalized text
// ("box-sizing:border-box").
func (r cssRule) declaresAny(decls ...string) bool {
	for _, d := range decls {
		if strings.Contains(r.Decls, d) {
			return true
		}
	}
	return false
}
