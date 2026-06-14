package buildcmd

import "testing"

func TestValidatePageBaseHref(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		basePath string
		slug     string
		pageName string
		wantErr  bool
	}{
		{
			name:     "no base tag passes",
			html:     `<html><head></head></html>`,
			basePath: "/en", slug: "contact", pageName: "contato",
		},
		{
			name:     "slug matches base href passes",
			html:     `<html><head><base href="/en/contact"></head></html>`,
			basePath: "/en", slug: "contact", pageName: "contato",
		},
		{
			name:     "page key used instead of slug fails",
			html:     `<html><head><base href="/en/contato"></head></html>`,
			basePath: "/en", slug: "contact", pageName: "contato",
			wantErr: true,
		},
		{
			name:     "no slug override passes",
			html:     `<html><head><base href="/pt/contato"></head></html>`,
			basePath: "/pt", slug: "contato", pageName: "contato",
		},
		{
			name:     "index page with trailing slash passes",
			html:     `<html><head><base href="/pt/"></head></html>`,
			basePath: "/pt", slug: "index", pageName: "index",
		},
		{
			name:     "index page wrong href fails",
			html:     `<html><head><base href="/pt/index"></head></html>`,
			basePath: "/pt", slug: "index", pageName: "index",
			wantErr: true,
		},
		{
			name:     "root deployment no base path passes",
			html:     `<html><head><base href="/contact"></head></html>`,
			basePath: "", slug: "contact", pageName: "contact",
		},
		{
			name:     "base href with trailing slash on non-index passes",
			html:     `<html><head><base href="/en/contact/"></head></html>`,
			basePath: "/en", slug: "contact", pageName: "contato",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePageBaseHref(tc.html, tc.basePath, tc.slug, tc.pageName)
			if tc.wantErr && err == nil {
				t.Errorf("expected error but got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
