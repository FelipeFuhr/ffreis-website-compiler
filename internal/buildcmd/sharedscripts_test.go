package buildcmd

import "testing"

// collectSharedScripts decides which scripts are cached rather than inlined per
// page. Getting it wrong is not a build failure — it silently inflates every
// page or loses the caching benefit — so the boundary cases are worth pinning.

func TestCollectSharedScripts_MarksOnlyScriptsUsedByMoreThanOnePage(t *testing.T) {
	pages := map[string]string{
		"index.html":   `<script src="/js/shared.js"></script><script src="/js/only-index.js"></script>`,
		"about.html":   `<script src="/js/shared.js"></script>`,
		"contact.html": `<script src="/js/only-contact.js"></script>`,
	}

	shared := collectSharedScripts(pages)

	if !shared["js/shared.js"] {
		t.Error("a script on two pages should be marked shared")
	}
	for _, single := range []string{"js/only-index.js", "js/only-contact.js"} {
		if shared[single] {
			t.Errorf("%q appears on one page only; it should not be shared", single)
		}
	}
}

// Two references on the SAME page are one page's usage, not two pages'.
func TestCollectSharedScripts_RepeatedUseOnOnePageIsNotShared(t *testing.T) {
	pages := map[string]string{
		"index.html": `<script src="/js/a.js"></script><script src="/js/a.js"></script>`,
	}

	if collectSharedScripts(pages)["js/a.js"] {
		t.Error("a script referenced twice on one page was marked shared")
	}
}

func TestCollectSharedScripts_IgnoresExternalScripts(t *testing.T) {
	pages := map[string]string{
		"index.html": `<script src="https://cdn.test/a.js"></script><script src="//cdn.test/b.js"></script>`,
		"about.html": `<script src="https://cdn.test/a.js"></script><script src="//cdn.test/b.js"></script>`,
	}

	shared := collectSharedScripts(pages)

	if len(shared) != 0 {
		t.Errorf("shared = %v, want none — external scripts are not ours to inline or cache", shared)
	}
}

// The leading slash is stripped so the key matches the on-disk asset path.
func TestCollectSharedScripts_KeysAreRootRelativeWithoutTheLeadingSlash(t *testing.T) {
	pages := map[string]string{
		"a.html": `<script src="/js/x.js"></script>`,
		"b.html": `<script src="/js/x.js"></script>`,
	}

	shared := collectSharedScripts(pages)

	if _, ok := shared["js/x.js"]; !ok {
		t.Errorf("shared = %v, want the key without a leading slash", shared)
	}
	if shared["/js/x.js"] {
		t.Error("the key retained its leading slash")
	}
}

func TestCollectSharedScripts_EmptyInputYieldsNoSharedScripts(t *testing.T) {
	for _, pages := range []map[string]string{nil, {}, {"a.html": "<p>no scripts</p>"}} {
		if shared := collectSharedScripts(pages); len(shared) != 0 {
			t.Errorf("shared = %v, want none", shared)
		}
	}
}
