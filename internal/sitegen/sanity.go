package sitegen

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type SanityConfig struct {
	CourseStartMatchesFirstSession         bool
	CourseDurationHoursMatchesSessionHours bool
}

func DefaultSanityConfig() SanityConfig {
	return SanityConfig{
		CourseStartMatchesFirstSession:         true,
		CourseDurationHoursMatchesSessionHours: true,
	}
}

func ValidateSiteSanity(siteData map[string]any, config SanityConfig) error {
	var errs []error

	courses, ok := siteData["courses"].(map[string]any)
	if !ok {
		// Nothing to validate; the contract layer should enforce shape.
		return nil
	}

	for courseID, courseValue := range courses {
		course, ok := courseValue.(map[string]any)
		if !ok {
			continue
		}
		variants, ok := digMap(course, "variants")
		if !ok {
			continue
		}

		for variantID, variantValue := range variants {
			variant, ok := variantValue.(map[string]any)
			if !ok {
				continue
			}

			cronograma, hasCronograma := digMap(variant, "cronograma")
			if !hasCronograma {
				continue
			}
			sessions, ok := digSlice(cronograma, "sessions")
			if !ok || len(sessions) == 0 {
				continue
			}

			firstSessionDate, ok := firstCronogramaSessionDate(sessions)
			if !ok {
				continue
			}

			basePath := fmt.Sprintf("courses.%s.variants.%s", courseID, variantID)

			if config.CourseStartMatchesFirstSession {
				startText, _ := variant["start_text"].(string)
				startText = strings.TrimSpace(startText)
				if startText == "" {
					errs = append(errs, fmt.Errorf("%s.start_text is empty but cronograma.sessions is present", basePath))
				} else if parsedStartDate, ok := parseStartTextToDate(startText); !ok {
					errs = append(errs, fmt.Errorf("%s.start_text %q could not be parsed as a date", basePath, startText))
				} else if !sameDay(parsedStartDate, firstSessionDate) {
					errs = append(errs, fmt.Errorf(
						"%s.start_text %q does not match the first cronograma session date %s",
						basePath,
						startText,
						firstSessionDate.Format("2006-01-02"),
					))
				}
			}

			if config.CourseDurationHoursMatchesSessionHours {
				durationHours, ok := coerceInt(variant["duration_hours"])
				if !ok {
					errs = append(errs, fmt.Errorf("%s.duration_hours is missing/invalid but cronograma.sessions is present", basePath))
				} else if totalHours, ok := sumCronogramaSessionHours(sessions); !ok {
					errs = append(errs, fmt.Errorf("%s.cronograma.sessions hours is missing/invalid", basePath))
				} else if durationHours != totalHours {
					errs = append(errs, fmt.Errorf(
						"%s.duration_hours (%d) does not match sum(cronograma.sessions.hours) (%d)",
						basePath,
						durationHours,
						totalHours,
					))
				}
			}
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func digMap(parent map[string]any, key string) (map[string]any, bool) {
	value, ok := parent[key].(map[string]any)
	return value, ok
}

func digSlice(parent map[string]any, key string) ([]any, bool) {
	value, ok := parent[key].([]any)
	return value, ok
}

func firstCronogramaSessionDate(sessions []any) (time.Time, bool) {
	first, ok := sessions[0].(map[string]any)
	if !ok {
		return time.Time{}, false
	}
	dateStr, ok := first["date"].(string)
	if !ok {
		return time.Time{}, false
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(dateStr))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func sumCronogramaSessionHours(sessions []any) (int, bool) {
	total := 0
	for idx, entry := range sessions {
		session, ok := entry.(map[string]any)
		if !ok {
			return 0, false
		}
		hours, ok := coerceInt(session["hours"])
		if !ok {
			_ = idx
			return 0, false
		}
		total += hours
	}
	return total, true
}

func coerceInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		// yaml unmarshals numbers into float64 when decoding into interface{}.
		if typed != float64(int(typed)) {
			return 0, false
		}
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func parseStartTextToDate(startText string) (time.Time, bool) {
	startText = strings.TrimSpace(startText)
	if startText == "" {
		return time.Time{}, false
	}

	// ISO date.
	if parsed, err := time.Parse("2006-01-02", startText); err == nil {
		return parsed, true
	}

	// Numeric Brazilian-ish format: dd/mm/yyyy
	if parsed, err := time.Parse("02/01/2006", startText); err == nil {
		return parsed, true
	}

	// Portuguese month format: dd/<month>/yyyy (e.g., 19/Maio/2026)
	parts := strings.Split(startText, "/")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	day, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return time.Time{}, false
	}
	year, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil {
		return time.Time{}, false
	}

	month, ok := parsePtBRMonth(parts[1])
	if !ok {
		return time.Time{}, false
	}

	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC), true
}

func parsePtBRMonth(raw string) (time.Month, bool) {
	normalized := normalizeMonthName(raw)
	switch normalized {
	case "janeiro", "jan":
		return time.January, true
	case "fevereiro", "fev":
		return time.February, true
	case "marco", "mar":
		return time.March, true
	case "abril", "abr":
		return time.April, true
	case "maio", "mai":
		return time.May, true
	case "junho", "jun":
		return time.June, true
	case "julho", "jul":
		return time.July, true
	case "agosto", "ago":
		return time.August, true
	case "setembro", "set":
		return time.September, true
	case "outubro", "out":
		return time.October, true
	case "novembro", "nov":
		return time.November, true
	case "dezembro", "dez":
		return time.December, true
	default:
		return 0, false
	}
}

func normalizeMonthName(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	replacer := strings.NewReplacer(
		"á", "a",
		"à", "a",
		"â", "a",
		"ã", "a",
		"é", "e",
		"ê", "e",
		"í", "i",
		"ó", "o",
		"ô", "o",
		"õ", "o",
		"ú", "u",
		"ç", "c",
	)
	return replacer.Replace(raw)
}

