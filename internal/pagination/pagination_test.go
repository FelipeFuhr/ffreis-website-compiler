package pagination

import "testing"

func TestPageHref(t *testing.T) {
	cases := []struct {
		name     string
		baseHref string
		pageNum  int
		want     string
	}{
		{"page one has no /page/ segment", "/blog", 1, "/blog/"},
		{"page two", "/blog", 2, "/blog/page/2/"},
		{"large page number", "/blog", 42, "/blog/page/42/"},
		{"empty base href page one", "", 1, "/"},
		{"empty base href page two", "", 2, "/page/2/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PageHref(tc.baseHref, tc.pageNum); got != tc.want {
				t.Fatalf("PageHref(%q, %d) = %q, want %q", tc.baseHref, tc.pageNum, got, tc.want)
			}
		})
	}
}

func TestPaginate_EmptyItems(t *testing.T) {
	pages := Paginate(nil, 12, "/blog")

	if len(pages) != 1 {
		t.Fatalf("expected exactly 1 page for empty items, got %d", len(pages))
	}
	pg := pages[0]
	if pg.Number != 1 || pg.TotalPages != 1 {
		t.Fatalf("expected Number=1 TotalPages=1, got Number=%d TotalPages=%d", pg.Number, pg.TotalPages)
	}
	if pg.Items != nil {
		t.Fatalf("expected nil Items for empty input, got %v", pg.Items)
	}
	if pg.Bar != nil {
		t.Fatalf("expected nil Bar for the empty-items shortcut page, got %v", pg.Bar)
	}
}

func TestPaginate_SinglePageUnderCapacity(t *testing.T) {
	items := makeItems(5)
	pages := Paginate(items, 12, "/blog")

	if len(pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages))
	}
	pg := pages[0]
	if len(pg.Items) != 5 {
		t.Fatalf("expected all 5 items on the single page, got %d", len(pg.Items))
	}
	if len(pg.Bar) != 1 || pg.Bar[0].Number != 1 || !pg.Bar[0].Active || !pg.Bar[0].IsLast {
		t.Fatalf("expected single active+last bar item for page 1, got %+v", pg.Bar)
	}
}

func TestPaginate_ExactMultipleOfPerPage(t *testing.T) {
	items := makeItems(6)
	pages := Paginate(items, 3, "/blog")

	if len(pages) != 2 {
		t.Fatalf("expected exactly 2 pages for 6 items at 3/page, got %d", len(pages))
	}
	if len(pages[0].Items) != 3 || len(pages[1].Items) != 3 {
		t.Fatalf("expected 3+3 item split, got %d+%d", len(pages[0].Items), len(pages[1].Items))
	}
	if pages[0].Items[0] != items[0] || pages[1].Items[2] != items[5] {
		t.Fatalf("items were not sliced in order: page1=%v page2=%v", pages[0].Items, pages[1].Items)
	}
}

func TestPaginate_OffByOneRemainder(t *testing.T) {
	items := makeItems(7)
	pages := Paginate(items, 3, "/blog")

	if len(pages) != 3 {
		t.Fatalf("expected 3 pages for 7 items at 3/page, got %d", len(pages))
	}
	if len(pages[0].Items) != 3 || len(pages[1].Items) != 3 {
		t.Fatalf("expected first two pages full at 3 items, got %d and %d", len(pages[0].Items), len(pages[1].Items))
	}
	if len(pages[2].Items) != 1 {
		t.Fatalf("expected the last page to carry the 1-item remainder, got %d", len(pages[2].Items))
	}
	if pages[2].Items[0] != items[6] {
		t.Fatalf("expected the remainder page to contain the last item, got %v", pages[2].Items)
	}
}

func TestPaginate_NonPositivePerPageDefaultsTo12(t *testing.T) {
	items := makeItems(13)

	for _, perPage := range []int{0, -1, -100} {
		pages := Paginate(items, perPage, "/blog")
		if len(pages) != 2 {
			t.Fatalf("perPage=%d: expected default of 12/page to yield 2 pages for 13 items, got %d", perPage, len(pages))
		}
		if len(pages[0].Items) != 12 || len(pages[1].Items) != 1 {
			t.Fatalf("perPage=%d: expected 12+1 split, got %d+%d", perPage, len(pages[0].Items), len(pages[1].Items))
		}
	}
}

func TestPaginate_EveryPageCarriesTotalPagesAndBaseHref(t *testing.T) {
	items := makeItems(10)
	pages := Paginate(items, 4, "/projects")

	if len(pages) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(pages))
	}
	for i, pg := range pages {
		if pg.Number != i+1 {
			t.Fatalf("page %d: expected Number=%d, got %d", i, i+1, pg.Number)
		}
		if pg.TotalPages != 3 {
			t.Fatalf("page %d: expected TotalPages=3, got %d", i, pg.TotalPages)
		}
		if pg.BaseHref != "/projects" {
			t.Fatalf("page %d: expected BaseHref=/projects, got %q", i, pg.BaseHref)
		}
	}
}

func TestComputeBar_SinglePage(t *testing.T) {
	bar := computeBar(1, 1, "/blog")
	if len(bar) != 1 {
		t.Fatalf("expected 1 bar item, got %d: %+v", len(bar), bar)
	}
	item := bar[0]
	if item.Type != "page" || item.Number != 1 || item.Href != "/blog/" || !item.Active || !item.IsLast {
		t.Fatalf("unexpected single-page bar item: %+v", item)
	}
}

