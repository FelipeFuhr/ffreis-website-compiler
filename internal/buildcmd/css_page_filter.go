package buildcmd

import (
	"regexp"
	"strconv"
	"strings"
)

// This file implements -split-css-per-page: when a page's stylesheet is
// inlined (always true for head CSS, optionally true for body CSS via
// -inline-body-css), filter out rules whose selectors reference a class, id,
// or tag name that never appears anywhere in that page's own rendered HTML.
//
// Scope and safety: this is a class/id/tag presence filter, not a real CSS
// selector engine. It never checks combinator structure (".a .b" only
// requires both ".a" and ".b" to appear SOMEWHERE on the page, not that a
// ".b" is actually nested inside an ".a") and it does not inspect attribute
// selectors ("[data-foo]") or the arguments of functional pseudo-classes
// (":not(.x)", ":is(.y)") at all -- any selector containing either is left
// alone (neither it nor the rule it's part of is ever dropped on that
// basis). Both simplifications are deliberately one-directional: they can
// only cause a rule to be KEPT that strict analysis would have dropped,
// never the reverse. Shipping a few unused bytes is an acceptable cost;
// dropping a rule the page actually needs is not.
//
// A page's pseudo-class STATES (:hover, :focus, ::before, …) are never
// checked against anything -- they contribute no extra requirement, since
// whether an element is currently hovered isn't a property of the static
// HTML this filter ever sees.

var (
	// cssBracketGroupRE / cssParenGroupRE strip attribute-selector brackets and
	// pseudo-class/function parens from a selector before token extraction, so
	// neither their contents (which may contain '.', '#', or arbitrary text)
	// nor commas inside them (":not(.a, .b)") are misread as selector tokens
	// or top-level selector-list separators.
	cssBracketGroupRE = regexp.MustCompile(`\[[^\]]*\]`)
	cssParenGroupRE   = regexp.MustCompile(`\([^)]*\)`)

	// Token-extraction regexes, applied to a selector already stripped of
	// bracket/paren groups.
	cssClassTokenRE = regexp.MustCompile(`\.([A-Za-z_-][\w-]*)`)
	cssIDTokenRE    = regexp.MustCompile(`#([A-Za-z_-][\w-]*)`)
	// A bare identifier is only a tag name when it isn't preceded by '.', '#',
	// or ':' (which would make it a class, id, or pseudo-class/element name
	// instead). The allowed preceding characters are start-of-string,
	// whitespace, or a combinator/list character.
	cssTagTokenRE = regexp.MustCompile(`(?:^|[\s>+~,])\s*([A-Za-z][\w-]*)`)

	// HTML-side scan: every class= / id= attribute value, and every opening
	// tag name, found anywhere in the page's rendered HTML.
	htmlClassAttrRE = regexp.MustCompile(`(?i)\bclass\s*=\s*["']([^"']*)["']`)
	htmlIDAttrRE    = regexp.MustCompile(`(?i)\bid\s*=\s*["']([^"']*)["']`)
	htmlTagNameRE   = regexp.MustCompile(`<([A-Za-z][\w-]*)`)

	// Conditional-group at-rules recurse into their body and apply the same
	// filtering to the rules nested inside; any other at-rule with a body
	// (@font-face, @keyframes, @page, @layer, …) is kept wholesale, unfiltered
	// — deliberately, since none of them have a page-HTML-matchable selector
	// and guessing wrong about an unrecognized at-rule shape is exactly the
	// kind of mistake this filter is built to never make.
	cssConditionalAtRuleRE = regexp.MustCompile(`(?i)^@(media|supports|container)\b`)
)

// filterCSSForPageUsage drops CSS rules whose selectors reference no class,
// id, or tag name present anywhere in pageHTML. css is expected to already be
// fully flattened (flattenCSSImports has run) but not yet minified.
func filterCSSForPageUsage(css, pageHTML string) string {
	used := collectUsedTokens(pageHTML)
	masked, slots := maskCSSNoise(css)
	filtered := filterCSSBlocks(masked, used)
	return unmaskCSSNoise(filtered, slots)
}

// collectUsedTokens builds the set of class names, id names, and lowercase
// tag names that appear anywhere in pageHTML.
func collectUsedTokens(pageHTML string) map[string]bool {
	used := make(map[string]bool)
	for _, m := range htmlClassAttrRE.FindAllStringSubmatch(pageHTML, -1) {
		for _, c := range strings.Fields(m[1]) {
			used["."+c] = true
		}
	}
	for _, m := range htmlIDAttrRE.FindAllStringSubmatch(pageHTML, -1) {
		if id := strings.TrimSpace(m[1]); id != "" {
			used["#"+id] = true
		}
	}
	for _, m := range htmlTagNameRE.FindAllStringSubmatch(pageHTML, -1) {
		used[strings.ToLower(m[1])] = true
	}
	return used
}

// cssNoisePlaceholder is a token substituted for comments, quoted strings,
// and url(...) content before structural (brace/comma) scanning, so that a
// literal '{', '}', ',', or ';' inside any of those can never be misread as
// CSS structure. Restored verbatim in the final output by unmaskCSSNoise.
type cssNoiseSlot struct {
	token    string
	original string
}

