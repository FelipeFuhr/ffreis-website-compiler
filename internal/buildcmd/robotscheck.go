package buildcmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const fileRobotsTxt = "robots.txt"

// checkRobotsTxtSafety fails the build when the site's robots.txt disallows
// the entire site for every crawler (a "User-agent: *" block containing a
// bare "Disallow: /" rule) on what is treated as a real content build
// (opts.contentSource != "mock" — the same signal that already gates the
// mock-content anti-leak guard in parseBuildOptions).
//
// This is the fleet's most common accidental-deindex mistake: a pre-launch
// site intentionally ships a blanket disallow while it has no production
// audience yet, and if that file is never revisited before the site's first
// real deployment, the entire site silently never gets crawled — with no
// build failure or warning to catch it. Dev-only builds (-content-source
// mock) are exempt since blocking crawlers there is expected and safe.
// -content-source dev builds are also exempt in practice: parseBuildOptions
// sets -allow-blanket-robots-disallow automatically for them, since a
// dev.ffreis.com build commonly ships the same blanket block. A genuine,
// deliberate full block on a real -content-source=prod build (rare) can opt
// out with -allow-blanket-robots-disallow passed explicitly.
func checkRobotsTxtSafety(assetsDir string, opts buildOptions) error {
	if opts.contentSource == "mock" || opts.allowBlanketRobotsDisallow {
		return nil
	}

	path := filepath.Join(assetsDir, fileRobotsTxt)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no robots.txt at all is not this check's concern
		}
		return fmt.Errorf("reading %s for safety check: %w", path, err)
	}
	defer f.Close()

	blocked, err := robotsTxtBlanketDisallows(f)
	if err != nil {
		return fmt.Errorf("scanning %s for safety check: %w", path, err)
	}
	if blocked {
		return fmt.Errorf(
			"%s contains a blanket \"Disallow: /\" under \"User-agent: *\", which "+
				"blocks every crawler from the entire site; this looks like a "+
				"dev-only placeholder left in place for a real content build "+
				"(-content-source=%s). Fix the file before shipping to prod, or "+
				"pass -allow-blanket-robots-disallow if a full block is deliberate here",
			path, opts.contentSource,
		)
	}
	return nil
}

// robotsTxtBlanketDisallows scans robots.txt content for a bare "Disallow: /"
// rule (no path segment after the slash) inside a wildcard "User-agent: *"
// block. It does not attempt a full robots.txt parse — only the specific
// blanket-block shape used fleet-wide as a dev/pre-launch placeholder.
func robotsTxtBlanketDisallows(r io.Reader) (bool, error) {
	scanner := bufio.NewScanner(r)
	inWildcardBlock := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		switch key {
		case "user-agent":
			inWildcardBlock = value == "*"
		case "disallow":
			if inWildcardBlock && value == "/" {
				return true, nil
			}
		}
	}
	return false, scanner.Err()
}
