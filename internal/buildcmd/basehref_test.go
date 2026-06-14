package buildcmd

import (
	"strings"
	"testing"
)

func TestValidateRenderedPageStructure(t *testing.T) {
	validPage := func(title, h1, metaDesc string) string {
		return `<!DOCTYPE html><html><head>` +
			`<title>` + title + `</title>` +
			`<meta name="description" content="` + metaDesc + `">` +
			`</head><body><h1>` + h1 + `</h1></body></html>`
	}

	tests := []struct {
		name     string
		html     string
		wantErr  bool
		errMatch string
	}{
		{
			name: "all structural elements present passes",
			html: validPage("Page Title", "Page Heading", "A description."),
		},
		{
			name:     "empty title fails",
			html:     validPage("", "Page Heading", "A description."),
			wantErr:  true,
			errMatch: "<title>",
		},
		{
			name:     "whitespace-only title fails",
			html:     validPage("   ", "Page Heading", "A description."),
			wantErr:  true,
			errMatch: "<title>",
		},
		{
			name:     "empty h1 fails",
			html:     validPage("Page Title", "", "A description."),
			wantErr:  true,
			errMatch: "<h1>",
		},
		{
			name:     "empty meta description fails",
			html:     validPage("Page Title", "Heading", ""),
			wantErr:  true,
			errMatch: "description",
		},
		{
			name: "h1 with inner HTML passes",
			html: validPage("Title", "<span>Heading text</span>", "Desc"),
		},
		{
			name: "no h1 in page passes (some pages omit it)",
			html: `<html><head><title>T</title><meta name="description" content="D"></head><body></body></html>`,
		},
		{
			name: "no title tag passes (catches only what is present)",
			html: `<html><head><meta name="description" content="D"></head><body><h1>H</h1></body></html>`,
		},
		{
			name: "meta description attribute order reversed passes",
			html: `<html><head><title>T</title><meta content="desc" name="description"></head><body><h1>H</h1></body></html>`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRenderedPageStructure("testpage", tc.html)
			if tc.wantErr && err == nil {
				t.Errorf("expected error but got nil")
				return
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if tc.wantErr && tc.errMatch != "" && !strings.Contains(err.Error(), tc.errMatch) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.errMatch)
			}
		})
	}
}

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
