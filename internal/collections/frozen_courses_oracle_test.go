package collections

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// A FROZEN copy of internal/courses as it stood immediately before Phase 3
// deleted it (docs/decisions/0001-generic-content-collections.md §6).
//
// Same rationale as frozen_projects_oracle_test.go: the oracle test for a
// migrated collection has to keep running after its typed loader is gone, or
// the equivalence proof is deleted along with the production code. Only
// listings still imports a live package, because it migrates in Phase 5.
//
// Do NOT "fix" or refactor anything below. Its entire value is being a
// byte-faithful record of the behaviour the engine has to reproduce:
//
//   - the exact field set of baseMap, including the keys the YAML never sets
//     (udemy_url "", price_usd_cents 0, supported_languages []any{})
//   - the exact zero values — []any{} and map[string]any{}, never nil, because
//     nil and empty print differently through dig/toJSON
//   - Slugify's collapse-and-trim, and the "slug defaults from title" rule
//   - formatMoney's cents <= 0 -> "" and its %02d fraction
//   - the (order, title) comparator and the required-title error
//   - ToCurrentCourse's extra three keys, including Module's nested shape
//
// If the engine and this disagree, the engine is wrong.
//
// Source: internal/courses/courses.go at commit 4b881eb.

type frozenModule struct {
	Title   string   `yaml:"title"`
	Lessons []string `yaml:"lessons"`
}

type frozenCourse struct {
	Title               string            `yaml:"title"`
	Description         string            `yaml:"description"`
	Platform            string            `yaml:"platform"`
	Level               string            `yaml:"level"`
	AvailabilityType    string            `yaml:"availability_type"`
	UdemyURL            string            `yaml:"udemy_url"`
	SupportedLanguages  []string          `yaml:"supported_languages"`
	LocalizedCTALabels  map[string]string `yaml:"localized_cta_labels"`
	LocalizedPriceNotes string            `yaml:"localized_price_notes"`
	Order               int               `yaml:"order"`

	// Landing-page + checkout fields.
	Slug            string         `yaml:"slug"`
	SaleMode        string         `yaml:"sale_mode"`
	PriceUsdCents   int            `yaml:"price_usd_cents"`
	PriceBrlCents   int            `yaml:"price_brl_cents"`
	LongDescription string         `yaml:"long_description"`
	WhatYouLearn    []string       `yaml:"what_you_learn"`
	Curriculum      []frozenModule `yaml:"curriculum"`
}

var (
	frozenSlugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
	frozenSlugTrim     = regexp.MustCompile(`^-+|-+$`)
)

func frozenSlugify(title string) string {
	s := frozenSlugNonAlnum.ReplaceAllString(strings.ToLower(title), "-")
	return frozenSlugTrim.ReplaceAllString(s, "")
}

func frozenCoursePath(slug string) string {
	return "/courses/" + slug + "/"
}

func frozenFormatMoney(symbol string, cents int) string {
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

func frozenLoadCoursesFile(path string) ([]frozenCourse, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading courses file %s: %w", path, err)
	}

	var list []frozenCourse
	if err := yaml.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parsing courses file %s: %w", path, err)
	}

	for i := range list {
		if list[i].Title == "" {
			return nil, fmt.Errorf("courses[%d]: missing required field 'title'", i)
		}
		if list[i].Slug == "" {
			list[i].Slug = frozenSlugify(list[i].Title)
		}
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].Order != list[j].Order {
			return list[i].Order < list[j].Order
		}
		return list[i].Title < list[j].Title
	})

	return list, nil
}

func frozenCourseBaseMap(c frozenCourse) map[string]any {
	langs := make([]any, len(c.SupportedLanguages))
	for j, l := range c.SupportedLanguages {
		langs[j] = l
	}
	ctaLabels := make(map[string]any, len(c.LocalizedCTALabels))
	for k, v := range c.LocalizedCTALabels {
		ctaLabels[k] = v
	}
	return map[string]any{
		"title":                 c.Title,
		"slug":                  c.Slug,
		"href":                  frozenCoursePath(c.Slug),
		"description":           c.Description,
		"platform":              c.Platform,
		"level":                 c.Level,
		"availability_type":     c.AvailabilityType,
		"sale_mode":             c.SaleMode,
		"price_usd_cents":       c.PriceUsdCents,
		"price_brl_cents":       c.PriceBrlCents,
		"price_usd_display":     frozenFormatMoney("$", c.PriceUsdCents),
		"price_brl_display":     frozenFormatMoney("R$", c.PriceBrlCents),
		"udemy_url":             c.UdemyURL,
		"supported_languages":   langs,
		"localized_cta_labels":  ctaLabels,
		"localized_price_notes": c.LocalizedPriceNotes,
		"order":                 c.Order,
	}
}

func frozenCoursesToSiteDataList(list []frozenCourse) []any {
	out := make([]any, len(list))
	for i, c := range list {
		out[i] = frozenCourseBaseMap(c)
	}
	return out
}

func frozenToCurrentCourse(c frozenCourse) map[string]any {
	m := frozenCourseBaseMap(c)

	learn := make([]any, len(c.WhatYouLearn))
	for i, item := range c.WhatYouLearn {
		learn[i] = item
	}
	m["what_you_learn"] = learn

	modules := make([]any, len(c.Curriculum))
	for i, mod := range c.Curriculum {
		lessons := make([]any, len(mod.Lessons))
		for j, l := range mod.Lessons {
			lessons[j] = l
		}
		modules[i] = map[string]any{"title": mod.Title, "lessons": lessons}
	}
	m["curriculum"] = modules
	m["long_description"] = c.LongDescription
	return m
}
