package buildcmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// injectNavigationEnhancements inserts two <head> elements into every rendered page:
//   - Cross-document view-transition CSS: eliminates the flash-of-unstyled-content on
//     page navigation by fading between pages instead of doing a hard repaint.
//   - Speculation Rules JSON: tells supporting browsers to prerender same-origin pages
//     the user is likely to visit (on hover, eagerness=moderate), making navigations
//     feel near-instant.
//
// Both features degrade silently in unsupported browsers, so no flag is provided.
func injectNavigationEnhancements(html string) string {
	const inject = `<style>@view-transition{navigation:auto}</style>` +
		"\n    " +
		`<script type="speculationrules">{"prerender":[{"where":{"href_matches":"/*"},"eagerness":"moderate"}]}</script>`
	return strings.Replace(html, "</head>", inject+headEndTag, 1)
}

const defaultTrackerCDNBase = "https://cdn.ffreis.com"

// injectTracker injects the versioned ffreis-tracker-sdk <script src=...> tag
// plus a Tracker.init(...) snippet before </head>. It is a no-op when the
// tracker is disabled or required config is missing.
//
// The init snippet runs on DOMContentLoaded so the script is loaded with defer
// without races against the rest of the page. Both siteId and endpoint are
// JSON-encoded to escape quotes safely.
func injectTracker(html string, opts buildOptions) string {
	if !opts.trackerEnabled {
		return html
	}
	if opts.trackerSDKVersion == "" || opts.trackerSiteID == "" || opts.trackerEndpoint == "" {
		return html
	}
	cdnBase := opts.trackerCDNBase
	if cdnBase == "" {
		cdnBase = defaultTrackerCDNBase
	}
	cdnBase = strings.TrimRight(cdnBase, "/")
	siteID := jsonQuote(opts.trackerSiteID)
	endpoint := jsonQuote(opts.trackerEndpoint)
	inject := fmt.Sprintf(
		`<script src="%s/tracker-sdk/v%s/tracker.min.js" defer></script>`+
			"\n    "+
			`<script>window.addEventListener('DOMContentLoaded',function(){if(window.Tracker)window.Tracker.init({siteId:%s,endpoint:%s});});</script>`,
		cdnBase, opts.trackerSDKVersion, siteID, endpoint,
	)
	return strings.Replace(html, "</head>", inject+headEndTag, 1)
}

// jsonQuote returns a JSON-quoted string literal safe to embed inline in a
// JavaScript object literal. json.Marshal escapes quotes, backslashes, and
// any control characters; it cannot fail for a plain string input.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// injectDevData injects window.__devBuild = <JSON> before </head> when the
// -dev-data flag is set. opts.devDataJSON must be pre-populated by
// buildDevDataPayload before any pages are rendered; this function is a
// no-op when devDataJSON is empty.
func injectDevData(html string, opts buildOptions) string {
	if opts.devDataJSON == "" {
		return html
	}
	inject := "<script>window.__devBuild=" + opts.devDataJSON + ";</script>"
	return strings.Replace(html, "</head>", inject+headEndTag, 1)
}

// perPageStylesheetRelPath returns the conventional per-page stylesheet path
// (css/pages/<pageName>.css) for pageName, or "" when pageName is empty.
// pageName is always compiler-internal (a loaded template's name, or one of
// the fixed "course"/"listing"/"post" content-type names, or a configured
// section name) — never raw user input — so a plain filepath.Join is safe
// here without readAsset's extra path-escape guarding.
func perPageStylesheetRelPath(pageName string) string {
	if pageName == "" {
		return ""
	}
	return "css/pages/" + pageName + ".css"
}

// injectPerPageStylesheet adds a <link rel="stylesheet"> for
// css/pages/<pageName>.css at the end of <head> — but ONLY when that file
// actually exists under assetsDir. A page/site with no per-page stylesheet
// sees zero change, so this is purely additive and needs no opt-in flag.
// Injected last (after any explicit <link> tags already in the template)
// so page-specific rules win the cascade over shared tokens/fonts/app/public
// stylesheets. Must run before transformStylesheets/inlineLocalAssets so the
// injected tag flows through the SAME existing inline/fingerprint pipeline
// as every other stylesheet link — no new CSS-handling code needed, this is
// purely "decide whether to add one more <link> tag before head closes".
func injectPerPageStylesheet(html, assetsDir, pageName string) string {
	relPath := perPageStylesheetRelPath(pageName)
	if relPath == "" {
		return html
	}
	if _, err := os.Stat(filepath.Join(assetsDir, relPath)); err != nil {
		return html
	}
	inject := `<link rel="stylesheet" href="/` + relPath + `">`
	return strings.Replace(html, "</head>", inject+headEndTag, 1)
}

