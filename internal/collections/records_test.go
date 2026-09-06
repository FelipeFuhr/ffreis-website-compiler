package collections

import (
	"reflect"
	"strings"
	"testing"
)

func projectOnly(t *testing.T, c Collection, raw []map[string]any, opts ProjectOptions) []any {
	t.Helper()
	got, err := c.Project(raw, opts)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	return got.Records
}

func firstRecord(t *testing.T, records []any) map[string]any {
	t.Helper()
	if len(records) == 0 {
		t.Fatal("no records")
	}
	m, ok := records[0].(map[string]any)
	if !ok {
		t.Fatalf("record is %T, want map[string]any", records[0])
	}
	return m
}

// TestProjectionAppliesZeroValues pins the zero values the typed structs used
// to contribute. The empty list and map must be NON-NIL: ffreis-website renders
// data-filter-tags="{{toJSON (dig $p "stack")}}", where nil marshals to `null`
// and an empty slice to `[]`.
func TestProjectionAppliesZeroValues(t *testing.T) {
	c := Collection{
		Name: "t",
		Records: RecordSpec{Fields: []Field{
			{Name: "title", Type: TypeString},
			{Name: "count", Type: TypeInt},
			{Name: "flag", Type: TypeBool},
			{Name: "items", Type: TypeList},
			{Name: "meta", Type: TypeMap},
			{Name: "loose", Type: TypeAny},
		}},
	}
	rec := firstRecord(t, projectOnly(t, c, []map[string]any{{"title": "only"}}, ProjectOptions{}))

	want := map[string]any{
		"title": "only",
		"count": 0,
		"flag":  false,
		"items": []any{},
		"meta":  map[string]any{},
		"loose": nil,
	}
	if !reflect.DeepEqual(want, rec) {
		t.Errorf("zero values differ:\n%s", describeMapDiff(want, rec))
	}

	// Guard the nil-vs-empty distinction explicitly: DeepEqual would also pass
	// if both sides were nil, so assert non-nil directly.
	if items, _ := rec["items"].([]any); items == nil {
		t.Error("an absent list projected to a nil []any; it must be empty-but-non-nil so toJSON emits [] not null")
	}
	if meta, _ := rec["meta"].(map[string]any); meta == nil {
		t.Error("an absent map projected to a nil map; it must be empty-but-non-nil so toJSON emits {} not null")
	}
}

// TestProjectionDropsUnlistedKeysByDefault pins decision record Q1: with
// passthrough false (the default), a YAML key the fields list does not name is
// dropped, exactly as the typed structs dropped it.
func TestProjectionDropsUnlistedKeysByDefault(t *testing.T) {
	raw := []map[string]any{{
		"title":               "T",
		"available_languages": []any{"pt"},
	}}
	c := Collection{Name: "t", Records: RecordSpec{Fields: []Field{{Name: "title", Type: TypeString}}}}

	rec := firstRecord(t, projectOnly(t, c, raw, ProjectOptions{}))
	if _, present := rec["available_languages"]; present {
		t.Error("passthrough is false, so available_languages must be dropped (decision record Q1)")
	}

	c.Records.Passthrough = true
	rec = firstRecord(t, projectOnly(t, c, raw, ProjectOptions{}))
	if got, _ := rec["available_languages"].([]any); len(got) != 1 {
		t.Errorf("passthrough true must keep available_languages, got %v", rec["available_languages"])
	}
}

func TestProjectionRequiredFields(t *testing.T) {
	c := Collection{
		Name:    "projects",
		Records: RecordSpec{Fields: []Field{{Name: "title", Type: TypeString, Required: true}}},
	}
	_, err := c.Project([]map[string]any{{"title": "ok"}, {}}, ProjectOptions{})
	if err == nil {
		t.Fatal("expected an error for a record missing a required field")
	}
	// The message must match the typed loaders' so a bad content file reads the
	// same after the migration.
	if want := `projects[1]: missing required field "title"`; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err, want)
	}
}

func TestProjectionRequireList(t *testing.T) {
	c := Collection{
		Name: "listings",
		Records: RecordSpec{
			Fields:  []Field{{Name: "id", Type: TypeString}, {Name: "title", Type: TypeString}},
			Require: []string{"id", "title"},
		},
	}
	if _, err := c.Project([]map[string]any{{"id": "a", "title": "T"}}, ProjectOptions{}); err != nil {
		t.Fatalf("valid record rejected: %v", err)
	}
	if _, err := c.Project([]map[string]any{{"id": "a"}}, ProjectOptions{}); err == nil {
		t.Error("expected an error when a require: entry is empty")
	}
}

