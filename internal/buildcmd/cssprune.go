package buildcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// cssPruneOptions controls per-page CSS pruning: dropping rules whose
// selectors don't match anything in the page's own rendered HTML, so an
// inlined <style> block carries only what that specific page needs instead
// of a shared stylesheet's full contents unconditionally. Opt-in via
// -prune-unused-css-per-page; see loadCSSSafelist for the safelist file.
type cssPruneOptions struct {
	enabled  bool
	safelist []string // substrings; a selector containing any of these is always kept
}

// pruneCSSForPage filters css down to the rules relevant to pageHTML. A rule
// survives if ANY of its comma-separated selectors is judged relevant (see
// selectorIsRelevant); a rule is dropped only when NONE of its selectors
// could possibly apply to this page.
//
// This is a conservative, token-presence match, not a real selector engine:
// it checks whether a selector's tag/class/id/attribute names appear
// anywhere in the page's HTML, not whether the actual DOM structure (nesting,
// combinators) would match. That asymmetry only ever causes under-pruning
// (keeping a rule that isn't really needed), never over-pruning — the unsafe
// direction — so it's deliberate.
//
// Classes/attributes a client-side script adds after page load (.active,
// [hidden], [aria-expanded], …) never appear in server-rendered HTML at all;
// pruning based on static presence alone would strip their rules and break
// the page's interactive states. Two things guard against that:
//
//  1. Referenced local JS (inline <script> bodies, and local <script src>
//     files resolved against assetsDir) is scanned for the same class="..."/
//     id="..." patterns as HTML, since a huge share of "dynamic" markup is a
//     site's own JS template-literal strings containing those names
//     literally (e.g. a catalog grid rendered client-side into an initially
//     empty container) — not actually unknowable, just not present in the
//     server-rendered HTML alone. See extractPageTokens.
//  2. opts.safelist is the escape hatch for what's left: classes/attributes
//     that are genuinely assembled at runtime (string concatenation,
//     framework-added state classes) rather than ever appearing as a literal
//     in the page's own HTML or JS. Any selector containing a safelisted
//     substring is always kept, regardless of what's in pageHTML/JS.
func pruneCSSForPage(css, pageHTML, assetsDir string, opts cssPruneOptions) string {
	if !opts.enabled || strings.TrimSpace(css) == "" {
		return css
	}

	protectedCSS, restore := protectCSSLiterals(css)
	tokens := extractPageTokens(pageHTML, assetsDir)
	statements := parseCSSStatements(protectedCSS)
	kept := filterCSSStatements(statements, tokens, opts.safelist)
	return restore(joinCSSStatements(kept))
}

// cssStatement is one top-level CSS construct: either a rule (selectorList {
// declarations }) or an at-rule, with or without a block.
type cssStatement struct {
	prelude  string // selector list, or "@media (...)", "@import url(...)", etc.
	body     string // raw contents between { and } — re-parsed recursively for @media/@supports
	hasBlock bool
}

// atRuleKind classifies an at-rule prelude for pruning purposes.
type atRuleKind int

const (
	atRuleNone       atRuleKind = iota // not an at-rule (a normal selector list)
	atRuleRecurse                      // @media/@supports/@container: filter its body like a nested stylesheet
	atRuleAlwaysKeep                   // @keyframes/@font-face/@page/@property/@layer/unknown: keep verbatim
)

