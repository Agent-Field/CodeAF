package session

import "testing"

func TestUserFacingUpdateRequiresExplicitLeadingMarker(t *testing.T) {
	for _, tc := range []struct {
		in, out string
		marked  bool
	}{
		{"[update] **First result is ready.**", "**First result is ready.**", true},
		{" \n[update]\nFirst result.", "First result.", true},
		{"[update]", "", true},
		{"[up", "[up", false},
		{"Ordinary tool narration.", "Ordinary tool narration.", false},
		{"Use `[update]` to address a person.", "Use `[update]` to address a person.", false},
	} {
		out, marked := UserFacingUpdate(tc.in)
		if out != tc.out || marked != tc.marked {
			t.Fatalf("%q => %q,%v; want %q,%v", tc.in, out, marked, tc.out, tc.marked)
		}
	}
}
