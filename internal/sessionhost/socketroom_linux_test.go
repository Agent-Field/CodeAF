//go:build linux

package sessionhost

import "testing"

// The room arithmetic assumes the widths of the two names in a chat's path.
func TestC5_SocketRoomMatchesTheRealLayout(t *testing.T) {
	folders, err := layoutFor("/d", "alice", chatA)
	if err != nil {
		t.Fatal(err)
	}
	want := len("/d") + len("/people/") + personDirWidth + len("/") + sessionIDWidth
	if len(folders.dir) != want {
		t.Fatalf("a chat's folder is %d bytes, the room check assumes %d", len(folders.dir), want)
	}
}