func transformPage(html string, opts buildOptions, assetsDir, pageName string, mirrorer *externalAssetMirrorer) (string, map[string]string, error) {
	html = injectPerPageStylesheet(html, assetsDir, pageName)
	html = injectNavigationEnhancements(html)
	html = injectTracker(html, opts)
	html = injectDevData(html, opts)

	if opts.inlineAssets {
		// Full asset inlining (CSS + JS + images). Converts url() to data URIs so the
		// page is fully self-contained. Does not fingerprint (data URIs need no cache key).
		updated, err := inlineLocalAssets(html, assetsDir)
		if err != nil {
			return "", nil, fmt.Errorf("inlining assets: %w", err)
		}
		html = updated
	} else {
		updated, err := applyPositionBasedTransforms(html, opts, assetsDir)
		if err != nil {
			return "", nil, err
		}
		html = updated
	}

	// Hoist any external @import (e.g. a Google Fonts stylesheet) left inside
	// an inlined <style> block out into a real <link rel="stylesheet"> plus a
	// preconnect hint. Runs after both the -inline-assets and the default
	// branch above, since either can produce a <style> block containing one
	// (flattenCSSImports always leaves an external @import verbatim — see
	// expandCSSImport in inlining.go).
	hoistedHTML, err := hoistExternalCSSImports(html)
	if err != nil {
		return "", nil, fmt.Errorf("hoisting external CSS imports: %w", err)
	}
	html = hoistedHTML

	// Default lazy-loading: any <img> not already carrying a loading attribute
	// gets loading="lazy" so the browser defers fetching/decoding it until it's
	// near the viewport. Above-the-fold images opt out by setting
	// loading="eager" explicitly in the template — the same attribute
	// processLQIPImages below reads to decide which images get a blur-up
	// placeholder — so eager images are left untouched here. Runs after SVG
	// inlining (inside applyPositionBasedTransforms above) so icons already
	// converted to inline <svg> are correctly skipped, and before LQIP so it
	// still sees the loading="eager" it depends on. Unconditional (no flag),
	// like injectCachedScriptPreloads: always beneficial, never load-order
	// breaking.
	lazyHTML, err := injectDefaultLazyLoading(html)
	if err != nil {
		return "", nil, fmt.Errorf("injecting default lazy loading: %w", err)
	}
	html = lazyHTML

	// LQIP: generate blurry thumbnails for above-fold images and swap to full on load.
	lqipHTML, err := processLQIPImages(html, assetsDir)
	if err != nil {
		return "", nil, fmt.Errorf("processing LQIP images: %w", err)
	}
	html = lqipHTML

	// Inline small raster images as base64 data URIs. Runs after LQIP so that
	// LQIP-processed images (whose src is already a data URI) are correctly skipped.
	if opts.rasterInlineThreshold > 0 {
		inlinedHTML, err := inlineSmallLocalRasterImages(html, assetsDir, opts.rasterInlineThreshold)
		if err != nil {
			return "", nil, fmt.Errorf("inlining small raster images: %w", err)
		}
		html = inlinedHTML
	}

	// Fingerprint all remaining external local asset references so CloudFront
	// can serve them with immutable (1-year) cache headers. basePath is
	// prepended to root-absolute refs so they resolve correctly when the site
	// is deployed under a path prefix (e.g. petlook /en, /pt, /jp).
	fingerprintedHTML, toCopy, err := fingerprintLocalAssets(html, assetsDir, opts.basePath)
	if err != nil {
		return "", nil, fmt.Errorf("fingerprinting assets: %w", err)
	}
	html = fingerprintedHTML

	if mirrorer != nil {
		updated, err := mirrorer.rewriteHTML(html)
		if err != nil {
			return "", nil, fmt.Errorf("mirroring external assets: %w", err)
		}
		html = updated
	}

	return html, toCopy, nil
}

