package tui3

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/Agent-Field/codeaf/internal/pair"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// The add-machine card's tests: the fleet and the link are fakes, so each test
// states the fleet the card is told about and reads the frame back.

type fakeFleet struct {
	size  int
	err   error
	calls int
}

func (f *fakeFleet) FleetSize(context.Context) (int, error) { f.calls++; return f.size, f.err }

func addMachineRig(door Approvals, size int, known bool) *app {
	a := newTestApp(&fakeAgent{model: "m"})
	a.approvals = door
	a.addMachine = addMachine{size: size, known: known}
	return a
}

func addMachineFrame(a *app, width int) string {
	ctx := ambientBandContext(a, session.SessionRow{}, a.now(), width)
	return plain(strings.Join(drawAddMachineBand(a, ctx), "\n"))
}

func TestAddMachineCardShowsForAFleetOfOne(t *testing.T) {
	a := addMachineRig(&fakeApprovals{}, 1, true)
	got := addMachineFrame(a, 80)
	for _, want := range []string{"Add another machine", "pick up your work anywhere, exactly where you left it.", addMachineShow} {
		if !strings.Contains(strings.Join(strings.Fields(got), " "), want) {
			t.Fatalf("card lacks %q:\n%s", want, got)
		}
	}
}

func TestAddMachineCardIsAbsentWhenItCannotWork(t *testing.T) {
	cases := map[string]*app{
		"two devices":    addMachineRig(&fakeApprovals{}, 2, true),
		"unknown fleet":  addMachineRig(&fakeApprovals{}, 0, false),
		"no approvals":   addMachineRig(nil, 1, true),
		"a bigger fleet": addMachineRig(&fakeApprovals{}, 5, true),
	}
	for name, a := range cases {
		if got := addMachineFrame(a, 80); got != "" {
			t.Errorf("%s drew a card:\n%s", name, got)
		}
	}
}

