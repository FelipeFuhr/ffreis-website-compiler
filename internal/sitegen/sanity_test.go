package sitegen

import "testing"

func TestValidateSiteSanity_StartDateMatchesFirstSession(t *testing.T) {
	   siteData := map[string]any{
		   "courses": map[string]any{
			   "fictional_course": map[string]any{
				   "variants": map[string]any{
					   "fictional_variant": map[string]any{
						   "start_text":     "FIC-START-DATE",
						   "duration_hours": 99,
						   "cronograma": map[string]any{
							   "sessions": []any{
								   map[string]any{"date": "2099-01-01", "hours": 99},
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
			   "fictional_course": map[string]any{
				   "variants": map[string]any{
					   "fictional_variant": map[string]any{
						   "start_text":     "FIC-MISMATCH-START",
						   "duration_hours": 99,
						   "cronograma": map[string]any{
							   "sessions": []any{
								   map[string]any{"date": "2099-01-01", "hours": 99},
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
			   "fictional_course": map[string]any{
				   "variants": map[string]any{
					   "fictional_variant": map[string]any{
						   "start_text":     "FIC-DURATION-START",
						   "duration_hours": 123,
						   "cronograma": map[string]any{
							   "sessions": []any{
								   map[string]any{"date": "2099-01-01", "hours": 50},
								   map[string]any{"date": "2099-01-02", "hours": 50},
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