// applyPositionBasedTransforms is the default asset transform path (used when
// -inline-assets is not set). It applies:
//   - Position-based CSS loading: head CSS inlined as <style>, body CSS deferred.
//   - SVG icon inlining for icons below svgInlineSizeLimit.
//   - JS inlining for scripts below opts.jsInlineThreshold (when > 0).
func applyPositionBasedTransforms(html string, opts buildOptions, assetsDir string) (string, error) {
	// Position-based CSS: head CSS → <style> (critical), body CSS → deferred external
	// (or inlined when -inline-body-css is set).
	updated, err := transformStylesheets(html, assetsDir, opts.embedFonts, opts.inlineBodyCSS, opts.cssPrune)
	if err != nil {
		return "", fmt.Errorf("transforming stylesheets: %w", err)
	}
	html = updated

	// Inline small SVGs as <svg> elements to eliminate per-icon HTTP requests.
	updated, err = inlineLocalSVGs(html, assetsDir)
	if err != nil {
		return "", fmt.Errorf("inlining SVGs: %w", err)
	}
	html = updated

	// Inline scripts below the size threshold to eliminate per-script HTTP requests.
	// Scripts referenced by more than one page use jsSharedInlineThreshold (when >= 0)
	// as their effective limit so they can be cached externally instead of inlined.
	if opts.jsInlineThreshold > 0 {
		updated, err = inlineSmallLocalScripts(html, assetsDir,
			opts.jsInlineThreshold, opts.jsSharedInlineThreshold, opts.sharedScripts)
		if err != nil {
			return "", fmt.Errorf("inlining small scripts: %w", err)
		}
		html = updated
	}
	// Defer every remaining local <script src> tag so the browser can keep
	// parsing/painting instead of blocking on each script's fetch in turn.
	updated, err = injectScriptDefer(html)
	if err != nil {
		return "", fmt.Errorf("deferring scripts: %w", err)
	}
	html = updated

	// Inject <link rel="preload" as="script"> hints for any local scripts that were NOT
	// inlined (i.e. they will be fingerprinted and served as separate cached files).
	// Preloads let the browser start fetching these scripts at HTML parse time instead
	// of waiting to encounter the <script> tag later in the body.
	html = injectCachedScriptPreloads(html)
	return html, nil
}

// injectScriptDefer adds `defer` to every local <script src="..."> tag that
// doesn't already declare `defer`, `async`, or `type="module"` (a module
// script is deferred by the HTML spec already). A script tag the template
// author already marked defer/async keeps whatever they chose.
//
// defer lets the browser continue parsing the rest of the document instead
// of blocking on each script's fetch in turn, while defer's own spec
// guarantee — deferred scripts execute in document order, after parsing
// completes but before DOMContentLoaded — keeps every script's relative
// execution order identical to today's synchronous load. That's the same
// "strictly faster, never a behavior change" shape as
// injectCachedScriptPreloads/injectDefaultLazyLoading, so — like them —
// this is unconditional, no flag.
//
// Scoped to local scripts only, matching inlineSmallLocalScripts/
// injectCachedScriptPreloads: an externally-hosted script may have an
// author-intended synchronous-load reason (e.g. a polyfill gating what
// runs after it) that this compiler has no visibility into.
func injectScriptDefer(html string) (string, error) {
	return replaceTagWith(html, scriptTagRE, func(tag string, refs []string) (string, error) {
		src := refs[1]
		if isExternalRef(src) {
			return tag, nil
		}
		if strings.EqualFold(getTagAttr(tag, "type"), "module") {
			return tag, nil
		}
		if scriptDeferOrAsyncRE.MatchString(tag) {
			return tag, nil
		}
		// tag is the whole "<script ...src=...>...</script>" span --
		// scriptTagRE requires an immediately-following close tag with no
		// body — so addOrReplaceAttr's generic "insert before the tag's
		// last >" would land on </script>'s '>' instead of the opening
		// tag's, producing invalid markup like "</script defer>".
		// Insert right after the OPENING tag's own '>' (the first one)
		// explicitly instead.
		idx := strings.Index(tag, ">")
		if idx == -1 {
			return tag, nil
		}
		// Bare `defer`, not `defer="defer"`. Both are valid HTML, but the
		// value form is the XHTML spelling and html-validate's default
		// attribute-boolean-style ("omit") rejects it — so every consumer
		// running the fleet's own markup gate sees one error per deferred
		// script in output this compiler generated. Measured on ffreis-editify:
		// 8 of its 35 html-validate errors were exactly this.
		return tag[:idx] + ` defer` + tag[idx:], nil
	})
}

