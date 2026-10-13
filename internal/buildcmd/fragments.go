package buildcmd

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"ffreis-website-compiler/internal/sitegen"
)

const (
	// fragmentsHTMLFile holds one <template> block per fragment page for a site.
	// Written to the output root alongside the other generated files (sitemap.xml,
	// the rewritten web manifests) and only when the site has a fragment page.
	fragmentsHTMLFile = "fragments.html"

	// fragmentsJSFile is the generated dependency-free mount script that pairs
	// with fragmentsHTMLFile.
	fragmentsJSFile = "fragments.js"

	// fragmentTemplateIDPrefix prefixes each generated <template> element's id,
	// so the page named "dashboard" becomes <template id="tpl-dashboard">.
	fragmentTemplateIDPrefix = "tpl-"

	// fragmentCSSDir is the output subdirectory the generated per-fragment and
	// hoisted-shared CSS files are written to, matching where a site's own
	// stylesheets live.
	fragmentCSSDir = "css"

	// fragmentSharedCSSBase is the pre-hash name of the single file every
	// stylesheet referenced by more than one fragment is hoisted into.
	fragmentSharedCSSBase = "fragment-shared.css"

	// fragmentCSSLinkID is the id of the singleton <link> the mount script
	// swaps to the mounted fragment's own stylesheet.
	fragmentCSSLinkID = "fragment-css"

	// fragmentSharedCSSLinkID is the id of the <link> carrying the hoisted
	// shared stylesheet. Separate from fragmentCSSLinkID so navigating between
	// fragments never re-fetches the shared CSS.
	fragmentSharedCSSLinkID = "fragment-shared-css"
)

// documentShellTagRE matches the document-shell tags a fragment must not carry.
// \b keeps <header> from matching the <head> branch.
var documentShellTagRE = regexp.MustCompile(`(?i)<(html|head|body)\b`)

// fragmentReservedSlugs maps a page slug a fragment build cannot share the
// output root with to the generated file it would collide with.
var fragmentReservedSlugs = map[string]string{
	strings.TrimSuffix(fragmentsHTMLFile, ".html"): fragmentsHTMLFile,
}

// fragmentPage is one page rendered as a reusable <template> fragment.
type fragmentPage struct {
	name string
	// html is the fragment's inner content after the fragment asset transforms,
	// with its stylesheet links already stripped into stylesheets.
	html string
	// stylesheets holds the local stylesheet hrefs the fragment's content
	// declared, normalised to assets-dir-relative paths, in document order.
	stylesheets []string
}

// fragmentBuildInput bundles everything writing a site's fragment bundle needs.
type fragmentBuildInput struct {
	opts      buildOptions
	assetsDir string
	siteData  map[string]any
	// pages are the fragment pages, already filtered out of the regular pages.
	pages []sitegen.PageTemplate
	// regularPages are the pages written as standalone HTML documents, needed
	// only to reject a slug that collides with a generated fragment file.
	regularPages []sitegen.PageTemplate
}

// fragmentCSSBundle records which stylesheet URL the mount script should use
// for each fragment. Values are typed as any because a fragment with no CSS of
// its own is emitted to the generated script as JSON null.
type fragmentCSSBundle struct {
	// own maps every fragment name to the URL of the file holding the
	// stylesheets only that fragment references, or nil when it has none.
	own map[string]any
	// shared is the URL of the single file every stylesheet referenced by more
	// than one fragment was hoisted into, or nil when no stylesheet is shared.
	shared any
}

// splitFragmentPages partitions pages into the regular pages written as
// standalone HTML documents and the pages a site opted into fragment output
// with pages.<name>.fragment: true. When the site declares no fragment page it
// returns the pages slice untouched and no fragments, so such a site builds
// byte-for-byte identically to before this mode existed.
func splitFragmentPages(pages []sitegen.PageTemplate, siteData map[string]any) (regular, fragments []sitegen.PageTemplate) {
	pagesData, _ := siteData["pages"].(map[string]any)
	keep := pages[:0:0]
	for _, p := range pages {
		pd, _ := pagesData[p.Name].(map[string]any)
		if isFragment, _ := pd["fragment"].(bool); isFragment {
			fragments = append(fragments, p)
			continue
		}
		keep = append(keep, p)
	}
	if len(fragments) == 0 {
		return pages, nil
	}
	return keep, fragments
}

