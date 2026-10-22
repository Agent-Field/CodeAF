package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// TestPairHelpIsAnsweredWhereMovingChatsIsOff proves C15 and C26: a build with
// moving chats off still explains `codeaf pair` when asked, and refuses only
// the pairing itself, with the sentence that says how to turn it on.
func TestPairHelpIsAnsweredWhereMovingChatsIsOff(t *testing.T) {
	t.Setenv(cell.EnvVar, "")
	for _, ask := range []string{"--help", "-h", "-help"} {
		out, _ := captureUsage(t)
		err := newPairDoor(strings.NewReader(""), io.Discard).run(context.Background(), []string{ask})
		if code := exitCodeOf(err); code != 0 {
			t.Fatalf("codeaf pair %s exited %d: %v", ask, code, err)
		}
		if !strings.Contains(out.String(), "codeaf pair") || !strings.Contains(out.String(), "CODEAF_CELLS=1") {
			t.Fatalf("codeaf pair %s printed no usage:\n%s", ask, out.String())
		}
	}
	err := newPairDoor(strings.NewReader(""), io.Discard).run(context.Background(), nil)
	if err == nil || err.Error() != cell.OffReason {
		t.Fatalf("pairing with moving chats off: %v, want %q", err, cell.OffReason)
	}
}
