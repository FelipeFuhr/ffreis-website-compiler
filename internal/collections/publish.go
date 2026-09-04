package collections

import "strings"

// PublishOptions carries the flag values a publish entry can read a limit from.
type PublishOptions struct {
	ItemsPerPage int
}

// ApplyPublish writes the collection's records into site data at each declared
// key, honouring the per-entry limit. Called AFTER contract validation, like
// loadOptionalContent today, so published keys need no contract entry and
// cannot dangle.
func (c Collection) ApplyPublish(siteData map[string]any, records []any, opts PublishOptions) {
	for _, p := range c.Publish {
		SetDottedSiteData(siteData, p.Key, limitRecords(records, p.resolveLimit(opts)))
	}
}

func (p Publish) resolveLimit(opts PublishOptions) int {
	if p.LimitFromFlag == itemsPerPageFlag {
		return opts.ItemsPerPage
	}
	return p.Limit
}

// limitRecords truncates to the first n records. n <= 0 means "no limit",
// matching injectProjectsHomeCarousel / injectCoursesHomeCarousel.
func limitRecords(records []any, n int) []any {
	if n > 0 && n < len(records) {
		return records[:n]
	}
	return records
}

// SetDottedSiteData writes value at a dotted site-data path, reproducing the
// two existing injectors' differing behaviour on missing intermediates:
//
//   - "projects" (one segment) is set directly on siteData, like
//     injectProjectsHomeCarousel.
//   - "pages.index.courses_carousel_items" is a NO-OP when siteData["pages"] is
//     absent, but CREATES the "index" map when only that is missing — exactly
//     what injectCoursesHomeCarousel does today. The silent no-op is deliberate
//     current behaviour (decision record §3.2), not an oversight to fix here.
//
// The rule generalising both: the first segment's container must already
// exist; deeper intermediates are created on demand.
func SetDottedSiteData(siteData map[string]any, path string, value any) {
	segments := strings.Split(path, ".")
	if len(segments) == 1 {
		siteData[segments[0]] = value
		return
	}

	current, ok := siteData[segments[0]].(map[string]any)
	if !ok {
		return // first segment absent (or not a map) — no-op.
	}

	for _, seg := range segments[1 : len(segments)-1] {
		next, ok := current[seg].(map[string]any)
		if !ok {
			next = make(map[string]any)
			current[seg] = next
		}
		current = next
	}
	current[segments[len(segments)-1]] = value
}

// LookupDottedSiteData reads the value at a dotted site-data path, reporting
// whether it is present. It is the read-side mirror of SetDottedSiteData and
// backs DetailSpec.PageDataFrom.
//
// It creates nothing: an intermediate that is absent, nil, or not a map means
// "not present", never a fabricated empty map. That distinction is the whole
// point — the caller turns a missing path into a loud error rather than
// rendering a page with silently empty copy.
func LookupDottedSiteData(siteData map[string]any, path string) (any, bool) {
	segments := strings.Split(path, ".")

	current := siteData
	for _, seg := range segments[:len(segments)-1] {
		next, ok := current[seg].(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}

	v, ok := current[segments[len(segments)-1]]
	return v, ok
}
