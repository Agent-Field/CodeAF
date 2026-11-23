package main

import (
	"io"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pair"
)

func TestQAPairApprovalAcceptsExplicitAffirmatives(t *testing.T) {
	for _, answer := range []string{"a", "A", "y", "Y", "yes", " YES "} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			person := newTerminal(strings.NewReader(answer+"\n"), io.Discard)
			if !person.approveLink(t.Context(), "Approve this computer?") {
				t.Fatal("an explicit affirmative declined pairing")
			}
		})
	}
	for _, answer := range []string{"", "d", "n", "no", "maybe", "\x1b"} {
		person := newTerminal(strings.NewReader(answer+"\n"), io.Discard)
		if person.approveLink(t.Context(), "Approve this computer?") {
			t.Fatalf("%q unexpectedly approved pairing", answer)
		}
	}
}

func TestQAPairApproveCodeShowsACode(t *testing.T) {
	rig := newPairRig(t)
	shown := rig.show(t, "approve", "--code")
	code := shown.code(t)
	if _, err := pair.ReadJoinCode(code); err != nil {
		t.Fatalf("no usable pairing code: %q", code)
	}
	shown.stop()
	if err := shown.end(t); err != nil {
		t.Fatal(err)
	}
}