func TestProjectionRejectsMistypedValues(t *testing.T) {
	cases := []struct {
		name  string
		field Field
		value any
	}{
		{"string", Field{Name: "f", Type: TypeString}, 3},
		{"int", Field{Name: "f", Type: TypeInt}, "3"},
		{"int fractional", Field{Name: "f", Type: TypeInt}, 3.5},
		{"bool", Field{Name: "f", Type: TypeBool}, "yes"},
		{"list", Field{Name: "f", Type: TypeList}, "a,b"},
		{"map", Field{Name: "f", Type: TypeMap}, []any{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Collection{Name: "t", Records: RecordSpec{Fields: []Field{tc.field}}}
			if _, err := c.Project([]map[string]any{{"f": tc.value}}, ProjectOptions{}); err == nil {
				t.Errorf("expected a type error for %T value %v", tc.value, tc.value)
			}
		})
	}

	// A whole float is accepted, so `order: 3.0` behaves like `order: 3`.
	c := Collection{Name: "t", Records: RecordSpec{Fields: []Field{{Name: "f", Type: TypeInt}}}}
	rec := firstRecord(t, projectOnly(t, c, []map[string]any{{"f": 3.0}}, ProjectOptions{}))
	if rec["f"] != 3 {
		t.Errorf("whole float coerced to %#v, want int 3", rec["f"])
	}
}

func TestDeriveTemplateForm(t *testing.T) {
	c := Collection{
		Name: "t",
		Records: RecordSpec{
			Fields: []Field{{Name: "slug", Type: TypeString}},
			Derive: []Derive{{Name: "href", Template: "/courses/{{.slug}}/"}},
		},
	}
	rec := firstRecord(t, projectOnly(t, c, []map[string]any{{"slug": "mlops"}}, ProjectOptions{}))
	if rec["href"] != "/courses/mlops/" {
		t.Errorf("href = %v, want /courses/mlops/", rec["href"])
	}
}

// TestDeriveTemplateSeesBasePath pins decision record §3.4: base_path is
// available to derive templates under a reserved key, so a collection can
// prepend it (as posts do) without the engine hardcoding either convention.
func TestDeriveTemplateSeesBasePath(t *testing.T) {
	c := Collection{
		Name: "t",
		Records: RecordSpec{
			Fields: []Field{{Name: "slug", Type: TypeString}},
			Derive: []Derive{{Name: "href", Template: "{{." + BasePathKey + "}}/blog/{{.slug}}/"}},
		},
	}
	rec := firstRecord(t, projectOnly(t, c,
		[]map[string]any{{"slug": "post"}}, ProjectOptions{BasePath: "/pt"}))
	if rec["href"] != "/pt/blog/post/" {
		t.Errorf("href = %v, want /pt/blog/post/", rec["href"])
	}
}

func TestDeriveMoneyForm(t *testing.T) {
	c := Collection{
		Name: "t",
		Records: RecordSpec{
			Fields: []Field{{Name: "cents", Type: TypeInt}},
			Derive: []Derive{{Name: "display", Money: &MoneySpec{Field: "cents", Symbol: "$"}}},
		},
	}
	for _, tc := range []struct {
		cents int
		want  string
	}{{0, ""}, {-1, ""}, {1, "$0.01"}, {99, "$0.99"}, {100, "$1"}, {4900, "$49"}, {4999, "$49.99"}} {
		rec := firstRecord(t, projectOnly(t, c, []map[string]any{{"cents": tc.cents}}, ProjectOptions{}))
		if rec["display"] != tc.want {
			t.Errorf("money(%d) = %v, want %q", tc.cents, rec["display"], tc.want)
		}
	}
}

// TestDefaultFromSlugifyRunsBeforeDerive pins the ordering courses depends on:
// slug is filled from title, and only then does href interpolate it.
func TestDefaultFromSlugifyRunsBeforeDerive(t *testing.T) {
	c := Collection{
		Name: "t",
		Records: RecordSpec{
			Fields: []Field{
				{Name: "title", Type: TypeString},
				{Name: "slug", Type: TypeString, DefaultFrom: &DefaultFrom{Slugify: "title"}},
			},
			Derive: []Derive{{Name: "href", Template: "/courses/{{.slug}}/"}},
		},
	}

	rec := firstRecord(t, projectOnly(t, c, []map[string]any{{"title": "MLOps in Production!"}}, ProjectOptions{}))
	if rec["slug"] != "mlops-in-production" {
		t.Errorf("slug = %v, want mlops-in-production", rec["slug"])
	}
	if rec["href"] != "/courses/mlops-in-production/" {
		t.Errorf("href = %v, want /courses/mlops-in-production/", rec["href"])
	}

	// An explicit slug wins over the derived one.
	rec = firstRecord(t, projectOnly(t, c,
		[]map[string]any{{"title": "MLOps in Production!", "slug": "custom"}}, ProjectOptions{}))
	if rec["slug"] != "custom" {
		t.Errorf("explicit slug was overwritten: %v", rec["slug"])
	}
}

