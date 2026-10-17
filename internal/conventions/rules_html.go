package conventions

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Structural rules over built pages: landmarks (R2), forms (R3), the LCP image
// (R1), the reset and font-family subset (R6/R8) and the SEO head set (R10).
//
// All of them are DomainOutput — they run against a built dist/ tree, never
// against src/. A template partial has no <main> and no <title> by design, so a
// rule that read source would report a violation on every correctly built site,
// and the gate would be deleted within a week.

// selectsHTML is the file filter every rule in this file shares.
func selectsHTML(rel string) bool {
	return strings.EqualFold(filepath.Ext(rel), ".html")
}

// ---------------------------------------------------------------- R2 landmarks

const ruleNameSingleMainLandmark = "single-main-landmark"

var ruleSingleMainLandmark = Rule{
	Name:   ruleNameSingleMainLandmark,
	Domain: DomainOutput,
	Doc: "Every page has exactly one <main>. It is the target of the skip link and of " +
		"the \"jump to main content\" command in every screen reader; zero means there is " +
		"nothing to jump to, and two means the command lands somewhere arbitrary. " +
		"Unlike nav and footer this needs no exception anywhere — a page with content " +
		"has a main region even when it has no chrome.",
	Selects: selectsHTML,
	Scan: func(rel, contents string) []Finding {
		tags := tokenizeHTML(contents)
		var opens []htmlTag
		for _, t := range tags {
			if !t.Closing && t.Name == "main" {
				opens = append(opens, t)
			}
		}
		switch {
		case len(opens) == 0:
			return []Finding{{
				Rule: ruleNameSingleMainLandmark, File: rel,
				Detail: "no <main> element — wrap the page's primary content in <main> so the skip " +
					"link and screen-reader landmark navigation have a target",
			}}
		case len(opens) > 1:
			findings := make([]Finding, 0, len(opens)-1)
			for _, t := range opens[1:] {
				findings = append(findings, Finding{
					Rule: ruleNameSingleMainLandmark, File: rel, Line: t.Line,
					Detail: fmt.Sprintf("second <main> on the page (the first is at line %d) — "+
						"exactly one is allowed; make the others <section> or <aside>", opens[0].Line),
				})
			}
			return findings
		}
		return nil
	},
}

const ruleNameChromeLandmarks = "page-chrome-landmarks"

var ruleChromeLandmarks = Rule{
	Name:   ruleNameChromeLandmarks,
	Domain: DomainOutput,
	Doc: "A page carrying site chrome declares it semantically: at least one <nav> and " +
		"at least one <footer>. Separate from single-main-landmark precisely because " +
		"these two DO have legitimate exceptions — a login screen, an app shell or an " +
		"embedded view may ship no chrome at all. Declare those pages as exceptions " +
		"with the reason; do not weaken the rule.",
	Selects: selectsHTML,
	Scan: func(rel, contents string) []Finding {
		tags := tokenizeHTML(contents)
		present := map[string]bool{}
		for _, t := range tags {
			if !t.Closing {
				present[t.Name] = true
			}
		}
		var findings []Finding
		for _, want := range []struct{ name, why string }{
			{"nav", "site navigation belongs in a <nav> landmark so it can be skipped and jumped to"},
			{"footer", "the page's footer belongs in a <footer> landmark"},
		} {
			if !present[want.name] {
				findings = append(findings, Finding{
					Rule: ruleNameChromeLandmarks, File: rel,
					Detail: fmt.Sprintf("no <%s> element — %s. If this page deliberately ships no "+
						"chrome (auth screen, app shell, embed), declare it as an exception with that reason",
						want.name, want.why),
				})
			}
		}
		return findings
	},
}

// -------------------------------------------------------------------- R3 forms

// formControls are the elements that take a user value and therefore need a
// name. <button> is excluded: its text content is its name.
var formControls = map[string]bool{"input": true, "select": true, "textarea": true}

