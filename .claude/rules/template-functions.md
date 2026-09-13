---
paths:
  - "internal/sitegen/**"
---

## Template functions

The compiler registers these functions in `internal/sitegen/sitegen.go`:
- `dict(k, v, ...)` — builds a `map[string]any` from pairs
- `list(v, ...)` — builds a `[]any`
- `safeHTML(s)` — returns `template.HTML`, bypasses HTML escaping
- `toJSON(v)` — marshals any value to JSON, returns `template.JS` for `<script>` embedding
- `dig(root, keys...)` — safe nested key access with access-tracing for contract validation
- `required(v, msg)` — panics with msg if v is nil/zero
- `trimSuffix(s, suffix)` — wraps `strings.TrimSuffix`
- `has(slice, val)` — returns true if val (string) is present in slice ([]any or []string); used by listing templates to check `available_languages` membership