func TestSortByOrderThenTitle(t *testing.T) {
	c := Collection{
		Name: "t",
		Records: RecordSpec{
			Fields: []Field{{Name: "title", Type: TypeString}, {Name: "order", Type: TypeInt}},
			Sort:   []SortBy{{By: "order"}, {By: "title"}},
		},
	}
	raw := []map[string]any{
		{"title": "Beta", "order": 2},
		{"title": "Zulu", "order": 1},
		{"title": "Alpha", "order": 1},
		{"title": "NoOrder"},
	}
	records := projectOnly(t, c, raw, ProjectOptions{})

	var got []string
	for _, r := range records {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("record is %T", r)
		}
		got = append(got, m["title"].(string))
	}
	want := []string{"NoOrder", "Alpha", "Zulu", "Beta"} // order 0, then 1 (Alpha<Zulu), then 2
	if !reflect.DeepEqual(want, got) {
		t.Errorf("sorted order = %v, want %v", got, want)
	}
}

func TestSortDescending(t *testing.T) {
	c := Collection{
		Name: "t",
		Records: RecordSpec{
			Fields: []Field{{Name: "order", Type: TypeInt}},
			Sort:   []SortBy{{By: "order", Dir: "desc"}},
		},
	}
	records := projectOnly(t, c,
		[]map[string]any{{"order": 1}, {"order": 3}, {"order": 2}}, ProjectOptions{})
	var got []int
	for _, r := range records {
		got = append(got, r.(map[string]any)["order"].(int))
	}
	if want := []int{3, 2, 1}; !reflect.DeepEqual(want, got) {
		t.Errorf("desc sort = %v, want %v", got, want)
	}
}

// TestEmptySortPreservesFileOrder pins decision record Q7: listings depend on
// file order because it encodes upstream ranking.
func TestEmptySortPreservesFileOrder(t *testing.T) {
	c := Collection{
		Name: "t",
		Records: RecordSpec{
			Fields: []Field{{Name: "id", Type: TypeString}, {Name: "score", Type: TypeInt}},
			Sort:   []SortBy{},
		},
	}
	records := projectOnly(t, c, []map[string]any{
		{"id": "c", "score": 1},
		{"id": "a", "score": 9},
		{"id": "b", "score": 5},
	}, ProjectOptions{})

	var got []string
	for _, r := range records {
		got = append(got, r.(map[string]any)["id"].(string))
	}
	if want := []string{"c", "a", "b"}; !reflect.DeepEqual(want, got) {
		t.Errorf("order = %v, want file order %v", got, want)
	}
}

func TestProjectDetailsAddExtraFields(t *testing.T) {
	c := Collection{
		Name: "t",
		Records: RecordSpec{
			Fields: []Field{{Name: "slug", Type: TypeString}},
		},
		Detail: &DetailSpec{
			Enabled: true, Template: "d", Path: "/d/{{.slug}}/", DataKey: "CurrentD",
			ExtraFields: []Field{
				{Name: "body", Type: TypeString},
				{Name: "bullets", Type: TypeList},
			},
		},
	}
	got, err := c.Project([]map[string]any{
		{"slug": "a", "body": "text", "bullets": []any{"one"}},
		{"slug": "b"},
	}, ProjectOptions{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	if len(got.Details) != 2 {
		t.Fatalf("got %d detail records, want 2", len(got.Details))
	}
	if got.Details[0]["body"] != "text" {
		t.Errorf("detail body = %v, want text", got.Details[0]["body"])
	}
	// The index records must NOT carry the extra fields.
	if _, present := firstRecord(t, got.Records)["body"]; present {
		t.Error("extra_fields leaked into the index record; they belong to the detail page only")
	}
	// Absent extra fields take their typed zero value, non-nil for lists.
	if b, _ := got.Details[1]["bullets"].([]any); b == nil {
		t.Error("absent extra list field projected to nil; want empty-but-non-nil")
	}
	if got.Details[1]["body"] != "" {
		t.Errorf("absent extra string field = %#v, want \"\"", got.Details[1]["body"])
	}
}

func TestProjectSkipsDetailsWhenDisabled(t *testing.T) {
	c := builtinProjects()
	got, err := c.Project([]map[string]any{{"title": "T"}}, ProjectOptions{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got.Details != nil {
		t.Errorf("detail is disabled, but Project returned %d detail records", len(got.Details))
	}
}
