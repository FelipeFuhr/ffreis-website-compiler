---
paths:
  - "internal/sitegen/sanity*.go"
  - "internal/validatesanitycmd/**"
---

## Site sanity invariants (`internal/sitegen/sanity.go`)

`ValidateSiteSanity` cross-checks a course variant's human-facing fields
against its own `cronograma`. Two rules, both on by default and individually
switchable via `<sanity-dir>/sanity.yaml`:

- `course_start_matches_first_session` — `start_text` must resolve to the same
  calendar day as `cronograma.sessions[0].date`.
- `course_duration_hours_matches_session_hours` — `duration_hours` must equal
  `sum(cronograma.sessions[].hours)`.

A variant with no `cronograma`, or with an empty `sessions` list, is skipped.
That is the intended escape hatch for a date-to-be-arranged variant (a
`start_text` like `A combinar` paired with no cronograma), and it is the only
legitimate skip in this file.

### Date forms — why this gate was once vacuous

A bare YAML scalar (`date: 2026-09-03`) resolves to the `!!timestamp` tag and
is decoded as a `time.Time`, which `normalizeYAMLValue` (`sitegen_funcs.go`)
then rewrites to RFC3339 (`2026-09-03T00:00:00Z`). That normalization happens
inside `parseSiteData`, i.e. **before any validation runs**. So RFC3339 — not
`"2006-01-02"` — is the shape real consumer site data arrives in; the bare-date
layout only survives when the author quoted the value.

`sessionDateLayouts` is the single list of every form that can arrive. Both
session dates and `start_text` are parsed against it. Do not reintroduce a
single hardcoded layout.

### Never return `nil` from a sanity helper on a parse failure

`firstCronogramaSessionDate` used to return `(time.Time{}, false)` on a parse
error, and `validateVariantSanity` turned that into an early `return nil`. The
combination meant **every variant that had sessions at all passed both rules**,
for the entire life of the gate, because no real session date ever parsed. A
value this package cannot read is an `error` naming the offending dotted path
(`courses.<id>.variants.<id>.cronograma.sessions[0].date`), and it must not
suppress the sibling rule — `duration_hours` is still checked when the date is
unreadable.

`ValidateSiteSanity` does still return `nil` when `courses` is not a map.
That is deliberate: a site may use `courses` for an unrelated list-shaped
concept, and the site-data contract layer owns shape enforcement.

### `start_text` formats understood

ISO/RFC3339, `dd/mm/yyyy`, and `dd/<pt-BR month>/yyyy` (`19/Maio/2026`;
accent-insensitive, 3-letter abbreviations accepted). **English month names are
not understood** — an English-language data repo writing `May/04/2026` is
rejected with "could not be parsed as a date".
