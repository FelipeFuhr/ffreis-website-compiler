package cssusage

import (
	"regexp"
	"strings"
)

// cssRule is one top-level prelude+body pair found by scanRules: either a
// style rule (prelude is a selector list) or an at-rule (prelude starts with
// "@"; container at-rules like @media have their body rescanned recursively,
// everything else's body is opaque to this scanner).
type cssRule struct {
	prelude string
	body    string
}

var commentRE = regexp.MustCompile(`(?s)/\*.*?\*/`)

func stripComments(css string) string {
	return commentRE.ReplaceAllString(css, "")
}

// scanRules splits css into its top-level prelude+{body} blocks. It tracks
// brace depth and quoted-string state only -- enough to find each top-level
// block's true extent without being confused by braces inside declaration
// values (e.g. content: "}") -- and is not a full CSS parser. A statement
// with no block at all (@import "x.css"; @charset "utf-8";) produces no rule
// and is silently absorbed into the next rule's prelude; since any prelude
// starting with "@" is treated as a non-selector at-rule (see
// unusedSelectorsInCSS), this never misattributes a real selector.
func scanRules(css string) []cssRule {
	var rules []cssRule
	depth := 0
	segStart := 0
	openIdx := 0
	bodyStart := 0
	var inString byte
	n := len(css)

	for i := 0; i < n; i++ {
		c := css[i]
		if inString != 0 {
			switch c {
			case '\\':
				i++
			case inString:
				inString = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			inString = c
		case '{':
			if depth == 0 {
				openIdx = i
				bodyStart = i + 1
			}
			depth++
		case '}':
			switch {
			case depth == 1:
				depth = 0
				rules = append(rules, cssRule{
					prelude: css[segStart:openIdx],
					body:    css[bodyStart:i],
				})
				segStart = i + 1
			case depth > 1:
				depth--
			}
			// depth == 0: stray closing brace in malformed CSS, ignore.
		}
	}
	return rules
}

// containerAtRules are at-rules whose body is itself a nested stylesheet of
// ordinary rules (same page, conditionally applied) rather than a flat
// declaration block or a non-selector block like @keyframes/@font-face.
var containerAtRules = map[string]struct{}{
	"media":     {},
	"supports":  {},
	"layer":     {},
	"container": {},
	"scope":     {},
}

var vendorPrefixRE = regexp.MustCompile(`^-[a-zA-Z]+-`)

func atRuleName(prelude string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(prelude), "@")
	end := len(trimmed)
	for i, r := range trimmed {
		if !isIdentRune(r) {
			end = i
			break
		}
	}
	name := strings.ToLower(trimmed[:end])
	return vendorPrefixRE.ReplaceAllString(name, "")
}

func isIdentRune(r rune) bool {
	return r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// unusedSelectorsInCSS returns every selector (one comma-branch each) in css
// whose required class/id tokens are provably absent from tokens.
func unusedSelectorsInCSS(css string, tokens domTokens) []string {
	return collectUnusedSelectors(stripComments(css), tokens)
}

func collectUnusedSelectors(css string, tokens domTokens) []string {
	var findings []string
	for _, rule := range scanRules(css) {
		prelude := strings.TrimSpace(rule.prelude)
		if prelude == "" {
			continue
		}
		if strings.HasPrefix(prelude, "@") {
			if _, isContainer := containerAtRules[atRuleName(prelude)]; isContainer {
				findings = append(findings, collectUnusedSelectors(rule.body, tokens)...)
			}
			// Non-container at-rules (@keyframes, @font-face, @page, @property,
			// ...) have no DOM-selectable prelude; never flagged.
			continue
		}
		findings = append(findings, checkSelectorList(prelude, tokens)...)
	}
	return findings
}

func checkSelectorList(prelude string, tokens domTokens) []string {
	var findings []string
	for _, raw := range splitTopLevelCommas(prelude) {
		selector := strings.TrimSpace(raw)
		if selector == "" {
			continue
		}
		classes, ids, determinable := extractSelectorTokens(selector)
		if !determinable {
			// Bare tag/universal/pseudo-only selector -- a token-based check
			// can't tell whether it's used, so never flag it.
			continue
		}
		if anyTokenAbsent(classes, ids, tokens) {
			findings = append(findings, selector)
		}
	}
	return findings
}

// splitTopLevelCommas splits a selector list on "," while respecting
// parenthesis/bracket nesting, so functional pseudo-classes like
// :is(.a, .b) and attribute selectors don't get split mid-argument.
func splitTopLevelCommas(s string) []string {
	var parts []string
	var buf strings.Builder
	parenDepth, bracketDepth := 0, 0

	for _, r := range s {
		switch r {
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		}
		if r == ',' && parenDepth == 0 && bracketDepth == 0 {
			parts = append(parts, buf.String())
			buf.Reset()
			continue
		}
		buf.WriteRune(r)
	}
	parts = append(parts, buf.String())
	return parts
}

var (
	notFuncRE    = regexp.MustCompile(`(?i):not\(([^()]*)\)`)
	classTokenRE = regexp.MustCompile(`\.[-\w]+`)
	idTokenRE    = regexp.MustCompile(`#[-\w]+`)
)

// extractSelectorTokens returns the class/id tokens a selector requires.
// :not(...) arguments are stripped first: requiring an absent class there
// would be backwards (":not(.hidden)" matches BECAUSE .hidden is absent, so
// treating ".hidden" as a required-present token would make a genuinely used
// rule look unused -- exactly the false positive this package must avoid).
func extractSelectorTokens(selector string) (classes, ids []string, determinable bool) {
	cleaned := notFuncRE.ReplaceAllString(selector, "")
	for _, m := range classTokenRE.FindAllString(cleaned, -1) {
		classes = append(classes, strings.TrimPrefix(m, "."))
	}
	for _, m := range idTokenRE.FindAllString(cleaned, -1) {
		ids = append(ids, strings.TrimPrefix(m, "#"))
	}
	return classes, ids, len(classes)+len(ids) > 0
}

// anyTokenAbsent reports whether at least one required class/id is absent
// from tokens. If a class never appears anywhere in the page's rendered
// markup, no element can carry it, so any selector requiring it provably
// cannot match that page -- the one case this heuristic can assert with
// certainty rather than merely guess at.
func anyTokenAbsent(classes, ids []string, tokens domTokens) bool {
	for _, c := range classes {
		if _, ok := tokens.classes[c]; !ok {
			return true
		}
	}
	for _, id := range ids {
		if _, ok := tokens.ids[id]; !ok {
			return true
		}
	}
	return false
}
