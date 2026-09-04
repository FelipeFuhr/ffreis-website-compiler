package bundlecmd

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds the caller-supplied projection rules for one mobile client's content bundle: which
// content-data keys are whitelisted into the bundle (and which of those are required — the drift
// signal against the destination schema), the CDN base used to rewrite brand logo URIs, and
// per-language html_lang overrides. bundlecmd itself knows nothing about any single product's
// schema; every product-specific value lives in a config file like this one, loaded via
// -bundle-config. See testdata/petlook.bundle-config.yaml for petlook-mobile's reference config
// (petlook-mobile/contract/content-bundle.schema.json documents the resulting shape) — that file
// is also what TestEmit_MatchesGolden exercises to prove this package's output is unchanged.
type Config struct {
	// CDNBase is prefixed onto a brand's logo_s3_uri ("<cdn_base>/assets/<uri>") to produce an
	// absolute logo_url. Empty is valid for a config whose brands never set logo_s3_uri.
	CDNBase string `yaml:"cdn_base"`
	// LangAliases maps a data-root language directory name (e.g. "jp") to the BCP-47 html_lang
	// value to emit when that language's site.yaml does not set html_lang explicitly. A language
	// absent from this map falls back to its directory name unchanged.
	LangAliases map[string]string `yaml:"lang_aliases"`
	// UISections defines the named blocks projected under the bundle's "ui" key (e.g. nav,
	// generator, errors for petlook). Each section whitelists which "ui.<name>" content-data keys
	// are copied into the bundle, and which of those are required (emit fails if a required key
	// is absent from the merged content data).
	UISections []UISection `yaml:"ui_sections"`
	// LangLinkKeys projects the array-of-objects living at a ui section's "lang_links" key (only
	// applied when a section's Keys includes "lang_links"), one whitelist entry per object field.
	LangLinkKeys []string `yaml:"lang_link_keys"`
	// RecommendationKeys projects the top-level "recommendations" array. Every listed key is
	// always emitted for every item (coerced to string, empty if absent from the source item).
	RecommendationKeys []string `yaml:"recommendation_keys"`
	// BrandKeys projects each item in the top-level "brands.items" array. Include "logo_url" here
	// for schema self-documentation even though its value is always computed (CDNBase +
	// "/assets/" + the item's logo_s3_uri) rather than copied verbatim from content data.
	BrandKeys []string `yaml:"brand_keys"`
}

// UISection is one named, whitelisted projection block under the bundle's "ui" key.
type UISection struct {
	Name     string   `yaml:"name"`
	Keys     []string `yaml:"keys"`
	Required []string `yaml:"required"`
}

// LoadConfig reads and structurally validates a bundle projection config from path.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("reading bundle config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing bundle config yaml: %w", err)
	}
	if len(cfg.UISections) == 0 {
		return Config{}, fmt.Errorf("bundle config: ui_sections must declare at least one section")
	}
	for _, sec := range cfg.UISections {
		if sec.Name == "" {
			return Config{}, fmt.Errorf("bundle config: ui_sections entry missing 'name'")
		}
		if len(sec.Keys) == 0 {
			return Config{}, fmt.Errorf("bundle config: ui_sections[%s] must declare at least one key", sec.Name)
		}
	}
	return cfg, nil
}