// injectCachedScriptPreloads injects a <link rel="preload" as="script" href="..."> hint
// before </head> for every local <script src="..."> tag that survived the inline step
// (i.e. will be served as a separate cached file). Preloads that are already present and
// external script URLs are skipped. The injected hrefs are local paths so they will be
// fingerprinted by the subsequent fingerprintLocalAssets step, resulting in the same
// content-hash as the script tag itself — the browser's preload cache hit fires exactly
// when the deferred script tag is encountered.
func injectCachedScriptPreloads(html string) string {
	// Collect all remaining local <script src> hrefs.
	var preloads []string
	seen := map[string]bool{}
	scriptTagRE.ReplaceAllStringFunc(html, func(tag string) string {
		m := scriptTagRE.FindStringSubmatch(tag)
		if m == nil {
			return tag
		}
		src := m[1]
		if isExternalRef(src) || seen[src] {
			return tag
		}
		seen[src] = true
		preloads = append(preloads, src)
		return tag
	})
	if len(preloads) == 0 {
		return html
	}

	// Collect hrefs already declared as preloads in the document so we don't duplicate.
	existingPreloads := map[string]bool{}
	preloadTagRE.ReplaceAllStringFunc(html, func(tag string) string {
		m := preloadTagRE.FindStringSubmatch(tag)
		if m != nil {
			existingPreloads[m[1]] = true
		}
		return tag
	})

	// Build the injection block.
	var sb strings.Builder
	for _, src := range preloads {
		if existingPreloads[src] {
			continue
		}
		sb.WriteString(`<link rel="preload" as="script" href="`)
		sb.WriteString(src)
		sb.WriteString("\">\n    ")
	}
	inject := strings.TrimRight(sb.String(), "\n ")
	if inject == "" {
		return html
	}
	return headEndRE.ReplaceAllString(html, inject+headEndTag)
}

// transformStylesheets applies position-based CSS loading: stylesheet links in <head>
// are inlined (critical path, zero-latency), while links in <body> are kept external
// and given the media="print" onload deferred-loading pattern (non-blocking).
//
// This mirrors the JS-at-end convention: document position signals loading priority.
// Templates that want a stylesheet deferred simply place its <link> in the body.
// If </head> is absent the entire document is treated as head (all CSS inlined).
func transformStylesheets(doc, assetsDir string, embedFonts, inlineBodyCSS bool, prune cssPruneOptions) (string, error) {
	loc := headEndRE.FindStringIndex(doc)
	if loc == nil {
		return inlineLocalStylesheetsPreserveURLs(doc, doc, assetsDir, embedFonts, prune)
	}
	head, err := inlineLocalStylesheetsPreserveURLs(doc[:loc[0]], doc, assetsDir, embedFonts, prune)
	if err != nil {
		return "", err
	}
	var tail string
	if inlineBodyCSS {
		tail, err = inlineLocalStylesheetsPreserveURLs(doc[loc[0]:], doc, assetsDir, embedFonts, prune)
	} else {
		tail, err = deferLocalStylesheets(doc[loc[0]:], assetsDir)
	}
	if err != nil {
		return "", err
	}
	return head + tail, nil
}

// deferLocalStylesheets rewrites local <link rel="stylesheet"> tags found in the
// document body to use the media="print" onload deferred-loading pattern, which
// fetches the CSS without blocking rendering. A <noscript> fallback is appended after
// each transformed tag for environments where JavaScript is unavailable.
// External stylesheet URLs are left unchanged.
func deferLocalStylesheets(doc, assetsDir string) (string, error) {
	return replaceTagWith(doc, stylesheetTagRE, func(tag string, refs []string) (string, error) {
		href := refs[1]
		if isExternalRef(href) {
			return tag, nil
		}
		// Verify the asset exists; skip if not found.
		if _, _, err := readAsset(assetsDir, href); err != nil {
			return tag, nil
		}
		deferred := addOrReplaceAttr(tag, "media", "print")
		deferred = addOrReplaceAttr(deferred, "onload", "this.media='all'")
		noscript := `<noscript>` + tag + `</noscript>`
		return deferred + "\n" + noscript, nil
	})
}

