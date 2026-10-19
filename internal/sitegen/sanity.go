package sitegen

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const dateLayout = "2006-01-02"

// sessionDateLayouts are every spelling a cronograma session date can actually
// reach this checker in, most specific first.
//
// A bare YAML scalar (`date: 2026-09-03`) resolves to the !!timestamp tag and
// is decoded as a time.Time, which the loader's normalizeYAMLValue then
// rewrites to RFC3339 ("2026-09-03T00:00:00Z") before any validation runs. So
// RFC3339 -- not dateLayout -- is the form real consumer site data arrives in;
// dateLayout only survives when the author quoted the value. Accepting one
// form and silently skipping the other is what made this gate vacuous, so
// accept every form that can occur and reject anything else loudly.
var sessionDateLayouts = []string{
	dateLayout,
	time.RFC3339,
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04:05 -0700 MST",
}

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
		errs = append(errs, validateCourseSanity(courseID, course, config)...)
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func validateCourseSanity(courseID string, course map[string]any, config SanityConfig) []error {
	variants, ok := digMap(course, "variants")
	if !ok {
		return nil
	}

	var errs []error
	for variantID, variantValue := range variants {
		variant, ok := variantValue.(map[string]any)
		if !ok {
			continue
		}
		errs = append(errs, validateVariantSanity(courseID, variantID, variant, config)...)
	}
	return errs
}

func validateVariantSanity(courseID, variantID string, variant map[string]any, config SanityConfig) []error {
	cronograma, hasCronograma := digMap(variant, "cronograma")
	if !hasCronograma {
		return nil
	}
	sessions, ok := digSlice(cronograma, "sessions")
	if !ok || len(sessions) == 0 {
		return nil
	}

	basePath := fmt.Sprintf("courses.%s.variants.%s", courseID, variantID)
	var errs []error

	if config.CourseStartMatchesFirstSession {
		// A date this checker cannot read is a hard failure, never a skip.
		// Returning nil here used to disable every rule below it for the whole
		// variant, which is how the gate passed for every input that had
		// sessions at all.
		firstSessionDate, err := firstCronogramaSessionDate(sessions)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s.%w", basePath, err))
		} else {
			errs = append(errs, validateCourseStartText(basePath, variant, firstSessionDate)...)
		}
	}
	if config.CourseDurationHoursMatchesSessionHours {
		errs = append(errs, validateCourseDurationHours(basePath, variant, sessions)...)
	}

	return errs
}

func validateCourseStartText(basePath string, variant map[string]any, firstSessionDate time.Time) []error {
	startText, _ := variant["start_text"].(string)
	startText = strings.TrimSpace(startText)
	if startText == "" {
		return []error{fmt.Errorf("%s.start_text is empty but cronograma.sessions is present", basePath)}
	}

	parsedStartDate, ok := parseStartTextToDate(startText)
	if !ok {
		return []error{fmt.Errorf("%s.start_text %q could not be parsed as a date", basePath, startText)}
	}
	if sameDay(parsedStartDate, firstSessionDate) {
		return nil
	}

	return []error{fmt.Errorf(
		"%s.start_text %q does not match the first cronograma session date %s",
		basePath,
		startText,
		firstSessionDate.Format(dateLayout),
	)}
}

func validateCourseDurationHours(basePath string, variant map[string]any, sessions []any) []error {
	durationHours, ok := coerceInt(variant["duration_hours"])
	if !ok {
		return []error{fmt.Errorf("%s.duration_hours is missing/invalid but cronograma.sessions is present", basePath)}
	}

	totalHours, err := sumCronogramaSessionHours(sessions)
	if err != nil {
		return []error{fmt.Errorf("%s.%w", basePath, err)}
	}
	if durationHours == totalHours {
		return nil
	}

	return []error{fmt.Errorf(
		"%s.duration_hours (%d) does not match sum(cronograma.sessions.hours) (%d)",
		basePath,
		durationHours,
		totalHours,
	)}
}

func digMap(parent map[string]any, key string) (map[string]any, bool) {
	value, ok := parent[key].(map[string]any)
	return value, ok
}

func digSlice(parent map[string]any, key string) ([]any, bool) {
	value, ok := parent[key].([]any)
	return value, ok
}

func firstCronogramaSessionDate(sessions []any) (time.Time, error) {
	first, ok := sessions[0].(map[string]any)
	if !ok {
		return time.Time{}, fmt.Errorf("cronograma.sessions[0] must be a mapping, got %T", sessions[0])
	}
	return parseSessionDate(first["date"])
}

func parseSessionDate(raw any) (time.Time, error) {
	switch typed := raw.(type) {
	case time.Time:
		// Site data that reached the checker without passing through
		// normalizeYAMLValue still carries a !!timestamp as a time.Time.
		return typed, nil
	case nil:
		return time.Time{}, errors.New("cronograma.sessions[0].date is missing")
	case string:
		return parseSessionDateString(typed)
	default:
		return time.Time{}, fmt.Errorf("cronograma.sessions[0].date must be a date, got %T (%v)", raw, raw)
	}
}

func parseSessionDateString(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, errors.New("cronograma.sessions[0].date is empty")
	}
	for _, layout := range sessionDateLayouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf(
		"cronograma.sessions[0].date %q is not a recognised date (want %s or RFC3339)",
		trimmed,
		dateLayout,
	)
}

func sumCronogramaSessionHours(sessions []any) (int, error) {
	total := 0
	for idx, entry := range sessions {
		session, ok := entry.(map[string]any)
		if !ok {
			return 0, fmt.Errorf("cronograma.sessions[%d] must be a mapping, got %T", idx, entry)
		}
		hours, ok := coerceInt(session["hours"])
		if !ok {
			return 0, fmt.Errorf("cronograma.sessions[%d].hours must be a whole number, got %v", idx, session["hours"])
		}
		total += hours
	}
	return total, nil
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

	// ISO date, or any timestamp form the loader may have normalized it into
	// (an unquoted `start_text: 2026-09-03` becomes RFC3339 just like a
	// session date does).
	for _, layout := range sessionDateLayouts {
		if parsed, err := time.Parse(layout, startText); err == nil {
			return parsed, true
		}
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
