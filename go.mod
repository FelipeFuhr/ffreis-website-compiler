module ffreis-website-compiler

go 1.25.8

toolchain go1.26.11

require gopkg.in/yaml.v3 v3.0.1

require (
	github.com/yuin/goldmark v1.8.2
	github.com/yuin/goldmark-meta v1.1.0
	golang.org/x/image v0.45.0
)

require gopkg.in/yaml.v2 v2.3.0 // indirect

// The fleet's shared SEO/convention engine. Pinned by a LOCAL replace rather
// than a version, because the module has NO release tag yet AND is still
// private under ffreis-org while this module is public. Both must change
// before this branch can merge -- docs/SEO-CORE-ADOPTION.md records the
// blockers. Do not paper over the visibility half with GOPRIVATE or a
// GIT_AUTH_TOKEN in CI: a public module that needs a token to build is broken
// for every other consumer.
require github.com/ffreis-org/ffreis-seo-core v0.0.0

replace github.com/ffreis-org/ffreis-seo-core => /media/ffreis/second/projects/ffreis-seo-core
