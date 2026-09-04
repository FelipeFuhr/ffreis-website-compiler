package buildcmd

import (
	"encoding/base64"
	"regexp"
	"strings"
	"sync"
)

// attrSearchCache and attrValueCache hold compiled regexes keyed by attribute name.
// Dynamic patterns are cached so each unique attr name pays the compilation cost only once.
var (
	attrSearchCache sync.Map // attr → *regexp.Regexp for presence/replace check
	attrValueCache  sync.Map // attr → *regexp.Regexp for value capture
)

// cachedAttrSearchRE returns (caching) a regex that matches `attr="value"` for replacement.
func cachedAttrSearchRE(attr string) *regexp.Regexp {
	if v, ok := attrSearchCache.Load(attr); ok {
		if re, ok2 := v.(*regexp.Regexp); ok2 {
			return re
		}
	}
	// regexp.QuoteMeta escapes regex metacharacters but doesn't fix invalid UTF-8;
	// regexp.MustCompile rejects any pattern that isn't valid UTF-8 outright (unlike
	// matching, which tolerates it in the subject text), so sanitize first.
	safeAttr := strings.ToValidUTF8(attr, "")
	// The value alternation requires the SAME quote character to open and
	// close (each branch just excludes its own delimiter from the body, so
	// the other quote type can appear freely inside). A naive ["'][^"']*["']
	// instead treats either quote character as a valid closer, so a value
	// that legitimately embeds the other quote type (e.g. onload's
	// this.media='all', double-quoted overall) gets truncated at the first
	// embedded quote on replay — the tag then grows a stray fragment every
	// time the transform reruns over its own output.
	re := regexp.MustCompile(`(?i)` + attrBoundary(safeAttr) + regexp.QuoteMeta(safeAttr) + `\s*=\s*(?:"[^"]*"|'[^']*')`)
	attrSearchCache.Store(attr, re)
	return re
}

// attrBoundary returns a `\b` word-boundary anchor when attr begins with a
// word character (ASCII letter, digit, or underscore) — the common case,
// since every real HTML attribute name (src, media, aria-hidden, ...) starts
// with a letter. Go's regexp \b asserts a transition between a word and a
// non-word rune; when attr itself starts with a non-word rune, \b can only
// match immediately before it if the preceding character is a word
// character. addOrReplaceAttr's own insert path always precedes a new
// attribute with a literal space (a non-word character), so for such an
// attr the \b assertion can never be satisfied by anything the function
// itself writes. That mismatch between what gets inserted and what the
// search regex can find is what breaks idempotency: a second call never
// matches the attribute it just inserted, so it appends a duplicate instead
// of replacing it. This only matters for malformed attr names (e.g. "!")
// that no real caller ever passes; omitting \b for them leaves matching
// behavior for every well-formed, letter-initial attribute name unchanged.
func attrBoundary(attr string) string {
	if attr != "" && isWordByte(attr[0]) {
		return `\b`
	}
	return ""
}

// isWordByte reports whether b is an ASCII word character, matching the
// character class Go's regexp \b anchor uses ([0-9A-Za-z_]).
func isWordByte(b byte) bool {
	return b == '_' || ('0' <= b && b <= '9') || ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z')
}

// cachedAttrValueRE returns (caching) a regex that captures the value of `attr="..."`.
func cachedAttrValueRE(attr string) *regexp.Regexp {
	if v, ok := attrValueCache.Load(attr); ok {
		if re, ok2 := v.(*regexp.Regexp); ok2 {
			return re
		}
	}
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(strings.ToValidUTF8(attr, "")) + `\s*=\s*["']([^"']+)["']`)
	attrValueCache.Store(attr, re)
	return re
}

// replaceTagWith applies replacer to every match of re in doc, rebuilding the
// document with a strings.Builder. refs contains one string per capture group.
func replaceTagWith(doc string, re *regexp.Regexp, replacer func(tag string, refs []string) (string, error)) (string, error) {
	matches := re.FindAllStringSubmatchIndex(doc, -1)
	if len(matches) == 0 {
		return doc, nil
	}

	var out strings.Builder
	last := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		out.WriteString(doc[last:start])
		tag := doc[start:end]

		refs := make([]string, len(m)/2)
		for i := 0; i < len(m); i += 2 {
			if m[i] >= 0 && m[i+1] >= 0 {
				refs[i/2] = doc[m[i]:m[i+1]]
			}
		}

		replacement, err := replacer(tag, refs)
		if err != nil {
			return "", err
		}
		out.WriteString(replacement)
		last = end
	}
	out.WriteString(doc[last:])
	return out.String(), nil
}

// addOrReplaceAttr sets an HTML attribute value in a tag, replacing it if present.
func addOrReplaceAttr(tag, attr, value string) string {
	// Sanitize once and reuse the SAME value for both the search regex and
	// the inserted/replacement text. Building the regex from a sanitized
	// attr while inserting the raw (possibly invalid-UTF-8) attr made the
	// two permanently disagree: a second call could never find the exact
	// bytes the first call had just written, so it kept prepending a fresh
	// copy of the raw attribute text in front of the stale one instead of
	// replacing it.
	safeAttr := strings.ToValidUTF8(attr, "")
	re := cachedAttrSearchRE(safeAttr)
	// htmlEscape guarantees value never contains a literal '"'. Without it, a
	// value containing a raw double quote produces an ambiguous run of quote
	// characters (open, embedded, close) that the ["'][^"']*["'] search
	// regex can't reparse the same way twice — each replay resolves the
	// ambiguity differently, so the tag grows a stray quote per call. No
	// real caller's value contains &, ", <, or > today, so this is a no-op
	// for every well-formed call site.
	quoted := safeAttr + `="` + htmlEscape(value) + `"`
	if re.MatchString(tag) {
		// ReplaceAllString (not ReplaceAllLiteralString) would interpret a
		// literal "$" in quoted as a submatch-reference template (Go's
		// regexp.Expand syntax: $1, $name, ${name}, ...). Since our search
		// regex has no capture groups, any "$" followed by digits/letters/
		// underscores in attr or value — e.g. attr="$000" — resolves to an
		// unmatched, empty reference and silently vanishes from the output
		// instead of being written literally, same as the insert branch
		// below already does via plain string concatenation.
		return re.ReplaceAllLiteralString(tag, quoted)
	}
	// Insert before the closing > of the tag.
	if idx := strings.LastIndex(tag, ">"); idx != -1 {
		return tag[:idx] + " " + quoted + tag[idx:]
	}
	return tag
}

// getTagAttr extracts the value of a named attribute from an HTML tag string.
func getTagAttr(tag, attr string) string {
	m := cachedAttrValueRE(attr).FindStringSubmatch(tag)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func htmlEscape(v string) string {
	v = strings.ReplaceAll(v, "&", "&amp;")
	v = strings.ReplaceAll(v, "\"", "&quot;")
	v = strings.ReplaceAll(v, "<", "&lt;")
	v = strings.ReplaceAll(v, ">", "&gt;")
	return v
}

func encodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// isExternalRef reports whether ref is an absolute external URL (http/https/scheme-relative).
func isExternalRef(ref string) bool {
	lower := strings.ToLower(strings.TrimSpace(ref))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "//")
}