var (
	atRuleRecurseRE = regexp.MustCompile(`(?i)^@(media|supports|container)\b`)
	atRuleRE        = regexp.MustCompile(`^@`)
	pseudoRE        = regexp.MustCompile(`::?[a-zA-Z-]+(\([^)]*\))?`)
	classTokenRE    = regexp.MustCompile(`\.[a-zA-Z0-9_-]+`)
	idTokenRE       = regexp.MustCompile(`#[a-zA-Z0-9_-]+`)
	attrTokenRE     = regexp.MustCompile(`\[\s*([a-zA-Z_:][-a-zA-Z0-9_:.]*)`)
	bracketStripRE  = regexp.MustCompile(`\[[^\]]*\]`)
	comboStripRE    = regexp.MustCompile(`[>+~*(),]`)
	tagWordRE       = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]*$`)
)

func classifyAtRule(prelude string) atRuleKind {
	if !atRuleRE.MatchString(prelude) {
		return atRuleNone
	}
	if atRuleRecurseRE.MatchString(prelude) {
		return atRuleRecurse
	}
	return atRuleAlwaysKeep
}

// cssStatementScanner holds the running state of parseCSSStatements' brace-
// depth scan. Extracted into its own type (with one method per character
// case) so each case's logic reads as a flat, low-complexity unit instead of
// nested ifs inside a switch inside a loop.
type cssStatementScanner struct {
	out        []cssStatement
	depth      int
	start      int
	preludeEnd int
}

// parseCSSStatements splits protected CSS (comments/strings/url() already
// replaced by brace-free placeholder tokens, see protectCSSLiterals) into
// top-level statements by tracking brace depth. Only depth-0 boundaries end a
// statement, so nested @media bodies stay intact as one statement's body,
// parsed recursively later.
func parseCSSStatements(css string) []cssStatement {
	s := &cssStatementScanner{preludeEnd: -1}
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{':
			s.openBrace(i)
		case '}':
			s.closeBrace(css, i)
		case ';':
			s.semicolon(css, i)
		}
	}
	if tail := strings.TrimSpace(css[s.start:]); tail != "" {
		// Trailing text with no terminating brace/semicolon (malformed, or
		// just whitespace already trimmed away) — keep verbatim rather than
		// silently drop content we can't classify.
		s.out = append(s.out, cssStatement{prelude: tail, hasBlock: false})
	}
	return s.out
}

func (s *cssStatementScanner) openBrace(i int) {
	if s.depth == 0 {
		s.preludeEnd = i
	}
	s.depth++
}

func (s *cssStatementScanner) closeBrace(css string, i int) {
	if s.depth > 0 {
		s.depth--
	}
	if s.depth != 0 || s.preludeEnd < 0 {
		return
	}
	s.out = append(s.out, cssStatement{
		prelude:  strings.TrimSpace(css[s.start:s.preludeEnd]),
		body:     css[s.preludeEnd+1 : i],
		hasBlock: true,
	})
	s.start = i + 1
	s.preludeEnd = -1
}

func (s *cssStatementScanner) semicolon(css string, i int) {
	if s.depth != 0 {
		return
	}
	stmt := strings.TrimSpace(css[s.start : i+1])
	if stmt != "" && stmt != ";" {
		s.out = append(s.out, cssStatement{prelude: stmt, hasBlock: false})
	}
	s.start = i + 1
}

func joinCSSStatements(stmts []cssStatement) string {
	var b strings.Builder
	for _, s := range stmts {
		if s.hasBlock {
			b.WriteString(s.prelude)
			b.WriteString(" {")
			b.WriteString(s.body)
			b.WriteString("}")
		} else {
			b.WriteString(s.prelude)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func filterCSSStatements(stmts []cssStatement, tokens htmlTokens, safelist []string) []cssStatement {
	kept := make([]cssStatement, 0, len(stmts))
	for _, s := range stmts {
		if filtered, ok := filterOneStatement(s, tokens, safelist); ok {
			kept = append(kept, filtered)
		}
	}
	return kept
}

// filterOneStatement decides whether a single statement survives pruning,
// returning the (possibly rewritten, for @media/@supports/@container) kept
// statement and true, or the zero value and false if it's dropped entirely.
func filterOneStatement(s cssStatement, tokens htmlTokens, safelist []string) (cssStatement, bool) {
	if !s.hasBlock {
		return s, true // bodyless statement (@import/@charset/malformed tail) — always keep
	}

	switch classifyAtRule(s.prelude) {
	case atRuleRecurse:
		inner := filterCSSStatements(parseCSSStatements(s.body), tokens, safelist)
		if len(inner) == 0 {
			return cssStatement{}, false
		}
		s.body = joinCSSStatements(inner)
		return s, true
	case atRuleAlwaysKeep:
		return s, true
	}

	kept := make([]string, 0, 1)
	for _, sel := range splitSelectorList(s.prelude) {
		if sel == "" {
			continue
		}
		if selectorIsRelevant(sel, tokens, safelist) {
			kept = append(kept, sel)
		}
	}
	if len(kept) == 0 {
		return cssStatement{}, false
	}
	s.prelude = strings.Join(kept, ", ")
	return s, true
}

// splitSelectorList splits a selector list on top-level commas, respecting
// paren depth so ":not(.a, .b)" isn't split into two selectors.
func splitSelectorList(prelude string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, r := range prelude {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(prelude[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(prelude[start:]))
	return parts
}

// selectorIsRelevant reports whether a single (non-comma-separated) selector
// could plausibly apply to this page. See pruneCSSForPage's doc comment for
// the safety reasoning.
func selectorIsRelevant(sel string, tokens htmlTokens, safelist []string) bool {
	if containsAny(sel, safelist) {
		return true
	}

	tags, classes, ids, attrs := selectorTokens(sel)
	if len(tags) == 0 && len(classes) == 0 && len(ids) == 0 && len(attrs) == 0 {
		return true // nothing extractable (e.g. a bare pseudo-class like ":hover") — err safe
	}
	if containsRootTag(tags) {
		return true // document root/body — always relevant
	}
	return allTokensPresent(tags, tokens.tags) &&
		allTokensPresent(classes, tokens.classes) &&
		allTokensPresent(ids, tokens.ids) &&
		allTokensPresent(attrs, tokens.attrs)
}

func containsRootTag(tags []string) bool {
	for _, t := range tags {
		if t == "html" || t == "body" {
			return true
		}
	}
	return false
}

// allTokensPresent reports whether every token in toks is a key in present.
// Shared by selectorIsRelevant's four token-kind checks (tags, classes, ids,
// attrs) — all four reduce to the same "is this required token reachable on
// the page" test once both sides are normalized (lowercase) the same way.
func allTokensPresent(toks []string, present map[string]bool) bool {
	for _, t := range toks {
		if !present[t] {
			return false
		}
	}
	return true
}

// selectorTokens extracts the simple-selector tokens (tag names, .classes,
// #ids, [attrs]) from a single compound/combinator selector, after stripping
// pseudo-classes/pseudo-elements (their dynamic state can't be observed in
// static HTML, so they're never part of the relevance check — only what they
// attach to is).
func selectorTokens(sel string) (tags, classes, ids, attrs []string) {
	sel = pseudoRE.ReplaceAllString(sel, " ")

	for _, m := range classTokenRE.FindAllString(sel, -1) {
		classes = append(classes, strings.TrimPrefix(m, "."))
	}
	for _, m := range idTokenRE.FindAllString(sel, -1) {
		ids = append(ids, strings.TrimPrefix(m, "#"))
	}
	for _, m := range attrTokenRE.FindAllStringSubmatch(sel, -1) {
		// Lowercased at extraction so allTokensPresent's plain map lookup
		// matches tokens.attrs, which is populated lowercase too (HTML
		// attribute names are case-insensitive).
		attrs = append(attrs, strings.ToLower(m[1]))
	}

	stripped := classTokenRE.ReplaceAllString(sel, " ")
	stripped = idTokenRE.ReplaceAllString(stripped, " ")
	stripped = bracketStripRE.ReplaceAllString(stripped, " ")
	stripped = comboStripRE.ReplaceAllString(stripped, " ")
	for _, word := range strings.Fields(stripped) {
		if tagWordRE.MatchString(word) {
			tags = append(tags, strings.ToLower(word))
		}
	}
	return
}

func containsAny(s string, substrings []string) bool {
	for _, sub := range substrings {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ── String/comment/url() protection for brace-depth-aware splitting ─────────

// protectCSSLiterals replaces block comments, url(...) content, and quoted
// string literals with brace/semicolon/comma-free placeholder tokens, so
// parseCSSStatements's brace-depth counting can't be confused by structural
// characters inside a comment or a string (e.g. content: "a: {b}"). Returns
// the protected text and a restore function that substitutes the originals
// back in; call restore on the final reassembled output, once, after pruning.
func protectCSSLiterals(css string) (string, func(string) string) {
	type slot struct{ token, original string }
	var slots []slot
	protect := func(re *regexp.Regexp, s string) string {
		return re.ReplaceAllStringFunc(s, func(m string) string {
			tok := cssProtectTokenPrefix + itoa(len(slots)) + cssProtectTokenSuffix
			slots = append(slots, slot{tok, m})
			return tok
		})
	}

	protected := css
	protected = protect(cssBlockCommentRE, protected)
	protected = protect(cssURLRE, protected)
	protected = protect(cssDQStringRE, protected)
	protected = protect(cssSQStringRE, protected)

	restore := func(s string) string {
		// Reverse order: later protections (strings) were applied to text
		// that no longer contains earlier placeholders, so restoring in any
		// order is actually safe here — but reverse mirrors protect() and
		// costs nothing.
		for i := len(slots) - 1; i >= 0; i-- {
			s = strings.ReplaceAll(s, slots[i].token, slots[i].original)
		}
		return s
	}
	return protected, restore
}

// Not a credential — a brace/comma/paren-free marker wrapping a protected
// CSS literal's placeholder index (see protectCSSLiterals). The leading NUL
// bytes keep it from ever colliding with real CSS text.
//
//nolint:gosec // G101 false positive: high-entropy string, not a secret
const (
	cssProtectTokenPrefix = "\x00CSSPRUNEPH"
	cssProtectTokenSuffix = "\x00"
)

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// ── HTML token extraction ────────────────────────────────────────────────────

// htmlTokens is the set of tag names, class names, id names, and attribute
// names present anywhere in a rendered page's HTML — the vocabulary
// selectorIsRelevant checks CSS selectors against.
type htmlTokens struct {
	tags    map[string]bool
	classes map[string]bool
	ids     map[string]bool
	attrs   map[string]bool
}

var (
	openTagRE   = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9-]*)((?:\s[^<>]*)?)/?>`)
	classAttrRE = regexp.MustCompile(`\bclass\s*=\s*"([^"]*)"|\bclass\s*=\s*'([^']*)'`)
	idAttrRE    = regexp.MustCompile(`\bid\s*=\s*"([^"]*)"|\bid\s*=\s*'([^']*)'`)
	attrNameRE  = regexp.MustCompile(`([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*(?:=|[\s/>]|$)`)
)

