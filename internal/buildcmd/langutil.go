package buildcmd

import "strings"

// currentLangPrefix returns the URL prefix code ("en", "pt", "jp") for the
// current build by stripping the leading "/" from base_path in site data.
// Returns "" if base_path is absent or not a string.
func currentLangPrefix(siteData map[string]any) string {
	bp, _ := siteData["base_path"].(string)
	return strings.TrimPrefix(bp, "/")
}

// isAvailable reports whether lang is considered available for a content item.
// An empty/nil available list means the item is available in all languages.
func isAvailable(available []string, lang string) bool {
	if len(available) == 0 {
		return true
	}
	for _, l := range available {
		if l == lang {
			return true
		}
	}
	return false
}

// redirectTarget returns the URL prefix to redirect to when content is not
// available in currentLang. Priority: site default → "en" → first available.
// Returns "" if available is empty (should not happen when called after isAvailable).
func redirectTarget(currentLang string, available []string, siteData map[string]any) string {
	siteDefault, _ := siteData["language_default"].(string)

	candidates := []string{siteDefault, "en"}
	for _, c := range candidates {
		if c != "" && c != currentLang && isAvailable(available, c) {
			return c
		}
	}
	for _, l := range available {
		if l != currentLang {
			return l
		}
	}
	return ""
}

// toLangsAny converts a []string to []any for template consumption.
func toLangsAny(langs []string) []any {
	if len(langs) == 0 {
		return nil
	}
	out := make([]any, len(langs))
	for i, l := range langs {
		out[i] = l
	}
	return out
}
