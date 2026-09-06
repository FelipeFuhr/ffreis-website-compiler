package cli

import (
	"os"
	"testing"
)

// TestRun_VersionDispatchesToVersioncmd proves the "version"/"-version"/
// "--version" case actually reaches versioncmd.Run rather than falling
// through to the default "unknown command" branch (which would os.Exit(1)
// and fail this test loudly rather than silently). versioncmd.Run with
// -json has no filesystem/network side effects, so it's safe to invoke
// cli.Run directly in-process rather than via a subprocess.
func TestRunVersionDispatchesToVersioncmd(t *testing.T) {
	for _, spelling := range []string{"version", "-version", "--version"} {
		t.Run(spelling, func(t *testing.T) {
			origArgs := os.Args
			defer func() { os.Args = origArgs }()
			os.Args = []string{"website-compiler", spelling, "-json"}

			// Run does not return a value; a dispatch failure (unknown
			// command, or versioncmd.Run returning a non-nil error) calls
			// os.Exit(1), which would abort the whole test binary. Reaching
			// the line after Run() is itself the assertion that dispatch
			// and execution both succeeded.
			Run("website-compiler")
		})
	}
}
