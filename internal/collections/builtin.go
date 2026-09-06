package collections

import "sort"

// Built-in default collections — decision record §7.3, option (a).
//
// Shipping projects/courses/listings definitions compiled into the binary is
// what makes the migration a pure internal refactor: a website with no
// collections.yaml behaves exactly as before, so the golden files test the
// ENGINE in isolation before any site's config becomes a variable. A site
// adopting an explicit collections.yaml overrides the built-in by name, and
// each built-in is deleted in its own small, revertible PR once every consumer
// has adopted one.
//
// These definitions reproduce internal/projects, internal/courses and
// internal/listings field-for-field, including the zero-value defaults the
// typed structs contributed. `projects` (Phase 2) and `courses` (Phase 3) are
// wired into the build; `listings` is the Phase 5 target and is defined here so
// the engine's generality is proven — and unit-tested — before that migration
// starts.

// itemsPerPageFlag is the CLI flag name the built-ins read their per-page and
// carousel limits from.
const itemsPerPageFlag = "items-per-page"

// builtinOrder is the order the built-in collections are processed in, and
// therefore the order their pages are written and their sitemap entries are
// emitted in.
//
// It is DECLARATION order, not alphabetical, and it is load-bearing: before the
// migration each collection had its own hand-written call in
// writeAllPaginatedContent, and projects' index pages were written before
// courses'. Alphabetical order would swap them and rewrite sitemap.xml for no
// reason. Keeping declaration order means migrating a collection changes zero
// bytes, and gives a site's own collections.yaml a predictable, author-controlled
// sequence rather than one that depends on what its collections are named.
var builtinOrder = []string{"projects", "courses", "listings"}

// Builtins returns a fresh copy of the built-in collection definitions, keyed
// by name. A fresh copy per call keeps a caller's mutation (e.g. resolving a
// source path) from leaking into another build in the same process.
func Builtins() map[string]Collection {
	return map[string]Collection{
		"projects": builtinProjects(),
		"courses":  builtinCourses(),
		"listings": builtinListings(),
	}
}

// builtinProjects reproduces internal/projects: a bare YAML list, six fields,
// sorted by (order asc, title asc), published to siteData["projects"] for the
// home carousel, with paginated /projects/ index pages and no detail page.
func builtinProjects() Collection {
	return Collection{
		Name:    "projects",
		Section: "projects",
		Source: Source{
			Kind:         SourceFile,
			Shape:        ShapeList,
			PathFromFlag: "projects-file",
		},
		Records: RecordSpec{
			Fields: []Field{
				{Name: "title", Type: TypeString, Required: true},
				{Name: "description", Type: TypeString},
				{Name: "stack", Type: TypeList},
				{Name: "href", Type: TypeString},
				{Name: "date", Type: TypeString},
				{Name: "order", Type: TypeInt},
			},
			// false reproduces today's behaviour. Flipping it would start
			// honouring available_languages on ffreis-website's project cards
			// and change the rendered HTML — decision record Q1.
			Passthrough: false,
			Sort:        []SortBy{{By: "order"}, {By: "title"}},
		},
		Publish: []Publish{
			{Key: "projects", Value: ValueRecords, LimitFromFlag: itemsPerPageFlag},
		},
		Index: &IndexSpec{
			Enabled:  true,
			Template: "projects",
			BasePath: "/projects",
			Paginate: PaginateSpec{PerPageFromFlag: itemsPerPageFlag},
			Inject: map[string]string{
				"items":      "pages.projects.items",
				"pagination": "pages.projects.pagination",
			},
			// Empty attrs == Path only, matching writePaginatedPages today.
			Sitemap: SitemapAttrs{},
		},
		Detail: &DetailSpec{Enabled: false},
	}
}

// builtinCourses reproduces internal/courses, including Slugify-derived slugs,
// the /courses/<slug>/ href, and the two formatMoney display fields.
//
// Wired into the build by Phase 3; internal/courses is gone, and the frozen
// copy in frozen_courses_oracle_test.go is what keeps proving this definition
// still matches it record for record.
func builtinCourses() Collection {
	return Collection{
		Name:    "courses",
		Section: "courses",
		Source: Source{
			Kind:         SourceFile,
			Shape:        ShapeList,
			PathFromFlag: "courses-file",
		},
		Records: RecordSpec{
			Fields: []Field{
				{Name: "title", Type: TypeString, Required: true},
				{Name: "slug", Type: TypeString, DefaultFrom: &DefaultFrom{Slugify: "title"}},
				{Name: "description", Type: TypeString},
				{Name: "platform", Type: TypeString},
				{Name: "level", Type: TypeString},
				{Name: "availability_type", Type: TypeString},
				{Name: "udemy_url", Type: TypeString},
				{Name: "supported_languages", Type: TypeList},
				{Name: "localized_cta_labels", Type: TypeMap},
				{Name: "localized_price_notes", Type: TypeString},
				{Name: "order", Type: TypeInt},
				{Name: "sale_mode", Type: TypeString},
				{Name: "price_usd_cents", Type: TypeInt},
				{Name: "price_brl_cents", Type: TypeInt},
			},
			Derive: []Derive{
				{Name: "href", Template: "/courses/{{.slug}}/"},
				{Name: "price_usd_display", Money: &MoneySpec{Field: "price_usd_cents", Symbol: "$"}},
				{Name: "price_brl_display", Money: &MoneySpec{Field: "price_brl_cents", Symbol: "R$"}},
			},
			Sort: []SortBy{{By: "order"}, {By: "title"}},
		},
		Publish: []Publish{
			// A no-op when pages.index is absent, exactly like the
			// injectCoursesHomeCarousel this replaced (see SetDottedSiteData).
			{Key: "pages.index.courses_carousel_items", Value: ValueRecords, LimitFromFlag: itemsPerPageFlag},
		},
		Index: &IndexSpec{
			Enabled:  true,
			Template: "courses",
			BasePath: "/courses",
			Paginate: PaginateSpec{PerPageFromFlag: itemsPerPageFlag},
			Inject: map[string]string{
				"items":      "pages.courses.items",
				"pagination": "pages.courses.pagination",
			},
			Sitemap: SitemapAttrs{},
		},
		Detail: &DetailSpec{
			Enabled:  true,
			Template: "course",
			Path:     "/courses/{{.slug}}/",
			DataKey:  "CurrentCourse",
			// Types matter: ToCurrentCourse emits []any{} (not nil) for an
			// absent what_you_learn/curriculum, and "" for long_description.
			ExtraFields: []Field{
				{Name: "what_you_learn", Type: TypeList},
				{Name: "curriculum", Type: TypeList},
				{Name: "long_description", Type: TypeString},
			},
			Sitemap: SitemapAttrs{Changefreq: "monthly", Priority: "0.6"},
		},
	}
}

