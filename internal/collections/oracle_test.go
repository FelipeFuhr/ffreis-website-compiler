package collections

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"ffreis-website-compiler/internal/listings"
)

// Oracle tests: run the SAME content file through the typed loader the engine
// replaces and through the engine's built-in collection, and require the two
// outputs to be deeply equal.
//
// This is what makes the migration provable rather than plausible. The typed
// loaders are the oracle; the decision record (§6, Phase 5 gate 1) calls for
// exactly this — "keep internal/listings in the tree until this passes, purely
// as the oracle". Deep equality also catches the failure modes the record flags
// as silent: an absent list becoming nil instead of []any{}, an absent bool
// vanishing instead of being false, a dropped or added key.
//
// projects and courses have already migrated, so their oracles are the frozen,
// test-only copies of the deleted packages (frozen_projects_oracle_test.go,
// frozen_courses_oracle_test.go). listings still imports its live package
// because it migrates in Phase 5.

func loadRaw(t *testing.T, c Collection, name string) []map[string]any {
	t.Helper()
	raw, err := LoadRecords(c.Source, LoadOptions{Path: filepath.Join("testdata", name)})
	if err != nil {
		t.Fatalf("loading %s: %v", name, err)
	}
	if len(raw) == 0 {
		t.Fatalf("loading %s produced no records — the oracle would compare nothing", name)
	}
	return raw
}

func TestBuiltinProjectsMatchesProjectsPackage(t *testing.T) {
	for _, file := range []string{"projects.yaml", "projects-mock.yaml"} {
		t.Run(file, func(t *testing.T) {
			path := filepath.Join("testdata", file)

			// Oracle: a frozen copy of the typed loader Phase 2 deleted
			// (see frozen_projects_oracle_test.go).
			loaded, err := frozenLoadProjectsFile(path)
			if err != nil {
				t.Fatalf("oracle frozenLoadProjectsFile: %v", err)
			}
			want := frozenProjectsToSiteDataList(loaded)

			// Engine.
			c := builtinProjects()
			got, err := c.Project(loadRaw(t, c, file), ProjectOptions{})
			if err != nil {
				t.Fatalf("engine Project: %v", err)
			}

			assertRecordsEqual(t, want, got.Records)
			if got.Details != nil {
				t.Errorf("projects has no detail page, but Project returned %d detail records", len(got.Details))
			}
		})
	}
}

func TestBuiltinCoursesMatchesCoursesPackage(t *testing.T) {
	// courses-rich.yaml is synthetic: the two real files leave 8 of the 17
	// struct fields unset, so without it the oracle would never compare a
	// non-zero price, an explicit slug, or a nested curriculum. See the header
	// of that file.
	for _, file := range []string{"courses.yaml", "courses-mock.yaml", "courses-rich.yaml"} {
		t.Run(file, func(t *testing.T) {
			path := filepath.Join("testdata", file)

			// Oracle: a frozen copy of the typed loader Phase 3 deleted
			// (see frozen_courses_oracle_test.go).
			loaded, err := frozenLoadCoursesFile(path)
			if err != nil {
				t.Fatalf("oracle frozenLoadCoursesFile: %v", err)
			}
			want := frozenCoursesToSiteDataList(loaded)

			c := builtinCourses()
			got, err := c.Project(loadRaw(t, c, file), ProjectOptions{})
			if err != nil {
				t.Fatalf("engine Project: %v", err)
			}

			assertRecordsEqual(t, want, got.Records)

			// The detail projection must reproduce ToCurrentCourse, which adds
			// long-form landing content on top of the catalog fields.
			if len(got.Details) != len(loaded) {
				t.Fatalf("got %d detail records, want %d", len(got.Details), len(loaded))
			}
			for i, course := range loaded {
				wantDetail := frozenToCurrentCourse(course)
				if !reflect.DeepEqual(wantDetail, got.Details[i]) {
					t.Errorf("detail record %d (%s) differs from ToCurrentCourse:\n%s",
						i, course.Title, describeMapDiff(wantDetail, got.Details[i]))
				}
			}
		})
	}
}

