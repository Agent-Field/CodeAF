package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
	"testing"
	"time"
)

func backApp(t *testing.T) *app {
	t.Helper()
	lab := newHomeLab(t)
	mine := lab.session("-alpha", "aaaa000000000001", "here", "/tmp/alpha", time.Now())
	return lab.app(mine)
}

func TestEscapeReachesHomeAndPreservesRunningWorkAndDrafts(t *testing.T) {
	a := backApp(t)
	agent := &rewindFake{fakeAgent: &fakeAgent{model: "m", past: rewindPast()}}
	a.agent = agent
	a.state = stateWorking
	a.parks = []parked{{text: "parked words"}}
	a.follows = []queued{{text: "queued words"}}
	a.input.setText("half a thought\nand another line")
	for range 8 {
		drive(t, a, key("esc"))
	}
	if !a.at(pageHome) || a.rew.on || a.rewSheet.open || agent.stops != 0 || len(agent.cuts) != 0 || a.state != stateWorking {
		t.Fatal("Escape did more than navigate home")
	}
	if len(a.parks) != 1 || len(a.follows) != 1 {
		t.Fatal("Escape dropped waiting messages")
	}
	if a.input.String() != "half a thought\nand another line" {
		t.Fatal("lost conversation draft")
	}
	a.home.box.setText("a home draft")
	for range 8 {
		drive(t, a, key("esc"))
	}
	if !a.at(pageHome) || a.home.box.String() != "a home draft" {
		t.Fatal("Escape left Home or lost its draft")
	}
	a.closeHome()
	if a.input.String() != "half a thought\nand another line" {
		t.Fatal("returning lost the draft")
	}
	drive(t, a, key("ctrl+c"))
	if agent.stops != 1 {
		t.Fatal("Ctrl+C no longer interrupts")
	}
}

func TestEscapeDismissesCommandListsWithoutLosingEitherDraft(t *testing.T) {
	for _, home := range []bool{false, true} {
		a := backApp(t)
		if home {
			a.openHome()
			typeHome(a, "/mo")
		} else {
			a.input.setText("/mo")
			a.syncLists()
		}
		drive(t, a, key("esc"))
		if home {
			if a.home.cmd.open || a.home.box.String() != "/mo" {
				t.Fatal("Home command dismiss lost draft or kept list")
			}
		} else {
			if a.menu.open || a.input.String() != "/mo" || a.at(pageHome) {
				t.Fatal("conversation command dismiss skipped a layer")
			}
		}
		for range 4 {
			drive(t, a, key("esc"))
		}
		if !a.at(pageHome) {
			t.Fatal("Escape did not settle on Home")
		}
	}
}

func TestEscapeFromEveryPlaceSettlesOnHome(t *testing.T) {
	for _, where := range []page{pageHome, pageTasks, pageSettings, pageMemory, pageSearch, pageSpend, pageStanding} {
		t.Run(string(rune('0'+where)), func(t *testing.T) {
			a, _ := driveToPlace(t, newHomeLab(t), where)
			for range 12 {
				drive(t, a, key("esc"))
			}
			if !a.at(pageHome) {
				t.Fatalf("%v did not reach Home", where)
			}
		})
	}
}

func TestDoubleSpaceNoLongerNavigates(t *testing.T) {
	a := backApp(t)
	for _, draft := range []string{"", "\n", "words"} {
		a.input.setText(draft)
		drive(t, a, key(" "))
		drive(t, a, key(" "))
		if a.at(pageHome) || a.input.String() != draft+"  " {
			t.Fatalf("spaces changed navigation or draft %q", draft)
		}
	}
	for _, where := range []page{pageTasks, pageMemory, pageSearch, pageSpend, pageStanding} {
		a, box := driveToPlace(t, newHomeLab(t), where)
		drive(t, a, key(" "))
		drive(t, a, key(" "))
		if a.at(pageHome) {
			t.Fatalf("spaces navigated from %v", where)
		}
		if box != nil && box.String() != "  " {
			t.Fatalf("spaces lost from %v", where)
		}
	}
}

func TestEscapeDefersSubharnessUntilExplicitAnswer(t *testing.T) {
	a := backApp(t)
	a.subPage = subPage{open: true, card: &subCard{name: "pending", offer: 42}}
	a.state = stateWorking
	drive(t, a, key("esc"))
	if a.subPage.open || !a.awaitingSubharness() {
		t.Fatal("Escape answered or lost the offer")
	}
	for range 4 {
		drive(t, a, key("esc"))
	}
	if !a.at(pageHome) || !a.awaitingSubharness() {
		t.Fatal("pending offer prevented back navigation")
	}
	a.closeHome()
	a.openSubharness("")
	if !a.subPage.open || a.subPage.card.offer != 42 {
		t.Fatal("/subharness did not resume offer")
	}
	a.withdrawSubharnessProposal(42, "pending")
	if a.awaitingSubharness() {
		t.Fatal("withdrawal left a deferred offer behind")
	}
}

