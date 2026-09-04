package buildcmd

import "log/slog"

// validateImageAltText warns — but does not fail the build — when a rendered
// <img> tag has no alt attribute at all.
//
// This is the same family of check as validateRenderedPageStructure
// (buildcmd.go): both catch content the template author almost certainly
// meant to fill in. Unlike that check, this one is a WARNING, not a build
// failure — this repo is a shared build engine consumed by four live sites,
// and none of them have ever had to satisfy an alt-text requirement before.
// Making this fatal on its first pass would risk breaking another site's CI
// over pre-existing content gaps with no warning period. Revisit once the
// fleet has had a chance to clean up any warnings this surfaces.
//
// An explicitly empty alt="" is valid, standard markup for a decorative
// image (screen readers correctly skip it) and is intentionally NOT flagged
// — only a fully absent alt attribute is, since that is almost always an
// accessibility gap rather than a deliberate choice.
func validateImageAltText(logger *slog.Logger, pageName, html string) {
	altAttrRE := cachedAttrSearchRE("alt")
	for _, m := range imgTagRE.FindAllStringSubmatch(html, -1) {
		tag, src := m[0], m[1]
		if altAttrRE.MatchString(tag) {
			continue
		}
		logger.Warn("img missing alt attribute — add alt=\"...\" (or alt=\"\" if purely decorative)",
			"page", pageName, "src", src)
	}
}
