package sitegen

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PageTemplate associates a page name (without extension) to its parsed template.
type PageTemplate struct {
	Name string
	Tmpl *template.Template
}

// LoadPageTemplatesFromRoot parses the shared layout/partials plus each page template.
// templatesRoot is expected to contain: layout/, partials/, pages/.
func LoadPageTemplatesFromRoot(templatesRoot string) ([]PageTemplate, error) {
	files, err := filepath.Glob(filepath.Join(templatesRoot, "pages", "*.gohtml"))
	if err != nil {
		return nil, err
	}
	partials, err := filepath.Glob(filepath.Join(templatesRoot, "partials", "*.gohtml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(partials)

	pages := make([]PageTemplate, 0, len(files))
	for _, pageFile := range files {
		name := strings.TrimSuffix(filepath.Base(pageFile), filepath.Ext(pageFile))
		parseFiles := []string{filepath.Join(templatesRoot, "layout", "base.gohtml")}
		parseFiles = append(parseFiles, partials...)
		parseFiles = append(parseFiles, pageFile)

		tpl, err := template.New("layout").Funcs(template.FuncMap{
			"dict":     dict,
			"list":     list,
			"safeHTML": safeHTML,
		}).ParseFiles(parseFiles...)
		if err != nil {
			return nil, err
		}
		pages = append(pages, PageTemplate{Name: name, Tmpl: tpl})
	}

	sort.Slice(pages, func(i, j int) bool {
		return pages[i].Name < pages[j].Name
	})

	return pages, nil
}

// LoadPageTemplates keeps backwards compatibility with older call sites.
func LoadPageTemplates(_ string) ([]PageTemplate, error) {
	newRoot := filepath.Join("src", "templates")
	if dirExists(newRoot) {
		return LoadPageTemplatesFromRoot(newRoot)
	}
	return LoadPageTemplatesFromRoot("templates")
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func dict(values ...any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("dict expects an even number of arguments")
	}
	m := make(map[string]any, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		k, ok := values[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict keys must be strings")
		}
		m[k] = values[i+1]
	}
	return m, nil
}

func list(values ...any) []any {
	return values
}

func safeHTML(v string) template.HTML {
	return template.HTML(v)
}
