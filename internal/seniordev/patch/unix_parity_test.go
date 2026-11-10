//go:build !windows

package patch

import (
	"testing"
)

// Unix parity contract 2: non-login Unix shell commands keep shell dispatch.
func TestUnixParityPatchShellDispatch(t *testing.T) {
	patch := "*** Begin Patch\n*** Add File: hello.txt\n+hello\n*** End Patch"
	got := MaybeParseApplyPatch([]string{"bash", "-c", "apply_patch <<'PATCH'\n" + patch + "\nPATCH"})
	want := MaybeNotApplyPatch
	if got.Type != want {
		t.Fatalf("shell dispatch = %q; want %q", got.Type, want)
	}
}
