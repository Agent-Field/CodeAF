package tui3

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestHomeCopyNameIsSecondAndCopiesTheFullCurrentTitle(t *testing.T) {
	a := newSwitchLab(t).open(180, 40)
	drive(t, a, key("right"))
	var got []string
	for _, v := range a.strip.verbs {
		got = append(got, string(v.key)+" "+v.word)
	}
	want := []string{"x close", "c copy name", "n new in project", "o open folder"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("conversation options = %v, want %v", got, want)
	}
	// A rename after the strip opens must not copy the old or clipped label.
	a.title = "Renamed conversation — café " + strings.Repeat("long title ", 20) + "end"
	cmd, handled := a.stripKey(key("c"))
	if !handled || cmd == nil || !reflect.DeepEqual(cmd(), tea.Raw(osc52(a.conversationName(), a.tmux))()) {
		t.Fatal("copy name did not copy the full live title after the menu opened")
	}
}

func TestHomeCopyNameUsesLatestSnapshotWithoutALocalFolder(t *testing.T) {
	for name, hosted := range map[string]bool{"local": false, "remote": true} {
		t.Run(name, func(t *testing.T) {
			a := newSwitchLab(t).open(180, 40)
			if hosted {
				a.host = "far-machine"
			}
			row := session.SessionRow{Transcript: "/far/conversation/transcript.jsonl", Title: "Old name"}
			verbs := a.homeReadingVerbs(homeLine{}, switcherRow{kind: switcherConversation, session: row, gone: true})
			if len(verbs) != 2 || verbs[1].key != 'c' {
				t.Fatalf("name needs no local folder: %v", verbs)
			}
			current := row
			current.Title = "Current saved name"
			a.home.world = session.World{Projects: []session.Project{{Sessions: []session.SessionRow{current}}}}
			cmd := verbs[1].do()
			if cmd == nil || !reflect.DeepEqual(cmd(), tea.Raw(osc52(homeName(current), a.tmux))()) {
				t.Fatal("copy used the old menu snapshot instead of the current conversation name")
			}
		})
	}
}