// maybeWriteFragmentBundle renders every fragment page into fragments.html,
// writes each fragment's CSS to its own fingerprinted file, and emits the
// mount script. It is a no-op for a site with no fragment page.
func maybeWriteFragmentBundle(logger *slog.Logger, in fragmentBuildInput) error {
	if len(in.pages) == 0 {
		return nil
	}
	if err := checkFragmentOutputCollisions(in); err != nil {
		return err
	}

	fragments, toCopy, err := renderFragmentPages(logger, in)
	if err != nil {
		return err
	}
	css, err := writeFragmentCSS(in, fragments, toCopy)
	if err != nil {
		return err
	}
	// Assets referenced from fragment markup and from the generated CSS files
	// are copied by the same helper the page pipeline uses, so a fragment's
	// images and fonts land in dist under their content-hashed names too.
	if err := writeHashedAssets(in.opts.outDir, in.assetsDir, toCopy); err != nil {
		return err
	}
	return writeFragmentOutputs(logger, in.opts.outDir, fragments, css)
}

// checkFragmentOutputCollisions fails the build when a regular page's slug
// would resolve to one of the generated fragment files, rather than letting the
// fragment bundle silently overwrite that page.
func checkFragmentOutputCollisions(in fragmentBuildInput) error {
	for _, p := range in.regularPages {
		slug := resolvePageSlug(in.siteData, p.Name)
		if file, reserved := fragmentReservedSlugs[slug]; reserved {
			return fmt.Errorf(
				"page %q would be written to %s, which this site's fragment output also claims: "+
					"give the page a different pages.%s.slug",
				p.Name, file, p.Name,
			)
		}
	}
	return nil
}

// renderFragmentPages renders each fragment page's content block, strips and
// records its stylesheet links, and applies the fragment asset transforms.
// Returns the fragments plus the hashedRelPath → originalRelPath copy
// instructions their markup produced.
func renderFragmentPages(logger *slog.Logger, in fragmentBuildInput) ([]fragmentPage, map[string]string, error) {
	fragments := make([]fragmentPage, 0, len(in.pages))
	toCopy := make(map[string]string)

	for _, page := range in.pages {
		rendered, err := sitegen.RenderFragment(page, in.siteData)
		if err != nil {
			return nil, nil, err
		}
		if shell := documentShellTagRE.FindString(rendered); shell != "" {
			return nil, nil, fmt.Errorf(
				"fragment page %q renders a %q tag: a fragment is mounted inside the consuming app's "+
					"own document and must not carry a document shell",
				page.Name, shell,
			)
		}
		// Same pre-transform alt-text audit regular pages get in writePages.
		validateImageAltText(logger, page.Name, rendered)

		stripped, hrefs, err := extractFragmentStylesheets(rendered)
		if err != nil {
			return nil, nil, fmt.Errorf("extracting stylesheets from fragment %s: %w", page.Name, err)
		}
		body, err := transformFragmentBody(stripped, in.opts, in.assetsDir, toCopy)
		if err != nil {
			return nil, nil, fmt.Errorf("transforming fragment %s: %w", page.Name, err)
		}
		fragments = append(fragments, fragmentPage{name: page.Name, html: body, stylesheets: hrefs})
	}
	return fragments, toCopy, nil
}

// extractFragmentStylesheets removes every local <link rel="stylesheet"> from a
// fragment's rendered content and returns the stripped markup plus the hrefs it
// carried. A fragment has no <head>, so the position-based head/body CSS split
// cannot apply: every stylesheet a fragment declares is that fragment's own CSS
// and becomes a separate file instead. Inlining it as <style> would duplicate
// the whole stylesheet into every clone of the template in the DOM.
//
// External stylesheet URLs are left in place: they cannot be read, minified or
// content-hashed at build time.
func extractFragmentStylesheets(doc string) (string, []string, error) {
	var hrefs []string
	seen := make(map[string]bool)
	stripped, err := replaceTagWith(doc, stylesheetTagRE, func(tag string, refs []string) (string, error) {
		href := refs[1]
		if isExternalRef(href) {
			return tag, nil
		}
		ref := strings.TrimPrefix(href, "/")
		if !seen[ref] {
			seen[ref] = true
			hrefs = append(hrefs, ref)
		}
		return "", nil
	})
	if err != nil {
		return "", nil, err
	}
	return stripped, hrefs, nil
}

