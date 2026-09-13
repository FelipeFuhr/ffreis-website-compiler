---
paths:
  - "internal/paritycmd/**"
  - "cmd/check-lang-parity/**"
---

## `check-lang-parity` command

Standalone command (no templates dir required). Reads a multilingual data directory,
merges `site.d/*.yaml` per language, flattens keys to dotted paths (up to depth 3),
and reports structural drift between language versions.

```
go run ./cmd/check-lang-parity [flags]

Flags:
  -data-root <path>      Path to the data/ directory containing lang subdirs (required)
  -langs <en,pt>         Comma-separated language list; auto-detects subdirs if omitted
  -skip-files <glob,...> Comma-separated site.d filenames to skip entirely
  -skip-keys <path,...>  Comma-separated dotted key paths to exclude from comparison
  -skip-config <path>    Path to .lang-parity-skip.yaml (default: <data-root>/../.lang-parity-skip.yaml)
  -max-depth <n>         Key flattening depth (default 3)
```

**Exit codes:** `0` = clean (file-missing warnings are non-fatal); `1` = key mismatch found.

**Internal package:** `internal/paritycmd/paritycmd.go`
- `Run(args []string, logger *slog.Logger) error` — entry point
- `flattenKeys(m map[string]any, prefix string, depth int) []string`
- `loadLangData(dataRoot, lang string, maxDepth int) (LangData, error)`
- `loadSkipConfig(path string) (SkipConfig, error)`
- `compareAllLangs(langs []LangData, skipConfig SkipConfig, opts Options) ParityReport`

**Skip config format** (`.lang-parity-skip.yaml`):
```yaml
skip:
  - en/site.d/10-courses.yaml   # suppresses all checks for this file across all langs
```
A skip entry for any lang suppresses the entire file from all presence and key checks.