// extractHTMLTokens scans rendered page HTML with a tag-level regex (not a
// full HTML parser — consistent with the rest of this package's asset
// scanning, see fingerprint.go) to build the token vocabulary
// selectorIsRelevant matches CSS selectors against. Content inside
// <script>/<style> tags is not excluded, but those don't contain HTML tag
// syntax in a way that would spuriously match a CSS selector token, so this
// is safe in the "never cause over-pruning" direction the whole feature
// leans on.
func extractHTMLTokens(html string) htmlTokens {
	t := htmlTokens{
		tags:    map[string]bool{},
		classes: map[string]bool{},
		ids:     map[string]bool{},
		attrs:   map[string]bool{},
	}
	for _, m := range openTagRE.FindAllStringSubmatch(html, -1) {
		t.tags[strings.ToLower(m[1])] = true
		attrText := m[2]
		addClassTokens(attrText, t.classes)
		addIDToken(attrText, t.ids)
		addAttrNameTokens(attrText, t.attrs)
	}
	return t
}

func addClassTokens(attrText string, classes map[string]bool) {
	for _, cm := range classAttrRE.FindAllStringSubmatch(attrText, -1) {
		classVal := cm[1]
		if classVal == "" {
			classVal = cm[2]
		}
		for _, c := range strings.Fields(classVal) {
			classes[c] = true
		}
	}
}

