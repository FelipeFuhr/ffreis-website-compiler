package buildcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRobotsTxt(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, fileRobotsTxt), []byte(body), 0o644); err != nil {
		t.Fatalf("writing robots.txt: %v", err)
	}
}

func TestRobotsTxtBlanketDisallows(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "blanket disallow under wildcard agent",
			body: "User-agent: *\nDisallow: /\n",
			want: true,
		},
		{
			name: "blanket disallow with comment and blank lines",
			body: "# dev only\n\nUser-agent: *\nDisallow: /\n",
			want: true,
		},
		{
			name: "path-scoped disallow is fine",
			body: "User-agent: *\nDisallow: /admin/\n",
			want: false,
		},
		{
			name: "allow-all is fine",
			body: "User-agent: *\nAllow: /\n",
			want: false,
		},
		{
			name: "blanket disallow scoped to a specific bot only",
			body: "User-agent: BadBot\nDisallow: /\n",
			want: false,
		},
		{
			name: "empty file",
			body: "",
			want: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := robotsTxtBlanketDisallows(strings.NewReader(c.body))
			if err != nil {
				t.Fatalf("robotsTxtBlanketDisallows: %v", err)
			}
			if got != c.want {
				t.Errorf("robotsTxtBlanketDisallows(%q) = %v, want %v", c.body, got, c.want)
			}
		})
	}
}

func TestCheckRobotsTxtSafety_FailsOnProdBlanketDisallow(t *testing.T) {
	dir := t.TempDir()
	writeRobotsTxt(t, dir, "User-agent: *\nDisallow: /\n")

	opts := buildOptions{contentSource: "prod"}
	if err := checkRobotsTxtSafety(dir, opts); err == nil {
		t.Fatal("expected an error for a blanket disallow on a prod build, got nil")
	}
}

func TestCheckRobotsTxtSafety_AllowsMockBuild(t *testing.T) {
	dir := t.TempDir()
	writeRobotsTxt(t, dir, "User-agent: *\nDisallow: /\n")

	opts := buildOptions{contentSource: "mock"}
	if err := checkRobotsTxtSafety(dir, opts); err != nil {
		t.Fatalf("expected no error for a mock build, got: %v", err)
	}
}

func TestCheckRobotsTxtSafety_AllowsExplicitOverride(t *testing.T) {
	dir := t.TempDir()
	writeRobotsTxt(t, dir, "User-agent: *\nDisallow: /\n")

	opts := buildOptions{contentSource: "prod", allowBlanketRobotsDisallow: true}
	if err := checkRobotsTxtSafety(dir, opts); err != nil {
		t.Fatalf("expected no error with the explicit override, got: %v", err)
	}
}

func TestCheckRobotsTxtSafety_AllowsMissingFile(t *testing.T) {
	dir := t.TempDir()
	opts := buildOptions{contentSource: "prod"}
	if err := checkRobotsTxtSafety(dir, opts); err != nil {
		t.Fatalf("expected no error when robots.txt does not exist, got: %v", err)
	}
}

func TestCheckRobotsTxtSafety_AllowsScopedDisallowOnProd(t *testing.T) {
	dir := t.TempDir()
	writeRobotsTxt(t, dir, "User-agent: *\nDisallow: /admin/\n")

	opts := buildOptions{contentSource: "prod"}
	if err := checkRobotsTxtSafety(dir, opts); err != nil {
		t.Fatalf("expected no error for a path-scoped disallow, got: %v", err)
	}
}

// The following two tests exercise checkRobotsTxtSafety together with
// parseBuildOptions, rather than a hand-built buildOptions{}, because the
// "-content-source dev implies -allow-blanket-robots-disallow" behavior lives
// in parseBuildOptions, not in checkRobotsTxtSafety itself. Building opts by
// hand (as the tests above do) would silently skip the very implication these
// tests are meant to prove.

func TestCheckRobotsTxtSafety_DevContentSourceAllowsBlanketDisallowWithoutExplicitFlag(t *testing.T) {
	dir := t.TempDir()
	writeRobotsTxt(t, dir, "User-agent: *\nDisallow: /\n")

	opts, err := parseBuildOptions([]string{
		"-website-root", ".",
		"-content-source", "dev",
	})
	if err != nil {
		t.Fatalf("parseBuildOptions: %v", err)
	}
	// -allow-blanket-robots-disallow was NOT passed above; content-source=dev
	// must imply it so this build succeeds despite the blanket disallow.
	if err := checkRobotsTxtSafety(dir, opts); err != nil {
		t.Fatalf("expected no error for a dev build with a blanket disallow and no explicit -allow-blanket-robots-disallow, got: %v", err)
	}
}

func TestCheckRobotsTxtSafety_ProdContentSourceStillFailsOnBlanketDisallowRegressionCheck(t *testing.T) {
	dir := t.TempDir()
	writeRobotsTxt(t, dir, "User-agent: *\nDisallow: /\n")

	opts, err := parseBuildOptions([]string{
		"-website-root", ".",
	})
	if err != nil {
		t.Fatalf("parseBuildOptions: %v", err)
	}
	// content-source=dev's new implication must not loosen prod's behavior:
	// the default (prod) build with the same blanket-disallow robots.txt and
	// no explicit -allow-blanket-robots-disallow must still fail.
	if err := checkRobotsTxtSafety(dir, opts); err == nil {
		t.Fatal("expected an error for a prod build with a blanket disallow and no explicit -allow-blanket-robots-disallow, got nil")
	}
}
