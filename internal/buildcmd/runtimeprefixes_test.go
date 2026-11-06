package buildcmd

import "testing"

func TestResolveRuntimePrefixes_ReadsTheSitesOwnDeclaration(t *testing.T) {
	got := resolveRuntimePrefixes(map[string]any{
		"runtime_prefixes": []any{"/api/", "/ws/"},
	})
	if len(got) != 2 || got[0] != "/api/" || got[1] != "/ws/" {
		t.Fatalf("expected both declared prefixes, got %#v", got)
	}
}

func TestResolveRuntimePrefixes_AbsentKeyYieldsNothing(t *testing.T) {
	if got := resolveRuntimePrefixes(map[string]any{}); got != nil {
		t.Fatalf("expected nil for a site that declares none, got %#v", got)
	}
}

// Site data is YAML-decoded into any, so a mistyped entry (a list of maps, a
// number) must be skipped rather than panic the build.
func TestResolveRuntimePrefixes_SkipsNonStringAndBlankEntries(t *testing.T) {
	got := resolveRuntimePrefixes(map[string]any{
		"runtime_prefixes": []any{"  /api/  ", "", 42, map[string]any{"k": "v"}, "   "},
	})
	if len(got) != 1 || got[0] != "/api/" {
		t.Fatalf("expected only the one usable prefix, trimmed, got %#v", got)
	}
}

func TestResolveRuntimePrefixes_WrongTypeForTheKeyYieldsNothing(t *testing.T) {
	if got := resolveRuntimePrefixes(map[string]any{"runtime_prefixes": "/api/"}); got != nil {
		t.Fatalf("expected nil when the key is not a list, got %#v", got)
	}
}
