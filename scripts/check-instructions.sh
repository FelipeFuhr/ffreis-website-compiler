#!/usr/bin/env bash
# check-instructions.sh — guardrails for the AGENTS.md rules/reference split.
set -euo pipefail

status=0

manifest=".claude/reference/_manifest.json"
if [ -f "$manifest" ]; then
  missing=$(python3 - "$manifest" <<'PY'
import json, sys, pathlib
data = json.load(open(sys.argv[1]))
missing = []
for source, block in data.items():
    for entry in block.get("sections", []):
        target = pathlib.Path(entry["file"])
        if not target.is_file() or target.stat().st_size == 0:
            missing.append(f"{source} :: {entry.get('heading')} -> {entry['file']}")
print("\n".join(missing))
PY
)
  if [ -n "$missing" ]; then
    echo "FAIL instruction-coverage: missing or empty reference file(s):"
    while IFS= read -r line; do echo "  $line"; done <<< "$missing"
    status=1
  else
    echo "OK instruction-coverage"
  fi
else
  echo "OK instruction-coverage (no manifest -- split not present)"
fi

rules_dir=".claude/rules"
if [ -d "$rules_dir" ]; then
  inert=""
  for f in "$rules_dir"/*.md; do
    [ -e "$f" ] || continue
    if ! awk '/^---$/{n++; next} n==1' "$f" | grep -q '^paths:'; then
      inert="$inert$f"$'\n'
    fi
  done
  if [ -n "$inert" ]; then
    echo "FAIL path-scoped-rules: rule file(s) missing 'paths:' frontmatter:"
    while IFS= read -r line; do [ -n "$line" ] && echo "  $line"; done <<< "$inert"
    status=1
  else
    echo "OK path-scoped-rules"
  fi
else
  echo "OK path-scoped-rules (no .claude/rules/ dir)"
fi

exit $status