// transformFragmentBody applies the subset of the page pipeline that behaves
// correctly inside a <template>: small-SVG inlining, default lazy-loading, and
// asset fingerprinting. Discovered copy instructions are merged into toCopy.
//
// Three page transforms are deliberately skipped. CSS is handled by
// extractFragmentStylesheets above. JS inlining and script preloads are not
// applied because a <script> inside template content never executes when the
// template is cloned, so the consuming app owns the fragment's behaviour. LQIP
// is not applied because it injects a per-page <style> and <script> pair, which
// inside template content would be duplicated per clone and never run.
func transformFragmentBody(html string, opts buildOptions, assetsDir string, toCopy map[string]string) (string, error) {
	html, err := inlineLocalSVGs(html, assetsDir)
	if err != nil {
		return "", fmt.Errorf("inlining SVGs: %w", err)
	}
	html, err = injectDefaultLazyLoading(html)
	if err != nil {
		return "", fmt.Errorf("injecting default lazy loading: %w", err)
	}
	html, fragmentCopy, err := fingerprintLocalAssets(html, assetsDir, opts.basePath)
	if err != nil {
		return "", fmt.Errorf("fingerprinting assets: %w", err)
	}
	for hashed, orig := range fragmentCopy {
		toCopy[hashed] = orig
	}
	return html, nil
}

// writeFragmentCSS writes one CSS file per fragment that has stylesheets of its
// own plus, when any stylesheet is referenced by more than one fragment, a
// single hoisted shared file. It returns the URLs the mount script needs.
func writeFragmentCSS(in fragmentBuildInput, fragments []fragmentPage, toCopy map[string]string) (fragmentCSSBundle, error) {
	usage := countFragmentStylesheetUsage(fragments)
	bundle := fragmentCSSBundle{own: make(map[string]any, len(fragments))}

	for _, f := range fragments {
		url, err := writeOwnFragmentCSS(in, f, usage, toCopy)
		if err != nil {
			return fragmentCSSBundle{}, err
		}
		bundle.own[f.name] = url
	}

	shared, err := writeHoistedFragmentCSS(in, usage, toCopy)
	if err != nil {
		return fragmentCSSBundle{}, err
	}
	bundle.shared = shared
	return bundle, nil
}

// countFragmentStylesheetUsage counts how many fragments reference each
// stylesheet. This mirrors collectSharedScripts: a stylesheet used by more than
// one fragment is hoisted and loaded once instead of being treated as one
// fragment's route-scoped CSS, exactly as a script on more than one page is
// cached externally instead of inlined into each page.
func countFragmentStylesheetUsage(fragments []fragmentPage) map[string]int {
	usage := make(map[string]int)
	for _, f := range fragments {
		for _, ref := range f.stylesheets {
			usage[ref]++
		}
	}
	return usage
}

