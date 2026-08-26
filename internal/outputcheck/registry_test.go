package outputcheck

import (
	"errors"
	"strings"
	"testing"
)

// The registry is the extension point: a check type registered here becomes
// usable from a website's manifest without touching the runner. The invariant
// that matters is that an unknown type is an ERROR — a manifest naming a check
// the runner does not understand must not be silently dropped, because a
// dropped assertion looks exactly like a passing one.

type fakeCheck struct{ name string }

func (f fakeCheck) Name() string          { return f.name }
func (f fakeCheck) Check(Target) []string { return nil }

type fakeCorpusCheck struct{ name string }

func (f fakeCorpusCheck) Name() string                { return f.name }
func (f fakeCorpusCheck) CheckAll([]Target) []Finding { return nil }

func TestRegistry_BuildsARegisteredPerPageCheck(t *testing.T) {
	r := NewRegistry()
	r.Register("my-check", func(map[string]any) (Check, error) { return fakeCheck{"my-check"}, nil })

	check, err := r.Build("my-check", nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if check.Name() != "my-check" {
		t.Errorf("built check name = %q, want my-check", check.Name())
	}
}

func TestRegistry_BuildsARegisteredCorpusCheck(t *testing.T) {
	r := NewRegistry()
	r.RegisterCorpus("whole-site", func(map[string]any) (CorpusCheck, error) {
		return fakeCorpusCheck{"whole-site"}, nil
	})

	check, err := r.BuildCorpus("whole-site", nil)
	if err != nil {
		t.Fatalf("BuildCorpus: %v", err)
	}
	if check.Name() != "whole-site" {
		t.Errorf("built check name = %q, want whole-site", check.Name())
	}
}

// The two registries are separate namespaces: a per-page check must not be
// buildable as a corpus check or vice versa.
func TestRegistry_PerPageAndCorpusNamespacesAreSeparate(t *testing.T) {
	r := NewRegistry()
	r.Register("shared-name", func(map[string]any) (Check, error) { return fakeCheck{"shared-name"}, nil })

	if _, err := r.BuildCorpus("shared-name", nil); err == nil {
		t.Error("a per-page check should not be buildable as a corpus check")
	}
}

func TestRegistry_UnknownTypeIsAnErrorListingWhatIsKnown(t *testing.T) {
	r := NewRegistry()
	r.Register("known-a", func(map[string]any) (Check, error) { return fakeCheck{"known-a"}, nil })
	r.Register("known-b", func(map[string]any) (Check, error) { return fakeCheck{"known-b"}, nil })

	_, err := r.Build("typo", nil)
	if err == nil {
		t.Fatal("an unknown check type must be an error, not a silently dropped assertion")
	}
	for _, want := range []string{`"typo"`, "known-a", "known-b"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %s", err, want)
		}
	}
}

func TestRegistry_UnknownCorpusTypeIsAnErrorListingWhatIsKnown(t *testing.T) {
	r := NewRegistry()
	r.RegisterCorpus("links", func(map[string]any) (CorpusCheck, error) {
		return fakeCorpusCheck{"links"}, nil
	})

	_, err := r.BuildCorpus("linkz", nil)
	if err == nil {
		t.Fatal("an unknown corpus check type must be an error")
	}
	if !strings.Contains(err.Error(), "links") {
		t.Errorf("err = %v, want the known types listed", err)
	}
}

func TestRegistry_TypesAreListedSorted(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"zebra", "alpha", "middle"} {
		r.Register(name, func(map[string]any) (Check, error) { return fakeCheck{name}, nil })
		r.RegisterCorpus(name, func(map[string]any) (CorpusCheck, error) {
			return fakeCorpusCheck{name}, nil
		})
	}

	if got := strings.Join(r.Types(), "|"); got != "alpha|middle|zebra" {
		t.Errorf("Types() = %v, want sorted", r.Types())
	}
	if got := strings.Join(r.CorpusTypes(), "|"); got != "alpha|middle|zebra" {
		t.Errorf("CorpusTypes() = %v, want sorted", r.CorpusTypes())
	}
}

func TestRegistry_EmptyRegistryListsNothing(t *testing.T) {
	r := NewRegistry()
	if len(r.Types()) != 0 || len(r.CorpusTypes()) != 0 {
		t.Errorf("a new registry should list nothing; got %v / %v", r.Types(), r.CorpusTypes())
	}
}

// A factory that rejects its configuration must surface that error rather than
// yielding a half-configured check.
func TestRegistry_FactoryErrorsAreSurfaced(t *testing.T) {
	wantErr := errors.New("bad config")
	r := NewRegistry()
	r.Register("picky", func(map[string]any) (Check, error) { return nil, wantErr })
	r.RegisterCorpus("picky", func(map[string]any) (CorpusCheck, error) { return nil, wantErr })

	if _, err := r.Build("picky", map[string]any{"x": 1}); !errors.Is(err, wantErr) {
		t.Errorf("Build err = %v, want the factory's own error", err)
	}
	if _, err := r.BuildCorpus("picky", nil); !errors.Is(err, wantErr) {
		t.Errorf("BuildCorpus err = %v, want the factory's own error", err)
	}
}

// Findings are what an operator reads when a build fails, so every field has to
// be in the rendered line.
func TestFinding_StringIncludesEveryFieldAnOperatorNeeds(t *testing.T) {
	got := Finding{
		Rule:    "no-empty-headings",
		Route:   "/checkout/",
		Lang:    "pt",
		Check:   "structure",
		Message: "h1 is empty",
	}.String()

	for _, want := range []string{"structure", "pt", "/checkout/", "h1 is empty", "no-empty-headings"} {
		if !strings.Contains(got, want) {
			t.Errorf("Finding.String() = %q, missing %q", got, want)
		}
	}
}
