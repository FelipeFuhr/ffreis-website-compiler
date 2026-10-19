# Changelog

## [1.2.1](https://github.com/FelipeFuhr/ffreis-website-compiler/compare/v1.2.0...v1.2.1) (2026-10-19)


### Bug Fixes

* **sanity:** course sanity gate passed for every input with sessions ([#154](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/154)) ([afd89af](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/afd89afa3d0b17dd8e1e8e376f1945f56b9c9e6c))

## [1.2.0](https://github.com/FelipeFuhr/ffreis-website-compiler/compare/v1.1.2...v1.2.0) (2026-10-18)


### Features

* **conventions:** four gates for defects found live on forma.ffreis.com ([#152](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/152)) ([1531f01](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/1531f01e92c09a207071bd505020773264fcbdf4))

## [1.1.2](https://github.com/FelipeFuhr/ffreis-website-compiler/compare/v1.1.1...v1.1.2) (2026-10-18)


### Bug Fixes

* **conventions:** css-reset-present read only the first stylesheet, and cried wolf ([#149](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/149)) ([649a0f1](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/649a0f1eab2022077c837bdf17abde1788b9df78))

## [1.1.1](https://github.com/FelipeFuhr/ffreis-website-compiler/compare/v1.1.0...v1.1.1) (2026-10-17)


### Bug Fixes

* **buildcmd:** don't inject a per-page stylesheet the template already links ([#147](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/147)) ([bd36f03](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/bd36f03c81cd9d87bef0bb12fe3eb51c1116ef69))

## [1.1.0](https://github.com/FelipeFuhr/ffreis-website-compiler/compare/v1.0.1...v1.1.0) (2026-10-17)


### Features

* **conventions:** enforce fleet style rules in the tool, not per-site tests ([#143](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/143)) ([54d3d25](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/54d3d2588c8cd5cb7f624b56cc0e09d14c075177))
* **conventions:** landmark, form, LCP, reset and SEO gates ([#145](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/145)) ([37c0507](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/37c05075fe5af195fb5d20984c48c9624c40bc4e))


### Bug Fixes

* **buildcmd:** make generated HTML pass the fleet's own markup gate ([#142](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/142)) ([f5ccc6a](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/f5ccc6a47931b4f2f8c7c4907ae2d2d28dc24723))

## [1.0.1](https://github.com/FelipeFuhr/ffreis-website-compiler/compare/v1.0.0...v1.0.1) (2026-10-16)


### Bug Fixes

* **buildcmd:** fingerprint deferred stylesheets and authored og:image refs ([#139](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/139)) ([cc83cc2](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/cc83cc2b708b284f0743083c3d824a6f5833caeb))

## 1.0.0 (2026-10-16)


### Features

* **accessibility:** warn on missing img alt text, default lazy loading ([#100](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/100)) ([0502211](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/05022116e842f5e3ccbdc5a5b040db0a26948bb7))
* add declarative compiled-output tests ([#92](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/92)) ([3b54e9b](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/3b54e9bd5a1a2f5b68d33da437c38ba833882cfa))
* add things back ([ac90438](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/ac904388cccd385c779bb249c144d2c4a569e590))
* add things back ([aa08c58](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/aa08c58014c4ad92267fa8611fa4cfa4792a7d58))
* add things back ([35d008c](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/35d008cbbc3fcc0ee918873754e567da18c248fd))
* add trimSuffix template function ([#7](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/7)) ([7b6d708](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/7b6d70870d43765807cdc4f97765a4de51aefe6d))
* arithmetic template funcs + validate-css command ([#126](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/126)) ([e8f9cc1](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/e8f9cc1aff66bd3a74e4a7c6337930d704e9189f))
* **assets:** copy well-known discovery files verbatim from src/assets ([#96](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/96)) ([2e90499](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/2e904994c8e85ee2967d1cdbd161ba5143820a79))
* automatic per-page CSS scoping ([#121](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/121)) ([0b8fe96](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/0b8fe96b96d0e1a47f1475b87189f057f0119141))
* **build:** add --content-source flag with prod/mock anti-leak guard ([#78](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/78)) ([4f17965](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/4f17965bd1567c2f2884d24804e50ca0694950fe))
* **buildcmd:** defer local scripts + hoist external [@import](https://github.com/import) out of inlined CSS ([#132](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/132)) ([4ef3222](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/4ef3222348e01cc6b2e3581ffd16e6094af3cafb))
* **ci:** wire release-please and document the stable-pointer versioning contract ([#117](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/117)) ([f69ef66](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/f69ef669eed65d7596a0ca7db2c27a15f2dfe25b))
* **cli:** add version --json capability manifest, and unblock releases ([#135](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/135)) ([1b34e06](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/1b34e064ee4230ff9798b7b0f293104c737e9073))
* **contract:** add compiler_consumed contract section ([#23](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/23)) ([618edae](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/618edae2470f9519c3a5f0ec958a055e48d0d12f))
* **courses:** per-course landing pages at /courses/&lt;slug&gt;/ ([#79](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/79)) ([bba7af9](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/bba7af95a9127e544cbb93a71794e4d972a4ad99))
* **deps:** migrate to ffreis-platform-standards ([#6](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/6)) ([d2bead8](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/d2bead8c873e2229fd093a17eba550c8efbbb041))
* emit-content-bundle subcommand (petlook mobile content bundle) ([370e4f1](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/370e4f184d809543e5654a4da6c5f1d6b2a346d6))
* exclude pages marked internal:true from build output ([#9](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/9)) ([c565403](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/c565403e553e0de20e260efffce0297402710b6d))
* fingerprint webmanifest icons + add json_assets_exist output check ([#102](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/102)) ([d8f9388](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/d8f9388d66e64207262401d6b2c7f1c2dd31d44f))
* first commit ([0053be0](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/0053be08ce4d65bdaa833e5d0e9d9cd0f882c779))
* fixing ci ([a2d19ad](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/a2d19ad1e71d75529e729f3876b3422e2f2aae1d))
* **hreflang:** inject hreflang alternate links for multilingual sites ([#22](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/22)) ([b878165](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/b878165e94897cf065a7d7a60776425472496813))
* **i18n:** add content-level language routing with redirect stubs ([#25](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/25)) ([cf44542](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/cf4454260caad3f9052460df59a6c64a1faab02d))
* **i18n:** add per-page slug translation and parallel-page lang switching ([#27](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/27)) ([3a40066](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/3a4006641fdaecd3680e8908fd3a3b14e19799fb))
* **i18n:** allow content-only languages via a content_languages superset ([#77](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/77)) ([5903e34](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/5903e34e59e8efaa94557796096d68efce10bdce))
* improvements to match existing website ([3ef2ae0](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/3ef2ae01f2cc637e00e01f65705dc5eab642251e))
* inject og:image/twitter:image from a site-wide og-image asset ([#124](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/124)) ([7f59549](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/7f59549956f928362e3c0b1bcafb6a4a5fd1a731))
* **inline:** add js-shared-inline-threshold and script preload injection ([5013421](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/5013421abaa689b8f0c8f12156a28001715ac436))
* **listings:** generate a real detail page per listing (-listings-file) ([#99](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/99)) ([209e9a8](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/209e9a87b43f23b106e69540a497835f1a9a86c7))
* Markdown blog post processing, RSS feed; add compiled binaries to .gitignore ([#18](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/18)) ([05d29f8](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/05d29f8b2eb08b643d24fd75bf2f2eb40ebc0c4a))
* **output-tests:** make compiled-output checks composable ([#94](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/94)) ([064f663](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/064f663296352d850485a4a8037a5a21091be251))
* per-page CSS pruning, fragment pages, and generic listings ([#123](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/123)) ([92a0f19](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/92a0f19c84135d9a6808176ff220bfdd68d9285d))
* platform leveling improvements ([#8](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/8)) ([23b7607](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/23b7607b54773255414a7953c7fc891ac705d23a))
* production asset optimization, refactor, and test suite expansion ([#20](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/20)) ([1052a5b](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/1052a5ba070164b35793219f20fca52525cbb9ae))
* projects, courses, and pagination support for listing pages ([#19](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/19)) ([53e9bc2](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/53e9bc2c25b5a9a3a2240d3cd7cad85c92e35d37))
* **sections:** -disable-sections flag (hide blog/courses/projects) ([#80](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/80)) ([1177f6f](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/1177f6f7f7f45f21210d87104f1bbd6590eae465))
* **sections:** unify page/path/prune section gating into one table ([#101](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/101)) ([4dde543](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/4dde543b9635efa7b44e2d8e0a697578f85eeffd))
* **tracker:** inject ffreis-tracker-sdk script + Tracker.init before &lt;/head&gt; ([#33](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/33)) ([3553114](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/35531148cfd5574fe99cfd1514a9979bdbffc621))


### Bug Fixes

* address review comments - symlink traversal, fuzz scope, and download integrity ([12e55ac](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/12e55acbefd188c0d00ff20a9cd67fba1ed2bd11))
* **assetusage:** recognize symlink-aliased per-page stylesheets ([#134](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/134)) ([14c7b8a](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/14c7b8adee977a149e6d7584fcc1d97e70e1616e))
* **assetusage:** recognize the css/pages/&lt;pageName&gt;.css convention ([#129](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/129)) ([4061785](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/40617855aa9af633d657bdcaaf54cdbfaa85a60e))
* **buildcmd:** make addOrReplaceAttr idempotent for malformed attr/value shapes ([#111](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/111)) ([6ba6090](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/6ba60908bc64bf44da68724e86a2d0b719247c59))
* **build:** fail the build on a blanket robots.txt Disallow for real content ([#98](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/98)) ([900127f](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/900127f7985e5ac7275ec4d092da923ec42016ee))
* ci ([a308fd7](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/a308fd72d6c6b1505f2c8797098c81aea6d39480))
* ci ([e48ba3d](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/e48ba3db8dce1cad512a3b2c0d2fabcdebe442a4))
* **ci:** bump ffreis-workflows-container pin (old SHA unreachable) ([#39](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/39)) ([6eee787](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/6eee78787697dffa2de6d892d1b7676f654524d9))
* **ci:** bump PLATFORM_STANDARDS_SHA to a SHA that has bootstrap_lefthook.sh ([#36](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/36)) ([1122e69](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/1122e6904f804a7c6fc48f69d05b936fa9e61ca9))
* **ci:** eliminate startup_failure on go-ci + pr-hygiene; fix lefthook bootstrap URL ([9af4fc5](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/9af4fc5b99484326e6769746ff93139c78ea57a4))
* **ci:** gofmt lqip.go, promote golang.org/x/image to direct dependency ([#17](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/17)) ([ac9fea5](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/ac9fea5e9ed05438bdf4b04cb108d6b36f6a6eeb))
* **ci:** grant issues:write to release-please job ([#93](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/93)) ([a08c667](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/a08c66786c68be1e853b1c2de5ea37d3250c419b))
* **ci:** remove redundant standalone scorecards.yml ([#89](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/89)) ([1886f4d](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/1886f4dd3e982403e657fa59d8be0ad9a532331f))
* **fingerprint:** honor base_path when emitting absolute asset URLs ([#30](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/30)) ([0dd4365](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/0dd436550701bc66dd177b495f66fae4fe2a3bb1))
* **gitleaks:** convert v8 [[allowlist]] slice to v7 [allowlist] map ([#40](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/40)) ([d3d2518](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/d3d2518e85a9068f8cd367cd66e9d313df050674))
* **grype:** bump workflows-general SHA to prevent self-scan CVEs ([#53](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/53)) ([01fb437](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/01fb4372000529bfdfa6b49a568d2c1dbbaef54d))
* **hooks:** implement lefthook release-tier targets ([#91](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/91)) ([b13930c](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/b13930c762d663b40ead33ed2c2914ed5286111d))
* **linkcheck:** register trailing-slash form for non-index pages ([#26](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/26)) ([834ee36](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/834ee36174bba0f85734a9cea0acefc1831498e7))
* platform standards ([#5](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/5)) ([7503c0a](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/7503c0ab11abf53a05d2b4b4532db4bb085d9da6))
* **posts:** fail build on unsupported available_languages codes ([#76](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/76)) ([9d49a27](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/9d49a27c37d23fbacbfce6f418e41834de08b151))
* **posts:** prefix blog listing hrefs with base_path ([#75](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/75)) ([106e204](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/106e2043bb66830052631160d0f4782287055f56))
* redirect stub fallback and unknown-lang validation for blog posts ([#74](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/74)) ([26207a9](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/26207a920f0ffa8c038df4ccf6823a81fea07fdc))
* resolve SonarQube issues ([#61](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/61)) ([9157d5e](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/9157d5ebcc93923bd9fcef0b036224c5ffb28800))
* **sec:** bump Go toolchain 1.25.8 → 1.25.10 (4 Go stdlib CVEs) ([#37](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/37)) ([dc978fe](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/dc978fe8bd12fe2c0b9f27214adb6e5e99d3ffcd))
* **sitegen:** normalize bare YAML date scalars to RFC3339 strings ([#97](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/97)) ([35bc570](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/35bc57091283cdbc3ea5b5d95d12e2488c0f6117))
* validate &lt;base href&gt; matches page slug at compile time ([#69](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/69)) ([378d5bf](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/378d5bf68ca36e71bb89f1f3c7c7f7d8cf36869a))
* **validate-css:** skip pages.&lt;name&gt;.internal pages ([#128](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/128)) ([9e84337](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/9e843379ee63adc7f1a96527bf6bde4b578b4587))


### Performance Improvements

* page-loading pipeline — CSS inlining, LQIP, fingerprinting, position-based deferral ([#16](https://github.com/FelipeFuhr/ffreis-website-compiler/issues/16)) ([993a5dc](https://github.com/FelipeFuhr/ffreis-website-compiler/commit/993a5dce475dcc3cd83e891be026288c37837454))
