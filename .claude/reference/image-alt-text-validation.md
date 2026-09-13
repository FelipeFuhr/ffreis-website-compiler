## Image alt-text validation (warning only, `imgalt.go` — `validateImageAltText`)

Runs per page in `writePages` (`buildcmd.go`) on the **pre-transform** rendered
HTML — before SVG inlining removes `<img>` tags for small local SVGs — so it
still catches icons that are about to be inlined, not just raster images.

For every `<img>` tag with no `alt` attribute at all, logs a `logger.Warn`
(page name + src). An explicitly empty `alt=""` is valid markup for a
decorative image and is **not** flagged — only a fully absent `alt` is.

This is deliberately a **warning, not a build failure** — unlike
`validateRenderedPageStructure`'s title/h1/description checks (same file),
which do fail the build. This repo is a shared build engine consumed by four
live sites; making alt-text enforcement fatal on its first pass could break
another site's CI over pre-existing content gaps with no warning period.
Revisit turning this fatal (opt-in flag, then default) once the fleet has had
a chance to see and address the warnings.
