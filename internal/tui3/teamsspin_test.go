package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func TestTeamsWorkingCardsAnimateAndYieldToQuestions(t *testing.T) {
	a, id, _ := teamsHostedLab(t)
	a.width, a.height = 160, 60
	a.linear, a.pal = false, newPalette(tokens.TrueColor, false)
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	a.state = stateWorking
	team := mustTeam(t, a, id)
	var member teamMember
	for _, m := range team.Members {
		if m.Key != team.Manager {
			member = m
			break
		}
	}
	watch := &behindWatch{}
	watch.turning.Store(true)
	a.behind[member.Key] = &kept{conv: Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: member.File}, watch: watch}
	paint := func() string {
		return plain(strings.Join(a.teamsMemberCards(&teamsDraw{a: a}, team, 155, 0), "\n"))
	}
	first := paint()
	if n := strings.Count(first, "working "+tokens.Spinner(wallSpin(now))); n != 2 {
		t.Fatalf("manager and held member need matching work spinners, got %d:\n%s", n, first)
	}
	now = now.Add(spinnerStep * frameInterval)
	if second := paint(); second == first || strings.Count(second, "working "+tokens.Spinner(wallSpin(now))) != 2 {
		t.Fatal("active cards did not advance together")
	}
	a.frontWaits = true
	watch.waits.Store(true)
	if waiting := paint(); strings.Contains(waiting, "working") || !strings.Contains(waiting, "? waiting on you") {
		t.Fatalf("a question retained its work spinner:\n%s", waiting)
	}
	a.frontWaits = false
	watch.waits.Store(false)
	a.tp.packets = []teamstore.Packet{{Team: teamstore.Person, Origin: id, State: teamstore.PacketOpen, Question: "Choose the layout"}}
	rows := a.teamsManagerCard(&teamsDraw{a: a}, team, a.teamsCrew(team)[0], 155, 0)
	if strings.Contains(plain(rows[0]), "working") || !strings.Contains(plain(rows[0]), "? Choose the layout") {
		t.Fatal("pending team decision did not replace the work spinner")
	}
}

func TestTeamsRemoteWorkWakesAndKeepsOnlyVisibleCardsAnimating(t *testing.T) {
	a, id, _ := teamsHostedLab(t)
	a.width, a.height = 160, 60
	a.linear, a.pal = false, newPalette(tokens.TrueColor, false)
	a.state = stateIdle
	team := mustTeam(t, a, id)
	var member teamMember
	for _, m := range team.Members {
		if m.Key != team.Manager {
			member = m
			break
		}
	}
	delete(a.behind, member.Key)
	a.tp.previews[member.Key] = teamsPreview{}
	a.tp.sel = id
	a.teamsBody(160, 55)
	if a.teamsSpinning() {
		t.Fatal("idle cards kept the paint clock running")
	}
	old := surfaceTick
	t.Cleanup(func() { surfaceTick = old })
	var delays []time.Duration
	surfaceTick = func(d time.Duration, cb func(time.Time) tea.Msg) tea.Cmd {
		delays = append(delays, d)
		return func() tea.Msg { return nil }
	}
	row := session.SessionRow{Live: true, Presence: session.SessionPresence{State: session.PresenceWorking}}
	a.painting = false
	a.Update(doorMsg{front: a.frontGen, fold: func(bool) tea.Cmd {
		return a.teamsFold(teamsGot{worldAsked: true, worldKnown: true, world: map[string]session.SessionRow{member.File: row}})
	}})
	if !a.painting || !a.teamsSpinning() {
		t.Fatal("an external member starting work did not wake the idle clock")
	}
	delays = nil
	a.paint()
	if len(delays) == 0 || delays[len(delays)-1] != a.frameEvery()*spinnerStep {
		t.Fatalf("external work needs only spinner-cadence frames: %v", delays)
	}
	row.Presence.State = session.PresenceWaiting
	a.tp.world[member.File] = row
	a.paint()
	if a.teamsSpinning() || a.painting {
		t.Fatal("waiting on the person retained the animation clock")
	}
	row.Presence.State = session.PresenceWorking
	a.tp.world[member.File] = row
	a.tp.paneWheel, a.tp.paneOffset = true, 0
	a.teamsBody(160, 8)
	if a.teamsSpinning() {
		t.Fatal("offscreen member retained the animation clock")
	}
	a.teamsBody(160, 55)
	if !a.teamsSpinning() {
		t.Fatal("returning the active member to view lost its animation")
	}
	var header int
	for _, card := range a.tp.cards {
		if card.key == member.Key {
			header = card.y
		}
	}
	// A single visible top-border row owns the spinner even when none of
	// its clickable body rows fit. Scrolling one row farther hides it.
	a.tp.paneOffset, a.tp.paneRoom = header, 1
	if !a.teamsSpinning() {
		t.Fatal("top-border-only spinner lost its clock")
	}
	// Every clock read crosses a spinner boundary. Emission must be an
	// explicit fact, rather than comparing glyphs from two sampled moments.
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	a.clock = func() time.Time {
		now = now.Add(spinnerStep * frameInterval)
		return now
	}
	if !a.teamsSpinning() {
		t.Fatal("crossing a spinner boundary stopped the active card's clock")
	}
	a.tp.paneOffset = header + 1
	if a.teamsSpinning() {
		t.Fatal("body-only card retained its offscreen spinner clock")
	}
	a.tp.sel, a.tp.paneOffset = teamsAllRow, 0
	a.teamsBody(160, 55)
	if a.teamsSpinning() {
		t.Fatal("static overview aliases retained the animation clock")
	}
}