func addIDToken(attrText string, ids map[string]bool) {
	for _, im := range idAttrRE.FindAllStringSubmatch(attrText, -1) {
		idVal := im[1]
		if idVal == "" {
			idVal = im[2]
		}
		if idVal != "" {
			ids[idVal] = true
		}
	}
}

func addAttrNameTokens(attrText string, attrs map[string]bool) {
	for _, am := range attrNameRE.FindAllStringSubmatch(attrText, -1) {
		attrs[strings.ToLower(am[1])] = true
	}
}

var (
	scriptTagInnerRE = regexp.MustCompile(`(?is)<script([^>]*)>(.*?)</script>`)
	scriptSrcAttrRE  = regexp.MustCompile(`(?i)\bsrc\s*=\s*["']([^"']+)["']`)
	// jsHyphenAttrRE finds HTML-attribute-shaped tokens (data-foo=, aria-bar=)
	// inside arbitrary JS source text. Hyphenated identifiers are syntactically
	// impossible as plain JS variable/property names (obj.data-tone is a
	// subtraction expression, not valid property access), so this is a
	// precise, low-false-positive signal that we're looking at an HTML
	// attribute name embedded in a template-literal string — unlike attrNameRE,
	// which would also match ordinary JS identifiers and is only applied
	// within a single HTML tag's already-bounded attribute text.
	jsHyphenAttrRE = regexp.MustCompile(`\b([a-zA-Z][a-zA-Z0-9]*-[a-zA-Z0-9-]*)\s*=`)
	// quotedIdentifierRE extracts a class-name-shaped token from inside a
	// quoted JS string literal fragment, e.g. 'fallback' or "is-open".
	quotedIdentifierRE = regexp.MustCompile(`'([a-zA-Z_-][a-zA-Z0-9_-]*)'|"([a-zA-Z_-][a-zA-Z0-9_-]*)"`)
)

