package buildcmd

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"ffreis-website-compiler/internal/listings"
	"ffreis-website-compiler/internal/sitegen"
	"ffreis-website-compiler/internal/sitemap"
)

// injectListingsData replaces siteData["listings"] with the computed listing
// maps (each now carrying a real "href" to its new detail page), so any
// existing client-side rendering fed by this key gains real per-listing URLs
// with no other change required on the consuming site.
func injectListingsData(siteData map[string]any, list []listings.Listing) {
	siteData["listings"] = listings.ToSiteDataList(list)
}

// maybeWriteListingDetailPages renders one detail page per listing at
// /listings/<id>/ using the internal "listing" template, when that template
// and loaded listings are both available. Returns the detail-page URLs for
// the sitemap. This is the last typed detail writer; Phase 5 replaces it with
// the generic writeCollectionDetailPages, which already reproduces it.
func maybeWriteListingDetailPages(
	logger *slog.Logger,
	opts buildOptions,
	content *optionalContent,
	siteData map[string]any,
	assetsDir string,
	mirrorer *externalAssetMirrorer,
) ([]sitemap.URLItem, error) {
	if content.listingTemplate == nil || len(content.listings) == 0 {
		return nil, nil
	}
	return writeListingDetailPages(logger, opts, *content.listingTemplate, content.listings, siteData, assetsDir, mirrorer)
}

// writeListingDetailPages renders and writes one HTML file per listing using
// the listing template, and returns a sitemap URL item for each.
func writeListingDetailPages(
	logger *slog.Logger,
	opts buildOptions,
	listingTpl sitegen.PageTemplate,
	list []listings.Listing,
	siteData map[string]any,
	assetsDir string,
	mirrorer *externalAssetMirrorer,
) ([]sitemap.URLItem, error) {
	allToCopy := make(map[string]string)
	urls := make([]sitemap.URLItem, 0, len(list))

	for _, l := range list {
		toCopy, err := renderAndWriteListing(logger, opts, listingTpl, l, siteData, assetsDir, mirrorer)
		if err != nil {
			return nil, err
		}
		for k, v := range toCopy {
			allToCopy[k] = v
		}
		urls = append(urls, sitemap.URLItem{
			Path:       "/listings/" + l.ID + "/",
			Changefreq: "weekly",
			Priority:   "0.7",
		})
	}

	if err := writeHashedAssets(opts.outDir, assetsDir, allToCopy); err != nil {
		return nil, err
	}
	return urls, nil
}

// renderAndWriteListing renders a single listing detail page and writes it to
// /listings/<id>/index.html. Returns the asset-copy map for deferred
// fingerprinting.
func renderAndWriteListing(
	logger *slog.Logger,
	opts buildOptions,
	listingTpl sitegen.PageTemplate,
	l listings.Listing,
	siteData map[string]any,
	assetsDir string,
	mirrorer *externalAssetMirrorer,
) (map[string]string, error) {
	templateData := map[string]any{
		"PageName":       "listing",
		"SiteData":       siteData,
		"CurrentListing": listings.ToCurrentListing(l),
	}

	var rendered bytes.Buffer
	if err := listingTpl.Tmpl.ExecuteTemplate(&rendered, "layout", templateData); err != nil {
		return nil, fmt.Errorf("rendering listing %s: %w", l.ID, err)
	}

	htmlOut, toCopy, err := transformPage(rendered.String(), opts, assetsDir, mirrorer)
	if err != nil {
		return nil, fmt.Errorf("transforming listing %s: %w", l.ID, err)
	}

	target := filepath.Join(opts.outDir, "listings", l.ID, "index.html")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, fmt.Errorf("creating directory for listing %s: %w", l.ID, err)
	}
	if err := os.WriteFile(target, []byte(htmlOut), 0o644); err != nil { //nolint:gosec
		return nil, fmt.Errorf(errFmtWriting, target, err)
	}
	logger.Info("generated listing detail page", "id", l.ID, "target", target)
	return toCopy, nil
}
