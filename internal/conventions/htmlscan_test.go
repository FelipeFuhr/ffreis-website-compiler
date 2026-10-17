package conventions

import "testing"

func tagNames(tags []htmlTag) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		n := t.Name
		if t.Closing {
			n = "/" + n
		}
		out = append(out, n)
	}
	return out
}

func TestTokenizeHTML_IgnoresAngleBracketsInScriptAndStyle(t *testing.T) {
	// This is the reason the rules tokenize instead of grepping tag patterns:
	// `if (a < b)` and a CSS child selector both contain a '<' that a tag regex
	// reads as the start of an element, and every structural count downstream is
	// then wrong in a way nobody notices.
	html := `<body><script>if (a<b && c>d) { x() }</script>` +
		`<style>.a>.b{color:red}</style><main>ok</main></body>`
	got := tagNames(tokenizeHTML(html))
	want := []string{"body", "script", "/script", "style", "/style", "main", "/main", "/body"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestTokenizeHTML_SkipsCommentsAndDoctype(t *testing.T) {
	html := "<!doctype html>\n<!-- <main>commented out</main> -->\n<main>real</main>"
	got := tagNames(tokenizeHTML(html))
	if len(got) != 2 || got[0] != "main" || got[1] != "/main" {
		t.Fatalf("got %v, want the real <main> only", got)
	}
}

func TestTokenizeHTML_ReadsEveryAttributeQuotingStyle(t *testing.T) {
	tags := tokenizeHTML(`<input type="email" name='user' size=20 required>`)
	if len(tags) != 1 {
		t.Fatalf("expected one tag, got %v", tagNames(tags))
	}
	in := tags[0]
	for name, want := range map[string]string{
		"type": "email", "name": "user", "size": "20",
	} {
		if got := in.AttrValue(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	// A bare boolean attribute must read as PRESENT with an empty value. Testing
	// it with `!= ""` is the bug this distinction exists to prevent, and it
	// matters immediately: the compiler emits boolean attributes bare.
	if !in.HasAttr("required") {
		t.Error(`bare "required" was not seen as present`)
	}
	if in.HasAttr("disabled") {
		t.Error("absent attribute reported as present")
	}
}

func TestTokenizeHTML_AngleBracketInAttributeValue(t *testing.T) {
	tags := tokenizeHTML(`<input placeholder="a > b" type="text"><main>x</main>`)
	names := tagNames(tags)
	if len(names) != 3 || names[0] != "input" || names[1] != "main" {
		t.Fatalf("a quoted '>' broke tag boundaries: %v", names)
	}
}

func TestTokenizeHTML_ReportsLineNumbers(t *testing.T) {
	tags := tokenizeHTML("<html>\n<body>\n\n<main>x</main>\n")
	for _, tag := range tags {
		if tag.Name == "main" && !tag.Closing && tag.Line != 4 {
			t.Fatalf("<main> reported on line %d, want 4", tag.Line)
		}
	}
}

func TestTokenizeHTML_UnterminatedRawTextDoesNotHang(t *testing.T) {
	tags := tokenizeHTML(`<body><script>never closed`)
	if len(tags) != 2 {
		t.Fatalf("expected body+script, got %v", tagNames(tags))
	}
}

func TestIndexCloseTag_RequiresATagBoundary(t *testing.T) {
	// "</style" is a prefix of "</styles", and a naive search would stop at the
	// wrong place and swallow the rest of the document.
	html := `<style>a{}</styles-not-real</style><main>x</main>`
	got := tagNames(tokenizeHTML(html))
	found := false
	for _, n := range got {
		if n == "main" {
			found = true
		}
	}
	if !found {
		t.Fatalf("raw-text skip overran the real end tag: %v", got)
	}
}

func TestElementIDsAndLabelTargets(t *testing.T) {
	tags := tokenizeHTML(`<label for="a">A</label><input id="a"><label for="ghost">G</label>`)
	ids := elementIDs(tags)
	if !ids["a"] || ids["ghost"] {
		t.Errorf("ids = %v", ids)
	}
	targets := labelTargets(tags)
	if !targets["a"] || !targets["ghost"] {
		t.Errorf("label targets = %v", targets)
	}
}
