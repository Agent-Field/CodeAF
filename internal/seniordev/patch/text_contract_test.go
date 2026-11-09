//go:build !windows

package patch

import "testing"

// Text contract C1: both historical and non-login shell argv parse patches.
func TestTextContractPatchShellForms(t *testing.T) {
	patch := "*** Begin Patch\n*** Add File: hello.txt\n+hello\n*** End Patch"
	for _, flag := range []string{"-lc", "-c"} {
		got := MaybeParseApplyPatch([]string{"bash", flag, "apply_patch <<'PATCH'\n" + patch + "\nPATCH"})
		if got.Type != MaybeBody || got.Args == nil || got.Args.Patch != patch {
			t.Fatalf("%s = %#v", flag, got)
		}
	}
}
