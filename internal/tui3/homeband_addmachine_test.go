package tui3

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/pair"
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

type linkPairing struct {
	fakePairing
	link string
}

func (l *linkPairing) Link(context.Context) (string, error) { return l.link, nil }

func addMachineRig(door Pairing, size int, known bool) *app {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pairing = door
	a.addMachine = addMachine{size: size, known: known}
	return a
}

func addMachineFrame(a *app, width int) string {
	ctx := ambientBandContext(a, session.SessionRow{}, a.now(), width)
	return plain(strings.Join(drawAddMachineBand(a, ctx), "\n"))
}

func TestAddMachineCardShowsForAFleetOfOne(t *testing.T) {
	a := addMachineRig(&fakePairing{}, 1, true)
	got := addMachineFrame(a, 80)
	for _, want := range []string{"Add another machine", "pick up your work anywhere, exactly where you left it.", addMachineShow} {
		if !strings.Contains(strings.Join(strings.Fields(got), " "), want) {
			t.Fatalf("card lacks %q:\n%s", want, got)
		}
	}
}

func TestAddMachineCardIsAbsentWhenItCannotWork(t *testing.T) {
	cases := map[string]*app{
		"two devices":    addMachineRig(&fakePairing{}, 2, true),
		"unknown fleet":  addMachineRig(&fakePairing{}, 0, false),
		"no pairing":     addMachineRig(nil, 1, true),
		"a bigger fleet": addMachineRig(&fakePairing{}, 5, true),
	}
	for name, a := range cases {
		if got := addMachineFrame(a, 80); got != "" {
			t.Errorf("%s drew a card:\n%s", name, got)
		}
	}
}

func TestAddMachineOpensBothInstructionVariantsAndTheLink(t *testing.T) {
	a := addMachineRig(&linkPairing{link: "https://codeaf.link/p/k7m2q9xd#abc"}, 1, true)
	cmd := a.toggleAddMachine()
	if cmd == nil {
		t.Fatal("opening the card did not ask for a link")
	}
	if got := addMachineFrame(a, 80); !strings.Contains(got, addMachineMaking) {
		t.Fatalf("no waiting word before the link arrives:\n%s", got)
	}
	a.tookLink(cmd().(pairLinkMsg))
	got := addMachineFrame(a, 80)
	for _, want := range []string{"https://codeaf.link/p/k7m2q9xd#abc", addMachineApp, addMachineCLI, addMachineThen, addMachineHide} {
		if !strings.Contains(got, want) {
			t.Fatalf("opened card lacks %q:\n%s", want, got)
		}
	}
	a.toggleAddMachine()
	if got := addMachineFrame(a, 80); strings.Contains(got, addMachineCLI) || strings.Contains(got, "codeaf.link") {
		t.Fatalf("closing kept the instructions or the link:\n%s", got)
	}
}

func TestAddMachineWithoutALinkShowsInstructionsAlone(t *testing.T) {
	a := addMachineRig(&fakePairing{}, 1, true)
	if a.toggleAddMachine() != nil {
		t.Fatal("a door with no link was asked for one")
	}
	got := addMachineFrame(a, 80)
	if !strings.Contains(got, addMachineApp) || !strings.Contains(got, addMachineCLI) || strings.Contains(got, addMachineMaking) {
		t.Fatalf("instructions wrong:\n%s", got)
	}
}

func TestAddMachineCardObeysWidthAndVocabulary(t *testing.T) {
	a := addMachineRig(&linkPairing{link: "https://codeaf.link/p/k7m2q9xd#Qm9vYmFyYmF6cXV4"}, 1, true)
	a.toggleAddMachine()
	a.addMachine.link = "https://codeaf.link/p/k7m2q9xd#Qm9vYmFyYmF6cXV4"
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
	a := addMachineRig(&fakePairing{}, 0, false)
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
	a := addMachineRig(&fakePairing{}, 1, true)
	a.fleet = &fakeFleet{err: context.DeadlineExceeded}
	a.tookFleet(a.askFleet()().(fleetMsg))
	if !a.addMachineWanted() {
		t.Fatal("a failed ask turned the card off")
	}
}

var altD = tea.KeyPressMsg{Code: 'd', Mod: tea.ModAlt}

