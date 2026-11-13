package tui3

import (
	"encoding/base64"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/history"
)

func TestHistoryArrowsWalkWholeMultilineEntries(t *testing.T) {
	a, _ := recallApp(t,
		history.Entry{Text: "older prompt", Cwd: "/tmp/lab"},
		history.Entry{Text: "first line\nsecond line", Cwd: "/tmp/lab"},
	)
	a.width = 140
	a.welcome.open = false
	typeInto(t, a, "my draft")
	for _, step := range []struct{ key, want string }{
		{"up", "first line\nsecond line"},
		{"up", "older prompt"},
		{"down", "first line\nsecond line"},
		{"down", "my draft"},
	} {
		drive(t, a, key(step.key))
		if got := a.input.String(); got != step.want {
			t.Fatalf("%s shows %q, want %q", step.key, got, step.want)
		}
		if step.want != "my draft" && !strings.Contains(plain(frame(a)), "↑ older · ↓ newer") {
			t.Fatalf("history does not show its navigation keys:\n%s", plain(frame(a)))
		}
	}
}

func TestHistoryAndCopyKeepTheirActionsOnNarrowFrames(t *testing.T) {
	a, _ := recallApp(t, history.Entry{Text: "an old prompt", Cwd: "/tmp/lab"})
	a.welcome.open = false
	a.width = 40
	drive(t, a, key("up"))
	if screen := plain(frame(a)); !strings.Contains(screen, "↑↓ history") {
		t.Fatalf("narrow history hides its arrows:\n%s", screen)
	}
	a = copyApp(t)
	a.width = 40
	if screen := plain(frame(a)); !strings.Contains(screen, "enter copy") {
		t.Fatalf("narrow copy mode hides its copy action:\n%s", screen)
	}
}

func TestRoomHistoryArrowsWalkWholeMultilineEntries(t *testing.T) {
	a, _ := roomRecallApp(t,
		history.Entry{Text: "older prompt", Cwd: "/tmp/lab"},
		history.Entry{Text: "first line\nsecond line", Cwd: "/tmp/lab"},
	)
	typeInto(t, a, "room draft")
	for _, step := range []struct{ key, want string }{
		{"up", "first line\nsecond line"},
		{"up", "older prompt"},
		{"down", "first line\nsecond line"},
		{"down", "room draft"},
	} {
		drive(t, a, key(step.key))
		if got := a.input.String(); got != step.want {
			t.Fatalf("%s shows %q, want %q", step.key, got, step.want)
		}
	}
}

func TestCopyModeOffersEnterToCopy(t *testing.T) {
	a := copyApp(t)
	if screen := plain(frame(a)); !strings.Contains(screen, "enter copy") {
		t.Fatalf("copy mode hides the copy action:\n%s", screen)
	}
	a.copy.at = rowWith(t, a, "charlie")
	cmd, taken := a.copyKey(key("enter"))
	if !taken || cmd == nil {
		t.Fatal("enter did not copy the selected line")
	}
	raw, ok := cmd().(tea.RawMsg)
	if !ok {
		t.Fatal("copy did not produce a terminal clipboard write")
	}
	seq, _ := raw.Msg.(string)
	payload, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b]52;c;"), "\a"))
	if err != nil || string(payload) != "  · charlie" {
		t.Fatalf("clipboard contains %q, error %v", payload, err)
	}
	if screen := plain(frame(a)); !strings.Contains(screen, "clipboard sent") {
		t.Fatalf("copy has no visible feedback:\n%s", screen)
	}
	drive(t, a, key("up"))
	if screen := plain(frame(a)); strings.Contains(screen, "clipboard sent") {
		t.Fatalf("moving to a new selection kept stale copy feedback:\n%s", screen)
	}
}
