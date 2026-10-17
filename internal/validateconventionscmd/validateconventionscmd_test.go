package validateconventionscmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/conventions"
)

func siteWith(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRun_FailsAndListsEveryViolation(t *testing.T) {
	// A site adopting a rule needs the whole list at once; reporting the first
	// violation only would turn adoption into a guessing loop.
	root := siteWith(t, map[string]string{
		"conventions.yaml":     "rules:\n  no-px-literals:\n    enabled: true\n",
		"src/assets/css/a.css": ".a{margin:2px}\n.b{top:4px}\n",
	})
	err := Run([]string{"-website-root", root}, nil)
	if err == nil {
		t.Fatal("expected the gate to fail")
	}
	if !strings.Contains(err.Error(), "2 conventions violation(s)") {
		t.Errorf("expected both violations counted, got: %v", err)
	}
	for _, want := range []string{`"2px"`, `"4px"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the message to name %s, got: %v", want, err)
		}
	}
}

func TestRun_PassesOnACleanSite(t *testing.T) {
	root := siteWith(t, map[string]string{
		"conventions.yaml":     "rules:\n  no-px-literals:\n    enabled: true\n",
		"src/assets/css/a.css": ".a{margin:1rem}",
	})
	if err := Run([]string{"-website-root", root}, nil); err != nil {
		t.Fatalf("expected a clean site to pass, got: %v", err)
	}
}

func TestRun_NoConfigIsNotAFailure(t *testing.T) {
	// A site that has adopted no rules must not fail; adoption is per-site and
	// deliberate.
	root := siteWith(t, map[string]string{"src/assets/css/a.css": ".a{margin:2px}"})
	if err := Run([]string{"-website-root", root}, nil); err != nil {
		t.Fatalf("expected no-config to pass, got: %v", err)
	}
}

func TestRun_RejectsUnknownFlag(t *testing.T) {
	if err := Run([]string{"-nope"}, nil); err == nil {
		t.Fatal("expected an unknown flag to be rejected")
	}
}

// TestWriteBaseline_ProducesAConfigThatStillFails is the property that keeps a
// generated baseline from being merged unread: -write-baseline writes a
// placeholder reason, and the loader rejects that placeholder by name.
func TestWriteBaseline_ProducesAConfigThatStillFails(t *testing.T) {
	root := siteWith(t, map[string]string{
		"conventions.yaml":     "rules:\n  no-px-literals:\n    enabled: true\n",
		"src/assets/css/a.css": ".a{margin:2px}",
	})
	if err := Run([]string{"-website-root", root, "-write-baseline"}, nil); err != nil {
		t.Fatalf("writing a baseline should succeed: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(root, conventions.ConfigFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "src/assets/css/a.css") {
		t.Errorf("baseline should list the violating file, got:\n%s", written)
	}

	err = Run([]string{"-website-root", root}, nil)
	if err == nil {
		t.Fatal("a freshly generated baseline must NOT pass — otherwise it can be merged unread")
	}
	if !strings.Contains(err.Error(), "placeholder") {
		t.Errorf("the failure should point at the placeholder reason, got: %v", err)
	}
}

func TestWriteBaseline_ThenARealReasonPasses(t *testing.T) {
	root := siteWith(t, map[string]string{
		"conventions.yaml": "rules:\n  no-px-literals:\n    enabled: true\n" +
			"    exceptions:\n      - path: src/assets/css/a.css\n        reason: legacy vendor reset, migrating in Q4\n",
		"src/assets/css/a.css": ".a{margin:2px}",
		"src/assets/css/b.css": ".b{margin:1rem}",
	})
	if err := Run([]string{"-website-root", root}, nil); err != nil {
		t.Fatalf("a real reason should be accepted, got: %v", err)
	}
}

func TestRun_ListPrintsEveryRuleWithItsDoc(t *testing.T) {
	if err := Run([]string{"-list"}, nil); err != nil {
		t.Fatalf("-list should not fail: %v", err)
	}
}
