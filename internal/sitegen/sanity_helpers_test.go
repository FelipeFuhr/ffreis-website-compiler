package sitegen

import (
	"testing"
	"time"
)

// These helpers parse human-entered course data — Brazilian dates typed by
// hand, hours that arrive as strings or YAML floats. They decide whether a
// sanity check fires, so a silent misparse means a real inconsistency ships
// unreported.

func TestCoerceInt_AcceptsEveryShapeYAMLProduces(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  any
		want   int
		wantOK bool
	}{
		{"int", 42, 42, true},
		{"int64", int64(42), 42, true},
		{"float64 with no fraction", float64(42), 42, true},
		{"float64 zero", float64(0), 0, true},
		{"negative float64", float64(-3), -3, true},
		{"string of digits", "42", 42, true},
		{"string with surrounding whitespace", "  42  ", 42, true},
		{"negative string", "-7", -7, true},
		{"float64 with a fraction is rejected", 42.5, 0, false},
		{"non-numeric string is rejected", "forty-two", 0, false},
		{"empty string is rejected", "", 0, false},
		{"bool is rejected", true, 0, false},
		{"nil is rejected", nil, 0, false},
		{"slice is rejected", []any{1}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := coerceInt(tc.value)
			if ok != tc.wantOK {
				t.Fatalf("coerceInt(%#v) ok = %v, want %v", tc.value, ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Errorf("coerceInt(%#v) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

func TestSameDay_IgnoresTimeOfDayButNotTheDate(t *testing.T) {
	morning := time.Date(2026, time.May, 19, 8, 0, 0, 0, time.UTC)
	evening := time.Date(2026, time.May, 19, 23, 59, 0, 0, time.UTC)
	nextDay := time.Date(2026, time.May, 20, 0, 0, 0, 0, time.UTC)
	nextMonth := time.Date(2026, time.June, 19, 8, 0, 0, 0, time.UTC)
	nextYear := time.Date(2027, time.May, 19, 8, 0, 0, 0, time.UTC)

	if !sameDay(morning, evening) {
		t.Error("same calendar day at different times should compare equal")
	}
	for name, other := range map[string]time.Time{
		"next day": nextDay, "next month": nextMonth, "next year": nextYear,
	} {
		if sameDay(morning, other) {
			t.Errorf("%s compared equal to the original day", name)
		}
	}
}

func TestParseStartTextToDate_AcceptsEveryFormatCourseDataUses(t *testing.T) {
	want := time.Date(2026, time.May, 19, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct{ name, text string }{
		{"ISO", "2026-05-19"},
		{"numeric Brazilian", "19/05/2026"},
		{"Portuguese month name", "19/Maio/2026"},
		{"Portuguese month lowercase", "19/maio/2026"},
		{"Portuguese month abbreviated", "19/mai/2026"},
		{"surrounding whitespace", "  2026-05-19  "},
		{"whitespace inside the parts", " 19 / maio / 2026 "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseStartTextToDate(tc.text)
			if !ok {
				t.Fatalf("parseStartTextToDate(%q) failed", tc.text)
			}
			if !got.Equal(want) {
				t.Errorf("parseStartTextToDate(%q) = %v, want %v", tc.text, got, want)
			}
		})
	}
}

func TestParseStartTextToDate_RejectsWhatItCannotUnderstand(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"empty", ""},
		{"whitespace only", "   "},
		{"free text", "next Monday"},
		{"too few parts", "19/2026"},
		{"too many parts", "19/05/2026/extra"},
		{"non-numeric day", "dia/maio/2026"},
		{"non-numeric year", "19/maio/este"},
		{"unknown month name", "19/Smarch/2026"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := parseStartTextToDate(tc.text); ok {
				t.Errorf("parseStartTextToDate(%q) succeeded; an unparseable date must be reported, not guessed", tc.text)
			}
		})
	}
}

func TestParsePtBRMonth_AcceptsFullAndAbbreviatedNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want time.Month
	}{
		{"janeiro", "janeiro", time.January},
		{"jan", "jan", time.January},
		{"fevereiro", "fevereiro", time.February},
		{"fev", "fev", time.February},
		{"marco with cedilla", "março", time.March},
		{"marco without cedilla", "marco", time.March},
		{"mar", "mar", time.March},
		{"abril", "abril", time.April},
		{"abr", "abr", time.April},
		{"maio", "maio", time.May},
		{"mai", "mai", time.May},
		{"junho", "junho", time.June},
		{"jun", "jun", time.June},
		{"julho", "julho", time.July},
		{"jul", "jul", time.July},
		{"agosto", "agosto", time.August},
		{"ago", "ago", time.August},
		{"setembro", "setembro", time.September},
		{"set", "set", time.September},
		{"outubro", "outubro", time.October},
		{"out", "out", time.October},
		{"novembro", "novembro", time.November},
		{"nov", "nov", time.November},
		{"dezembro", "dezembro", time.December},
		{"dez", "dez", time.December},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parsePtBRMonth(tc.raw)
			if !ok {
				t.Fatalf("parsePtBRMonth(%q) failed", tc.raw)
			}
			if got != tc.want {
				t.Errorf("parsePtBRMonth(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// Course data is typed by hand, so casing and accents vary. Normalising both is
// the difference between a sanity check running and silently skipping.
func TestParsePtBRMonth_IsCaseAndAccentInsensitive(t *testing.T) {
	for _, raw := range []string{"MAIO", "Maio", "  maio  ", "MAI"} {
		if got, ok := parsePtBRMonth(raw); !ok || got != time.May {
			t.Errorf("parsePtBRMonth(%q) = (%v, %v), want (May, true)", raw, got, ok)
		}
	}
	for _, raw := range []string{"MARÇO", "Março", "MARCO"} {
		if got, ok := parsePtBRMonth(raw); !ok || got != time.March {
			t.Errorf("parsePtBRMonth(%q) = (%v, %v), want (March, true)", raw, got, ok)
		}
	}
}

func TestParsePtBRMonth_RejectsUnknownNames(t *testing.T) {
	for _, raw := range []string{"", "   ", "May", "smarch", "13"} {
		if _, ok := parsePtBRMonth(raw); ok {
			t.Errorf("parsePtBRMonth(%q) succeeded; an unknown month must be reported", raw)
		}
	}
}

func TestNormalizeMonthName_StripsAccentsAndCase(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"MARÇO", "marco"},
		{"  Março  ", "marco"},
		{"JANEIRO", "janeiro"},
		{"Setembro", "setembro"},
		{"áàâã", "aaaa"},
		{"éê", "ee"},
		{"í", "i"},
		{"óôõ", "ooo"},
		{"ú", "u"},
		{"ç", "c"},
		{"already-plain", "already-plain"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			if got := normalizeMonthName(tc.raw); got != tc.want {
				t.Errorf("normalizeMonthName(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