// unnamedInputTypes are input types that carry no user-visible value and need
// no label.
var unnamedInputTypes = map[string]bool{
	"hidden": true, "submit": true, "button": true, "reset": true, "image": true,
}

// control is one form control found on a page, with the label context it was
// found in.
type control struct {
	tag htmlTag
	// insideLabel is true when the control is nested in a <label>, which names
	// it just as well as a for=/id pair. Finding this requires tracking nesting,
	// which is the reason this package tokenizes rather than greps.
	insideLabel bool
	// typ is the lowercased input type, defaulting to "text".
	typ string
}

// collectControls walks the tag stream once and returns every labelable
// control.
func collectControls(tags []htmlTag) []control {
	var out []control
	labelDepth := 0
	for _, t := range tags {
		if t.Name == "label" {
			if t.Closing {
				if labelDepth > 0 {
					labelDepth--
				}
			} else {
				labelDepth++
			}
			continue
		}
		if t.Closing || !formControls[t.Name] {
			continue
		}
		typ := strings.ToLower(t.AttrValue("type"))
		if typ == "" {
			typ = "text"
		}
		if t.Name == "input" && unnamedInputTypes[typ] {
			continue
		}
		if t.HasAttr("hidden") {
			continue // not rendered at all
		}
		out = append(out, control{tag: t, insideLabel: labelDepth > 0, typ: typ})
	}
	return out
}

// isHiddenFromAssistiveTech reports whether a control was deliberately removed
// from the accessibility tree AND from the tab order, which is the spam-honeypot
// pattern: aria-hidden="true" plus tabindex="-1", visually hidden by CSS.
//
// Found by running these rules against petlook-website, where
// `name="contactHoneypot"` is exactly that. Requiring an accessible name on an
// element that is correctly hidden from assistive tech is incoherent, and it is
// the kind of false positive whose natural fix is to stop enabling the rule.
//
// aria-hidden WITHOUT tabindex="-1" is not excused: ARIA forbids aria-hidden on
// a focusable element, so that combination is reported as its own defect rather
// than quietly skipped.
func isHiddenFromAssistiveTech(c control) bool {
	return strings.EqualFold(c.tag.AttrValue("aria-hidden"), "true") &&
		c.tag.AttrValue("tabindex") == "-1"
}

// hasProgrammaticName reports whether the control has an accessible name, and
// which mechanism provided it.
func hasProgrammaticName(c control, labelFor, ids map[string]bool) (string, bool) {
	if id := c.tag.AttrValue("id"); id != "" && labelFor[id] {
		return "label[for]", true
	}
	if c.insideLabel {
		return "wrapping label", true
	}
	if c.tag.AttrValue("aria-label") != "" {
		return "aria-label", true
	}
	if v := c.tag.AttrValue("aria-labelledby"); v != "" {
		for _, ref := range tokenRefs(v) {
			if ids[ref] {
				return "aria-labelledby", true
			}
		}
		return "", false // dangling reference: names nothing
	}
	return "", false
}

const ruleNameLabeledFormControls = "labeled-form-controls"