func TestBuiltinListingsMatchesListingsPackage(t *testing.T) {
	const file = "listings.yaml"
	path := filepath.Join("testdata", file)

	loaded, err := listings.LoadListingsFile(path)
	if err != nil {
		t.Fatalf("oracle LoadListingsFile: %v", err)
	}
	want := listings.ToSiteDataList(loaded)

	c := builtinListings()
	got, err := c.Project(loadRaw(t, c, file), ProjectOptions{})
	if err != nil {
		t.Fatalf("engine Project: %v", err)
	}

	// This is the window.CASABOA_LISTINGS byte contract (decision record §1.3):
	// the engine publishes the same []any of map[string]any into the same
	// site-data key, and casaboa's template does the toJSON. Byte-identity
	// therefore reduces to this deep-equality check over 25 known records.
	assertRecordsEqual(t, want, got.Records)

	if len(got.Details) != len(loaded) {
		t.Fatalf("got %d detail records, want %d", len(got.Details), len(loaded))
	}
	for i, l := range loaded {
		wantDetail := listings.ToCurrentListing(l)
		if !reflect.DeepEqual(wantDetail, got.Details[i]) {
			t.Errorf("detail record %d (%s) differs from ToCurrentListing:\n%s",
				i, l.ID, describeMapDiff(wantDetail, got.Details[i]))
		}
	}

	// File order must be preserved — it encodes upstream ranking (Q7).
	for i, l := range loaded {
		rec, ok := got.Records[i].(map[string]any)
		if !ok {
			t.Fatalf("record %d is %T, want map[string]any", i, got.Records[i])
		}
		if rec["id"] != l.ID {
			t.Errorf("record %d is id=%v, want %s — listings must preserve file order",
				i, rec["id"], l.ID)
		}
	}
}

// TestSlugifyMatchesCoursesOracle cross-checks the engine's copy of Slugify
// against the frozen courses.Slugify rather than trusting that the copy is
// faithful.
func TestSlugifyMatchesCoursesOracle(t *testing.T) {
	inputs := []string{
		"MLOps in Production!",
		"Kubernetes for ML Engineers",
		"  leading and trailing  ",
		"Multiple---Hyphens",
		"ALL CAPS TITLE",
		"acentuação e ç",
		"123 numbers 456",
		"---",
		"",
		"a",
		"Trailing punctuation???",
		"Mixed: colons, commas & ampersands",
	}
	for _, in := range inputs {
		if want, got := frozenSlugify(in), Slugify(in); want != got {
			t.Errorf("Slugify(%q) = %q, frozen courses.Slugify = %q", in, got, want)
		}
	}
}

// TestFormatMoneyMatchesCoursesOracle cross-checks FormatMoney against the
// frozen courses.formatMoney. That function was unexported, so the oracle is
// reached through the price_usd_display / price_brl_display keys the frozen
// ToSiteDataList emits — the same indirection the live package needed.
//
// The values cover the cases §6.1 calls out as easy to get wrong: the
// cents <= 0 -> "" rule and the %02d fraction.
func TestFormatMoneyMatchesCoursesOracle(t *testing.T) {
	for _, cents := range []int{-100, -1, 0, 1, 5, 9, 99, 100, 101, 110, 999, 4900, 4999, 100000} {
		oracle := frozenCoursesToSiteDataList([]frozenCourse{{
			Title:         "x",
			PriceUsdCents: cents,
			PriceBrlCents: cents,
		}})
		rec, ok := oracle[0].(map[string]any)
		if !ok {
			t.Fatalf("oracle record is %T, want map[string]any", oracle[0])
		}

		if want, got := rec["price_usd_display"], FormatMoney("$", cents); want != got {
			t.Errorf("FormatMoney($, %d) = %q, frozen courses gives %q", cents, got, want)
		}
		if want, got := rec["price_brl_display"], FormatMoney("R$", cents); want != got {
			t.Errorf("FormatMoney(R$, %d) = %q, frozen courses gives %q", cents, got, want)
		}
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func assertRecordsEqual(t *testing.T, want, got []any) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("got %d records, want %d", len(got), len(want))
	}
	for i := range want {
		wantMap, ok := want[i].(map[string]any)
		if !ok {
			t.Fatalf("oracle record %d is %T, want map[string]any", i, want[i])
		}
		gotMap, ok := got[i].(map[string]any)
		if !ok {
			t.Fatalf("engine record %d is %T, want map[string]any", i, got[i])
		}
		if !reflect.DeepEqual(wantMap, gotMap) {
			t.Errorf("record %d differs from the typed-loader oracle:\n%s", i, describeMapDiff(wantMap, gotMap))
		}
	}
}

// describeMapDiff reports per-key differences, including the value types,
// because nil-vs-empty-slice is the exact failure mode these tests exist to
// catch and it is invisible in a plain %v dump.
func describeMapDiff(want, got map[string]any) string {
	var out string
	for k, wv := range want {
		gv, present := got[k]
		if !present {
			out += "  missing key " + k + "\n"
			continue
		}
		if !reflect.DeepEqual(wv, gv) {
			out += "  " + k + ": want " + describeValue(wv) + ", got " + describeValue(gv) + "\n"
		}
	}
	for k, gv := range got {
		if _, present := want[k]; !present {
			out += "  unexpected key " + k + " = " + describeValue(gv) + "\n"
		}
	}
	if out == "" {
		return "  (no per-key difference found)\n"
	}
	return out
}

func describeValue(v any) string {
	if v == nil {
		return "nil"
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Map:
		if rv.IsNil() {
			return fmt.Sprintf("nil %s", rv.Type())
		}
		return fmt.Sprintf("%s len=%d %v", rv.Type(), rv.Len(), v)
	default:
		return fmt.Sprintf("%s %v", rv.Type(), v)
	}
}
