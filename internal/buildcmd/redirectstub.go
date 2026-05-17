package buildcmd

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeRedirectStub creates outDir/index.html containing an immediate client-side
// redirect to targetURL. Used when a content item is not available in the current
// build language — the stub ensures the URL resolves and silently forwards the
// visitor to the best available language version.
//
// The stub uses three redirect mechanisms in order of speed:
//  1. window.location.replace() — instant, no history entry
//  2. <meta http-equiv="refresh"> — fallback for JS-disabled browsers
//  3. <a href> — last resort for fully stripped environments
//
// <link rel="canonical"> points to targetURL so search engines index only the
// canonical language version, not the stub page.
func writeRedirectStub(outDir, targetURL string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating redirect stub dir %s: %w", outDir, err)
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<link rel="canonical" href="%s">
<meta http-equiv="refresh" content="0;url=%s">
<script>window.location.replace("%s");</script>
</head>
<body><a href="%s">This content is not available in this language.</a></body>
</html>`, targetURL, targetURL, targetURL, targetURL)

	target := filepath.Join(outDir, "index.html")
	if err := os.WriteFile(target, []byte(html), 0o644); err != nil { //nolint:gosec
		return fmt.Errorf(errFmtWriting, target, err)
	}
	return nil
}