var ruleLabeledFormControls = Rule{
	Name:   ruleNameLabeledFormControls,
	Domain: DomainOutput,
	Doc: "Every form control has a programmatic name: a <label for=\"id\">, a <label> that " +
		"wraps it, a non-empty aria-label, or an aria-labelledby that points at an " +
		"element that exists. A placeholder is not a name — it vanishes the moment " +
		"someone types, is not reliably exposed as the control's name, and leaves " +
		"voice control with nothing to say. A dangling label[for] or aria-labelledby " +
		"is reported too: it looks correct in review and names nothing at runtime.",
	Selects: selectsHTML,
	Scan: func(rel, contents string) []Finding {
		tags := tokenizeHTML(contents)
		ids := elementIDs(tags)
		labelFor := labelTargets(tags)

		var findings []Finding
		for _, c := range collectControls(tags) {
			if isHiddenFromAssistiveTech(c) {
				continue
			}
			if strings.EqualFold(c.tag.AttrValue("aria-hidden"), "true") {
				findings = append(findings, Finding{
					Rule: ruleNameLabeledFormControls, File: rel, Line: c.tag.Line,
					Detail: fmt.Sprintf("%s has aria-hidden=\"true\" but is still focusable — ARIA "+
						"forbids that: keyboard users reach a control their screen reader cannot "+
						"describe. Add tabindex=\"-1\" if it is a honeypot, or drop the aria-hidden "+
						"and give it a label", describeControl(c)),
				})
				continue
			}
			if _, ok := hasProgrammaticName(c, labelFor, ids); ok {
				continue
			}
			what := describeControl(c)
			detail := fmt.Sprintf("%s has no programmatic name — give it a <label for>, wrap it "+
				"in a <label>, or set aria-label", what)
			if c.tag.AttrValue("placeholder") != "" {
				detail = fmt.Sprintf("%s is labelled only by its placeholder %q — a placeholder is "+
					"not a name: it disappears on input and voice control cannot address the field. "+
					"Add a <label for> (visually hidden if the design needs it) or aria-label",
					what, c.tag.AttrValue("placeholder"))
			}
			if v := c.tag.AttrValue("aria-labelledby"); v != "" {
				detail = fmt.Sprintf("%s has aria-labelledby=%q but no element on the page has that "+
					"id — the control is unnamed at runtime", what, v)
			}
			findings = append(findings, Finding{
				Rule: ruleNameLabeledFormControls, File: rel, Line: c.tag.Line, Detail: detail,
			})
		}

		// A label pointing at nothing is the mirror failure, and just as invisible
		// in review.
		for _, t := range tags {
			if t.Closing || t.Name != "label" {
				continue
			}
			f := t.AttrValue("for")
			if f != "" && !ids[f] {
				findings = append(findings, Finding{
					Rule: ruleNameLabeledFormControls, File: rel, Line: t.Line,
					Detail: fmt.Sprintf("<label for=%q> points at an id that does not exist on this "+
						"page — the label names nothing and clicking it focuses nothing", f),
				})
			}
		}
		return findings
	},
}

// describeControl names a control the way someone scanning a build log needs
// it: enough to find it in the template.
func describeControl(c control) string {
	what := "<" + c.tag.Name + ">"
	if c.tag.Name == "input" {
		what = fmt.Sprintf("<input type=%q>", c.typ)
	}
	switch {
	case c.tag.AttrValue("id") != "":
		return fmt.Sprintf("%s#%s", what, c.tag.AttrValue("id"))
	case c.tag.AttrValue("name") != "":
		return fmt.Sprintf("%s[name=%s]", what, c.tag.AttrValue("name"))
	}
	return what
}

const ruleNameFormAutocomplete = "form-autocomplete"

// autocompleteExpectations maps a control we can identify with confidence to
// the autocomplete token a browser and a password manager need.
//
// Only high-confidence signals are listed. The cost asymmetry decides the
// design: a missed field is a small loss, while one false positive on a field
// that must NOT autofill teaches everyone to distrust the gate.
var autocompleteExpectations = []struct {
	// typ matches the input type exactly when non-empty.
	typ string
	// nameLike matches the control's name or id, lowercased, exactly.
	nameLike []string
	expect   string
}{
	{typ: "email", expect: "email"},
	{typ: "tel", expect: "tel"},
	{typ: "password", expect: "current-password or new-password"},
	{nameLike: []string{"name", "fullname", "full_name", "full-name"}, expect: "name"},
	{nameLike: []string{"firstname", "first_name", "first-name", "givenname"}, expect: "given-name"},
	{nameLike: []string{"lastname", "last_name", "last-name", "surname", "familyname"}, expect: "family-name"},
	{nameLike: []string{"phone", "telephone", "mobile"}, expect: "tel"},
	{nameLike: []string{"address", "street", "address1", "addressline1"}, expect: "street-address"},
	{nameLike: []string{"city", "town"}, expect: "address-level2"},
	{nameLike: []string{"zip", "zipcode", "postcode", "postalcode", "postal_code"}, expect: "postal-code"},
	{nameLike: []string{"country"}, expect: "country-name"},
	{nameLike: []string{"organization", "company"}, expect: "organization"},
}

