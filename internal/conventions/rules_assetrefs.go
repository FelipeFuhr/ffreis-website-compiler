package conventions

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// ───────────────────────────────────────── references that resolve to nothing ──

const ruleNameResolvableAssetRefs = "resolvable-asset-refs"

// The compiler fingerprints an asset it can find, and when it cannot find one
// it leaves the reference exactly as written:
//
//	data, cleanRef, err := readAsset(fp.assetsDir, ref)
//	if err != nil {
//	    return ref, nil // asset not found — leave as-is
//	}
//
// That is deliberate (a reference may legitimately point at something this
// build does not own) and it is also the one failure mode no other gate here
// sees. The build stays green, validate-assets passes, html-validate passes,
// and the page 404s the asset in production.
//
// It is not hypothetical. knowalchemy-website shipped a 668 kB background
// image as a bare `url("alchemy-nebula-backdrop.jpg")` in
// src/assets/css/simulator.css. A url() resolves against its own stylesheet's
// directory, so the compiler rewrote it to "/css/alchemy-nebula-backdrop.jpg",
// found nothing there, and emitted the broken path. Every gate was green and
// the live site served a 404 until someone diffed the bucket against a local
// build.
var ruleResolvableAssetRefs = Rule{
	Name:   ruleNameResolvableAssetRefs,
	Domain: DomainOutput,
	Doc: "Every local asset reference in the built output resolves to a file that was " +
		"actually built. The compiler leaves a reference it cannot resolve untouched " +
		"rather than failing, so a mistyped or moved asset ships as a 404 with every " +
		"other gate green. Checks href/src/poster/data/srcset on the elements that load " +
		"an asset, and url() in both linked stylesheets and inline <style> blocks — each " +
		"resolved the way a browser would, which for a url() means against the " +
		"stylesheet's own directory and not the page's. Links in <a> are not checked: a " +
		"page URL is legitimately a directory, and clean URLs are no-html-extension-urls' " +
		"subject. Nor is anything a script builds at runtime, which is unknowable here.",
	Selects: selectsHTMLOrCSS,
	ScanTree: func(rel, contents string, tree Tree) []Finding {
		var refs []assetRef
		if strings.EqualFold(path.Ext(rel), ".css") {
			refs = cssURLRefs(contents, 0)
		} else {
			refs = htmlAssetRefs(contents)
		}

		var findings []Finding
		seen := map[string]bool{}
		for _, r := range refs {
			if !isCheckableAssetRef(r.ref) || seen[r.ref] {
				continue
			}
			seen[r.ref] = true
			if assetRefResolves(tree, r.ref) {
				continue
			}
			findings = append(findings, Finding{
				Rule: ruleNameResolvableAssetRefs, File: rel, Line: r.line,
				Detail: fmt.Sprintf("%s references %q, which is not in the built output — "+
					"the compiler leaves an asset it cannot resolve as-is, so this ships as a 404. "+
					"A url() in a stylesheet resolves against that stylesheet's directory, so an "+
					"asset at the assets root needs a root-absolute path (\"/name.jpg\")", r.what, r.ref),
			})
		}
		return findings
	},
}

func selectsHTMLOrCSS(rel string) bool {
	ext := strings.ToLower(path.Ext(rel))
	return ext == ".html" || ext == ".css"
}

// assetRef is one reference found in a file, with enough context to say where
// it came from in the finding.
type assetRef struct {
	ref  string
	what string
	line int
}

// assetRefAttrs are the element/attribute pairs that load a subresource. <a>
// and <form> are deliberately absent: they navigate, and a navigation target
// is legitimately a directory rather than a file.
var assetRefAttrs = map[string][]string{
	"link":   {"href"},
	"script": {"src"},
	"img":    {"src"},
	"source": {"src"},
	"video":  {"src", "poster"},
	"audio":  {"src"},
	"track":  {"src"},
	"embed":  {"src"},
	"object": {"data"},
	"input":  {"src"},
}

