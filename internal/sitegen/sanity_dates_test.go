package sitegen

import (
	"strings"
	"testing"
)

// absurdVariantYAML is a site-data document in the exact shape the loader
// produces for real consumer sites: the session date is a BARE YAML scalar,
// which yaml resolves to the !!timestamp tag. Both start_text and
// duration_hours are wildly wrong relative to the sessions, so every enabled
// sanity rule must fire.
const absurdVariantYAML = `
courses:
  fictional_course:
    variants:
      fictional_variant:
        start_text: "01/Janeiro/2030"
        duration_hours: 999
        cronograma:
          sessions:
            - date: 2026-09-03
              hours: 1
            - date: 2026-09-10
              hours: 2
`

// TestValidateSiteSanity_BareYAMLTimestampIsNotSilentlySkipped is the
// regression test for the vacuous gate: a bare `date: 2026-09-03` is decoded
// as time.Time and normalized to RFC3339 by parseSiteData, so a checker that
// only understands "2006-01-02" fails to parse it and -- if it returns nil on
// that failure -- silently accepts every variant that has sessions.
func TestValidateSiteSanity_BareYAMLTimestampIsNotSilentlySkipped(t *testing.T) {
	siteData, err := parseSiteData([]byte(absurdVariantYAML))
	if err != nil {
		t.Fatalf("parseSiteData: %v", err)
	}

	// Guard the premise of this test: if the loader ever stops normalizing
	// bare timestamps to RFC3339, this test is no longer exercising the bug
	// it was written for and must be revisited rather than quietly passing.
	rawDate := firstSessionDateValue(t, siteData)
	gotDate, ok := rawDate.(string)
	if !ok {
		t.Fatalf("premise broken: loader no longer yields a string date, got %T", rawDate)
	}
	if gotDate != "2026-09-03T00:00:00Z" {
		t.Fatalf("premise broken: expected loader to normalize to RFC3339, got %q", gotDate)
	}

	err = ValidateSiteSanity(siteData, DefaultSanityConfig())
	if err == nil {
		t.Fatal("sanity gate is VACUOUS: start_text 01/Janeiro/2030 and duration_hours 999 " +
			"were both accepted against sessions dated 2026-09-03 totalling 3 hours")
	}

	msg := err.Error()
	for _, want := range []string{"start_text", "duration_hours"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected %s violation to be reported, got: %v", want, msg)
		}
	}
}

// TestValidateSiteSanity_SessionDateAcceptsBothDateForms locks the layout fix:
// both the bare-date form (a quoted YAML string) and the RFC3339 form (what a
// bare YAML timestamp becomes) must be understood, and a correct variant in
// either form must pass.
func TestValidateSiteSanity_SessionDateAcceptsBothDateForms(t *testing.T) {
	cases := map[string]string{
		"iso date":            "2099-01-01",
		"rfc3339 utc":         "2099-01-01T00:00:00Z",
		"rfc3339 with offset": "2099-01-01T00:00:00+00:00",
		"go time string":      "2099-01-01 00:00:00 +0000 UTC",
	}
	for name, sessionDate := range cases {
		t.Run(name, func(t *testing.T) {
			siteData := variantSiteData("2099-01-01", 7, []any{
				map[string]any{"date": sessionDate, "hours": 7},
			})
			if err := ValidateSiteSanity(siteData, DefaultSanityConfig()); err != nil {
				t.Fatalf("expected a matching variant to pass, got %v", err)
			}
		})
	}
}

// TestValidateSiteSanity_SessionDateMismatchFailsForEveryDateForm proves the
// checker is live (not merely non-crashing) for each accepted date form: the
// same mismatch must be reported however the date is spelled.
func TestValidateSiteSanity_SessionDateMismatchFailsForEveryDateForm(t *testing.T) {
	cases := map[string]string{
		"iso date":            "2099-01-01",
		"rfc3339 utc":         "2099-01-01T00:00:00Z",
		"rfc3339 with offset": "2099-01-01T00:00:00+00:00",
	}
	for name, sessionDate := range cases {
		t.Run(name, func(t *testing.T) {
			siteData := variantSiteData("15/Marco/2031", 7, []any{
				map[string]any{"date": sessionDate, "hours": 7},
			})
			err := ValidateSiteSanity(siteData, DefaultSanityConfig())
			if err == nil {
				t.Fatal("expected the start_text mismatch to be reported")
			}
			if !strings.Contains(err.Error(), "2099-01-01") {
				t.Errorf("expected the first session date in the message, got: %v", err)
			}
		})
	}
}

// TestValidateSiteSanity_UnparseableSessionDateIsAnError locks the root cause:
// a session date the checker cannot parse must be reported, never swallowed
// into a nil return that disables every rule for the variant.
func TestValidateSiteSanity_UnparseableSessionDateIsAnError(t *testing.T) {
	cases := map[string]any{
		"garbage string": "FIC-NOT-A-DATE",
		"empty string":   "",
		"wrong type":     42,
		"missing":        nil,
	}
	for name, sessionDate := range cases {
		t.Run(name, func(t *testing.T) {
			session := map[string]any{"hours": 7}
			if sessionDate != nil {
				session["date"] = sessionDate
			}
			siteData := variantSiteData("2099-01-01", 7, []any{session})

			err := ValidateSiteSanity(siteData, DefaultSanityConfig())
			if err == nil {
				t.Fatal("an unparseable first session date must be an error, not a silent skip")
			}
			if !strings.Contains(err.Error(), "cronograma.sessions[0].date") {
				t.Errorf("expected the offending path in the message, got: %v", err)
			}
		})
	}
}

// TestValidateSiteSanity_UnparseableDateDoesNotDisableTheOtherRules proves the
// loud-failure fix did not merely relocate the silence: a bad date must not
// stop duration_hours from being checked too.
func TestValidateSiteSanity_UnparseableDateDoesNotDisableTheOtherRules(t *testing.T) {
	siteData := variantSiteData("2099-01-01", 999, []any{
		map[string]any{"date": "FIC-NOT-A-DATE", "hours": 7},
	})

	err := ValidateSiteSanity(siteData, DefaultSanityConfig())
	if err == nil {
		t.Fatal("expected errors for both the bad date and the duration mismatch")
	}
	if !strings.Contains(err.Error(), "duration_hours") {
		t.Errorf("expected duration_hours to still be checked, got: %v", err)
	}
}

func variantSiteData(startText string, durationHours int, sessions []any) map[string]any {
	return map[string]any{
		"courses": map[string]any{
			"fictional_course": map[string]any{
				"variants": map[string]any{
					"fictional_variant": map[string]any{
						"start_text":     startText,
						"duration_hours": durationHours,
						"cronograma": map[string]any{
							"sessions": sessions,
						},
					},
				},
			},
		},
	}
}

func firstSessionDateValue(t *testing.T, siteData map[string]any) any {
	t.Helper()
	node := siteData
	for _, key := range []string{"courses", "fictional_course", "variants", "fictional_variant", "cronograma"} {
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("premise broken: %q is not a map in the parsed site data", key)
		}
		node = next
	}
	sessions, ok := node["sessions"].([]any)
	if !ok || len(sessions) == 0 {
		t.Fatal("premise broken: parsed site data has no cronograma sessions")
	}
	first, ok := sessions[0].(map[string]any)
	if !ok {
		t.Fatal("premise broken: first cronograma session is not a map")
	}
	return first["date"]
}
