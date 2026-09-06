package collections

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"
)

// BasePathKey is the reserved record key exposing the site's base_path to
// derive templates. Posts prepend base_path to their href; courses and listings
// do not (decision record §3.4). Making it a template input keeps that a
// per-collection choice instead of hardcoding either convention — do NOT
// "unify" it during a migration, because casaboa's app.js and ffreis's
// course.gohtml both compensate on the consuming side and would double-prefix.
const BasePathKey = "__base_path"

var (
	slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
	slugTrim     = regexp.MustCompile(`^-+|-+$`)
)

// Slugify converts a title to a URL slug: lowercase, non-alphanumerics
// collapsed to single hyphens, trimmed. "MLOps in Production!" ->
// "mlops-in-production".
//
// This reproduces courses.Slugify exactly; TestSlugifyMatchesCoursesOracle
// cross-checks the two implementations rather than trusting the copy.
func Slugify(title string) string {
	s := slugNonAlnum.ReplaceAllString(strings.ToLower(title), "-")
	return slugTrim.ReplaceAllString(s, "")
}

// FormatMoney renders a price in cents as a currency string, dropping ".00"
// for whole amounts. 4900 -> "$49"; 4999 -> "$49.99"; 0 -> "" (no price).
//
// This reproduces courses.formatMoney exactly. That function is unexported, so
// TestFormatMoneyMatchesCoursesOracle cross-checks it through the
// price_usd_display / price_brl_display keys courses.ToSiteDataList emits.
func FormatMoney(symbol string, cents int) string {
	if cents <= 0 {
		return ""
	}
	whole := cents / 100
	frac := cents % 100
	if frac == 0 {
		return fmt.Sprintf("%s%d", symbol, whole)
	}
	return fmt.Sprintf("%s%d.%02d", symbol, whole, frac)
}

// ProjectOptions carries the per-build inputs a projection needs.
type ProjectOptions struct {
	// BasePath is site data's base_path, exposed to derive templates as
	// BasePathKey.
	BasePath string
}

// Projection is the outcome of projecting a collection's raw records.
type Projection struct {
	// Records are the index / publish records: the fields projection plus
	// derive. This is what feeds paginated pages, home carousels, and any
	// site-data key a publish entry names.
	Records []any
	// Details are the per-record detail-page maps — Records plus the detail
	// spec's extra fields — in the same order. Nil when the collection has no
	// enabled detail page, so a collection like projects never pays for it.
	Details []map[string]any
}

// projectedRecord pairs a projected record with the raw map it came from, so
// the detail projection can still reach fields the index projection dropped
// after sorting has reordered everything.
type projectedRecord struct {
	values map[string]any
	raw    map[string]any
}

// Project turns raw YAML records into the []any of map[string]any that Go
// templates consume, applying (in this order):
//
//  1. the fields projection — type coercion, defaults, and default_from;
//  2. the required/require checks;
//  3. derive;
//  4. sort.
//
// The order matters and matches the existing loaders: default_from must run
// before derive (courses derive href from the slug that default_from fills in),
// and sort must run last because it orders by projected fields.
func (c Collection) Project(raw []map[string]any, opts ProjectOptions) (Projection, error) {
	projected := make([]projectedRecord, 0, len(raw))
	for i, rec := range raw {
		out, err := c.Records.projectOne(rec)
		if err != nil {
			return Projection{}, fmt.Errorf("%s[%d]: %w", c.Name, i, err)
		}
		if err := c.Records.checkRequired(out, i, c.Name); err != nil {
			return Projection{}, err
		}
		if err := c.Records.applyDerive(out, opts); err != nil {
			return Projection{}, fmt.Errorf("%s[%d]: %w", c.Name, i, err)
		}
		projected = append(projected, projectedRecord{values: out, raw: rec})
	}

	c.Records.applySort(projected)

	result := Projection{Records: make([]any, len(projected))}
	for i, rec := range projected {
		result.Records[i] = rec.values
	}
	if c.Detail != nil && c.Detail.Enabled {
		result.Details = c.projectDetails(projected)
	}
	return result, nil
}

// projectDetails builds the per-record detail maps: a copy of the index record
// extended with the detail spec's extra fields, projected from the raw record
// with the same type/zero-value rules.
func (c Collection) projectDetails(projected []projectedRecord) []map[string]any {
	details := make([]map[string]any, len(projected))
	for i, rec := range projected {
		detail := make(map[string]any, len(rec.values)+len(c.Detail.ExtraFields))
		for k, v := range rec.values {
			detail[k] = v
		}
		for _, f := range c.Detail.ExtraFields {
			v, present := rec.raw[f.Name]
			if !present || v == nil {
				detail[f.Name] = zeroValue(f)
				continue
			}
			coerced, err := coerce(f, v)
			if err != nil {
				// A mistyped extra field falls back to its zero value rather
				// than failing the build: it is additive detail-page content,
				// and the index projection (which gates the build) has already
				// validated every field the site actually depends on.
				detail[f.Name] = zeroValue(f)
				continue
			}
			detail[f.Name] = coerced
		}
		details[i] = detail
	}
	return details
}