var ruleFormAutocomplete = Rule{
	Name:   ruleNameFormAutocomplete,
	Domain: DomainOutput,
	Doc: "Identity fields (email, phone, password, name, address) carry an autocomplete " +
		"token, so browsers and password managers can fill them and people do not retype " +
		"an address on a phone. WCAG 1.3.5 requires it. Only fields identifiable with " +
		"confidence from their type or name are checked, and any non-empty autocomplete " +
		"satisfies the rule: whether the token is the RIGHT one is a judgement this gate " +
		"deliberately leaves to review rather than guess at.",
	Selects: selectsHTML,
	Scan: func(rel, contents string) []Finding {
		var findings []Finding
		for _, c := range collectControls(tokenizeHTML(contents)) {
			if c.tag.HasAttr("autocomplete") || isHiddenFromAssistiveTech(c) {
				continue
			}
			expect, ok := expectedAutocomplete(c)
			if !ok {
				continue
			}
			findings = append(findings, Finding{
				Rule: ruleNameFormAutocomplete, File: rel, Line: c.tag.Line,
				Detail: fmt.Sprintf("%s has no autocomplete attribute — add autocomplete=%q "+
					"(WCAG 1.3.5) so the field can be autofilled", describeControl(c), expect),
			})
		}
		return findings
	},
}

func expectedAutocomplete(c control) (string, bool) {
	identity := strings.ToLower(c.tag.AttrValue("name"))
	if identity == "" {
		identity = strings.ToLower(c.tag.AttrValue("id"))
	}
	for _, e := range autocompleteExpectations {
		if e.typ != "" && c.typ == e.typ {
			return e.expect, true
		}
		for _, n := range e.nameLike {
			if identity == n {
				return e.expect, true
			}
		}
	}
	return "", false
}

const ruleNameFormErrorWiring = "form-error-wiring"

var ruleFormErrorWiring = Rule{
	Name:   ruleNameFormErrorWiring,
	Domain: DomainOutput,
	Doc: "A required control points at a describing element with aria-describedby, so the " +
		"error or hint for that field has somewhere to be announced from — without it a " +
		"screen-reader user hears \"invalid entry\" and nothing about which rule was broken. " +
		"Any aria-describedby that names a missing id is reported as well.\n" +
		"Deliberately NOT required: a static aria-invalid. It is a runtime state, and a " +
		"page that ships aria-invalid=\"false\" on every field has added markup, not " +
		"accessibility. Checking that the client sets it belongs to the rendered tier.",
	Selects: selectsHTML,
	Scan: func(rel, contents string) []Finding {
		tags := tokenizeHTML(contents)
		ids := elementIDs(tags)

		var findings []Finding
		for _, c := range collectControls(tags) {
			if isHiddenFromAssistiveTech(c) {
				continue
			}
			describedBy := c.tag.AttrValue("aria-describedby")
			if describedBy != "" {
				for _, ref := range tokenRefs(describedBy) {
					if !ids[ref] {
						findings = append(findings, Finding{
							Rule: ruleNameFormErrorWiring, File: rel, Line: c.tag.Line,
							Detail: fmt.Sprintf("%s has aria-describedby=%q but no element with id %q "+
								"exists — nothing is announced", describeControl(c), describedBy, ref),
						})
					}
				}
				continue
			}
			if !isRequiredControl(c) {
				continue
			}
			findings = append(findings, Finding{
				Rule: ruleNameFormErrorWiring, File: rel, Line: c.tag.Line,
				Detail: fmt.Sprintf("%s is required but has no aria-describedby — add an error/hint "+
					"element and reference its id, so the field's validation message has somewhere "+
					"to be announced from", describeControl(c)),
			})
		}
		return findings
	},
}

func isRequiredControl(c control) bool {
	return c.tag.HasAttr("required") || strings.EqualFold(c.tag.AttrValue("aria-required"), "true")
}

