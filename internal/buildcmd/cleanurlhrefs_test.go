package buildcmd

import "testing"

func TestCleanURLHref(t *testing.T) {
	cases := []struct {
		name string
		href string
		base string
		want string
	}{
		// The two shapes measured on forma: 108 relative, 55 root-absolute.
		{"relative becomes deployment-rooted", "login.html", "/en", "/en/login"},
		{"root-absolute keeps its root", "/login.html", "/en", "/login"},
		{"already prefixed is not double-prefixed", "/en/login.html", "/en", "/en/login"},

		// A relative href on a page that MOVED into a directory is the whole
		// reason this exists: resolving against the page dir would give
		// /en/404/account, which is the bug.
		{"relative resolves against root not page dir", "account.html", "/en", "/en/account"},

		// index.html keeps its directory form — resolvePageTarget leaves the
		// index page at <dir>/index.html — the directory IS its URL.
		{"index at root", "index.html", "/en", "/en/"},
		{"index absolute at deployment root", "/en/index.html", "/en", "/en/"},
		{"index at site root with empty base", "/index.html", "", "/"},
		{"nested index", "/en/guides/index.html", "/en", "/en/guides/"},

		// Suffixes must survive, and must not defeat the extension test.
		{"fragment preserved", "about.html#team", "/en", "/en/about#team"},
		{"query preserved", "search.html?q=x", "/en", "/en/search?q=x"},
		{"query then fragment", "search.html?q=x#hit", "/en", "/en/search?q=x#hit"},
		{"fragment containing a question mark", "a.html#w?y", "/en", "/en/a#w?y"},

		// Dot segments normalise rather than surviving into the output.
		{"leading ./", "./login.html", "/en", "/en/login"},
		{"parent segment", "../pt/about.html", "/en", "/pt/about"},

		// A sibling deployment is equally extension-free, so it IS rewritten.
		// Skipping it would break every language-switcher link.
		{"sibling deployment stripped", "/pt/about.html", "/en", "/pt/about"},

		// Left alone: not a local navigational .html target.
		{"external http", "http://x.test/a.html", "/en", "http://x.test/a.html"},
		{"external https", "https://x.test/a.html", "/en", "https://x.test/a.html"},
		{"protocol relative", "//cdn.test/a.html", "/en", "//cdn.test/a.html"},
		{"mailto", "mailto:a@b.test", "/en", "mailto:a@b.test"},
		{"tel", "tel:+5551", "/en", "tel:+5551"},
		{"javascript", "javascript:void(0)", "/en", "javascript:void(0)"},
		{"fragment only", "#main", "/en", "#main"},
		{"empty", "", "/en", ""},
		{"already extension-free", "/en/login", "/en", "/en/login"},
		{"directory form", "/en/blog/", "/en", "/en/blog/"},
		{"non-html extension", "/en/report.pdf", "/en", "/en/report.pdf"},
		{"html inside a longer extension is not a match", "a.xhtml", "/en", "a.xhtml"},

		// Empty base path (root deployment).
		{"relative with empty base", "login.html", "", "/login"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cleanURLHref(tc.href, "/"+trimSlashesForTest(tc.base))
			if got != tc.want {
				t.Fatalf("cleanURLHref(%q, %q) = %q, want %q", tc.href, tc.base, got, tc.want)
			}
		})
	}
}

// trimSlashesForTest mirrors the base normalisation normaliseCleanURLHrefs does,
// so the table above can be written with the natural "/en" form.
func trimSlashesForTest(s string) string {
	for len(s) > 0 && s[0] == '/' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func TestNormaliseCleanURLHrefsRewritesOnlyNavTags(t *testing.T) {
	// An asset reference must survive even though it is a <link> with an href.
	// fingerprintLocalAssets owns those; rewriting here would corrupt them.
	html := `<link rel="stylesheet" href="/css/app.html">` +
		`<a href="login.html">in</a>` +
		`<area href="map.html" shape="rect">` +
		`<form action="subscribe.html" method="post"></form>` +
		`<img src="a.html">`

	got := normaliseCleanURLHrefs(html, "/en")

	for _, want := range []string{
		`<link rel="stylesheet" href="/css/app.html">`, // untouched asset
		`<img src="a.html">`,                           // untouched asset
		`<a href="/en/login">in</a>`,
		`<area href="/en/map" shape="rect">`,
		`<form action="/en/subscribe" method="post"></form>`,
	} {
		if !contains(got, want) {
			t.Fatalf("expected %q in output, got:\n%s", want, got)
		}
	}
}

func TestNormaliseCleanURLHrefsPreservesOtherAttributes(t *testing.T) {
	html := `<a class="nav" href="products.html" aria-current="page" data-x="1">P</a>`
	got := normaliseCleanURLHrefs(html, "/en")
	want := `<a class="nav" href="/en/products" aria-current="page" data-x="1">P</a>`
	if got != want {
		t.Fatalf("attributes must survive:\n got %s\nwant %s", got, want)
	}
}

func TestNormaliseCleanURLHrefsIsIdempotent(t *testing.T) {
	html := `<a href="login.html">a</a><a href="/pt/about.html#x">b</a>` +
		`<a href="index.html">c</a><a href="search.html?q=1">d</a>`

	once := normaliseCleanURLHrefs(html, "/en")
	twice := normaliseCleanURLHrefs(once, "/en")
	if once != twice {
		t.Fatalf("transform must be idempotent:\n once: %s\ntwice: %s", once, twice)
	}
}

func TestNormaliseCleanURLHrefsHandlesSingleQuotedAndUppercaseTags(t *testing.T) {
	// Uppercase tag names and extra whitespace must still match. Single-quoted
	// values are deliberately NOT rewritten: the attribute regex requires double
	// quotes, matching normaliseAuthoredHrefs, and the compiler's own templates
	// emit double quotes. Documented here so the limit is a decision, not a
	// surprise.
	html := `<A   HREF="login.html">x</A><a href='login.html'>y</a>`
	got := normaliseCleanURLHrefs(html, "/en")
	if !contains(got, `HREF="/en/login"`) {
		t.Fatalf("uppercase tag/attr must be rewritten, got: %s", got)
	}
	if !contains(got, `href='login.html'`) {
		t.Fatalf("single-quoted value is out of scope and must be untouched, got: %s", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
