package collections

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// A FROZEN copy of internal/projects as it stood immediately before Phase 2
// deleted it (docs/decisions/0001-generic-content-collections.md §6).
//
// The oracle tests for courses and listings import their still-live packages,
// because those migrate in Phases 3 and 5. projects has no live package any
// more — so rather than lose the equivalence proof along with the production
// code, the reference implementation is vendored here, test-only.
//
// Do NOT "fix" or refactor anything below. Its entire value is being a
// byte-faithful record of the behaviour the engine has to reproduce: the exact
// field set, the exact zero values ([]any{} for an absent stack, not nil), the
// (order, title) comparator, and the required-title error. If the engine and
// this disagree, the engine is wrong.
//
// Source: internal/projects/projects.go at commit b654003.

type frozenProject struct {
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Stack       []string `yaml:"stack"`
	Href        string   `yaml:"href"`
	Date        string   `yaml:"date"`
	Order       int      `yaml:"order"`
}

func frozenLoadProjectsFile(path string) ([]frozenProject, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading projects file %s: %w", path, err)
	}

	var list []frozenProject
	if err := yaml.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parsing projects file %s: %w", path, err)
	}

	for i, p := range list {
		if p.Title == "" {
			return nil, fmt.Errorf("projects[%d]: missing required field 'title'", i)
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

func frozenProjectsToSiteDataList(list []frozenProject) []any {
	out := make([]any, len(list))
	for i, p := range list {
		stack := make([]any, len(p.Stack))
		for j, s := range p.Stack {
			stack[j] = s
		}
		out[i] = map[string]any{
			"title":       p.Title,
			"description": p.Description,
			"stack":       stack,
			"href":        p.Href,
			"date":        p.Date,
			"order":       p.Order,
		}
	}
	return out
}