// ------------------------------------------------------------- R1 LCP image

const ruleNameLCPImageNotLazy = "lcp-image-not-lazy"

var ruleLCPImageNotLazy = Rule{
	Name:   ruleNameLCPImageNotLazy,
	Domain: DomainOutput,
	Doc: "The page's first <img> is never loading=\"lazy\". A lazy first image is the " +
		"classic Largest-Contentful-Paint regression: the browser will not even request " +
		"it until layout has run, so the metric measures the round trip twice. The " +
		"compiler's own default no longer lazies it (and adds fetchpriority=\"high\"), so " +
		"a violation here means the attribute was hand-written — which the compiler " +
		"respects rather than silently overrides. First-img is a heuristic for the LCP " +
		"candidate; a page where it is genuinely wrong declares an exception.",
	Selects: selectsHTML,
	Scan: func(rel, contents string) []Finding {
		for _, t := range tokenizeHTML(contents) {
			if t.Closing || t.Name != "img" {
				continue
			}
			if strings.EqualFold(t.AttrValue("loading"), "lazy") {
				return []Finding{{
					Rule: ruleNameLCPImageNotLazy, File: rel, Line: t.Line,
					Detail: "the page's first <img> declares loading=\"lazy\" — delay it and the " +
						"Largest Contentful Paint waits on a request the browser could have started " +
						"immediately. Remove the attribute (the compiler leaves the first image eager " +
						"and marks it fetchpriority=\"high\"), or use loading=\"eager\" to say so explicitly",
				}}
			}
			return nil // first image found and it is not lazy
		}
		return nil
	},
}

// --------------------------------------------------------- R6/R8 first CSS

// firstPageCSS returns the first stylesheet a page loads: the content of its
// first <style> block, or the first local <link rel="stylesheet">, whichever
// comes first in document order. The second return value names the source for
// the finding; the third is false when the page loads no local CSS at all.
//
// External stylesheets (a Google Fonts href) are skipped rather than failed:
// they are not where a reset lives, and we cannot read them.
func firstPageCSS(contents string, tree Tree) (css string, source string, ok bool) {
	tags := tokenizeHTML(contents)
	for i, t := range tags {
		if t.Closing {
			continue
		}
		switch t.Name {
		case "style":
			if body, found := rawTextBetween(contents, tags, i); found && strings.TrimSpace(body) != "" {
				return body, "the page's first inline <style> block", true
			}
		case "link":
			if !strings.Contains(strings.ToLower(t.AttrValue("rel")), "stylesheet") {
				continue
			}
			href := t.AttrValue("href")
			if href == "" || isExternalHref(href) {
				continue
			}
			if body, found := tree.ReadFile(href); found {
				return body, href, true
			}
		}
	}
	return "", "", false
}

// rawTextBetween returns the text between tags[i] (a start tag) and its end
// tag. It is used for <style>, whose content the tokenizer skips over.
func rawTextBetween(contents string, tags []htmlTag, i int) (string, bool) {
	start := tags[i].Offset
	gt := strings.IndexByte(contents[start:], '>')
	if gt < 0 {
		return "", false
	}
	from := start + gt + 1
	for _, t := range tags[i+1:] {
		if t.Closing && t.Name == tags[i].Name {
			if t.Offset < from {
				return "", false
			}
			return contents[from:t.Offset], true
		}
	}
	return "", false
}

func isExternalHref(href string) bool {
	h := strings.ToLower(href)
	return strings.HasPrefix(h, "http://") || strings.HasPrefix(h, "https://") ||
		strings.HasPrefix(h, "//") || strings.HasPrefix(h, "data:")
}

const ruleNameCSSResetPresent = "css-reset-present"

