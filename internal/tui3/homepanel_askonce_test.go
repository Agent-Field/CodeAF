package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

type fakePermitAsker struct {
	names    []string
	ask      bool
	askCalls int
	answers  []bool
	err      error
}

func (f *fakePermitAsker) AskOnce() ([]string, bool) {
	f.askCalls++
	if !f.ask {
		return nil, false
	}
	return append([]string(nil), f.names...), true
}

func (f *fakePermitAsker) Answer(allow bool) error {
	f.answers = append(f.answers, allow)
	if f.err != nil {
		return f.err
	}
	f.ask = false
	return nil
}

// V25/T13 (Part 3): when Home opens in a local window and PermitAsk.AskOnce()
// says ask, Home raises one card naming the older paired computers; answering 1
// calls Answer(true) and ends the question for good.
func TestHomeAsksOnceToLetAnOlderPairedDeviceControlChats(t *testing.T) {
	asker := &fakePermitAsker{names: []string{"tower", "desk"}, ask: true}
	a := newApp(context.Background(), Options{
		Agent:     &fakeAgent{model: "m"},
		PermitAsk: asker,
		Approvals: devicesFake(),
	})
	a.width, a.height = 200, 70
	drain(t, a, a.openHome())

	ask, ok := a.homeAsking()
	if !ok {
		t.Fatal("Home did not raise the one-time permit question on open")
	}
	if ask.question.Head != permitAskHead {
		t.Fatalf("card head = %q, want %q", ask.question.Head, permitAskHead)
	}
	wantReason := "tower and desk can watch your chats here but cannot type yet."
	if ask.question.Reason != wantReason {
		t.Fatalf("card reason = %q, want %q", ask.question.Reason, wantReason)
	}
	if len(ask.question.Options) != 3 ||
		ask.question.Options[0].Label != permitAskOptionType ||
		ask.question.Options[1].Label != permitAskOptionKeep ||
		ask.question.Options[2].Label != permitAskOptionPick {
		t.Fatalf("card options = %+v", ask.question.Options)
	}
	screen := homeText(a)
	for _, want := range []string{permitAskHead, wantReason, permitAskOptionType, permitAskOptionKeep, permitAskOptionPick} {
		if !strings.Contains(screen, want) {
			t.Fatalf("Home screen lacked %q:\n%s", want, screen)
		}
	}

	// Refreshing Home during the same visit does not ask again.
	callsBefore := asker.askCalls
	a.refreshHome()
	a.rebuildMachines()
	if asker.askCalls != callsBefore {
		t.Fatalf("refreshing Home during the same visit called AskOnce %d extra times", asker.askCalls-callsBefore)
	}

	// Answering 1 calls Answer(true) and closes the card.
	drive(t, a, key("1"))
	if len(asker.answers) != 1 || !asker.answers[0] {
		t.Fatalf("pressing 1 recorded answers %v, want [true]", asker.answers)
	}
	if _, ok := a.homeAsking(); ok {
		t.Fatal("permit card remained open after answering 1")
	}

	// Reopening Home after Answer succeeded does not raise the card again.
	a.closeHome()
	drain(t, a, a.openHome())
	if _, ok := a.homeAsking(); ok {
		t.Fatal("Home raised the permit card again after Answer succeeded")
	}
}

// V25/T13 (Part 3): option 2 calls Answer(false), option 3 calls Answer(false)
// and opens `/devices`, `esc` leaves the card without answering so it returns
// on the next Home open, and an Answer error is shown on Home's foot without
// re-raising until Home reopens.
func TestHomeAsksOnceAnswersTwoAndThreeAndEscAndError(t *testing.T) {
	t.Run("option 2 keeps them watching", func(t *testing.T) {
		asker := &fakePermitAsker{names: []string{"tower"}, ask: true}
		a, _ := liveHomeRig(t, "dev_tower")
		a.permitAsk = asker
		a.closeHome()
		drain(t, a, a.openHome())

		ask, ok := a.homeAsking()
		if !ok || ask.question.Reason != "tower can watch your chats here but cannot type yet." {
			t.Fatalf("unexpected ask: ok=%t question=%+v", ok, ask.question)
		}
		drive(t, a, key("2"))
		if len(asker.answers) != 1 || asker.answers[0] {
			t.Fatalf("pressing 2 recorded answers %v, want [false]", asker.answers)
		}
		if _, ok := a.homeAsking(); ok {
			t.Fatal("card stayed open after pressing 2")
		}
		a.closeHome()
		drain(t, a, a.openHome())
		if _, ok := a.homeAsking(); ok {
			t.Fatal("card reappeared after option 2 succeeded")
		}
	})

	t.Run("option 3 keeps them watching and opens devices", func(t *testing.T) {
		asker := &fakePermitAsker{names: []string{"tower"}, ask: true}
		a, _ := liveHomeRig(t, "dev_tower")
		a.approvals = devicesFake()
		a.permitAsk = asker
		a.closeHome()
		drain(t, a, a.openHome())

		if _, ok := a.homeAsking(); !ok {
			t.Fatal("card not raised on open")
		}
		drive(t, a, key("3"))
		if len(asker.answers) != 1 || asker.answers[0] {
			t.Fatalf("pressing 3 recorded answers %v, want [false]", asker.answers)
		}
		if !a.pair.open || a.openDevicesCard() == nil {
			t.Fatalf("pressing 3 did not open the /devices card: pair.open=%t card=%T", a.pair.open, a.pair.card)
		}
	})

	t.Run("esc leaves the card without answering and returns on next open", func(t *testing.T) {
		asker := &fakePermitAsker{names: []string{"tower"}, ask: true}
		a, _ := liveHomeRig(t, "dev_tower")
		a.permitAsk = asker
		a.closeHome()
		drain(t, a, a.openHome())

		if _, ok := a.homeAsking(); !ok {
			t.Fatal("card not raised on open")
		}
		drive(t, a, key("esc"))
		if len(asker.answers) != 0 {
			t.Fatalf("esc called Answer: %v", asker.answers)
		}
		if _, ok := a.homeAsking(); ok {
			t.Fatal("esc did not dismiss the permit card")
		}
		if !a.at(pageHome) {
			t.Fatal("esc on the permit card closed Home instead of only the card")
		}
		// Refreshing on the same visit does not re-raise it.
		a.refreshHome()
		a.rebuildMachines()
		if _, ok := a.homeAsking(); ok {
			t.Fatal("card re-raised during the same Home visit after esc")
		}
		// Reopening Home raises it again.
		a.closeHome()
		drain(t, a, a.openHome())
		if _, ok := a.homeAsking(); !ok {
			t.Fatal("card did not return on the next Home open after esc")
		}
	})

	t.Run("Answer error is said on Home foot and not re-raised until reopen", func(t *testing.T) {
		wantErr := "could not write permits.json"
		asker := &fakePermitAsker{names: []string{"tower"}, ask: true, err: errors.New(wantErr)}
		a, _ := liveHomeRig(t, "dev_tower")
		a.permitAsk = asker
		a.closeHome()
		drain(t, a, a.openHome())

		drive(t, a, key("1"))
		if a.home.msg != wantErr {
			t.Fatalf("Home foot message = %q, want %q", a.home.msg, wantErr)
		}
		if _, ok := a.homeAsking(); ok {
			t.Fatal("card remained open after Answer error")
		}
		a.refreshHome()
		a.rebuildMachines()
		if _, ok := a.homeAsking(); ok {
			t.Fatal("card was raised again during the same Home visit after Answer error")
		}
		a.closeHome()
		drain(t, a, a.openHome())
		if _, ok := a.homeAsking(); !ok {
			t.Fatal("card was not raised when Home reopened after an Answer error")
		}
	})
}