// htmlAssetRefs collects subresource references from a built page: the
// attributes above, srcset candidates, and url() inside inline <style>.
func htmlAssetRefs(contents string) []assetRef {
	var refs []assetRef
	tags := tokenizeHTML(contents)
	for i, t := range tags {
		if t.Closing {
			continue
		}
		for _, attr := range assetRefAttrs[t.Name] {
			if v := t.AttrValue(attr); v != "" {
				refs = append(refs, assetRef{ref: v, what: "<" + t.Name + " " + attr + ">", line: t.Line})
			}
		}
		if t.Name == "img" || t.Name == "source" {
			for _, c := range srcsetCandidates(t.AttrValue("srcset")) {
				refs = append(refs, assetRef{ref: c, what: "<" + t.Name + " srcset>", line: t.Line})
			}
		}
		if t.Name == "style" {
			if body, at, ok := rawTextBody(contents, tags, i); ok {
				refs = append(refs, cssURLRefs(body, strings.Count(contents[:at], "\n"))...)
			}
		}
	}
	return refs
}

// rawTextBody returns the text between a raw-text start tag and its end tag,
// plus the body's byte offset in the document, so a finding inside an inline
// <style> still carries the page's own line number.
func rawTextBody(contents string, tags []htmlTag, start int) (body string, at int, ok bool) {
	gt := strings.IndexByte(contents[tags[start].Offset:], '>')
	if gt < 0 {
		return "", 0, false
	}
	from := tags[start].Offset + gt + 1
	for _, t := range tags[start+1:] {
		if t.Closing && t.Name == tags[start].Name {
			if t.Offset < from {
				return "", 0, false
			}
			return contents[from:t.Offset], from, true
		}
	}
	return "", 0, false
}

// cssURLRefs finds every url(...) in a stylesheet. linesBefore is how many
// newlines precede this text in the file being scanned, so a url() inside an
// inline <style> reports the page's line rather than the block's.
var cssURLRE = regexp.MustCompile(`(?i)url\(\s*(?:"([^"]*)"|'([^']*)'|([^)'"\s]*))\s*\)`)

func cssURLRefs(css string, linesBefore int) []assetRef {
	var refs []assetRef
	for _, m := range cssURLRE.FindAllStringSubmatchIndex(css, -1) {
		ref := ""
		for _, g := range []int{1, 2, 3} {
			if m[2*g] >= 0 && m[2*g+1] > m[2*g] {
				ref = css[m[2*g]:m[2*g+1]]
				break
			}
		}
		if ref == "" {
			continue
		}
		refs = append(refs, assetRef{
			ref: ref, what: "url()",
			line: linesBefore + 1 + strings.Count(css[:m[0]], "\n"),
		})
	}
	return refs
}

// srcsetCandidates splits a srcset into its URLs. A candidate is a URL
// optionally followed by a width or density descriptor.
func srcsetCandidates(srcset string) []string {
	if strings.TrimSpace(srcset) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(srcset, ",") {
		if f := strings.Fields(strings.TrimSpace(part)); len(f) > 0 {
			out = append(out, f[0])
		}
	}
	return out
}

// schemeRE matches a URL scheme, so mailto:, tel:, blob: and friends are left
// alone along with http(s):.
var schemeRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*:`)

// isCheckableAssetRef reports whether a reference is one this gate can judge:
// local, non-empty, and naming a file rather than a directory.
func isCheckableAssetRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	switch {
	case ref == "", strings.HasPrefix(ref, "#"):
		return false
	case strings.HasPrefix(ref, "//"), schemeRE.MatchString(ref):
		return false // another origin, or a non-fetchable scheme
	}
	p := strings.SplitN(strings.SplitN(ref, "#", 2)[0], "?", 2)[0]
	return p != "" && !strings.HasSuffix(p, "/")
}

// assetRefResolves reports whether a reference names a file in the built tree.
//
// A root-absolute reference is retried against progressively shorter prefixes.
// A site with a base_path emits "/en/css/app.css" while the tree it was built
// into holds "css/app.css" — the deployment adds the prefix on sync, so the
// literal path is absent from the very tree the page was built into. Demanding
// an exact match would report every asset on every multi-language site, which
// is a rule that gets switched off rather than fixed. The same tolerance is
// already how no-self-referential-links compares page paths.
//
// The cost is a reference that is wrong in its leading segments but whose tail
// happens to name a real file. That requires a same-named file to exist at a
// shallower depth, which fingerprinting makes unlikely, and it still catches
// the case this rule exists for.
func assetRefResolves(tree Tree, ref string) bool {
	if tree.Exists(ref) {
		return true
	}
	if !strings.HasPrefix(ref, "/") {
		return false
	}
	segs := strings.Split(strings.TrimPrefix(path.Clean(ref), "/"), "/")
	for i := 1; i < len(segs); i++ {
		if tree.Exists("/" + strings.Join(segs[i:], "/")) {
			return true
		}
	}
	return false
}