// THE CHORD REACHES THE CARD FROM HOME'S BOX, and a bare letter never does.
func TestAddMachineChordIsHomesAndOnlyWhereTheCardIs(t *testing.T) {
	a := addMachineRig(&fakePairing{}, 1, true)
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
	b := addMachineRig(&fakePairing{}, 2, true)
	b.openHome()
	drive(t, b, altD)
	if b.addMachine.open {
		t.Fatal("the chord opened a card that is not drawn")
	}
}

// THE CARD IS ON THE HOME SCREEN WITH NO ROW UNDER THE CURSOR: an empty home
// has nothing to select, and it is the person with nothing yet who needs it.
func TestAddMachineCardShowsOnAnEmptyHome(t *testing.T) {
	a := addMachineRig(&fakePairing{}, 1, true)
	a.openHome()
	got := strings.Join(strings.Fields(homeText(a)), " ")
	for _, want := range []string{"Add another machine", "exactly where you left it.", addMachineShow} {
		if !strings.Contains(got, want) {
			t.Fatalf("empty home lacks %q:\n%s", want, homeText(a))
		}
	}
	b := addMachineRig(&fakePairing{}, 2, true)
	b.openHome()
	if strings.Contains(homeText(b), "Add another machine") {
		t.Fatal("a fleet of two kept the card on home")
	}
}

type liveDoor struct {
	fakePairing
	join func(ctx context.Context, ui pair.LinkUI) (pair.LinkJoined, error)
}

func (l *liveDoor) PairByLink(ctx context.Context, ui pair.LinkUI) (pair.LinkJoined, error) {
	return l.join(ctx, ui)
}

// cardPump feeds the command's messages back through Update until the wait ends.
func cardPump(t *testing.T, a *app, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg, ok := cmd().(cardMsg)
		if !ok {
			return
		}
		cmd = a.tookCard(msg)
	}
}

func TestAddMachineCardShowsALiveLinkAndEndsOnPaired(t *testing.T) {
	approve := make(chan struct{})
	door := &liveDoor{join: func(ctx context.Context, ui pair.LinkUI) (pair.LinkJoined, error) {
		ui.Invited(pair.Invite{Ref: pair.LinkRef{Code: "K7M2Q9XD", Key: []byte("0123456789abcdef")}, Check: "4821"})
		<-approve
		return pair.LinkJoined{Fleet: pair.Fleet{Devices: 2, Workspaces: 3}}, nil
	}}
	a := addMachineRig(door, 1, true)
	cmd := a.toggleAddMachine()
	first := cmd().(cardMsg)
	cmd = a.tookCard(first)
	got := addMachineFrame(a, 80)
	for _, want := range []string{"codeaf.link/p/", "Check number: 4821"} {
		if !strings.Contains(got, want) {
			t.Fatalf("live card lacks %q:\n%s", want, got)
		}
	}
	close(approve)
	cardPump(t, a, cmd)
	got = addMachineFrame(a, 80)
	if !strings.Contains(got, "Paired - 3 workspaces available.") || strings.Contains(got, addMachineCLI) {
		t.Fatalf("card did not end on Paired:\n%s", got)
	}
}

func TestAddMachineCardShowsWhyTheWaitFailedAndClosingTakesItBack(t *testing.T) {
	stopped := make(chan struct{})
	door := &liveDoor{join: func(ctx context.Context, ui pair.LinkUI) (pair.LinkJoined, error) {
		<-ctx.Done()
		close(stopped)
		return pair.LinkJoined{}, ctx.Err()
	}}
	a := addMachineRig(door, 1, true)
	a.toggleAddMachine()
	a.toggleAddMachine()
	<-stopped
	if got := addMachineFrame(a, 80); strings.Contains(got, "Paired") || strings.Contains(got, addMachineMaking) {
		t.Fatalf("closed card kept the wait:\n%s", got)
	}
	failing := &liveDoor{join: func(context.Context, pair.LinkUI) (pair.LinkJoined, error) {
		return pair.LinkJoined{}, pair.ErrLinkExpired
	}}
	b := addMachineRig(failing, 1, true)
	cardPump(t, b, b.toggleAddMachine())
	if got := addMachineFrame(b, 80); !strings.Contains(got, pair.ErrLinkExpired.Error()[:20]) {
		t.Fatalf("failure not shown:\n%s", got)
	}
}