// inlineLocalSVGs replaces <img src="*.svg"> tags that reference small local
// SVG files with the SVG XML content inline, eliminating the HTTP request for
// the icon. SVGs >= svgInlineSizeLimit are left for normal fingerprinting.
//
// Runs in the default build pipeline after CSS transforms and before LQIP and
// fingerprinting, so that inlined SVGs are not double-processed. Skipped in
// -inline-assets mode where inlineLocalImages already handles SVGs as data URIs.
func inlineLocalSVGs(doc, assetsDir string) (string, error) {
	return replaceTagWith(doc, imgTagRE, func(tag string, refs []string) (string, error) {
		src := refs[1]
		if isExternalRef(src) {
			return tag, nil
		}
		if strings.ToLower(path.Ext(strings.SplitN(src, "?", 2)[0])) != ".svg" {
			return tag, nil
		}
		data, _, err := readAsset(assetsDir, src)
		if err != nil || len(data) >= svgInlineSizeLimit {
			return tag, nil // not found or too large: leave for fingerprinting
		}
		return svgFromImgTag(tag, data), nil
	})
}

// injectDefaultLazyLoading adds loading="lazy" to every <img> that doesn't
// already declare a loading attribute (eager, lazy, or otherwise) — see the
// call site in transformPage for why this runs unconditionally and where it
// sits relative to SVG inlining and LQIP.
func injectDefaultLazyLoading(html string) (string, error) {
	return replaceTagWith(html, imgTagRE, func(tag string, _ []string) (string, error) {
		if getTagAttr(tag, "loading") != "" {
			return tag, nil // explicit loading attribute present — respect it
		}
		return addOrReplaceAttr(tag, "loading", "lazy"), nil
	})
}

// svgFromImgTag prepares raw SVG bytes for inline HTML embedding:
//   - strips the <?xml ...?> processing instruction (invalid in HTML5)
//   - merges the img's class attribute into the <svg> root element's class
//   - converts a non-empty alt to aria-label + role="img" on <svg>;
//     an empty alt becomes aria-hidden="true" (decorative icon)
//   - copies width/height from the img when the <svg> root lacks them
func svgFromImgTag(imgTag string, svgData []byte) string {
	svg := strings.TrimSpace(xmlProcInstRE.ReplaceAllString(string(svgData), ""))

	loc := svgRootTagRE.FindStringIndex(svg)
	if loc == nil {
		return svg
	}

	root := svg[loc[0]:loc[1]]
	extra := ""

	imgClass := getTagAttr(imgTag, "class")
	imgAlt := getTagAttr(imgTag, "alt")
	imgWidth := getTagAttr(imgTag, "width")
	imgHeight := getTagAttr(imgTag, "height")

	if imgClass != "" {
		if existing := getTagAttr(root, "class"); existing != "" {
			root = strings.Replace(root, `class="`+existing+`"`, `class="`+imgClass+` `+existing+`"`, 1)
		} else {
			extra += ` class="` + imgClass + `"`
		}
	}
	if imgAlt != "" {
		// imgAlt is the raw attribute value extracted from HTML — already entity-encoded
		// by the template engine. Only escape " to avoid breaking the aria-label delimiter.
		extra += ` aria-label="` + strings.ReplaceAll(imgAlt, `"`, "&quot;") + `" role="img"`
	} else if getTagAttr(root, "aria-hidden") == "" {
		extra += ` aria-hidden="true"`
	}
	if imgWidth != "" && getTagAttr(root, "width") == "" {
		extra += ` width="` + imgWidth + `"`
	}
	if imgHeight != "" && getTagAttr(root, "height") == "" {
		extra += ` height="` + imgHeight + `"`
	}

	if extra != "" {
		if strings.HasSuffix(root, "/>") {
			root = root[:len(root)-2] + extra + "/>"
		} else {
			root = root[:len(root)-1] + extra + ">"
		}
	}

	return svg[:loc[0]] + root + svg[loc[1]:]
}