func maskCSSNoise(css string) (string, []cssNoiseSlot) {
	var slots []cssNoiseSlot
	// __CSSFILTERPH0__-style tokens, matching minifyCSS's own placeholder
	// format (fmt.Sprintf("__CSSPH%d__", ...)) rather than anything
	// comment/string/url()-shaped — a placeholder that itself looked like a
	// CSS comment would get re-masked by a later pass in this same loop
	// (cssBlockCommentRE matches ANY /* ... */ span, including one this loop
	// just inserted), nesting placeholders inside each other in a way
	// unmaskCSSNoise's single forward pass can't unwind.
	mask := func(re *regexp.Regexp, s string) string {
		return re.ReplaceAllStringFunc(s, func(m string) string {
			tok := "__CSSFILTERPH" + strconv.Itoa(len(slots)) + "__"
			slots = append(slots, cssNoiseSlot{tok, m})
			return tok
		})
	}
	// Order matters: preserved comments and url() before generic comments and
	// quoted strings, mirroring minifyCSS's own ordering rationale (a url()
	// may itself contain quotes that must not be masked independently of it).
	css = mask(cssPreservedCommentRE, css)
	css = mask(cssURLRE, css)
	css = mask(cssBlockCommentRE, css)
	css = mask(cssDQStringRE, css)
	css = mask(cssSQStringRE, css)
	return css, slots
}

func unmaskCSSNoise(css string, slots []cssNoiseSlot) string {
	for _, s := range slots {
		css = strings.ReplaceAll(css, s.token, s.original)
	}
	return css
}

// cssBlock is one top-level entity found by splitTopLevelCSSBlocks: either a
// qualified rule or an at-rule with a body ({prelude}{body}), or a bodyless
// at-rule statement (ends in ';', e.g. "@charset "UTF-8";").
type cssBlock struct {
	prelude  string
	body     string
	bodyless bool
}

// splitTopLevelCSSBlocks splits masked CSS text into top-level blocks, using
// brace-depth tracking so a block's body (which may itself contain nested
// braces, e.g. a @media block's rules) is captured whole rather than split.
func splitTopLevelCSSBlocks(css string) []cssBlock {
	var blocks []cssBlock
	i := 0
	preludeStart := 0
	for i < len(css) {
		switch css[i] {
		case '{':
			between := css[preludeStart:i]
			prelude := strings.TrimSpace(between)
			depth := 1
			j := i + 1
			for j < len(css) && depth > 0 {
				switch css[j] {
				case '{':
					depth++
				case '}':
					depth--
				}
				j++
			}
			body := ""
			if depth == 0 {
				body = css[i+1 : j-1]
			} else {
				// Unbalanced braces: treat the remainder as the body rather
				// than panic/crash — kept wholesale below, never filtered.
				body = css[i+1:]
				j = len(css)
			}
			if prelude != "" {
				blocks = append(blocks, cssBlock{prelude: prelude, body: body})
			}
			i = j
			preludeStart = j
		case ';':
			between := css[preludeStart : i+1]
			if strings.TrimSpace(between) != "" {
				blocks = append(blocks, cssBlock{prelude: strings.TrimSpace(between), bodyless: true})
			}
			i++
			preludeStart = i
		default:
			i++
		}
	}
	return blocks
}

// filterCSSBlocks recursively filters masked CSS text, dropping qualified
// rules whose selectors don't reference any token present in used, recursing
// into @media/@supports/@container bodies, and keeping every other at-rule
// (bodyless or not) wholesale.
func filterCSSBlocks(css string, used map[string]bool) string {
	blocks := splitTopLevelCSSBlocks(css)
	var out strings.Builder
	for _, b := range blocks {
		if b.bodyless {
			out.WriteString(b.prelude)
			out.WriteString("\n")
			continue
		}
		if strings.HasPrefix(b.prelude, "@") {
			if cssConditionalAtRuleRE.MatchString(b.prelude) {
				filteredBody := filterCSSBlocks(b.body, used)
				if strings.TrimSpace(filteredBody) == "" {
					continue // nothing inside survived: drop the whole conditional block
				}
				out.WriteString(b.prelude)
				out.WriteString("{")
				out.WriteString(filteredBody)
				out.WriteString("}\n")
				continue
			}
			// @font-face, @keyframes, @page, @layer, or anything unrecognized:
			// keep wholesale, unfiltered.
			out.WriteString(b.prelude)
			out.WriteString("{")
			out.WriteString(b.body)
			out.WriteString("}\n")
			continue
		}
		if selectorListIsUsed(b.prelude, used) {
			out.WriteString(b.prelude)
			out.WriteString("{")
			out.WriteString(b.body)
			out.WriteString("}\n")
		}
	}
	return out.String()
}

// selectorListIsUsed reports whether at least one comma-separated selector in
// selectorList has every class/id/tag token it references present in used. A
// selector with zero extractable tokens (":root", "*", a bare pseudo-class)
// is always considered used.
func selectorListIsUsed(selectorList string, used map[string]bool) bool {
	skeleton := cssBracketGroupRE.ReplaceAllString(selectorList, " ")
	skeleton = cssParenGroupRE.ReplaceAllString(skeleton, " ")
	for _, sel := range strings.Split(skeleton, ",") {
		if selectorIsUsed(sel, used) {
			return true
		}
	}
	return false
}

func selectorIsUsed(selector string, used map[string]bool) bool {
	required := map[string]bool{}
	for _, m := range cssClassTokenRE.FindAllStringSubmatch(selector, -1) {
		required["."+m[1]] = true
	}
	for _, m := range cssIDTokenRE.FindAllStringSubmatch(selector, -1) {
		required["#"+m[1]] = true
	}
	for _, m := range cssTagTokenRE.FindAllStringSubmatch(selector, -1) {
		required[strings.ToLower(m[1])] = true
	}
	for tok := range required {
		if !used[tok] {
			return false
		}
	}
	return true
}