// resetRequirements are the four reset rules the fleet's baseline calls core.
// Each is a (what we look for, how to say it) pair.
var resetRequirements = []struct {
	what  string
	check func(rules []cssRule) bool
	fix   string
}{
	{
		what: "box-sizing: border-box",
		check: func(rules []cssRule) bool {
			for _, r := range rules {
				if r.declaresAny("box-sizing:border-box") {
					return true
				}
			}
			return false
		},
		fix: "*, *::before, *::after { box-sizing: border-box } — without it padding is added " +
			"to every declared width and no layout arithmetic in the sheet is reliable",
	},
	{
		what: "a body margin reset",
		check: func(rules []cssRule) bool {
			for _, r := range rules {
				if selectorMentions(r.Selector, "body", "html") &&
					r.declaresAny("margin:0", "margin-block:0", "margin:0;") {
					return true
				}
			}
			return false
		},
		fix: "body { margin: 0 } — the user-agent's 8px body margin is the one px value " +
			"no amount of rem sizing removes",
	},
	{
		what: "responsive media defaults",
		check: func(rules []cssRule) bool {
			for _, r := range rules {
				if !selectorMentions(r.Selector, "img", "picture", "video", "canvas", "svg") {
					continue
				}
				if r.declaresAny("max-width:100%") && r.declaresAny("display:block") {
					return true
				}
			}
			return false
		},
		fix: "img, picture, video { display: block; max-width: 100% } — an image wider than " +
			"its container is the most common source of horizontal scroll on a phone",
	},
	{
		what: "form controls inheriting typography",
		check: func(rules []cssRule) bool {
			for _, r := range rules {
				if selectorMentions(r.Selector, "input", "button", "select", "textarea") &&
					r.declaresAny("font:inherit", "font-family:inherit") {
					return true
				}
			}
			return false
		},
		fix: "input, button, select, textarea { font: inherit } — otherwise every control " +
			"renders in the platform's own font at the platform's own size",
	},
}

var ruleCSSResetPresent = Rule{
	Name:   ruleNameCSSResetPresent,
	Domain: DomainOutput,
	Doc: "The first stylesheet a page loads contains the core reset: border-box sizing, a " +
		"body margin reset, responsive media defaults, and form controls inheriting " +
		"typography. Presence only — no reset is injected, because every site here has " +
		"its own and a second one would fight it. Checked on the built page rather than " +
		"on src/, since that is the only place the real load order is visible.",
	Selects: selectsHTML,
	ScanTree: func(rel, contents string, tree Tree) []Finding {
		css, source, ok := firstPageCSS(contents, tree)
		if !ok {
			return []Finding{{
				Rule: ruleNameCSSResetPresent, File: rel,
				Detail: "the page loads no local stylesheet, so there is no reset to check — if the " +
					"CSS is intentionally elsewhere, declare this page as an exception",
			}}
		}
		rules := parseCSSRules(css)
		var findings []Finding
		for _, req := range resetRequirements {
			if req.check(rules) {
				continue
			}
			findings = append(findings, Finding{
				Rule: ruleNameCSSResetPresent, File: rel,
				Detail: fmt.Sprintf("%s does not declare %s — add %s", source, req.what, req.fix),
			})
		}
		return findings
	},
}

const ruleNameFontFamilyDeclared = "font-family-declared"

var ruleFontFamilyDeclared = Rule{
	Name:   ruleNameFontFamilyDeclared,
	Domain: DomainOutput,
	Doc: "The first stylesheet declares a font-family on body, html or :root, so nothing on " +
		"the page falls back to the browser's default serif by accident. This is the " +
		"static subset of the font-attribution check: proving that EVERY text-bearing " +
		"selector resolves to a declared family needs computed styles, which is the " +
		"rendered tier, not this one.",
	Selects: selectsHTML,
	ScanTree: func(rel, contents string, tree Tree) []Finding {
		css, source, ok := firstPageCSS(contents, tree)
		if !ok {
			return nil // css-reset-present already reports a page with no local CSS
		}
		for _, r := range parseCSSRules(css) {
			if !selectorMentions(r.Selector, "body", "html", ":root") {
				continue
			}
			if r.declaresAny("font-family:", "font:") {
				return nil
			}
		}
		return []Finding{{
			Rule: ruleNameFontFamilyDeclared, File: rel,
			Detail: fmt.Sprintf("%s declares no font-family on body, html or :root — every element "+
				"that does not set one itself will render in the browser's default serif", source),
		}}
	},
}

