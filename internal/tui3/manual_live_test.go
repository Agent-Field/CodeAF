package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// V12/V13 (Part 5): every person-facing sentence that the terminal surface
// spells for a live view, the device watch/type switches, and the one-time
// Home question appears verbatim on the remote-control manual page.
func TestEverySentenceOfALiveViewTheTerminalSpellsIsOnTheRemoteControlPage(t *testing.T) {
	page, ok := manual.Chat().Page("remote-control")
	if !ok {
		t.Fatal("the page remote-control is not in the chat manual corpus")
	}
	sentences := []string{
		liveMessageHint("tower"),
		liveLegendHost("tower", true),
		devicesWatchKey,
		devicesTypeKey,
		devicesAccessBoth,
		devicesAccessWatch,
		devicesAccessNone,
		devicesTypeAskFor("tower"),
		devicesWatchOnFor("tower"),
		devicesWatchOffFor("tower"),
		devicesTypeOnFor("tower"),
		devicesTypeOffFor("tower"),
		permitAskHead,
		permitAskReason([]string{"tower"}),
		permitAskOptionType,
		permitAskOptionKeep,
		permitAskOptionPick,
		chatlist.LiveRoad("direct"),
		chatlist.LiveRoad("relay"),
	}
	for _, sentence := range sentences {
		if !strings.Contains(page, sentence) {
			t.Errorf("the page remote-control does not say %q - add it to internal/manual/chat/remote-control.md", sentence)
		}
	}
}
