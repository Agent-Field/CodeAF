package tui3

import "testing"

// These keys enter through the app router so page navigation cannot silently
// take Tab before either composer's visible command list receives it.
func TestTabChoosesSlashCommandsInBothComposers(t *testing.T) {
	for _, home := range []bool{false, true} {
		place := "conversation"
		if home {
			place = "home"
		}
		for _, tc := range []struct {
			name, typed, want string
			settings          bool
		}{
			{name: "bare", typed: "/set", settings: true},
			{name: "argument", typed: "/mod", want: "/model "},
			{name: "mid sentence", typed: "look at /comp", want: "look at /compact"},
			{name: "finished tag", typed: "do this /task", want: "do this /task"},
			{name: "no match", typed: "/zzzzmissing", want: "/zzzzmissing"},
		} {
			t.Run(place+"/"+tc.name, func(t *testing.T) {
				agent := &fakeAgent{model: "m"}
				a := newTestApp(agent)
				if home {
					a.openHome()
				}
				typeInto(t, a, tc.typed)
				if tc.name == "argument" {
					drive(t, a, key("down"))
				}
				drive(t, a, key("tab"))
				if tc.settings {
					if !a.at(pageSettings) {
						t.Fatal("Tab did not open the highlighted settings command")
					}
					return
				}
				e := &a.input
				if home {
					if !a.at(pageHome) {
						t.Fatal("Tab left Home while completing a command")
					}
					e = &a.home.box
				}
				if got := e.String(); got != tc.want {
					t.Fatalf("draft = %q, want %q", got, tc.want)
				}
				if len(agent.sent) != 0 || agent.packs != 0 {
					t.Fatal("completing a word submitted the draft or ran its embedded command")
				}
			})
		}
	}
}

func TestTabChoosesTheMovedSlashSelection(t *testing.T) {
	for _, home := range []bool{false, true} {
		a := newTestApp(&fakeAgent{model: "m"})
		if home {
			a.openHome()
		}
		typeInto(t, a, "please explain /")
		drive(t, a, key("down"))
		chosen, ok := a.menu.choice()
		if home {
			line, found := a.home.focusedLine()
			if !found || line.kind != homeCommand {
				t.Fatal("Home did not select a command")
			}
			chosen, ok = *line.cmd, true
		}
		if !ok {
			t.Fatal("no command selected")
		}
		drive(t, a, key("tab"))
		got := a.input.String()
		if home {
			got = a.home.box.String()
		}
		if want := "please explain /" + chosen.name; got != want {
			t.Fatalf("draft = %q, want moved selection %q", got, want)
		}
	}
}

func TestTabCommandOnNewChatLeavesThePreviousChatAlone(t *testing.T) {
	lab := newStartLab(t)
	drive(t, lab.a, key("ctrl+t"))
	typeInto(t, lab.a, "/mod")
	drive(t, lab.a, key("down"))
	drive(t, lab.a, key("tab"))
	if got := lab.a.input.String(); got != "/model " {
		t.Fatalf("draft = %q, want completed argument command", got)
	}
	if !lab.a.startingChat() || lab.made != 0 || len(lab.agent.sent) != 0 {
		t.Fatal("Tab created or changed a conversation before the draft was sent")
	}
}
