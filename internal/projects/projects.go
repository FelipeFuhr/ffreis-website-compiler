package projects

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Link is one labelled destination on a project card. A showcase entry
// routinely has more than one place to send a reader — the thing itself, the
// write-up about it, the source — so the card carries a list rather than a
// second and third single-purpose field.
type Link struct {
	Label string `yaml:"label"`
	Href  string `yaml:"href"`
	// External marks a link that leaves the site. Rendered with
	// rel="noopener noreferrer" and an external-link affordance.
	External bool `yaml:"external"`
}

// Project holds data for a single portfolio project.
type Project struct {
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Stack       []string `yaml:"stack"`
	Href        string   `yaml:"href"`
	Links       []Link   `yaml:"links"`
	Date        string   `yaml:"date"`
	Order       int      `yaml:"order"`
	// Draft keeps an in-progress entry out of every build that does not pass
	// -include-drafts. It lets an unfinished showcase live on the content
	// branch without depending on branch discipline to stay unpublished.
	Draft bool `yaml:"draft"`
}

// LoadProjectsFile reads a YAML list of projects from path and returns them
// sorted ascending by Order, then by Title for ties.
func LoadProjectsFile(path string) ([]Project, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading projects file %s: %w", path, err)
	}

	var list []Project
	if err := yaml.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parsing projects file %s: %w", path, err)
	}

	for i, p := range list {
		if p.Title == "" {
			return nil, fmt.Errorf("projects[%d]: missing required field 'title'", i)
		}
		for j, l := range p.Links {
			if l.Href == "" {
				return nil, fmt.Errorf("projects[%d].links[%d]: missing required field 'href'", i, j)
			}
			if l.Label == "" {
				return nil, fmt.Errorf("projects[%d].links[%d]: missing required field 'label'", i, j)
			}
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

// ToSiteDataList converts a slice of Project to the []any map format expected
// by Go templates via the dig/range template functions.
//
// basePath is the deployment's language prefix ("/en", "/pt", or "" for a
// single-language site). Root-absolute hrefs in links are resolved against it,
// because content authors write "/blog/x/" and mean "this site's blog", not the
// unprefixed root — which on a prefixed deployment is a 404 that the internal
// link checker cannot even see (it skips paths outside the deployment root as
// belonging to a sibling deployment).
func ToSiteDataList(list []Project, basePath string) []any {
	out := make([]any, len(list))
	for i, p := range list {
		stack := make([]any, len(p.Stack))
		for j, s := range p.Stack {
			stack[j] = s
		}
		links := make([]any, len(p.Links))
		for j, l := range p.Links {
			links[j] = map[string]any{
				"label":    l.Label,
				"href":     resolveHref(l.Href, l.External, basePath),
				"external": l.External,
			}
		}
		out[i] = map[string]any{
			"title":       p.Title,
			"description": p.Description,
			"stack":       stack,
			"href":        p.Href,
			"links":       links,
			"date":        p.Date,
			"order":       p.Order,
		}
	}
	return out
}

// WithoutDrafts returns the entries that are not marked draft. Applied by the
// build unless -include-drafts is set.
func WithoutDrafts(list []Project) []Project {
	out := make([]Project, 0, len(list))
	for _, p := range list {
		if !p.Draft {
			out = append(out, p)
		}
	}
	return out
}

// resolveHref prefixes a site-internal, root-absolute href with the deployment's
// base path. External links and relative hrefs are returned untouched, and an
// href that already starts with basePath is left alone so a hand-prefixed value
// does not become "/en/en/...".
func resolveHref(href string, external bool, basePath string) string {
	if external || basePath == "" || !strings.HasPrefix(href, "/") {
		return href
	}
	if href == basePath || strings.HasPrefix(href, basePath+"/") {
		return href
	}
	return basePath + href
}