func TestAddMachineOpensTwoStepsAndAPasteField(t *testing.T) {
	a := addMachineRig(&fakeApprovals{}, 1, true)
	if a.toggleAddMachine() != nil {
		t.Fatal("opening the card asked for something")
	}
	got := addMachineFrame(a, 80)
	for _, want := range []string{addMachineStepRun, addMachineStepPut, addMachineField, addMachineHide} {
		if !strings.Contains(got, want) {
			t.Fatalf("opened card lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "codeaf.agentfield.ai") {
		t.Fatalf("this machine showed a link:\n%s", got)
	}
	a.toggleAddMachine()
	if got := addMachineFrame(a, 80); strings.Contains(got, addMachineStepRun) {
		t.Fatalf("closing kept the steps:\n%s", got)
	}
}

func TestAddMachinePasteGoesToTheApproveScreenAndApproveEndsTheCard(t *testing.T) {
	door := &fakeApprovals{pending: spark(time.Now())}
	a, r := approveApp(t, door)
	a.addMachine = addMachine{size: 1, known: true}
	a.openHome()
	a.toggleAddMachine()
	cmd := a.paste(testLink)
	if cmd == nil {
		t.Fatal("a pasted link did not open the approve screen")
	}
	if a.addMachine.open {
		t.Fatal("the card stayed open under the approve screen")
	}
	drive(t, a, cmd())
	r.until("the card", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
	if !strings.Contains(door.did(), "pending:"+testLink) {
		t.Fatalf("door was not asked for the link: %s", door.did())
	}
	r.press("a")
	r.until("the card closed", func() bool { return !a.pair.open })
	if !strings.Contains(door.did(), "approve:k7m2q9xd") {
		t.Fatalf("not approved: %s", door.did())
	}
	if !strings.Contains(plain(frame(a)), "spark joined your devices") {
		t.Fatalf("no toast:\n%s", plain(frame(a)))
	}
	if a.addMachineWanted() {
		t.Fatal("a fleet of two kept the card")
	}
}

func TestAddMachinePasteThatIsNotALinkIsNotTheCards(t *testing.T) {
	a := addMachineRig(&fakeApprovals{}, 1, true)
	a.toggleAddMachine()
	if _, took := a.pasteLink("hello there"); took {
		t.Fatal("plain text was taken as a link")
	}
}

// THE CARD NEED NOT BE OPEN, OR EVEN DRAWN: a link anywhere on home is the
// approve screen's, and a link off home is not.
func TestPairLinkOnBareHomeShowsTheApproveScreen(t *testing.T) {
	for name, setup := range map[string]func(*app){
		"card closed":   func(a *app) { a.addMachine = addMachine{size: 1, known: true} },
		"fleet unknown": func(a *app) {},
	} {
		t.Run(name, func(t *testing.T) {
			door := &fakeApprovals{pending: spark(time.Now())}
			a, r := approveApp(t, door)
			setup(a)
			a.openHome()
			drive(t, a, tea.PasteStartMsg{}, tea.PasteMsg{Content: testLink}, tea.PasteEndMsg{})
			r.until("the card", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
			if got := plain(frame(a)); !strings.Contains(got, "wants to join your devices") {
				t.Fatalf("no approve screen on the frame:\n%s", got)
			}
		})
	}
}

func TestPairLinkTypedAndEnteredOnHomeShowsTheApproveScreen(t *testing.T) {
	door := &fakeApprovals{pending: spark(time.Now())}
	a, r := approveApp(t, door)
	a.openHome()
	typeText(t, a, testLink)
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	r.until("the card", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
	if a.home.box.String() != "" {
		t.Fatalf("the link stayed in the box: %q", a.home.box.String())
	}
}

func TestPairLinkThatRanOutSaysSoAndIsNotSilent(t *testing.T) {
	door := &fakeApprovals{err: pair.ErrLinkGone}
	a, r := approveApp(t, door)
	a.openHome()
	drive(t, a, tea.PasteStartMsg{}, tea.PasteMsg{Content: testLink}, tea.PasteEndMsg{})
	r.until("the failure", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.line != "" })
	if got := plain(frame(a)); !strings.Contains(got, "that link is not waiting any more") {
		t.Fatalf("no plain message:\n%s", got)
	}
}

func TestOrdinaryTextOnHomeIsNotTakenForALink(t *testing.T) {
	for _, text := range []string{"database", "fix the login bug", "https://example.com/p/k7m2q9xd#Qm9v"} {
		a, _ := approveApp(t, &fakeApprovals{})
		a.openHome()
		if _, took := a.pasteLink(text); took {
			t.Errorf("%q was taken for a pair link", text)
		}
	}
}

func TestPairLinkOffHomeIsNotTheHomesToTake(t *testing.T) {
	a, _ := approveApp(t, &fakeApprovals{})
	if _, took := a.pasteLink(testLink); took {
		t.Fatal("a link pasted off home was taken")
	}
}

func TestAddMachineCardObeysWidthAndVocabulary(t *testing.T) {
	a := addMachineRig(&fakeApprovals{}, 1, true)
	a.toggleAddMachine()
	for _, width := range []int{20, 32, 60} {
		for _, line := range strings.Split(addMachineFrame(a, width), "\n") {
			if ansi.StringWidth(line) > width {
				t.Errorf("width %d: row %q is %d wide", width, line, ansi.StringWidth(line))
			}
		}
	}
	low := strings.ToLower(addMachineFrame(a, 80))
	for _, banned := range []string{"node", "relay", "take", "lease", "manifest"} {
		if strings.Contains(low, banned) {
			t.Errorf("card says %q", banned)
		}
	}
}

func TestFleetIsAskedUntilItIsTwo(t *testing.T) {
	a := addMachineRig(&fakeApprovals{}, 0, false)
	fleet := &fakeFleet{size: 1}
	a.fleet = fleet
	a.tookFleet(a.askFleet()().(fleetMsg))
	if !a.addMachineWanted() {
		t.Fatal("a fleet of one did not turn the card on")
	}
	fleet.size = 2
	a.tookFleet(a.askFleet()().(fleetMsg))
	if a.addMachineWanted() || a.askFleet() != nil {
		t.Fatal("a fleet of two kept the card or the asking")
	}
}

func TestAFailedFleetAskKeepsWhatWasKnown(t *testing.T) {
	a := addMachineRig(&fakeApprovals{}, 1, true)
	a.fleet = &fakeFleet{err: context.DeadlineExceeded}
	a.tookFleet(a.askFleet()().(fleetMsg))
	if !a.addMachineWanted() {
		t.Fatal("a failed ask turned the card off")
	}
}

var altD = tea.KeyPressMsg{Code: 'd', Mod: tea.ModAlt}

// THE CHORD REACHES THE CARD FROM HOME'S BOX, and a bare letter never does.
func TestAddMachineChordIsHomesAndOnlyWhereTheCardIs(t *testing.T) {
	a := addMachineRig(&fakeApprovals{}, 1, true)
	a.openHome()
	drive(t, a, altD)
	if !a.addMachine.open {
		t.Fatal("the chord did not open the card")
	}
	drive(t, a, key("d"))
	if !a.addMachine.open {
		t.Fatal("a letter closed the card")
	}
	drive(t, a, altD)
	if a.addMachine.open {
		t.Fatal("the chord did not close the card")
	}
	b := addMachineRig(&fakeApprovals{}, 2, true)
	b.openHome()
	drive(t, b, altD)
	if b.addMachine.open {
		t.Fatal("the chord opened a card that is not drawn")
	}
}

// THE CARD IS ON THE HOME SCREEN WITH NO ROW UNDER THE CURSOR: an empty home
// has nothing to select, and it is the person with nothing yet who needs it.
func TestAddMachineCardShowsOnAnEmptyHome(t *testing.T) {
	a := addMachineRig(&fakeApprovals{}, 1, true)
	a.openHome()
	got := strings.Join(strings.Fields(homeText(a)), " ")
	for _, want := range []string{"Add another machine", "exactly where you left it.", addMachineShow} {
		if !strings.Contains(got, want) {
			t.Fatalf("empty home lacks %q:\n%s", want, homeText(a))
		}
	}
	b := addMachineRig(&fakeApprovals{}, 2, true)
	b.openHome()
	if strings.Contains(homeText(b), "Add another machine") {
		t.Fatal("a fleet of two kept the card on home")
	}
}

// A LINK PASTED ON HOME'S OPEN CARD SHOWS THE APPROVE SCREEN: home draws no
// panel, so the card must take the frame from it (the e2e journey, step 3a).
func TestAddMachinePasteOnHomeShowsTheApproveScreen(t *testing.T) {
	door := &fakeApprovals{pending: spark(time.Now())}
	a, r := approveApp(t, door)
	a.addMachine = addMachine{size: 1, known: true}
	a.openHome()
	drive(t, a, altD)
	drive(t, a, tea.PasteStartMsg{}, tea.PasteMsg{Content: testLink}, tea.PasteEndMsg{})
	r.until("the card", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
	if got := plain(frame(a)); !strings.Contains(got, "wants to join your devices") {
		t.Fatalf("no approve screen on the frame:\n%s", got)
	}
}
