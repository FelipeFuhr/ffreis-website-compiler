package conventions

import "strings"

// A deliberately small HTML tag tokenizer, used by the structural rules in
// rules_html.go.
//
// Why not one regex per rule, and why not golang.org/x/net/html:
//
//   - Several rules here are nesting-dependent. A <label> that wraps its
//     control names it just as well as a for=/id pair does, and no regex can
//     see that. A rule that missed the wrapping form would report every
//     correctly-labelled control in ffreis-forma as a violation — 94 of its
//     labels wrap rather than reference — and the first fix anyone reaches for
//     when a gate cries wolf is to delete the gate.
//   - x/net/html would also be correct, and more so. It is deliberately not
//     used: it would add a build dependency to every consumer of this
//     compiler, and this fleet has repeatedly had a new dependency break
//     workflows that were never audited for it. What these rules need from a
//     parser is a tag stream with correct quote and raw-text handling, which
//     is what this file is. It does not build a tree, resolve implied end
//     tags, or care about anything else a browser does.
//
// The input is this compiler's own output: generated from Go templates and
// already gated by html-validate, so attribute values are quoted and tags are
// closed. The tokenizer is still written to not mis-read a comment or a `<` in
// script/style text, because those two appear in real pages.

// htmlTag is one start or end tag in document order.
type htmlTag struct {
	// Name is lowercased.
	Name string
	// Closing is true for an end tag (</div>).
	Closing bool
	// Attr maps lowercased attribute name to value. A bare attribute
	// (`required`) is present with an empty value, which is why rules must
	// test presence with the two-result map lookup rather than comparing to "".
	Attr map[string]string
	// Line is 1-indexed, for the finding.
	Line int
	// Offset is the byte offset of the '<'.
	Offset int
}

// HasAttr reports whether the attribute is present at all, regardless of value.
func (t htmlTag) HasAttr(name string) bool {
	_, ok := t.Attr[name]
	return ok
}

// AttrValue returns a trimmed attribute value ("" when absent or empty).
func (t htmlTag) AttrValue(name string) string {
	return strings.TrimSpace(t.Attr[name])
}

// rawTextElements hold text that may legitimately contain '<' (`if (a<b)`, a
// CSS child selector). Their content is skipped rather than tokenized.
var rawTextElements = map[string]bool{
	"script":   true,
	"style":    true,
	"textarea": true,
	"title":    true,
}

// tokenizeHTML returns every start and end tag in document order.
func tokenizeHTML(s string) []htmlTag {
	var tags []htmlTag
	line := 1
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '\n' {
			line++
			i++
			continue
		}
		if c != '<' {
			i++
			continue
		}

		if skipTo, ok := skipNonTag(s, i); ok {
			line += strings.Count(s[i:skipTo], "\n")
			i = skipTo
			continue
		}

		tag, end, ok := parseTag(s, i, line)
		if !ok {
			i++
			continue
		}
		tags = append(tags, tag)
		line += strings.Count(s[i:end], "\n")
		i = end

		// Skip raw text so a '<' inside it is never read as a tag. The end tag
		// itself is still emitted, so depth tracking stays balanced.
		if !tag.Closing && rawTextElements[tag.Name] {
			closeAt := indexCloseTag(s, i, tag.Name)
			if closeAt < 0 {
				return tags // unterminated: nothing useful left to read
			}
			line += strings.Count(s[i:closeAt], "\n")
			i = closeAt
		}
	}
	return tags
}

// skipNonTag handles the three `<...>` forms that are not element tags:
// comments, doctypes/CDATA and processing instructions. It returns the offset
// just past the construct.
func skipNonTag(s string, i int) (int, bool) {
	if strings.HasPrefix(s[i:], "<!--") {
		if end := strings.Index(s[i+4:], "-->"); end >= 0 {
			return i + 4 + end + 3, true
		}
		return len(s), true
	}
	if strings.HasPrefix(s[i:], "<!") || strings.HasPrefix(s[i:], "<?") {
		if end := strings.IndexByte(s[i:], '>'); end >= 0 {
			return i + end + 1, true
		}
		return len(s), true
	}
	return 0, false
}