// builtinListings reproduces internal/listings: a `listings:`-wrapped list,
// file order preserved (the order encodes upstream ranking — decision record
// Q7), published raw to siteData["listings"] which is what casaboa's
// base.gohtml serialises into window.CASABOA_LISTINGS.
//
// Not wired into the build yet — that is Phase 5, deliberately last because
// the JS blob is the one place in this refactor where a defect ships silently.
func builtinListings() Collection {
	return Collection{
		Name:    "listings",
		Section: "listings",
		Source: Source{
			Kind:         SourceFile,
			Shape:        ShapeWrappedList,
			Key:          "listings",
			PathFromFlag: "listings-file",
			Fallback:     &Source{Kind: SourceSiteData, Shape: ShapeList, Key: "listings"},
		},
		Records: RecordSpec{
			Fields: []Field{
				{Name: "id", Type: TypeString, Required: true},
				{Name: "title", Type: TypeString, Required: true},
				{Name: "transaction", Type: TypeString},
				{Name: "price", Type: TypeString},
				{Name: "total", Type: TypeString},
				{Name: "meta", Type: TypeString},
				{Name: "place", Type: TypeString},
				{Name: "source", Type: TypeString},
				{Name: "sourceNote", Type: TypeString},
				{Name: "tone", Type: TypeString},
				{Name: "photoLabel", Type: TypeString},
				{Name: "score", Type: TypeInt},
				{Name: "priceLabel", Type: TypeString},
				{Name: "locationLabel", Type: TypeString},
				{Name: "imageLabel", Type: TypeString},
				{Name: "concern", Type: TypeString},
				// Typed as bool with a false default on purpose: under
				// passthrough an absent `sponsored:` would be ABSENT from the
				// JSON rather than false, changing window.CASABOA_LISTINGS for
				// any record that omits it (decision record §5.2).
				{Name: "sponsored", Type: TypeBool},
				{Name: "reasons", Type: TypeList},
				{Name: "breakdown", Type: TypeMap},
			},
			// Explicitly empty: preserve file order.
			Sort: []SortBy{},
			Derive: []Derive{
				{Name: "href", Template: "/listings/{{.id}}/"},
			},
		},
		Publish: []Publish{
			{Key: "listings", Value: ValueRecords},
		},
		Index: &IndexSpec{Enabled: false},
		Detail: &DetailSpec{
			Enabled:  true,
			Template: "listing",
			Path:     "/listings/{{.id}}/",
			DataKey:  "CurrentListing",
			Sitemap:  SitemapAttrs{Changefreq: "weekly", Priority: "0.7"},
		},
	}
}

// Resolve merges a website's collections.yaml over the built-in defaults: a
// collection the site defines replaces the built-in of the same name entirely,
// and a collection only the site defines is added. Passing the zero Config
// yields the built-ins unchanged, which is what a site with no collections.yaml
// gets.
func Resolve(cfg Config) map[string]Collection {
	merged := Builtins()
	for name, c := range cfg.Collections {
		c.Name = name
		merged[name] = c
	}
	return merged
}

// Order returns every resolved collection's name in processing order: the
// built-ins in builtinOrder first (whether or not a site overrode one), then
// any collection only the site's collections.yaml defines, alphabetically.
//
// Callers must iterate collections through this rather than ranging a map or
// sorting the names — see builtinOrder for why the order is part of the
// output contract.
func Order(defs map[string]Collection) []string {
	out := make([]string, 0, len(defs))
	seen := make(map[string]bool, len(defs))

	for _, name := range builtinOrder {
		if _, ok := defs[name]; ok {
			out = append(out, name)
			seen[name] = true
		}
	}

	extra := make([]string, 0, len(defs))
	for name := range defs {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)

	return append(out, extra...)
}
