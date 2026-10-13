package buildcmd

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig
	_ "image/png"  // register PNG decoder for image.DecodeConfig
	"path"
	"strings"

	_ "golang.org/x/image/webp" // register WebP decoder for image.DecodeConfig
)

// socialImageExtensions lists the candidate filenames checked, in priority
// order, for the site-wide Open Graph / Twitter Card preview image
// convention: src/assets/img/og-image.{png,jpg,jpeg,webp}. First match wins.
var socialImageExtensions = []string{"png", "jpg", "jpeg", "webp"}

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
// a template author to pre-compute it and hope fingerprinting notices. A
// template that does exactly that (renders its own absolute-URL og:image
// meta tag) ships a reference the fingerprinter silently skips and the file
// is never copied — confirmed by building and inspecting dist/ directly,
// not assumed.
//
// No-ops (returns html unchanged, nil toCopy) when: base_url is absent from
// site data (an og:image would otherwise be a non-absolute, spec-violating
// URL — same reasoning as injectHreflangAlternates's own no-op guard), no
// og-image.* file exists under assetsDir, or the page's own template
// already declares an og:image/twitter:image tag (template-authored content
// always wins over the site-wide default).
func injectSocialImage(html string, siteData map[string]any, assetsDir, basePath string) (string, map[string]string, error) {
	baseURL, _ := siteData["base_url"].(string)
	if baseURL == "" {
		return html, nil, nil
	}
	if strings.Contains(html, `property="og:image"`) || strings.Contains(html, `name="twitter:image"`) {
		return html, nil, nil
	}
	baseURL = strings.TrimRight(baseURL, "/")

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

	width, height, err := decodeImageDimensions(data)
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

func decodeImageDimensions(data []byte) (width, height int, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}
