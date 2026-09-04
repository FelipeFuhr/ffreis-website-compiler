package collections

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// LoadOptions carries what a source needs to resolve its records.
type LoadOptions struct {
	// Path is the resolved file path for a `file` source (from
	// -collection-source <name>=<path> or a legacy per-type flag). Empty means
	// no file was supplied, which triggers Source.Fallback when one is declared.
	Path string
	// SiteData is the merged site-data layer, for `site_data` sources.
	SiteData map[string]any
}

// LoadRecords resolves a collection's raw records. Returns nil (no error) when
// the source yields nothing at all, which the callers treat as "this
// collection contributes nothing to this build".
func LoadRecords(src Source, opts LoadOptions) ([]map[string]any, error) {
	recs, ok, err := loadOne(src, opts)
	if err != nil {
		return nil, err
	}
	if ok {
		return recs, nil
	}
	if src.Fallback != nil {
		// casaboa passes -listings-file only when the file exists, but copies
		// the same YAML into its site-data layer regardless. Without this
		// fallback a build with the layer and no flag would publish an empty
		// list — a silent regression in exactly the mode nobody tests
		// (decision record §7.2).
		return LoadRecords(*src.Fallback, opts)
	}
	return nil, nil
}

// loadOne resolves a single source, reporting whether it produced anything.
func loadOne(src Source, opts LoadOptions) ([]map[string]any, bool, error) {
	switch src.Kind {
	case SourceFile:
		path := opts.Path
		if path == "" {
			path = src.Path
		}
		if path == "" {
			return nil, false, nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, false, fmt.Errorf("reading %s file %s: %w", src.Shape, path, err)
		}
		recs, err := decodeShape(src, raw, path)
		if err != nil {
			return nil, false, err
		}
		return recs, true, nil

	case SourceSiteData:
		v, present := opts.SiteData[src.Key]
		if !present || v == nil {
			return nil, false, nil
		}
		recs, err := recordsFromValue(src, v)
		if err != nil {
			return nil, false, fmt.Errorf("site data %q: %w", src.Key, err)
		}
		return recs, true, nil

	default:
		return nil, false, fmt.Errorf("unknown source kind %q", src.Kind)
	}
}

// decodeShape parses a YAML file according to the source's shape.
func decodeShape(src Source, raw []byte, path string) ([]map[string]any, error) {
	switch src.Shape {
	case ShapeList:
		var list []any
		if err := yaml.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		return toRecordMaps(list)

	case ShapeWrappedList:
		var wrapper map[string]any
		if err := yaml.Unmarshal(raw, &wrapper); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		inner, ok := wrapper[src.Key].([]any)
		if !ok {
			return nil, fmt.Errorf("parsing %s: expected a top-level %q list", path, src.Key)
		}
		return toRecordMaps(inner)

	case ShapeKeyedMap:
		var keyed map[string]any
		if err := yaml.Unmarshal(raw, &keyed); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		return fromKeyedMap(src, keyed)

	default:
		return nil, fmt.Errorf("unknown source shape %q", src.Shape)
	}
}

// recordsFromValue converts an already-merged site-data value into records.
func recordsFromValue(src Source, v any) ([]map[string]any, error) {
	switch src.Shape {
	case ShapeKeyedMap:
		keyed, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected a map for shape %s, got %T", ShapeKeyedMap, v)
		}
		return fromKeyedMap(src, keyed)
	default:
		list, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("expected a list, got %T", v)
		}
		return toRecordMaps(list)
	}
}

// fromKeyedMap flattens a slug-keyed map into records, copying each map key
// into the record under src.IDField. Keys are visited in sorted order so the
// result is deterministic; a collection that cares about order declares a sort.
func fromKeyedMap(src Source, keyed map[string]any) ([]map[string]any, error) {
	keys := make([]string, 0, len(keyed))
	for k := range keyed {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		m, ok := keyed[k].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("entry %q is %T, want a map", k, keyed[k])
		}
		rec := make(map[string]any, len(m)+1)
		for rk, rv := range m {
			rec[rk] = rv
		}
		if src.IDField != "" {
			rec[src.IDField] = k
		}
		out = append(out, rec)
	}
	return out, nil
}

func toRecordMaps(list []any) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("entry %d is %T, want a map", i, item)
		}
		out = append(out, m)
	}
	return out, nil
}