// V14/V25/T13 (Part 3): the permit card is never raised when PermitAsk is nil,
// never on a hosted window, and never over another open card.
func TestHomeAsksOnceNotOverAnotherCardNotWhenNilAndNotOnAHostedWindow(t *testing.T) {
	t.Run("nil PermitAsk", func(t *testing.T) {
		a, _ := liveHomeRig(t, "dev_tower")
		if _, ok := a.homeAsking(); ok {
			t.Fatal("Home raised a card with PermitAsk == nil")
		}
	})

	t.Run("hosted window", func(t *testing.T) {
		asker := &fakePermitAsker{names: []string{"tower"}, ask: true}
		a := newApp(context.Background(), Options{
			Agent:     &fakeAgent{model: "m"},
			Host:      "remote-box",
			PermitAsk: asker,
		})
		a.width, a.height = 200, 70
		drain(t, a, a.openHome())
		a.refreshHome()
		if asker.askCalls != 0 {
			t.Fatalf("hosted window called AskOnce %d times", asker.askCalls)
		}
		if _, ok := a.homeAsking(); ok {
			t.Fatal("hosted window raised the permit card")
		}
	})

	t.Run("never over another open card", func(t *testing.T) {
		a, _ := liveHomeRig(t, "dev_tower")
		standOn(t, a, "c1")
		ask, ok := a.homeAsking()
		if !ok || ask.question.Head != moveAsk {
			t.Fatalf("expected Move card to be open, got ok=%t question=%+v", ok, ask.question)
		}

		asker := &fakePermitAsker{names: []string{"tower"}, ask: true}
		a.permitAsk = asker
		a.refreshHome()
		a.rebuildMachines()

		ask, ok = a.homeAsking()
		if !ok || ask.question.Head != moveAsk {
			t.Fatalf("refreshing Home overwrote the open Move card: ok=%t question=%+v", ok, ask.question)
		}
		if asker.askCalls != 0 {
			t.Fatalf("AskOnce was called while another card was open: %d", asker.askCalls)
		}
	})
}

// V27 (Part 4): a Home row for an offline computer shows `offline · last seen 14:02`
// when the roster knows its LastSeen timestamp and plain `offline` when not.
func TestAnOfflineRowOnHomeSaysWhenItWasLastSeenOnlyWhenKnown(t *testing.T) {
	a, _ := liveHomeRig(t)
	now := time.Date(2026, time.November, 23, 16, 30, 0, 0, time.Local)
	seen := time.Date(2026, time.November, 23, 14, 2, 0, 0, time.Local)
	a.clock = func() time.Time { return now }
	a.home.world.Read = now

	a.devRow.devices = []chatlist.Device{
		{ID: "dev_me", Name: "This Mac", Self: true},
		{ID: "dev_desk", Name: "desk", LastSeen: seen},
		{ID: "dev_plain", Name: "plain"},
	}
	a.fileMachines(homeMachinesMsg{
		rows: []chatlist.Row{
			{Cell: "c-desk", Title: "Desk work", Device: "desk", DeviceID: "dev_desk", Status: chatlist.Off},
			{Cell: "c-plain", Title: "Plain work", Device: "plain", DeviceID: "dev_plain", Status: chatlist.Off},
		},
	})
	a.rebuildMachines()

	screen := homeText(a)
	if !strings.Contains(screen, "desk offline · last seen 14:02") {
		t.Fatalf("Home screen lacked 'desk offline · last seen 14:02':\n%s", screen)
	}
	if !strings.Contains(screen, "plain offline") || strings.Contains(screen, "plain offline · last seen") {
		t.Fatalf("Home screen did not draw plain 'plain offline' for unknown LastSeen:\n%s", screen)
	}
}
