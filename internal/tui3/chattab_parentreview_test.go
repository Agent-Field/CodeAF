package tui3

import (
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"strings"
	"testing"
)

// Cached hit targets must change when equal-looking labels name different chats.
func TestChatHeaderCacheTracksDestinationIdentity(t *testing.T) {
	old := []chatTab{{key: "old", file: "old.jsonl", where: "/old", word: "Same name"}, {key: "front", word: "Current", here: true}}
	next := append([]chatTab(nil), old...)
	next[0].key, next[0].file, next[0].where = "new", "new.jsonl", "/new"
	memo := tabBar{width: 80, hot: -1, line: "cached", tabs: old}
	if memo.same(80, 0, -1, false, next) {
		t.Fatal("equal-looking tabs reuse a cached click destination from another conversation")
	}
}

// The first asynchronous history count can make the picker available without
// changing a single label. The cached strip must gain its working overflow door.
func TestChatHeaderCacheTracksPickerAvailability(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file, a.title, a.workspace = "/tmp/test-chat.jsonl", "Current chat", "/tmp"
	a.width, a.height = 80, 32
	a.hopKnown = 1
	a.tabsRow(80)
	a.hopKnown = 2
	a.tabsRow(80)
	for _, hit := range a.chatTabHits {
		if hit.kind == tabMore {
			return
		}
	}
	t.Fatal("newly available history did not add the picker to the cached header")
}

// Selection remains explicit when the user's environment disables every SGR.
func TestChatHeaderSelectionRemainsClearWithoutColor(t *testing.T) {
	a, _, _ := tabApp(t)
	a.pal.profile = tokens.NoColor
	line := plain(a.tabsRow(160))
	if !strings.Contains(line, "[Shipping the parser]") {
		t.Fatalf("no plain-text selection marker: %q", line)
	}
}

func TestLocalChatTabRoundTripKeepsTheCaret(t *testing.T) {
	a, _, _ := tabApp(t)
	current := a.file
	a.input.setText("a partly edited sentence")
	a.input.cursor = 4
	for _, tab := range a.tabList() {
		if tab.here {
			continue
		}
		cmd, ok := a.bringForward(tab.file)
		if !ok {
			t.Fatal("held tab did not open")
		}
		drain(t, a, cmd)
		cmd, ok = a.bringForward(current)
		if !ok {
			t.Fatal("original tab did not reopen")
		}
		drain(t, a, cmd)
		if a.input.String() != "a partly edited sentence" || a.input.cursor != 4 {
			t.Fatalf("local tab lost the caret: %q at %d", a.input.String(), a.input.cursor)
		}
		return
	}
	t.Fatal("fixture has no other chat")
}
