package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// L4-M2a, L4-M8b: help gives the real pairing code shape and both service
// overrides. The front page's line cap leaves codeaf relay to its own help.
func TestL4PairAndRelayHelp(t *testing.T) {
	if strings.Contains(usageText, "six-digit code for your chats") {
		t.Fatal("top-level help gives the wrong code shape")
	}
	for _, name := range []string{"CODEAF_SYNC_URL", "CODEAF_RELAY"} {
		if !strings.Contains(environmentText, name) {
			t.Fatal(name)
		}
	}
	var out bytes.Buffer
	d := newPairDoor(strings.NewReader(""), &out)
	old := usageOut
	usageOut = &out
	defer func() { usageOut = old }()
	_ = d.run(context.Background(), []string{"--help"})
	if !strings.Contains(out.String(), "04-063-213") || strings.Contains(out.String(), "six-digit") {
		t.Fatal(out.String())
	}
}
