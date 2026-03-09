package sitegen

import (
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
	f.Add("/tmp/non-existent")
	f.Add("")

	f.Fuzz(func(t *testing.T, root string) {
		if len(root) > 1024 {
			t.Skip()
		}
		_, _ = LoadPageTemplatesFromRoot(root)
	})
}
