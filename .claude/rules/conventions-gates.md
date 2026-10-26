---
paths:
  - "internal/conventions/**"
  - "internal/validateconventionscmd/**"
---

## `validate-conventions` — the fleet's shared style/structure rules

```
website-compiler validate-conventions -website-root . -dist dist
website-compiler validate-conventions -list              # what each rule means
website-compiler validate-conventions -write-baseline    # generate exceptions
```

Rules are declared per site in `conventions.yaml` at the website root. A
missing file means the site has adopted nothing and every rule is off; the
command says so out loud rather than exiting 0 silently, because "the gate ran
and found nothing" and "the gate had nothing to run" look identical in a CI log.

This package exists because the same rules had been accumulating as
hand-written per-site test files — `no-px-literals.test.js` and
`no-css-grid.test.js` in two of fourteen sites, each with its own copy of a
regex and its own set of bugs. **Adding a rule here, not in a site, is the
point of the package.** If a site needs a check, the check belongs in the
registry with a `conventions.yaml` opt-in.

### Four things that must not be "simplified" away

Each is enforced in code and covered by a test. They are the difference between
an exceptions mechanism and a suppression file:

1. **An exception without a `reason` is rejected** — not warned about. The
   reason is the entire value of a visible exception over a quietly weakened
   regex. The placeholder `-write-baseline` writes is rejected by name too, so
   a generated baseline cannot be merged unread.
2. **An enabled rule that scanned zero files is an error.** A gate that matched
   nothing reports "pass", which is the single most common way a check in this
   fleet has turned out to be measuring nothing. For an output rule the usual
   cause is a forgotten `-dist`.
3. **An exception matching no file is reported.** Stale entries are how a
   baseline becomes permanent; surfacing them makes burn-down possible.
4. **An unknown rule name or severity is a config error**, never a silently
   ignored key. A typo'd rule name that quietly disables the rule is the worst
   possible failure for a gate — and it is why no capability entry is needed
   per rule: adopting one against an older compiler fails loudly.

### Domains — source vs. built output

Every rule declares a `Domain`. Source rules read `src/assets/css` and
`src/assets/js`; output rules read the tree passed with `-dist`. The split is
not cosmetic:

- A structural rule run over `src/` would fail every correctly built site: a
  template partial has no `<main>` and no `<title>` by design.
- A style rule run over `dist/` would report every px literal twice, the second
  time at a generated path nobody can edit.

### Why there is a tokenizer in here

`htmlscan.go` is a small tag tokenizer, not a regex per rule and not
`golang.org/x/net/html`.

- Three rules are nesting-dependent. A `<label>` that **wraps** its control
  names it just as well as a `for=`/`id` pair, and that is how 83 of
  ffreis-forma's 94 labels work. No regex sees nesting, and a rule that
  reported every correctly-labelled control as a violation gets deleted, not
  fixed.
- `x/net/html` would be correct too. It is deliberately avoided: it would add a
  build dependency to every consumer of this compiler, and this fleet has
  repeatedly had a new dependency break workflows that were never audited for
  it.

It handles quoting and raw text (a `<` inside `<script>`/`<style>`) and nothing
else a browser does. `cssscan.go` is the same kind of thing for CSS: it finds
`selector { declarations }` pairs with whitespace normalised so minified and
expanded source compare equal. It does **not** resolve the cascade — whether a
declaration actually applies to a rendered element is a computed-style
question, i.e. the rendered tier, and must not be claimed here.

### Judgement calls recorded in the rules themselves

- `form-error-wiring` does **not** require a static `aria-invalid`. It is a
  runtime state; a page shipping `aria-invalid="false"` on every field has
  added markup, not accessibility.
- `form-autocomplete` accepts **any** non-empty token. Judging whether the
  token is the right one would mean guessing, and a gate that cries wolf is
  deleted.
- All three form rules skip a control that is `aria-hidden="true"` **and**
  `tabindex="-1"` — the spam-honeypot pattern, found on petlook-website.
  `aria-hidden` *without* `tabindex="-1"` is reported as its own defect: ARIA
  forbids it, because the control stays in a tab order a screen reader cannot
  describe.
- `resolvable-asset-refs` checks only subresources — `href`/`src`/`poster`/
  `data`/`srcset` on the elements that fetch something, and `url()`. It does
  **not** check `<a href>`: a page URL is legitimately a directory, and a rule
  that reported every clean URL as a broken asset would be switched off within
  the hour. It also tolerates a root-absolute reference whose leading segments
  are absent from the scanned tree, because a site with a `base_path` emits
  `/en/css/app.css` into a tree that holds `css/app.css` — the prefix is added
  on sync. The same suffix tolerance is how `no-self-referential-links`
  compares pages, and for the same reason.
- `no-css-grid` and `social-preview-image` are opt-in rather than fleet
  defaults — the first because current guidance is Grid for 2D structure, the
  second because satisfying it needs a brand asset only the site's owner can
  supply.
