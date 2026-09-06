package collections

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
// All three collections have now migrated, so every oracle here is a frozen,
// test-only copy of the deleted package (frozen_projects_oracle_test.go,
// frozen_courses_oracle_test.go, frozen_listings_oracle_test.go). No oracle
// imports a live package any more — that is the point of freezing them, since
// otherwise the equivalence proof would have been deleted alongside the
// production code it proves things about.

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
	// listings-sparse.yaml is synthetic: all 25 real records set all 19 fields,
	// so without it the oracle would never compare an ABSENT field — including
	// the absent `sponsored` decision record §5.2 names as the single
	// high-risk detail of this phase. See the header of that file.
	for _, file := range []string{"listings.yaml", "listings-sparse.yaml"} {
		t.Run(file, func(t *testing.T) {
			path := filepath.Join("testdata", file)

			// Oracle: a frozen copy of the typed loader Phase 5 deleted
			// (see frozen_listings_oracle_test.go).
			loaded, err := frozenLoadListingsFile(path)
			if err != nil {
				t.Fatalf("oracle frozenLoadListingsFile: %v", err)
			}
			want := frozenListingsToSiteDataList(loaded)

			c := builtinListings()
			got, err := c.Project(loadRaw(t, c, file), ProjectOptions{})
			if err != nil {
				t.Fatalf("engine Project: %v", err)
			}

			// This is the window.CASABOA_LISTINGS byte contract (decision record
			// §1.3): the engine publishes the same []any of map[string]any into
			// the same site-data key, and casaboa's template does the toJSON.
			// Byte-identity therefore reduces to this deep-equality check.
			assertRecordsEqual(t, want, got.Records)

			if len(got.Details) != len(loaded) {
				t.Fatalf("got %d detail records, want %d", len(got.Details), len(loaded))
			}
			for i, l := range loaded {
				wantDetail := frozenToCurrentListing(l)
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
		})
	}
}

// TestBuiltinListingsSponsoredNeverVanishes is decision record §5.2's acid test,
// stated directly rather than only as a consequence of the deep-equality above.
//
// The oracle comparison would catch a regression here, but it would report it as
// "record 0 differs" among a wall of other keys. This test names the failure, and
// asserts the three-way distinction the JS blob depends on: a record that omits
// `sponsored`, one that sets it false, and one that sets it true must produce a
// PRESENT bool key in all three cases, with only the last one true.
func TestBuiltinListingsSponsoredNeverVanishes(t *testing.T) {
	c := builtinListings()
	got, err := c.Project(loadRaw(t, c, "listings-sparse.yaml"), ProjectOptions{})
	if err != nil {
		t.Fatalf("engine Project: %v", err)
	}

	want := map[string]bool{
		"zeta-bare-001":             false, // `sponsored:` absent entirely
		"alpha-explicit-false-002":  false,
		"mid-sponsored-true-003":    true,
		"beta-empty-composites-004": false,
	}

	seen := make(map[string]bool)
	for i, r := range got.Records {
		rec, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("record %d is %T, want map[string]any", i, r)
		}
		id, _ := rec["id"].(string)
		raw, present := rec["sponsored"]
		if !present {
			t.Errorf("record %d (%s) has NO sponsored key. Decision record §5.2: an "+
				"absent `sponsored:` must project to false, not disappear — a key that "+
				"vanishes from window.CASABOA_LISTINGS breaks app.js's filter chips in "+
				"the browser and nothing else in the build notices.", i, id)
			continue
		}
		val, ok := raw.(bool)
		if !ok {
			t.Errorf("record %d (%s) has sponsored=%v (%T), want a bool", i, id, raw, raw)
			continue
		}
		if want[id] != val {
			t.Errorf("record %s has sponsored=%v, want %v", id, val, want[id])
		}
		seen[id] = true
	}

	// CONTROL: all four records must have been examined, or a projection that
	// dropped records entirely would pass the loop above vacuously.
	for id := range want {
		if !seen[id] {
			t.Errorf("record %s was never projected, so its sponsored value was not checked", id)
		}
	}
}

// TestBuiltinListingsRequiredFields preserves the coverage
// internal/listings/listings_test.go had over LoadListingsFile's two required
// fields. The engine reports them through the generic checkRequired path, so
// the wording differs from the frozen loader's; what must not differ is that
// both fields still FAIL rather than yielding a record with an empty id (which
// would render a detail page at /listings//).
func TestBuiltinListingsRequiredFields(t *testing.T) {
	c := builtinListings()

	for _, tc := range []struct {
		name    string
		record  map[string]any
		missing string
	}{
		{name: "missing id", record: map[string]any{"title": "No id here"}, missing: "id"},
		{name: "missing title", record: map[string]any{"id": "no-title"}, missing: "title"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Project([]map[string]any{tc.record}, ProjectOptions{})
			if err == nil {
				t.Fatalf("expected an error for a listing with no %s, got nil", tc.missing)
			}
			if want := `missing required field "` + tc.missing + `"`; !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		})
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
