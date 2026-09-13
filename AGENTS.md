# Agent Context

**This repo:** `ffreis-website-compiler` — Go CLI that builds and validates static
websites. Provides `cmd/website-compiler` (full CLI: build, serve, validate-*) and
`cmd/build-static` (CI-optimized build-only). Used by every website in the fleet,
both locally (via `ffreis-siteops`) and in CI/CD (via `ffreis-website-deployer`).

For the complete system map — how this repo relates to siteops, the deployer,
the inventory, and each website — see the private fleet inventory repository:

> the fleet inventory (private repo — do not name it in commits or PR descriptions)

Architecture detail (compiler layout detection in CI, command reference): `AGENTS.md`
links to `docs/ARCHITECTURE.md` in the same repo.

Do not look for cross-component flow documentation in this repo's README;
it covers only the compiler's own commands and flags.

## Where the rest lives

Every `##` section this file used to carry was moved **mechanically and
verbatim** into `.claude/rules/` (auto-loaded when a touched file matches its
`paths:` glob — zero cost otherwise) or `.claude/reference/` (read on demand,
by name — zero cost at session start). Nothing below was rewritten or
summarized in the move; `.claude/reference/_manifest.json` records the exact
heading → file → byte-count mapping, and `scripts/check-instructions.sh`
(`make lint-instructions`) fails if a mapped file ever goes missing or empty.

**Path-scoped rules** (auto-load the instant you touch a matching path):

| Heading | Rule file | Loads on |
| --- | --- | --- |
| Template functions | `.claude/rules/template-functions.md` | `internal/sitegen/**` |
| Blog post processing (`-posts-dir` flag) | `.claude/rules/blog-post-processing.md` | `internal/posts/**`, `internal/rss/**`, `internal/buildcmd/posts.go` |
| `check-lang-parity` command | `.claude/rules/check-lang-parity-command.md` | `internal/paritycmd/**`, `cmd/check-lang-parity/**` |

**On-demand reference** (read by name when the task needs it):

| Heading | Reference file |
| --- | --- |
| Versioning & the stable pointer | `.claude/reference/versioning-stable-pointer.md` |
| Public repo — private-repo hygiene | `.claude/reference/public-repo-hygiene.md` |
| Content-level language routing (`available_languages`) | `.claude/reference/language-routing-available-languages.md` |
| Hreflang alternate injection | `.claude/reference/hreflang-alternate-injection.md` |
| Automatic page transforms | `.claude/reference/automatic-page-transforms.md` |
| Image alt-text validation | `.claude/reference/image-alt-text-validation.md` |
| Template extensibility blocks | `.claude/reference/template-extensibility-blocks.md` |
| Keeping this file current | `.claude/reference/keeping-this-file-current.md` |