// projectOne builds one record map from one raw YAML map.
func (r RecordSpec) projectOne(rec map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(r.Fields))

	if r.Passthrough {
		for k, v := range rec {
			out[k] = v
		}
	}

	for _, f := range r.Fields {
		v, present := rec[f.Name]
		if !present || v == nil {
			out[f.Name] = zeroValue(f)
			continue
		}
		coerced, err := coerce(f, v)
		if err != nil {
			return nil, err
		}
		out[f.Name] = coerced
	}

	// default_from runs after every field is placed so it can read a sibling
	// field's projected value (slug <- title).
	for _, f := range r.Fields {
		if f.DefaultFrom == nil || f.DefaultFrom.Slugify == "" {
			continue
		}
		if s, _ := out[f.Name].(string); s != "" {
			continue
		}
		from, _ := out[f.DefaultFrom.Slugify].(string)
		out[f.Name] = Slugify(from)
	}

	return out, nil
}

// checkRequired enforces both Field.Required and RecordSpec.Require. The error
// text mirrors the existing loaders ("projects[0]: missing required field
// 'title'") so a bad content file reads the same after the migration.
func (r RecordSpec) checkRequired(out map[string]any, i int, name string) error {
	for _, f := range r.Fields {
		if !f.Required {
			continue
		}
		if isEmpty(out[f.Name]) {
			return fmt.Errorf("%s[%d]: missing required field %q", name, i, f.Name)
		}
	}
	for _, key := range r.Require {
		if isEmpty(out[key]) {
			return fmt.Errorf("%s[%d]: missing required field %q", name, i, key)
		}
	}
	return nil
}

func (r RecordSpec) applyDerive(out map[string]any, opts ProjectOptions) error {
	for _, d := range r.Derive {
		switch {
		case d.Money != nil:
			cents, _ := out[d.Money.Field].(int)
			out[d.Name] = FormatMoney(d.Money.Symbol, cents)
		default:
			v, err := renderDeriveTemplate(d, out, opts)
			if err != nil {
				return err
			}
			out[d.Name] = v
		}
	}
	return nil
}

// RenderPath renders a detail page's path template against one record, with
// base_path available under BasePathKey. Used by the detail writer for both the
// on-disk target and the sitemap entry, so the two can never disagree.
func RenderPath(pathTemplate string, record map[string]any, basePath string) (string, error) {
	return renderRecordTemplate("detail path", pathTemplate, record, basePath)
}

// RenderPageName renders a DetailSpec.PageNameFrom template against one record,
// giving the page identity the detail render and the per-page transforms use.
func RenderPageName(nameTemplate string, record map[string]any, basePath string) (string, error) {
	return renderRecordTemplate("page_name_from", nameTemplate, record, basePath)
}

// ResolvePageData renders a DetailSpec.PageDataFrom dotted-path template
// against one record and returns the site-data value it names, for the detail
// writer to expose as PageDataKey.
//
// An unresolvable path is an error, not a silent nil. page_data_from supplies
// the detail page's COPY — title, meta description, body HTML — so a path that
// resolves to nothing renders a structurally valid but contentless page that
// every other guard in the build (linkcheck, sitemap, structure validation)
// would happily pass. That is exactly the class of silent omission decision
// record §7.1 requires to fail loudly, and the same reasoning as
// validateCollectionSources rejecting an unknown -collection-source name.
func ResolvePageData(pathTemplate string, record map[string]any, siteData map[string]any, basePath string) (any, error) {
	path, err := renderRecordTemplate("page_data_from", pathTemplate, record, basePath)
	if err != nil {
		return nil, err
	}
	v, ok := LookupDottedSiteData(siteData, path)
	if !ok {
		return nil, fmt.Errorf(
			"page_data_from %q resolved to %q, which is not present in site data", pathTemplate, path)
	}
	return v, nil
}

