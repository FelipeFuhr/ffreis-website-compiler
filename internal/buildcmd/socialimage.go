package buildcmd

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"ffreis-website-compiler/internal/mediameta"
)

// socialImageExtensions lists the candidate filenames checked, in priority
// order, for the site-wide Open Graph / Twitter Card preview image
// convention: src/assets/img/og-image.{png,jpg,jpeg,webp}. First match wins.
var socialImageExtensions = []string{"png", "jpg", "jpeg", "webp"}

// metaTagRE matches any <meta> tag. Attributes are then read with getTagAttr
// rather than captured positionally, because meta attribute order is author's
// choice (`property` before `content` or after) and a regex that assumed one
// order would silently skip the other — the same class of silent miss this
// whole pass exists to fix.
var metaTagRE = regexp.MustCompile(`(?is)<meta\s+[^>]*>`)

// socialImageMetaKeys are the property/name values whose content attribute
// holds an image URL that must be fingerprinted like any other local asset.
var socialImageMetaKeys = map[string]bool{
	"og:image":            true,
	"og:image:url":        true,
	"og:image:secure_url": true,
	"twitter:image":       true,
	"twitter:image:src":   true,
}

// injectSocialImage adds <meta property="og:image">, its width/height, and
// <meta name="twitter:image"> to a page's <head>, using the site-wide
// src/assets/img/og-image.* convention (zero-config, mirrors favicon.ico):
// if the file exists, fingerprint and copy it like any other local asset,
// then emit tags pointing at base_url + the fingerprinted path.
//
// Open Graph/Twitter crawlers require an ABSOLUTE image URL, which the
// generic fingerprinting pass (fingerprintLocalAssets, fingerprint.go)
// cannot produce on its own: it only rewrites relative local references in
// place and explicitly treats any http(s):// value as external, leaving it
// untouched (isExternalRef). So this runs as its own pass in writePages, the
// same stage as injectHreflangAlternates, with direct access to base_url
// from site data — constructing the absolute URL itself rather than asking
// a template author to pre-compute it and hope fingerprinting notices.
//
// When the page's own template DOES declare an og:image/twitter:image, that
// content still wins over the site-wide default, but its reference is handed
// to fingerprintAuthoredSocialImages rather than ignored. Skipping it used to
// ship a tag naming a file the build never wrote (only fingerprinted copies
// reach the output — see copyStaticAssets), so the social preview 404'd after
// every deploy with a green build.
//
// No-ops (returns html unchanged, nil toCopy) when base_url is absent from
// site data (an og:image would otherwise be a non-absolute, spec-violating
// URL — same reasoning as injectHreflangAlternates's own no-op guard), or no
// og-image.* file exists under assetsDir and the template declares none.
func injectSocialImage(html string, siteData map[string]any, assetsDir, basePath string) (string, map[string]string, error) {
	baseURL, _ := siteData["base_url"].(string)
	if baseURL == "" {
		return html, nil, nil
	}
	baseURL = strings.TrimRight(baseURL, "/")

	// A template that declares its own og:image wins over the site-wide
	// default — but its reference still has to be fingerprinted and copied,
	// which is exactly what used to be skipped here. Returning early left the
	// authored tag naming a file that the build never wrote (only fingerprinted
	// copies reach the output; see copyStaticAssets), so the social preview
	// 404'd after every deploy while the build stayed green.
	if strings.Contains(html, `property="og:image"`) || strings.Contains(html, `name="twitter:image"`) {
		return fingerprintAuthoredSocialImages(html, baseURL, assetsDir, basePath)
	}

	relPath, data, ok := findSocialImage(assetsDir)
	if !ok {
		return html, nil, nil
	}

	fp := &assetFingerprinter{
		assetsDir:      assetsDir,
		normalizedBase: strings.TrimRight(basePath, "/"),
		hashCache:      make(map[string]string),
		toCopy:         make(map[string]string),
	}
	hashedRef, err := fp.resolve("/" + relPath)
	if err != nil {
		return "", nil, fmt.Errorf("fingerprinting social image: %w", err)
	}
	absoluteURL := baseURL + hashedRef

	width, height, err := mediameta.ImageDimensions(data)
	if err != nil {
		return "", nil, fmt.Errorf("reading social image dimensions (%s): %w", relPath, err)
	}

	var tags strings.Builder
	fmt.Fprintf(&tags, `<meta property="og:image" content="%s">`, absoluteURL)
	tags.WriteByte('\n')
	fmt.Fprintf(&tags, `<meta property="og:image:width" content="%d">`, width)
	tags.WriteByte('\n')
	fmt.Fprintf(&tags, `<meta property="og:image:height" content="%d">`, height)
	tags.WriteByte('\n')
	fmt.Fprintf(&tags, `<meta name="twitter:image" content="%s">`, absoluteURL)
	tags.WriteByte('\n')

	return strings.Replace(html, "</head>", tags.String()+"</head>", 1), fp.toCopy, nil
}