func TestEscapePeelsModalLayersBeforeHome(t *testing.T) {
	for _, test := range []struct {
		name    string
		open    func(*app)
		showing func(*app) bool
	}{
		{"model", func(a *app) { a.pick.open = true }, func(a *app) bool { return a.pick.open }},
		{"effort", func(a *app) { a.effPick.open = true }, func(a *app) bool { return a.effPick.open }},
		{"resume", func(a *app) { a.roster.open = true }, func(a *app) bool { return a.roster.open }},
		{"folder", func(a *app) { a.folder.open = true }, func(a *app) bool { return a.folder.open }},
		{"files", func(a *app) { a.shelf.open = true }, func(a *app) bool { return a.shelf.open }},
		{"connections", func(a *app) { a.connPanel.open = true }, func(a *app) bool { return a.connPanel.open }},
		{"harness", func(a *app) { a.harnPanel.open = true }, func(a *app) bool { return a.harnPanel.open }},
		{"crew", func(a *app) { a.crewUI = crewPanel{open: true, saved: -1, step: -1} }, func(a *app) bool { return a.crewUI.open }},
		{"permissions", func(a *app) { a.permPanel.open = true }, func(a *app) bool { return a.permPanel.open }},
		{"drafts", func(a *app) { a.draftPage.open = true }, func(a *app) bool { return a.draftPage.open }},
		{"subharness list", func(a *app) { a.subPage.open = true }, func(a *app) bool { return a.subPage.open }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := backApp(t)
			a.input.setText("keep this draft")
			test.open(a)
			drive(t, a, key("esc"))
			if test.showing(a) || a.at(pageHome) {
				t.Fatal("Escape skipped the modal's parent")
			}
			for range 4 {
				drive(t, a, key("esc"))
			}
			if !a.at(pageHome) || a.input.String() != "keep this draft" {
				t.Fatal("Escape failed to settle on Home with the draft")
			}
		})
	}
}

func TestEscapeFromTaskRoomReachesHomeWithoutStoppingTheTask(t *testing.T) {
	a, agent, _ := roomApp(t)
	door := backApp(t)
	a.open, a.resume = door.open, door.resume
	clickRail(t, a, 0)
	a.input.setText("a task correction")
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("Escape stayed in room")
	}
	for range 4 {
		drive(t, a, key("esc"))
	}
	if !a.at(pageHome) || agent.stops != 0 {
		t.Fatal("Escape failed to go Home without stopping")
	}
	a.closeHome()
	clickRail(t, a, 0)
	if a.input.String() != "a task correction" {
		t.Fatal("Escape lost the task draft")
	}
}

func TestBackPastAnApprovalKeepsItUnansweredAndReopenable(t *testing.T) {
	agent, a := wired([]session.Event{
		toolBegin("edit", "edit main.go"),
		consentEvent(3, "edit", "edit main.go", `tool "edit"`),
	})
	door := backApp(t)
	a.open, a.resume = door.open, door.resume
	typeLine(t, a, "fix it")
	settleAsk(a)
	for range 5 {
		drive(t, a, key("esc"))
	}
	if !a.at(pageHome) || !a.asking() || len(agent.answers) != 0 || agent.stops != 0 {
		t.Fatal("back navigation answered or stopped the pending question")
	}
	a.closeHome()
	drive(t, a, tea.KeyPressMsg{Code: 'y', Mod: tea.ModAlt})
	if !a.questioning() {
		t.Fatal("deferred question could not be reopened")
	}
}

// /CREW TYPED ON HOME STEPS OFF IT AND ESC STEPS BACK: a place cannot draw an
// overlay, so the panel opens over the conversation behind home, and the one
// level esc goes back is home itself (crewpanel.go's [app.openCrew]).
func TestCrewFromHomeComesBackToHome(t *testing.T) {
	a := backApp(t)
	runCmd(a.showPage(pageHome))
	if !a.at(pageHome) {
		t.Fatal("home did not open")
	}
	runCmd(a.homeSlash("/crew"))
	if !a.crewUI.open || a.at(pageHome) {
		t.Fatalf("/crew on home left the panel %v and home %v", a.crewUI.open, a.at(pageHome))
	}
	drive(t, a, key("esc"))
	if a.crewUI.open || !a.at(pageHome) {
		t.Fatal("esc on the panel did not come back to home")
	}
}