// parseTag reads one tag starting at s[i] == '<'. It returns the tag and the
// offset just past its '>'.
func parseTag(s string, i, line int) (htmlTag, int, bool) {
	j := i + 1
	closing := false
	if j < len(s) && s[j] == '/' {
		closing = true
		j++
	}
	nameStart := j
	for j < len(s) && isNameByte(s[j], j == nameStart) {
		j++
	}
	if j == nameStart {
		return htmlTag{}, 0, false // "<" followed by something that is not a name
	}
	name := strings.ToLower(s[nameStart:j])

	attrStart := j
	for j < len(s) {
		switch s[j] {
		case '"', '\'':
			q := s[j]
			j++
			for j < len(s) && s[j] != q {
				j++
			}
			if j < len(s) {
				j++
			}
		case '>':
			return htmlTag{
				Name:    name,
				Closing: closing,
				Attr:    parseAttrs(s[attrStart:j]),
				Line:    line,
				Offset:  i,
			}, j + 1, true
		default:
			j++
		}
	}
	return htmlTag{}, 0, false // unterminated tag
}

func isNameByte(c byte, first bool) bool {
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
		return true
	}
	if first {
		return false
	}
	return c >= '0' && c <= '9' || c == '-' || c == ':' || c == '_'
}

// parseAttrs reads a tag's attribute text into a map. Values may be
// double-quoted, single-quoted or unquoted; a bare attribute maps to "".
func parseAttrs(raw string) map[string]string {
	attrs := map[string]string{}
	i := 0
	for i < len(raw) {
		for i < len(raw) && (isSpaceByte(raw[i]) || raw[i] == '/') {
			i++
		}
		if i >= len(raw) {
			break
		}
		nameStart := i
		for i < len(raw) && !isSpaceByte(raw[i]) && raw[i] != '=' && raw[i] != '/' {
			i++
		}
		name := strings.ToLower(raw[nameStart:i])
		if name == "" {
			i++
			continue
		}
		for i < len(raw) && isSpaceByte(raw[i]) {
			i++
		}
		if i >= len(raw) || raw[i] != '=' {
			attrs[name] = "" // bare attribute
			continue
		}
		i++ // past '='
		for i < len(raw) && isSpaceByte(raw[i]) {
			i++
		}
		if i >= len(raw) {
			attrs[name] = ""
			break
		}
		if q := raw[i]; q == '"' || q == '\'' {
			i++
			valStart := i
			for i < len(raw) && raw[i] != q {
				i++
			}
			attrs[name] = raw[valStart:i]
			if i < len(raw) {
				i++
			}
			continue
		}
		valStart := i
		for i < len(raw) && !isSpaceByte(raw[i]) {
			i++
		}
		attrs[name] = raw[valStart:i]
	}
	return attrs
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// indexCloseTag finds the offset of this element's end tag at or after i, so
// the caller can jump over raw text. The end tag itself is left in place to be
// tokenized, keeping depth tracking balanced.
func indexCloseTag(s string, i int, name string) int {
	lower := strings.ToLower(s)
	needle := "</" + name
	for from := i; from < len(s); {
		k := strings.Index(lower[from:], needle)
		if k < 0 {
			return -1
		}
		k += from
		after := k + len(needle)
		// Require a tag boundary so "</style" does not match a hypothetical
		// "</styles" element.
		if after >= len(s) || s[after] == '>' || s[after] == '/' || isSpaceByte(s[after]) {
			return k
		}
		from = after
	}
	return -1
}

// elementIDs returns every id attribute value in the document, so a rule can
// tell a real reference from a dangling one.
func elementIDs(tags []htmlTag) map[string]bool {
	ids := map[string]bool{}
	for _, t := range tags {
		if t.Closing {
			continue
		}
		if id := t.AttrValue("id"); id != "" {
			ids[id] = true
		}
	}
	return ids
}

// labelTargets returns the set of ids referenced by `<label for="...">`.
func labelTargets(tags []htmlTag) map[string]bool {
	targets := map[string]bool{}
	for _, t := range tags {
		if t.Closing || t.Name != "label" {
			continue
		}
		if f := t.AttrValue("for"); f != "" {
			targets[f] = true
		}
	}
	return targets
}

// tokenRefs splits a space-separated IDREF list (aria-describedby,
// aria-labelledby).
func tokenRefs(v string) []string {
	return strings.Fields(v)
}