// extractPageTokens builds the full token vocabulary selectorIsRelevant
// checks CSS selectors against: extractHTMLTokens's static-HTML scan, plus
// every class="..."/id="..." found in the page's own JS — both inline
// <script> bodies and local <script src> files resolved against assetsDir.
//
// This matters because a large share of a page's real content is often
// client-rendered: a catalog grid, a cart, a modal — built by JS from a data
// source into a container that starts empty in the server-rendered HTML. The
// class names for that content aren't unknowable or "dynamic" in the sense
// that matters here; they're typically literal strings in the site's own JS
// template-literal markup (“ `<article class="card">` “), just not present
// in pageHTML itself. Scanning that JS source catches them without requiring
// every one declared in the safelist by hand — the safelist stays for
// genuinely runtime-assembled class/attribute names (string concatenation,
// a framework's own state classes).
func extractPageTokens(pageHTML, assetsDir string) htmlTokens {
	t := extractHTMLTokens(pageHTML)

	seen := map[string]bool{}
	for _, m := range scriptTagInnerRE.FindAllStringSubmatch(pageHTML, -1) {
		attrs, body := m[1], m[2]
		if src := scriptSrcAttrRE.FindStringSubmatch(attrs); src != nil {
			if isExternalRef(src[1]) {
				continue
			}
			data, cleanRef, err := readAsset(assetsDir, src[1])
			if err != nil || seen[cleanRef] {
				continue
			}
			seen[cleanRef] = true
			scanTextForClassIDAttrTokens(string(data), &t)
			continue
		}
		scanTextForClassIDAttrTokens(body, &t)
	}
	return t
}

