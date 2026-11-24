//go:build linux

package executor

import (
	"testing"

	"golang.org/x/sys/unix"
)

// The contract, in the words of a person whose chat shares a machine with other
// people's:
//
//	C2  a command in my chat cannot connect to an abstract Unix socket that a
//	    command in another chat is listening on (abstract sockets have no owner
//	    and no file mode, so only the fence can keep them apart). Where the kernel
//	    cannot do that, the chat's commands are refused rather than run with the
//	    gap. A fence nobody required keeps the ruleset it always had.
func TestC2_RequiredFenceScopesAbstractSocketsOrRefuses(t *testing.T) {
	for _, tc := range []struct {
		name       string
		require    bool
		abi        int
		wantScoped uint64
		wantOK     bool
	}{
		{"a laptop's fence is unchanged on an old kernel", false, 3, 0, true},
		{"a laptop's fence is unchanged on a new kernel", false, 7, 0, true},
		{"a required fence scopes abstract sockets where the kernel can", true, 6, unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET, true},
		{"and on every later version", true, 7, unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET, true},
		{"a required fence is refused where the kernel cannot", true, 5, 0, false},
		{"and where there is no Landlock at all", true, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scoped, ok := fenceScope(&Fence{Require: tc.require}, tc.abi)
			if scoped != tc.wantScoped || ok != tc.wantOK {
				t.Fatalf("scope %#x ok %v, want %#x %v", scoped, ok, tc.wantScoped, tc.wantOK)
			}
		})
	}
}
