package flagregistry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRegistry(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "flags.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

const twoGateRegistry = `{
  "version": 1,
  "project": "test-site",
  "flags": [
    {"name": "section_blog", "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false},
    {"name": "section_courses", "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": true},
    {"name": "content_source", "kind": "adapter", "category": "content", "binding": "compile", "effect": "route", "options": ["mock", "prod"], "default": "prod"}
  ]
}`

func TestLoad_ParsesDeclaredGates(t *testing.T) {
	reg, err := Load(writeRegistry(t, twoGateRegistry))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reg.Project != "test-site" {
		t.Errorf("project = %q, want test-site", reg.Project)
	}
	gates, err := reg.SectionGates()
	if err != nil {
		t.Fatalf("SectionGates: %v", err)
	}
	if len(gates) != 2 {
		t.Fatalf("gates = %v, want exactly the two section_* flags", gates)
	}
	if gates["blog"] {
		t.Error("section_blog declares default false; blog gate should be off")
	}
	if !gates["courses"] {
		t.Error("section_courses declares default true; courses gate should be on")
	}
}

func TestLoad_RejectsUnsupportedVersion(t *testing.T) {
	_, err := Load(writeRegistry(t, `{"version": 2, "flags": []}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("err = %v, want unsupported version", err)
	}
}

func TestLoad_RejectsFlagWithoutName(t *testing.T) {
	_, err := Load(writeRegistry(t, `{"version": 1, "flags": [{"kind": "gate"}]}`))
	if err == nil || !strings.Contains(err.Error(), "'name'") {
		t.Fatalf("err = %v, want missing name", err)
	}
}

// A mistyped default must not quietly decide what ships: "false" (a string) is
// truthy nonsense, not a disabled section.
func TestSectionGates_RejectsNonBooleanDefault(t *testing.T) {
	reg, err := Load(writeRegistry(t, `{"version": 1, "flags": [
	  {"name": "section_blog", "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": "false"}
	]}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := reg.SectionGates(); err == nil || !strings.Contains(err.Error(), "non-boolean default") {
		t.Fatalf("err = %v, want non-boolean default", err)
	}
}

func TestLoad_MissingFileIsAnError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("Load of a missing registry should fail")
	}
}