// classTokenCandidateRE pulls a plausible class-name token out of a class
// attribute's value text. Used both on the whole value (the common case: a
// plain space-separated class list) and, separately, on each whitespace
// field of it — a template literal like `class="card ${open ? 'is-open' :
// 'fallback'}"` has its class-list text polluted with JS expression syntax
// (${, ?, :, quotes), so a plain strings.Fields split yields unusable tokens
// like "${p.logo" or "'fallback'}". Re-scanning each field for this pattern
// recovers the quoted alternatives (is-open, fallback) a ternary expression
// embeds, which is precise enough to be worth the extra pass: it only ever
// adds tokens (never removes one a plain split would have found), so it
// can't make matching less safe, only less incomplete.
var classTokenCandidateRE = regexp.MustCompile(`^[a-zA-Z_-][a-zA-Z0-9_-]*$`)

// scanTextForClassIDAttrTokens applies the same class="..."/id="..." extraction
// extractHTMLTokens uses on HTML tag attributes to an arbitrary text blob
// (JS source), plus tag-name and the more conservative jsHyphenAttrRE
// attribute-name extraction. See extractPageTokens's doc comment for why.
func scanTextForClassIDAttrTokens(text string, t *htmlTokens) {
	for _, tm := range openTagRE.FindAllStringSubmatch(text, -1) {
		t.tags[strings.ToLower(tm[1])] = true
	}
	addClassTokensFromJS(text, t.classes)
	addIDToken(text, t.ids)
	for _, am := range jsHyphenAttrRE.FindAllStringSubmatch(text, -1) {
		t.attrs[strings.ToLower(am[1])] = true
	}
}

func addClassTokensFromJS(text string, classes map[string]bool) {
	for _, cm := range classAttrRE.FindAllStringSubmatch(text, -1) {
		classVal := cm[1]
		if classVal == "" {
			classVal = cm[2]
		}
		for _, c := range strings.Fields(classVal) {
			addClassCandidate(c, classes)
		}
	}
}

// addClassCandidate adds c to classes if it's already a clean token on its
// own, or — when JS expression syntax has leaked into it (e.g. "${p.logo" or
// "'fallback'}" from a template-literal ternary) — pulls any quoted
// identifier-shaped substrings out of it instead. See classTokenCandidateRE's
// doc comment for why this recovery pass is worth it.
func addClassCandidate(c string, classes map[string]bool) {
	if classTokenCandidateRE.MatchString(c) {
		classes[c] = true
		return
	}
	for _, qm := range quotedIdentifierRE.FindAllStringSubmatch(c, -1) {
		ident := qm[1]
		if ident == "" {
			ident = qm[2]
		}
		if ident != "" {
			classes[ident] = true
		}
	}
}

// ── Safelist loading ──────────────────────────────────────────────────────

// cssSafelistFile is the on-disk shape of a css-safelist.yaml: a flat list of
// selector substrings a selector is checked against (see selectorIsRelevant)
// to decide whether -prune-unused-css-per-page always keeps it. Typically
// classes/attributes a client-side script adds or removes after page load
// (.active, [hidden], .is-open, [aria-expanded]), which never appear in
// server-rendered HTML for a static-presence check to find.
type cssSafelistFile struct {
	AlwaysKeep []string `yaml:"always_keep"`
}

// loadCSSSafelist reads a css-safelist.yaml file. A missing file is not an
// error — it just means an empty safelist (every selector is judged on
// static-HTML presence alone), matching this package's existing
// degrade-gracefully convention for optional site files.
func loadCSSSafelist(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading css safelist %s: %w", path, err)
	}
	var doc cssSafelistFile
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing css safelist %s: %w", path, err)
	}
	return doc.AlwaysKeep, nil
}

// resolveCSSSafelistPath returns the safelist path to load: the explicit
// flag override if set, otherwise the conventional
// <website-root>/src/data/css-safelist.yaml location (dataDir is
// filepath.Dir(templatesDir), matching how site.yaml/site.contract.yaml are
// already found elsewhere in this package).
func resolveCSSSafelistPath(flagPath, templatesDir string) string {
	if flagPath != "" {
		return flagPath
	}
	return filepath.Join(filepath.Dir(templatesDir), "data", "css-safelist.yaml")
}
