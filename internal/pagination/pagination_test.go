package pagination

import (
	"reflect"
	"testing"
)

// items builds n distinct placeholder items so a slice boundary error shows up
// as the wrong value rather than the wrong length.
func items(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

// barShape renders a page bar as a compact string so a test failure shows the
// whole bar at a glance: "1 … 4 … 9" with the active page in brackets.
func barShape(bar []BarItem) string {
	out := ""
	for i, item := range bar {
		if i > 0 {
			out += " "
		}
		switch {
		case item.Type == "ellipsis":
			out += "…"
		case item.Active:
			out += "[" + itoa(item.Number) + "]"
		default:
			out += itoa(item.Number)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func TestPageHref_FirstPageHasNoPageSegment(t *testing.T) {
	if got := PageHref("/blog", 1); got != "/blog/" {
		t.Errorf("PageHref(page 1) = %q, want %q — page 1 is the section root", got, "/blog/")
	}
}

func TestPageHref_SubsequentPagesGetAPageSegment(t *testing.T) {
	if got := PageHref("/blog", 4); got != "/blog/page/4/" {
		t.Errorf("PageHref(page 4) = %q, want %q", got, "/blog/page/4/")
	}
}

func TestPaginate_SplitsItemsAcrossPagesWithoutLosingAny(t *testing.T) {
	pages := Paginate(items(7), 3, "/blog")

	if len(pages) != 3 {
		t.Fatalf("got %d pages, want 3 (7 items at 3 per page)", len(pages))
	}
	var seen []any
	for _, p := range pages {
		seen = append(seen, p.Items...)
	}
	if !reflect.DeepEqual(seen, items(7)) {
		t.Errorf("pages contain %v, want every item exactly once in order", seen)
	}
	if got := len(pages[2].Items); got != 1 {
		t.Errorf("last page has %d items, want 1 (the remainder)", got)
	}
}

func TestPaginate_ExactMultipleLeavesNoTrailingEmptyPage(t *testing.T) {
	pages := Paginate(items(6), 3, "/blog")
	if len(pages) != 2 {
		t.Fatalf("got %d pages, want 2 — 6 items at 3 per page must not produce an empty third", len(pages))
	}
}

func TestPaginate_EmptyInputStillYieldsOneRenderablePage(t *testing.T) {
	pages := Paginate(nil, 3, "/blog")

	if len(pages) != 1 {
		t.Fatalf("got %d pages, want 1 — an empty section still renders its listing page", len(pages))
	}
	if pages[0].Items != nil {
		t.Errorf("items = %v, want nil", pages[0].Items)
	}
	if pages[0].TotalPages != 1 {
		t.Errorf("TotalPages = %d, want 1", pages[0].TotalPages)
	}
}

// A caller that forgets to set perPage must not divide by zero or emit one page
// per item.
func TestPaginate_NonPositivePerPageFallsBackToTheDefault(t *testing.T) {
	for _, perPage := range []int{0, -5} {
		pages := Paginate(items(13), perPage, "/blog")
		if len(pages) != 2 {
			t.Errorf("perPage=%d gave %d pages, want 2 (13 items at the default 12)", perPage, len(pages))
		}
	}
}

func TestPaginate_EveryPageCarriesTheSameTotalAndBaseHref(t *testing.T) {
	pages := Paginate(items(10), 3, "/projects")
	for _, p := range pages {
		if p.TotalPages != 4 {
			t.Errorf("page %d reports TotalPages=%d, want 4", p.Number, p.TotalPages)
		}
		if p.BaseHref != "/projects" {
			t.Errorf("page %d reports BaseHref=%q, want /projects", p.Number, p.BaseHref)
		}
	}
}

// The bar is what a reader clicks, so its shape at each position is the
// behaviour worth pinning — including where ellipses do and do not appear.
func TestPaginate_BarShapeAtEachPosition(t *testing.T) {
	for _, tc := range []struct {
		name       string
		totalItems int
		page       int
		want       string
	}{
		{"single page collapses to just itself", 3, 1, "[1]"},
		{"two pages need no ellipsis", 6, 1, "[1] 2"},
		{"two pages, on the last", 6, 2, "1 [2]"},
		{"three pages, on the middle one", 9, 2, "1 [2] 3"},
		{"first of many: only a right ellipsis", 30, 1, "[1] … 10"},
		{"last of many: only a left ellipsis", 30, 10, "1 … [10]"},
		{"middle of many: ellipses on both sides", 30, 5, "1 … [5] … 10"},
		{"second of many: no left ellipsis", 30, 2, "1 [2] … 10"},
		{"penultimate of many: no right ellipsis", 30, 9, "1 … [9] 10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pages := Paginate(items(tc.totalItems), 3, "/blog")
			got := barShape(pages[tc.page-1].Bar)
			if got != tc.want {
				t.Errorf("bar = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPaginate_BarMarksTheLastPageForItsLabel(t *testing.T) {
	pages := Paginate(items(30), 3, "/blog")
	bar := pages[0].Bar
	last := bar[len(bar)-1]
	if !last.IsLast || last.Number != 10 {
		t.Errorf("final bar item = %+v, want the last page marked IsLast", last)
	}
	for _, item := range bar[:len(bar)-1] {
		if item.IsLast {
			t.Errorf("non-final bar item %+v is marked IsLast", item)
		}
	}
}

func TestPaginate_BarLinksResolveToTheirPage(t *testing.T) {
	pages := Paginate(items(30), 3, "/blog")
	for _, item := range pages[4].Bar {
		if item.Type == "ellipsis" {
			if item.Href != "" {
				t.Errorf("ellipsis carries href %q, want none", item.Href)
			}
			continue
		}
		if want := PageHref("/blog", item.Number); item.Href != want {
			t.Errorf("bar item %d href = %q, want %q", item.Number, item.Href, want)
		}
	}
}

func TestToSiteDataMap_ExposesTheFieldsTemplatesRead(t *testing.T) {
	pages := Paginate(items(30), 3, "/blog")
	data := ToSiteDataMap(pages[4], 3)

	for key, want := range map[string]any{
		"current_page":   5,
		"total_pages":    10,
		"items_per_page": 3,
		"prev_href":      "/blog/page/4/",
		"next_href":      "/blog/page/6/",
	} {
		if got := data[key]; got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	if bar, ok := data["bar"].([]any); !ok || len(bar) == 0 {
		t.Errorf("bar = %v, want a non-empty []any for template ranging", data["bar"])
	}
}

// An absent prev/next must be an empty string, not a link to page 0 or to one
// past the end — the template gates the arrow on emptiness.
func TestToSiteDataMap_OmitsPrevOnFirstAndNextOnLast(t *testing.T) {
	pages := Paginate(items(30), 3, "/blog")

	first := ToSiteDataMap(pages[0], 3)
	if first["prev_href"] != "" {
		t.Errorf("first page prev_href = %v, want empty", first["prev_href"])
	}
	if first["next_href"] != "/blog/page/2/" {
		t.Errorf("first page next_href = %v, want /blog/page/2/", first["next_href"])
	}

	last := ToSiteDataMap(pages[9], 3)
	if last["next_href"] != "" {
		t.Errorf("last page next_href = %v, want empty", last["next_href"])
	}
	if last["prev_href"] != "/blog/page/9/" {
		t.Errorf("last page prev_href = %v, want /blog/page/9/", last["prev_href"])
	}
}

func TestToSiteDataMap_SinglePageHasNeitherPrevNorNext(t *testing.T) {
	pages := Paginate(items(2), 12, "/blog")
	data := ToSiteDataMap(pages[0], 12)
	if data["prev_href"] != "" || data["next_href"] != "" {
		t.Errorf("prev=%v next=%v, want both empty on a single-page listing",
			data["prev_href"], data["next_href"])
	}
}

func TestToSiteDataMap_BarEntriesAreTemplateReadableMaps(t *testing.T) {
	pages := Paginate(items(30), 3, "/blog")
	bar, ok := ToSiteDataMap(pages[4], 3)["bar"].([]any)
	if !ok {
		t.Fatal("bar is not []any")
	}
	entry, ok := bar[0].(map[string]any)
	if !ok {
		t.Fatalf("bar[0] = %T, want map[string]any", bar[0])
	}
	for _, key := range []string{"type", "number", "href", "active"} {
		if _, present := entry[key]; !present {
			t.Errorf("bar entry missing key %q", key)
		}
	}
}