func TestTeamsNarrowCardsReserveTheirWorkingSpinner(t *testing.T) {
	a, id, _ := teamsHostedLab(t)
	a.linear, a.pal = false, newPalette(tokens.TrueColor, false)
	r := teamsCrewRow{key: a.frontTabKey(), file: a.file, word: "working"}
	for _, width := range []int{12, 20, 28, 40} {
		for _, role := range []string{"", "Manager", "Global manager"} {
			rows := a.teamsConversationCard(&teamsDraw{a: a}, mustTeam(t, a, id), r, role, nil, 0, 0, width)
			if !strings.Contains(rows[0], tokens.Spinner(wallSpin(a.now()))) {
				t.Fatalf("%s at %d lost the work mark: %s", role, width, plain(rows[0]))
			}
		}
	}
}

func TestTeamsGlobalManagerSpinnerAndReducedMotion(t *testing.T) {
	a, _, _ := teamsHostedLab(t)
	a.width, a.height = 160, 60
	key := a.frontTabKey()
	a.tp.previews[key] = teamsPreview{}
	a.wall.teams = append(a.wall.teams, team{ID: "global-root", Root: true, Name: teamstore.RootName, Manager: key, Members: []teamMember{{Key: key, File: a.file, Handle: "global"}}})
	a.tp.sel, a.state = teamsAllRow, stateWorking
	for _, mode := range []string{"animated", "ascii", "linear"} {
		a.linear = mode == "linear"
		a.pal = newPalette(tokens.TrueColor, mode == "ascii")
		a.teamsBody(160, 55)
		rows := a.teamsGlobalManagerCard(&teamsDraw{a: a}, 120, 0)
		mark := glyphRunASCII
		if mode == "animated" {
			mark = tokens.Spinner(wallSpin(a.now()))
		}
		if !strings.Contains(plain(rows[0]), "Global manager  working "+mark) || a.teamsSpinning() != (mode == "animated") {
			t.Fatalf("wrong global work state in %s: %s", mode, plain(rows[0]))
		}
	}
	a.linear, a.pal = false, newPalette(tokens.TrueColor, false)
	a.state = stateIdle
	rows := a.teamsGlobalManagerCard(&teamsDraw{a: a}, 120, 0)
	if strings.Contains(plain(rows[0]), "working") || a.teamsSpinning() {
		t.Fatal("idle global manager retained work indication")
	}
}
