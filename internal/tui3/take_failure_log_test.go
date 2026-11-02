package tui3

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// Contract F2 records the cause of every otherwise unexplained continuation failure.
func TestFailedTakeLogsTheCatchAllCause(t *testing.T) {
	var out bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&out)
	defer log.SetOutput(previous)
	a := &app{}
	err := errors.New("local admission could not read the saved tree")
	line := a.failedTake(homeTakenMsg{row: chatlist.Row{Cell: "saved-chat"}, err: err})
	if !strings.Contains(line, "could not be put in this folder") {
		t.Fatalf("failure line=%q", line)
	}
	if !strings.Contains(out.String(), err.Error()) || !strings.Contains(out.String(), "saved-chat") {
		t.Fatalf("missing cause in chat log: %q", out.String())
	}
}