// writeOwnFragmentCSS concatenates the stylesheets only this fragment
// references into one fingerprinted file. Returns nil when the fragment has no
// CSS of its own — it declared none, or every stylesheet it declared was
// hoisted into the shared file.
func writeOwnFragmentCSS(in fragmentBuildInput, f fragmentPage, usage map[string]int, toCopy map[string]string) (any, error) {
	refs := make([]string, 0, len(f.stylesheets))
	for _, ref := range f.stylesheets {
		if usage[ref] < 2 {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return nil, nil
	}
	url, err := writeFragmentCSSFile(in, "fragment-"+f.name+".css", refs, toCopy)
	if err != nil {
		return nil, err
	}
	return url, nil
}

// writeHoistedFragmentCSS writes every stylesheet referenced by more than one
// fragment into one shared file the consuming app loads once. Returns nil when
// no stylesheet is shared.
func writeHoistedFragmentCSS(in fragmentBuildInput, usage map[string]int, toCopy map[string]string) (any, error) {
	refs := make([]string, 0, len(usage))
	for ref, count := range usage {
		if count > 1 {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return nil, nil
	}
	// Map iteration order is random; sort so the file's bytes (and therefore
	// its content hash and filename) are reproducible across builds.
	sort.Strings(refs)
	url, err := writeFragmentCSSFile(in, fragmentSharedCSSBase, refs, toCopy)
	if err != nil {
		return nil, err
	}
	return url, nil
}

// writeFragmentCSSFile writes refs as one content-hashed CSS file under
// css/ and returns the URL the mount script should point a <link> at.
func writeFragmentCSSFile(in fragmentBuildInput, baseName string, refs []string, toCopy map[string]string) (string, error) {
	css, err := fragmentCSSText(in, refs, toCopy)
	if err != nil {
		return "", err
	}

	data := []byte(css)
	rel := path.Join(fragmentCSSDir, insertHashInPath(baseName, assetContentHash(data)))
	target := filepath.Join(in.opts.outDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("creating dir for %s: %w", rel, err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil { //nolint:gosec
		return "", fmt.Errorf(errFmtWriting, rel, err)
	}
	// Always root-absolute: a fragment is mounted under whatever client-side
	// route the consuming app is on, so a document-relative href would resolve
	// differently per route.
	return strings.TrimRight(in.opts.basePath, "/") + "/" + rel, nil
}

// fragmentCSSText reads refs and runs them through the same helpers head-CSS
// inlining uses — flattenCSSImports with preserveURLs, then minifyCSS — so a
// fragment's stylesheet is minified and @import-flattened exactly like a
// critical-path <style> block, and its url() references then get the same
// content-hash rewrite fingerprintStyleBlocks applies to an inline <style>.
func fragmentCSSText(in fragmentBuildInput, refs []string, toCopy map[string]string) (string, error) {
	inlineOpts := cssInlineOpts{preserveURLs: true, embedFonts: in.opts.embedFonts}

	var sb strings.Builder
	for _, ref := range refs {
		raw, cssPath, err := readAsset(in.assetsDir, ref)
		if err != nil {
			return "", fmt.Errorf("reading fragment stylesheet %s: %w", ref, err)
		}
		flattened, err := flattenCSSImports(string(raw), cssPath, in.assetsDir, nil, inlineOpts)
		if err != nil {
			return "", fmt.Errorf("flattening @import in %s: %w", ref, err)
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(minifyCSS(flattened))
	}
	return fingerprintFragmentCSSURLs(in, sb.String(), toCopy)
}

// fingerprintFragmentCSSURLs rewrites url() references in a generated fragment
// CSS file to their content-hashed paths. The originals under css/, fonts/ and
// images/ are never copied to dist, so an un-rewritten url() would point at a
// file that does not exist.
func fingerprintFragmentCSSURLs(in fragmentBuildInput, css string, toCopy map[string]string) (string, error) {
	fp := &assetFingerprinter{
		assetsDir:      in.assetsDir,
		normalizedBase: strings.TrimRight(in.opts.basePath, "/"),
		hashCache:      make(map[string]string),
		toCopy:         make(map[string]string),
	}
	out, err := rewriteCSSURLs(css, func(ref string) (string, bool, error) {
		if isExternalRef(ref) || isDataURI(ref) {
			return ref, false, nil
		}
		newRef, resolveErr := fp.resolve(ref)
		return newRef, newRef != ref, resolveErr
	})
	if err != nil {
		return "", err
	}
	for hashed, orig := range fp.toCopy {
		toCopy[hashed] = orig
	}
	return out, nil
}

// writeFragmentOutputs writes fragments.html and fragments.js to the output root.
func writeFragmentOutputs(logger *slog.Logger, outDir string, fragments []fragmentPage, css fragmentCSSBundle) error {
	mountScript, err := renderFragmentMountScript(css)
	if err != nil {
		return err
	}

	outputs := []struct{ name, body string }{
		{fragmentsHTMLFile, renderFragmentsHTML(fragments)},
		{fragmentsJSFile, mountScript},
	}
	for _, out := range outputs {
		target := filepath.Join(outDir, out.name)
		if err := os.WriteFile(target, []byte(out.body), 0o644); err != nil { //nolint:gosec
			return fmt.Errorf(errFmtWriting, target, err)
		}
		logger.Info("generated fragment output", "target", target, "fragments", len(fragments))
		fmt.Fprintln(os.Stdout, target)
	}
	return nil
}

// renderFragmentsHTML concatenates one <template> block per fragment. Fragments
// arrive in the page order LoadPageTemplatesFromRoot sorted, so the output is
// reproducible.
func renderFragmentsHTML(fragments []fragmentPage) string {
	var sb strings.Builder
	sb.WriteString("<!-- Generated by website-compiler: one <template> per fragment page. -->\n")
	for _, f := range fragments {
		sb.WriteString(`<template id="` + fragmentTemplateIDPrefix + f.name + `">` + "\n")
		sb.WriteString(strings.TrimSpace(f.html))
		sb.WriteString("\n</template>\n")
	}
	return sb.String()
}

// fragmentMountScriptTemplate is the generated mount snippet. It takes the two
// JSON tables writeFragmentCSS produced and is otherwise fixed: dependency-free
// vanilla JS, called by hand-written application code this compiler knows
// nothing about, so the API shape has to be legible from the file itself.
//
// Deliberately carries no strict-mode pragma. libmagic classifies any file
// containing that pragma's exact quoted literal as application/javascript, and
// the fleet's staged-file hygiene hook rejects a staged file whose MIME type is
// not text/*, so writing it here (even inside a comment) makes this Go file
// unstageable. Nothing in the snippet needs strict mode: it is an ES5 IIFE that
// declares every local with var and assigns exactly one intentional global.
const fragmentMountScriptTemplate = `/* Generated by website-compiler — do not edit.
 *
 * Mounts the <template> fragments in fragments.html. Load that file's markup
 * into the document (it is a plain HTML partial: fetch it and insert it, or
 * include it server-side), load this script, then on every client-side route
 * change call:
 *
 *   window.websiteFragments.mount("<fragment name>", targetElement);
 *
 * mount() replaces targetElement's children with a clone of that fragment's
 * template content and points a singleton <link id="%s"> at the fragment's own
 * stylesheet. It returns true on success, or false when no <template> for that
 * name is in the document (or targetElement is missing).
 *
 * ownCSS maps every fragment name to its own stylesheet URL, or null when the
 * fragment declared none — then the <link> swap is skipped and whatever CSS is
 * already applied stays. sharedCSS is the one stylesheet referenced by more
 * than one fragment, hoisted so it loads once instead of per fragment; it is
 * null when no stylesheet is shared.
 */
(function (global) {
  var OWN_CSS = %s;
  var SHARED_CSS = %s;
  var TEMPLATE_ID_PREFIX = %s;
  var OWN_LINK_ID = %s;
  var SHARED_LINK_ID = %s;

  function stylesheetLink(id) {
    var link = document.getElementById(id);
    if (!link) {
      link = document.createElement("link");
      link.id = id;
      link.rel = "stylesheet";
      document.head.appendChild(link);
    }
    return link;
  }

  function applyStylesheet(id, href) {
    if (!href) {
      return false;
    }
    var link = stylesheetLink(id);
    if (link.getAttribute("href") !== href) {
      link.setAttribute("href", href);
    }
    return true;
  }

  function mount(name, target) {
    var template = document.getElementById(TEMPLATE_ID_PREFIX + name);
    if (!template || !target) {
      return false;
    }
    applyStylesheet(SHARED_LINK_ID, SHARED_CSS);
    applyStylesheet(OWN_LINK_ID, OWN_CSS[name]);
    target.textContent = "";
    target.appendChild(template.content.cloneNode(true));
    return true;
  }

  global.websiteFragments = {
    names: Object.keys(OWN_CSS),
    ownCSS: OWN_CSS,
    sharedCSS: SHARED_CSS,
    mount: mount
  };
})(window);
`

// renderFragmentMountScript fills fragmentMountScriptTemplate in. Every value
// is JSON-encoded, so json.Marshal does the quoting and escaping, and its
// sorted map output keeps the generated script reproducible.
func renderFragmentMountScript(css fragmentCSSBundle) (string, error) {
	own, err := json.Marshal(css.own)
	if err != nil {
		return "", fmt.Errorf("encoding fragment css table: %w", err)
	}
	shared, err := json.Marshal(css.shared)
	if err != nil {
		return "", fmt.Errorf("encoding shared fragment css url: %w", err)
	}
	return fmt.Sprintf(
		fragmentMountScriptTemplate,
		fragmentCSSLinkID,
		own,
		shared,
		jsonQuote(fragmentTemplateIDPrefix),
		jsonQuote(fragmentCSSLinkID),
		jsonQuote(fragmentSharedCSSLinkID),
	), nil
}
