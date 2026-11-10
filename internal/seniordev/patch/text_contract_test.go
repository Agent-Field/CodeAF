//go:build !windows

package patch

import "testing"

// Unix parity 2 contract items 1/2: gated bytes, shell or status expectations.
// Text contract C1: Unix intercepts only the historical login shell form;
// non-login shell argv retain shell dispatch.
func TestTextContractPatchShellForms(t *testing.T) {
	patch := "*** Begin Patch\n*** Add File: hello.txt\n+hello\n*** End Patch"
	for _, flag := range []string{"-lc", "-c"} {
		got := MaybeParseApplyPatch([]string{"bash", flag, "apply_patch <<'PATCH'\n" + patch + "\nPATCH"})
		if flag == "-c" {
			if got.Type != MaybeNotApplyPatch || got.Args != nil {
				t.Fatalf("non-login dispatch = %#v", got)
			}
			continue
		}
		if got.Type != MaybeBody || got.Args == nil || got.Args.Patch != patch {
			t.Fatalf("%s = %#v", flag, got)
		}
	}
}