// ----------------------------------------------------------------- R10 SEO head

const ruleNameSEOMetaPresent = "seo-meta-present"

var ruleSEOMetaPresent = Rule{
	Name:   ruleNameSEOMetaPresent,
	Domain: DomainOutput,
	Doc: "Every page has a non-empty <title>, a non-empty meta description and a canonical " +
		"link. The compiler generates all three from site data, so a failure means a page " +
		"was added without them — which is invisible until a search engine indexes the " +
		"wrong URL or shows a snippet it invented.",
	Selects: selectsHTML,
	Scan: func(rel, contents string) []Finding {
		tags := tokenizeHTML(contents)
		var findings []Finding

		if !hasNonEmptyTitle(contents, tags) {
			findings = append(findings, Finding{
				Rule: ruleNameSEOMetaPresent, File: rel,
				Detail: "no non-empty <title> — it is the page's name in search results, " +
					"in the browser tab and in every bookmark",
			})
		}
		if metaContent(tags, "name", "description") == "" {
			findings = append(findings, Finding{
				Rule: ruleNameSEOMetaPresent, File: rel,
				Detail: "no non-empty <meta name=\"description\"> — without one the search snippet " +
					"is assembled from whatever text the crawler finds first",
			})
		}
		if linkHref(tags, "canonical") == "" {
			findings = append(findings, Finding{
				Rule: ruleNameSEOMetaPresent, File: rel,
				Detail: "no <link rel=\"canonical\"> — duplicate URLs (trailing slash, query " +
					"parameters, a language prefix) then compete with each other in the index",
			})
		}
		return findings
	},
}

const ruleNameSocialPreviewImage = "social-preview-image"

var ruleSocialPreviewImage = Rule{
	Name:   ruleNameSocialPreviewImage,
	Domain: DomainOutput,
	Doc: "Every page declares a non-empty og:image, so a shared link renders as a card " +
		"rather than bare text. This needs no per-page markup: drop " +
		"src/assets/img/og-image.{png,jpg,jpeg,webp} in the site and the compiler " +
		"fingerprints it and emits og:image, its dimensions and twitter:image absolutely. " +
		"Opt-in, because satisfying it requires a real brand asset that only the site's " +
		"owner can supply.",
	Selects: selectsHTML,
	Scan: func(rel, contents string) []Finding {
		tags := tokenizeHTML(contents)
		if metaContent(tags, "property", "og:image") != "" || metaContent(tags, "name", "og:image") != "" {
			return nil
		}
		return []Finding{{
			Rule: ruleNameSocialPreviewImage, File: rel,
			Detail: "no og:image — add src/assets/img/og-image.png (1200x630) to the site and the " +
				"compiler emits the tag for every page; no per-page markup needed",
		}}
	},
}

func hasNonEmptyTitle(contents string, tags []htmlTag) bool {
	for i, t := range tags {
		if t.Closing || t.Name != "title" {
			continue
		}
		if body, ok := rawTextBetween(contents, tags, i); ok && strings.TrimSpace(body) != "" {
			return true
		}
	}
	return false
}

// metaContent returns the content of the first <meta> whose keyAttr equals
// want. Attribute order is the author's choice, so both are read by name.
func metaContent(tags []htmlTag, keyAttr, want string) string {
	for _, t := range tags {
		if t.Closing || t.Name != "meta" {
			continue
		}
		if strings.EqualFold(t.AttrValue(keyAttr), want) {
			return t.AttrValue("content")
		}
	}
	return ""
}

func linkHref(tags []htmlTag, rel string) string {
	for _, t := range tags {
		if t.Closing || t.Name != "link" {
			continue
		}
		if strings.EqualFold(t.AttrValue("rel"), rel) {
			return t.AttrValue("href")
		}
	}
	return ""
}
