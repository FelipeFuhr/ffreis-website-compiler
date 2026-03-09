package sitegen

import (
"path/filepath"
"strings"
"testing"
)

func FuzzDictNoPanic(f *testing.F) {
f.Add("a,b,c,d")
f.Add("key")
f.Add("")

f.Fuzz(func(t *testing.T, raw string) {
parts := strings.Split(raw, ",")
args := make([]any, 0, len(parts)*2)
for i, p := range parts {
args = append(args, p)
args = append(args, i)
}
if len(parts)%2 == 1 && len(args) > 0 {
args = args[:len(args)-1]
}
_, _ = dict(args...)
})
}

func FuzzLoadPageTemplatesFromRootNoPanic(f *testing.F) {
f.Add("templates")
f.Add("subdir/templates")
f.Add("")

f.Fuzz(func(t *testing.T, root string) {
if len(root) > 1024 {
t.Skip()
}
// Constrain all paths to a temp directory so the fuzzer cannot read
// or stat files outside the test sandbox. Clean the input first, then
// skip any absolute paths or paths that would escape via "..".
rel := filepath.Clean(filepath.FromSlash(root))
if filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
t.Skip()
}
safePath := filepath.Join(t.TempDir(), rel)
_, _ = LoadPageTemplatesFromRoot(safePath)
})
}