// googleFontsCSSOrigin is special-cased in hoistExternalCSSImports: Google's
// own documented recommendation is to preconnect to fonts.googleapis.com (the
// CSS) AND fonts.gstatic.com (the font files that CSS response references),
// not just the host the @import itself names. Specific and verifiable enough
// to hardcode; a general CDN-to-related-host table is not attempted here.
const (
	googleFontsCSSOrigin  = "https://fonts.googleapis.com"
	googleFontsFileOrigin = "https://fonts.gstatic.com"
)

// hoistExternalCSSImports finds @import url("https://...") statements left
// inside an inlined <style> block — flattenCSSImports always leaves an
// external @import verbatim (see expandCSSImport in inlining.go), since the
// compiler has no local file to expand it from — and hoists each into a real
// <link rel="stylesheet"> immediately before that <style> block, plus a
// <link rel="preconnect"> for its origin.
//
// An @import nested inside a <style> block still carries @import's inherent
// "this stylesheet isn't ready until the import resolves" semantics, and is
// less reliably/early discovered by a browser's preload scanner than an
// explicit sibling <link> — the same reason every web-performance guide
// recommends <link> over @import, regardless of whether the importing
// stylesheet is itself inline or external. Hoisting changes nothing about
// what loads or how the page looks, only when the browser can start
// fetching it — like injectCachedScriptPreloads/injectDefaultLazyLoading,
// this is unconditional, no flag: strictly faster, never a behavior change.
//
// The hoisted <link> is inserted immediately before the <style> block it
// came from (not collected and dropped at the end of <head>) so cascade
// order is preserved exactly: @import rules apply before the rest of that
// stylesheet, same as a preceding sibling <link> would. Each distinct
// import URL is hoisted once per page even if it's repeated across multiple
// <style> blocks (e.g. separately inlined head and body CSS both importing
// the same font).
func hoistExternalCSSImports(html string) (string, error) {
	seenImport := map[string]bool{}
	seenOrigin := map[string]bool{}

	return replaceTagWith(html, styleTagRE, func(_ string, refs []string) (string, error) {
		attrs, content := refs[1], refs[2]

		var prefix strings.Builder
		newContent := cssImportRE.ReplaceAllStringFunc(content, func(stmt string) string {
			m := cssImportRE.FindStringSubmatch(stmt)
			if m == nil {
				return stmt
			}
			ref := strings.TrimSpace(m[1])
			if !isExternalRef(ref) {
				return stmt // local @import: flattenCSSImports already expanded it
			}
			// @import's optional trailing media query ("@import url(...)
			// print;") must carry over as the hoisted <link>'s media
			// attribute — dropping it would silently make a conditional
			// import unconditional.
			media := strings.TrimSpace(m[2])
			if seenImport[ref] {
				return "" // already hoisted elsewhere on this page
			}
			seenImport[ref] = true

			origin := originOf(ref)
			if !seenOrigin[origin] {
				seenOrigin[origin] = true
				prefix.WriteString(`<link rel="preconnect" href="` + origin + `">` + "\n    ")
				if origin == googleFontsCSSOrigin && !seenOrigin[googleFontsFileOrigin] {
					seenOrigin[googleFontsFileOrigin] = true
					prefix.WriteString(`<link rel="preconnect" href="` + googleFontsFileOrigin + `" crossorigin>` + "\n    ")
				}
			}
			mediaAttr := ""
			if media != "" {
				mediaAttr = ` media="` + htmlEscape(media) + `"`
			}
			prefix.WriteString(`<link rel="stylesheet" href="` + ref + `"` + mediaAttr + `>` + "\n    ")
			return ""
		})

		return prefix.String() + "<style" + attrs + ">" + newContent + "</style>", nil
	})
}

// originOf returns rawURL's scheme://host, or rawURL itself if it doesn't
// parse as an absolute URL (defensive only — isExternalRef already confirmed
// an http(s):// or scheme-relative prefix before this is ever called).
func originOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return rawURL
	}
	return u.Scheme + "://" + u.Host
}