// renderRecordTemplate renders one per-record template (a detail path or a
// page_data_from path) with base_path available under BasePathKey. Shared so
// both resolve a record identically — a path and its page copy disagreeing
// about which record they name would be very hard to spot in the output.
func renderRecordTemplate(what, tmplText string, record map[string]any, basePath string) (string, error) {
	tmpl, err := template.New(what).Parse(tmplText)
	if err != nil {
		return "", fmt.Errorf("parsing %s template %q: %w", what, tmplText, err)
	}
	data := make(map[string]any, len(record)+1)
	for k, v := range record {
		data[k] = v
	}
	data[BasePathKey] = basePath

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering %s template %q: %w", what, tmplText, err)
	}
	return buf.String(), nil
}

func renderDeriveTemplate(d Derive, out map[string]any, opts ProjectOptions) (string, error) {
	tmpl, err := template.New(d.Name).Parse(d.Template)
	if err != nil {
		return "", fmt.Errorf("derive %q: parsing template: %w", d.Name, err)
	}
	data := make(map[string]any, len(out)+1)
	for k, v := range out {
		data[k] = v
	}
	data[BasePathKey] = opts.BasePath

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("derive %q: %w", d.Name, err)
	}
	return buf.String(), nil
}

// applySort sorts in place. An empty Sort preserves file order, which listings
// depend on because it encodes upstream ranking (decision record Q7) — the
// engine makes this configurable rather than picking one convention.
func (r RecordSpec) applySort(recs []projectedRecord) {
	if len(r.Sort) == 0 {
		return
	}
	sort.SliceStable(recs, func(i, j int) bool {
		for _, key := range r.Sort {
			cmp := compareValues(recs[i].values[key.By], recs[j].values[key.By])
			if cmp == 0 {
				continue
			}
			if key.Dir == "desc" {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	})
}

// compareValues orders two projected field values. Only the types the sort keys
// can realistically hold (int, string, bool) are ordered; anything else compares
// equal, leaving SliceStable to preserve file order.
func compareValues(a, b any) int {
	switch av := a.(type) {
	case int:
		bv, ok := b.(int)
		if !ok {
			return 0
		}
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		default:
			return 0
		}
	case string:
		bv, ok := b.(string)
		if !ok {
			return 0
		}
		return strings.Compare(av, bv)
	case bool:
		bv, ok := b.(bool)
		if !ok {
			return 0
		}
		switch {
		case !av && bv:
			return -1
		case av && !bv:
			return 1
		default:
			return 0
		}
	default:
		return 0
	}
}

// zeroValue is the value a field takes when the YAML omits it.
//
// The empty list and map are deliberately non-nil: the typed structs this
// replaces built them with make(), so they marshal as [] and {} rather than
// null. ffreis-website's project card renders
// data-filter-tags="{{toJSON (dig $p "stack")}}", so a nil here would change
// the emitted HTML for any record without a stack.
func zeroValue(f Field) any {
	if f.Default != nil {
		return f.Default
	}
	switch f.Type {
	case TypeInt:
		return 0
	case TypeBool:
		return false
	case TypeList:
		return []any{}
	case TypeMap:
		return map[string]any{}
	case TypeAny:
		return nil
	default:
		return ""
	}
}

// coerce validates a raw YAML value against the field's declared type. It is
// strict on purpose: the typed structs it replaces would have failed to
// unmarshal a mistyped value, and silently accepting one would let a content
// error reach the rendered page.
func coerce(f Field, v any) (any, error) {
	switch f.Type {
	case TypeString:
		s, ok := v.(string)
		if !ok {
			return nil, typeErr(f, v, "a string")
		}
		return s, nil
	case TypeInt:
		switch n := v.(type) {
		case int:
			return n, nil
		case float64:
			// yaml.v3 only yields float64 for values written with a decimal
			// point; accept a whole one so `order: 3.0` behaves like `order: 3`.
			if n == float64(int(n)) {
				return int(n), nil
			}
			return nil, typeErr(f, v, "a whole number")
		default:
			return nil, typeErr(f, v, "an integer")
		}
	case TypeBool:
		b, ok := v.(bool)
		if !ok {
			return nil, typeErr(f, v, "a boolean")
		}
		return b, nil
	case TypeList:
		l, ok := v.([]any)
		if !ok {
			return nil, typeErr(f, v, "a list")
		}
		return l, nil
	case TypeMap:
		m, ok := v.(map[string]any)
		if !ok {
			return nil, typeErr(f, v, "a map")
		}
		return m, nil
	default:
		// TypeAny and an unset type pass the value through untouched.
		return v, nil
	}
}

func typeErr(f Field, v any, want string) error {
	return fmt.Errorf("field %q must be %s, got %T", f.Name, want, v)
}

// isEmpty reports whether a projected value counts as "missing" for a required
// check. It matches the existing loaders, which test the zero value.
func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case int:
		return t == 0
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	default:
		return false
	}
}