func TestComputeBar_MiddlePageBothEllipses(t *testing.T) {
	// Documented example: P=5, T=7 -> 1 … 5 … 7(last)
	bar := computeBar(5, 7, "/blog")
	want := []BarItem{
		{Type: "page", Number: 1, Href: "/blog/", Active: false, IsLast: false},
		{Type: "ellipsis"},
		{Type: "page", Number: 5, Href: "/blog/page/5/", Active: true, IsLast: false},
		{Type: "ellipsis"},
		{Type: "page", Number: 7, Href: "/blog/page/7/", Active: false, IsLast: true},
	}
	assertBarEqual(t, bar, want)
}

func TestComputeBar_NearStartOnlyRightEllipsis(t *testing.T) {
	// Documented example: P=2, T=7 -> 1 2 … 7(last)
	bar := computeBar(2, 7, "/blog")
	want := []BarItem{
		{Type: "page", Number: 1, Href: "/blog/", Active: false, IsLast: false},
		{Type: "page", Number: 2, Href: "/blog/page/2/", Active: true, IsLast: false},
		{Type: "ellipsis"},
		{Type: "page", Number: 7, Href: "/blog/page/7/", Active: false, IsLast: true},
	}
	assertBarEqual(t, bar, want)
}

func TestComputeBar_NearEndOnlyLeftEllipsis(t *testing.T) {
	// Documented example: P=6, T=7 -> 1 … 6 7(last)
	bar := computeBar(6, 7, "/blog")
	want := []BarItem{
		{Type: "page", Number: 1, Href: "/blog/", Active: false, IsLast: false},
		{Type: "ellipsis"},
		{Type: "page", Number: 6, Href: "/blog/page/6/", Active: true, IsLast: false},
		{Type: "page", Number: 7, Href: "/blog/page/7/", Active: false, IsLast: true},
	}
	assertBarEqual(t, bar, want)
}

func TestComputeBar_TwoPagesNoEllipsisEitherSide(t *testing.T) {
	// T=2 is too small for either ellipsis threshold to trigger.
	first := computeBar(1, 2, "/blog")
	want := []BarItem{
		{Type: "page", Number: 1, Href: "/blog/", Active: true, IsLast: false},
		{Type: "page", Number: 2, Href: "/blog/page/2/", Active: false, IsLast: true},
	}
	assertBarEqual(t, first, want)

	second := computeBar(2, 2, "/blog")
	want2 := []BarItem{
		{Type: "page", Number: 1, Href: "/blog/", Active: false, IsLast: false},
		{Type: "page", Number: 2, Href: "/blog/page/2/", Active: true, IsLast: true},
	}
	assertBarEqual(t, second, want2)
}

func TestToSiteDataMap_FirstPageHasNoPrevLink(t *testing.T) {
	pages := Paginate(makeItems(10), 4, "/projects")
	m := ToSiteDataMap(pages[0], 4)

	if m["current_page"] != 1 {
		t.Fatalf("expected current_page=1, got %v", m["current_page"])
	}
	if m["total_pages"] != 3 {
		t.Fatalf("expected total_pages=3, got %v", m["total_pages"])
	}
	if m["items_per_page"] != 4 {
		t.Fatalf("expected items_per_page=4, got %v", m["items_per_page"])
	}
	if m["prev_href"] != "" {
		t.Fatalf("expected empty prev_href on first page, got %v", m["prev_href"])
	}
	if m["next_href"] != "/projects/page/2/" {
		t.Fatalf("expected next_href to point at page 2, got %v", m["next_href"])
	}
}

func TestToSiteDataMap_MiddlePageHasBothLinks(t *testing.T) {
	pages := Paginate(makeItems(10), 4, "/projects")
	m := ToSiteDataMap(pages[1], 4)

	if m["prev_href"] != "/projects/" {
		t.Fatalf("expected prev_href to point at page 1 (no /page/ segment), got %v", m["prev_href"])
	}
	if m["next_href"] != "/projects/page/3/" {
		t.Fatalf("expected next_href to point at page 3, got %v", m["next_href"])
	}
}

func TestToSiteDataMap_LastPageHasNoNextLink(t *testing.T) {
	pages := Paginate(makeItems(10), 4, "/projects")
	last := pages[len(pages)-1]
	m := ToSiteDataMap(last, 4)

	if m["next_href"] != "" {
		t.Fatalf("expected empty next_href on the last page, got %v", m["next_href"])
	}
	if m["prev_href"] != "/projects/page/2/" {
		t.Fatalf("expected prev_href to point at page 2, got %v", m["prev_href"])
	}
}

func TestToSiteDataMap_BarShapeMatchesComputeBar(t *testing.T) {
	pages := Paginate(makeItems(10), 4, "/projects")
	m := ToSiteDataMap(pages[1], 4)

	bar, ok := m["bar"].([]any)
	if !ok {
		t.Fatalf("expected bar to be []any, got %T", m["bar"])
	}
	if len(bar) != len(pages[1].Bar) {
		t.Fatalf("expected bar length %d, got %d", len(pages[1].Bar), len(bar))
	}
	firstEntry, ok := bar[0].(map[string]any)
	if !ok {
		t.Fatalf("expected bar entries to be map[string]any, got %T", bar[0])
	}
	for _, key := range []string{"type", "number", "href", "active"} {
		if _, present := firstEntry[key]; !present {
			t.Fatalf("expected bar entry to carry key %q, got %+v", key, firstEntry)
		}
	}
}

func makeItems(n int) []any {
	items := make([]any, n)
	for i := range items {
		items[i] = i
	}
	return items
}

func assertBarEqual(t *testing.T, got, want []BarItem) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("bar length mismatch: got %d want %d\ngot:  %+v\nwant: %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bar[%d] mismatch: got %+v want %+v", i, got[i], want[i])
		}
	}
}