// fingerprintAuthoredSocialImages rewrites the content attribute of every
// template-authored social-image meta tag to the fingerprinted filename, and
// returns the copy instructions so the file actually reaches the output.
//
// The hard part is the URL shape. Open Graph requires an absolute URL, so a
// correctly-written template emits one — ffreis-arcade's head partial builds
// `{{base_url}}{{base_path}}/assets/social/og-image.png`. The generic
// fingerprinting pass treats any http(s):// value as external and skips it
// (isExternalRef), which is why these tags were invisible to it. Here, a value
// that starts with the site's own base_url is stripped back to a site-relative
// path, fingerprinted, and reassembled — so same-origin absolute URLs are
// handled while a genuinely external (CDN-hosted) og:image is still left alone.
//
// baseURL must already be trimmed of its trailing slash.
func fingerprintAuthoredSocialImages(html, baseURL, assetsDir, basePath string) (string, map[string]string, error) {
	normalizedBase := strings.TrimRight(basePath, "/")
	fp := &assetFingerprinter{
		assetsDir:      assetsDir,
		normalizedBase: normalizedBase,
		hashCache:      make(map[string]string),
		toCopy:         make(map[string]string),
	}

	out, err := replaceTagWith(html, metaTagRE, func(tag string, _ []string) (string, error) {
		key := getTagAttr(tag, "property")
		if key == "" {
			key = getTagAttr(tag, "name")
		}
		if !socialImageMetaKeys[key] {
			return tag, nil
		}

		content := getTagAttr(tag, "content")
		if content == "" || isDataURI(content) {
			return tag, nil
		}

		// Reduce the value to a site-relative reference, remembering whether it
		// was absolute so the rewrite can restore the same shape.
		ref := content
		wasAbsolute := false
		if baseURL != "" && strings.HasPrefix(ref, baseURL) {
			ref = strings.TrimPrefix(ref, baseURL)
			wasAbsolute = true
		} else if isExternalRef(ref) {
			return tag, nil // a different origin's image: not ours to fingerprint
		}

		// base_path is already baked into an authored absolute URL, and
		// fp.resolve prepends it again for root-absolute refs, so strip it
		// first or the result double-prefixes to /en/en/...
		if normalizedBase != "" && strings.HasPrefix(ref, normalizedBase+"/") {
			ref = strings.TrimPrefix(ref, normalizedBase)
		}
		if ref == "" {
			return tag, nil
		}

		hashed, err := fp.resolve(ref)
		if err != nil {
			return "", err
		}
		if hashed == ref {
			return tag, nil // asset not found on disk: leave the tag untouched
		}

		newContent := hashed
		if wasAbsolute {
			newContent = baseURL + hashed
		}
		return fp.rewriteAttr(tag, "content", content, newContent), nil
	})
	if err != nil {
		return "", nil, fmt.Errorf("fingerprinting authored social image: %w", err)
	}
	if len(fp.toCopy) == 0 {
		// Keep this function's no-op contract identical to injectSocialImage's:
		// a nil map, not an empty one, when there was nothing to rewrite (every
		// authored tag was external, a data URI, or named a file not on disk).
		return out, nil, nil
	}
	return out, fp.toCopy, nil
}

// findSocialImage checks src/assets/img/og-image.{png,jpg,jpeg,webp} in that
// priority order and returns the first one found, already read into memory
// so the caller does not re-read the file just to decode its dimensions.
func findSocialImage(assetsDir string) (relPath string, data []byte, ok bool) {
	for _, ext := range socialImageExtensions {
		candidate := path.Join("img", "og-image."+ext)
		content, _, err := readAsset(assetsDir, candidate)
		if err == nil {
			return candidate, content, true
		}
	}
	return "", nil, false
}
