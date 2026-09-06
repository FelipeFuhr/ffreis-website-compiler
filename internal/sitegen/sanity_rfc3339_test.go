package sitegen

import "testing"

// The existing sanity tests all spell a session date as a bare "2099-01-01".
// Real site data never looks like that: `date: 2099-01-01` in YAML resolves to
// the !!timestamp tag, decodes as time.Time, and normalizeYAMLValue renders it
// as RFC3339 before any consumer sees it. Parsing only the bare layout made
// firstCronogramaSessionDate fail, and validateVariantSanity reads that failure
// as "no date, nothing to check" and returns nil — so BOTH course checks went
// silently inert on every real site while the unit tests stayed green.
//
// These tests use the shape the data actually has.

func rfc3339CourseData(startText string, durationHours int, sessionHours int) map[string]any {
	return map[string]any{
		"courses": map[string]any{
			"fictional_course": map[string]any{
				"variants": map[string]any{
					"fictional_variant": map[string]any{
						"start_text":     startText,
						"duration_hours": durationHours,
						"cronograma": map[string]any{
							"sessions": []any{
								map[string]any{"date": "2099-01-01T00:00:00Z", "hours": sessionHours},
							},
						},
					},
				},
			},
		},
	}
}

func TestValidateSiteSanity_RFC3339SessionDatePasses(t *testing.T) {
	if err := ValidateSiteSanity(rfc3339CourseData("2099-01-01", 99, 99), DefaultSanityConfig()); err != nil {
		t.Fatalf("expected sanity validation to pass, got %v", err)
	}
}

func TestValidateSiteSanity_RFC3339SessionDateStillCatchesStartMismatch(t *testing.T) {
	if err := ValidateSiteSanity(rfc3339CourseData("2099-06-15", 99, 99), DefaultSanityConfig()); err == nil {
		t.Fatal("expected a start_text mismatch to fail with an RFC3339 session date")
	}
}

func TestValidateSiteSanity_RFC3339SessionDateStillCatchesHoursMismatch(t *testing.T) {
	if err := ValidateSiteSanity(rfc3339CourseData("2099-01-01", 40, 99), DefaultSanityConfig()); err == nil {
		t.Fatal("expected a duration_hours mismatch to fail with an RFC3339 session date")
	}
}

func TestParseSessionDateAcceptsBothShapes(t *testing.T) {
	for _, in := range []string{"2099-01-01", "2099-01-01T00:00:00Z", " 2099-01-01T00:00:00Z "} {
		if _, ok := parseSessionDate(in); !ok {
			t.Errorf("parseSessionDate(%q) = not ok, want ok", in)
		}
	}
	if _, ok := parseSessionDate("not-a-date"); ok {
		t.Error("parseSessionDate(\"not-a-date\") = ok, want not ok")
	}
}
