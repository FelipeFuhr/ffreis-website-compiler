package sitegen

import "testing"

func TestValidateSiteSanity_StartDateMatchesFirstSession(t *testing.T) {
	siteData := map[string]any{
		"courses": map[string]any{
			"c1": map[string]any{
				"variants": map[string]any{
					"v1": map[string]any{
						"start_text":     "19/Maio/2026",
						"duration_hours": 8,
						"cronograma": map[string]any{
							"sessions": []any{
								map[string]any{"date": "2026-05-19", "hours": 8},
							},
						},
					},
				},
			},
		},
	}

	if err := ValidateSiteSanity(siteData, DefaultSanityConfig()); err != nil {
		t.Fatalf("expected sanity validation to pass, got %v", err)
	}
}

func TestValidateSiteSanity_StartDateMismatchFails(t *testing.T) {
	siteData := map[string]any{
		"courses": map[string]any{
			"c1": map[string]any{
				"variants": map[string]any{
					"v1": map[string]any{
						"start_text":     "20/Maio/2026",
						"duration_hours": 8,
						"cronograma": map[string]any{
							"sessions": []any{
								map[string]any{"date": "2026-05-19", "hours": 8},
							},
						},
					},
				},
			},
		},
	}

	if err := ValidateSiteSanity(siteData, DefaultSanityConfig()); err == nil {
		t.Fatal("expected sanity validation to fail")
	}
}

func TestValidateSiteSanity_DurationMismatchFails(t *testing.T) {
	siteData := map[string]any{
		"courses": map[string]any{
			"c1": map[string]any{
				"variants": map[string]any{
					"v1": map[string]any{
						"start_text":     "2026-05-19",
						"duration_hours": 10,
						"cronograma": map[string]any{
							"sessions": []any{
								map[string]any{"date": "2026-05-19", "hours": 6},
								map[string]any{"date": "2026-05-20", "hours": 2},
							},
						},
					},
				},
			},
		},
	}

	if err := ValidateSiteSanity(siteData, DefaultSanityConfig()); err == nil {
		t.Fatal("expected sanity validation to fail")
	}
}

