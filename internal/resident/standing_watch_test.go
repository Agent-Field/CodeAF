package resident

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/watchdog"
)

type fakeStandingWatch struct {
	status     watchdog.Status
	installs   int
	uninstalls int
	err        error
}

func (watch *fakeStandingWatch) Install(context.Context) error {
	watch.installs++
	if watch.err == nil {
		watch.status.Installed = true
	}
	return watch.err
}

func (watch *fakeStandingWatch) Uninstall(context.Context) error {
	watch.uninstalls++
	if watch.err == nil {
		watch.status.Installed = false
	}
	return watch.err
}

func (watch *fakeStandingWatch) Status() (watchdog.Status, error) {
	return watch.status, watch.err
}

func TestFirstCharterRatificationOffersWatchAndYesInstallsIt(t *testing.T) {
	graph := openStore(t)
	charter := createProposedStandingCharter(t, graph, "charter-first", "standing-session")
	command, err := graph.RequestCommand(store.Command{
		SessionID: "standing-session", Kind: store.CommandCharterRatify,
		Target: charter.ID, Instruction: "yes, stand this up",
	})
	if err != nil {
		t.Fatal(err)
	}
	watch := &fakeStandingWatch{}
	reconciler := New(graph, nil, nil).WithStandingWatch(watch)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	settled, found, err := graph.CommandBySeq(command.Seq)
	if err != nil || !found || settled.Status != store.CommandApplied {
		t.Fatalf("ratification command = %+v found=%t err=%v", settled, found, err)
	}
	messages, err := graph.Messages("standing-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	questions := 0
	for _, message := range messages {
		if message.QuestionSeq != 0 {
			questions++
		}
	}
	if questions != 1 {
		t.Fatalf("standing watch questions = %d, messages=%+v", questions, messages)
	}

	enable, err := graph.RequestCommand(store.Command{
		SessionID: "standing-session", Kind: store.CommandStandingWatchEnable,
		Instruction: "yes, always",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if watch.installs != 1 {
		t.Fatalf("installs = %d, want 1", watch.installs)
	}
	settled, found, err = graph.CommandBySeq(enable.Seq)
	if err != nil || !found || settled.Status != store.CommandApplied {
		t.Fatalf("enable command = %+v found=%t err=%v", settled, found, err)
	}
	decision, err := graph.StandingWatchDecisionState()
	if err != nil || decision != store.StandingWatchEnabled {
		t.Fatalf("decision = %q err=%v", decision, err)
	}
	messages, err = graph.Messages("standing-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantReceipt := "I'll keep watch — a quiet check every few minutes, even with no terminal open."
	if messages[len(messages)-1].Body != wantReceipt {
		t.Fatalf("last receipt = %q, want %q", messages[len(messages)-1].Body, wantReceipt)
	}
}

func TestInstalledOrDeclinedWatchIsNeverOfferedAgain(t *testing.T) {
	for _, test := range []struct {
		name      string
		installed bool
		decline   bool
	}{
		{name: "already installed", installed: true},
		{name: "declined before restart", decline: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := openStore(t)
			first := createProposedStandingCharter(t, graph, "charter-one", "standing-session")
			if test.decline {
				if err := graph.SetCharterStatus(first.ID, store.CharterActive, store.Ratification{
					Origin: store.OriginUser, SessionID: "standing-session", Evidence: "yes",
				}); err != nil {
					t.Fatal(err)
				}
				if posted, err := graph.OfferStandingWatch("standing-session", first.ID); err != nil || !posted {
					t.Fatalf("seed offer posted=%t err=%v", posted, err)
				}
				if err := graph.RecordStandingWatchDecision(store.StandingWatchDeclined, "only while around"); err != nil {
					t.Fatal(err)
				}
			}
			second := createProposedStandingCharter(t, graph, "charter-two", "standing-session")
			command, err := graph.RequestCommand(store.Command{
				SessionID: "standing-session", Kind: store.CommandCharterRatify,
				Target: second.ID, Instruction: "yes",
			})
			if err != nil {
				t.Fatal(err)
			}
			watch := &fakeStandingWatch{status: watchdog.Status{Installed: test.installed}}
			if err := New(graph, nil, nil).WithStandingWatch(watch).Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			settled, _, _ := graph.CommandBySeq(command.Seq)
			if settled.Status != store.CommandApplied {
				t.Fatalf("ratification command = %+v", settled)
			}
			messages, err := graph.Messages("standing-session", 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			questions := 0
			for _, message := range messages {
				if message.QuestionSeq != 0 {
					questions++
				}
			}
			want := 0
			if test.decline {
				want = 1 // the seeded, already-decided question only
			}
			if questions != want {
				t.Fatalf("questions = %d, want %d", questions, want)
			}
		})
	}
}

func TestEnabledStandingWatchRepairsItselfOnResidentStart(t *testing.T) {
	graph := openStore(t)
	charter := createProposedStandingCharter(t, graph, "charter-repair", "standing-session")
	if err := graph.SetCharterStatus(charter.ID, store.CharterActive, store.Ratification{
		Origin: store.OriginUser, SessionID: "standing-session", Evidence: "yes",
	}); err != nil {
		t.Fatal(err)
	}
	if posted, err := graph.OfferStandingWatch("standing-session", charter.ID); err != nil || !posted {
		t.Fatalf("offer posted=%t err=%v", posted, err)
	}
	if err := graph.RecordStandingWatchDecision(store.StandingWatchEnabled, "yes, always"); err != nil {
		t.Fatal(err)
	}
	watch := &fakeStandingWatch{}
	if err := New(graph, nil, nil).WithStandingWatch(watch).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if watch.installs != 1 || !watch.status.Installed {
		t.Fatalf("repair installs=%d status=%+v", watch.installs, watch.status)
	}
}

func TestStandingWatchYesSurvivesTransientInstallFailure(t *testing.T) {
	graph := openStore(t)
	charter := createProposedStandingCharter(t, graph, "charter-retry", "standing-session")
	if err := graph.SetCharterStatus(charter.ID, store.CharterActive, store.Ratification{
		Origin: store.OriginUser, SessionID: "standing-session", Evidence: "yes",
	}); err != nil {
		t.Fatal(err)
	}
	if posted, err := graph.OfferStandingWatch("standing-session", charter.ID); err != nil || !posted {
		t.Fatalf("offer posted=%t err=%v", posted, err)
	}
	command, err := graph.RequestCommand(store.Command{
		SessionID: "standing-session", Kind: store.CommandStandingWatchEnable, Instruction: "yes, always",
	})
	if err != nil {
		t.Fatal(err)
	}
	watch := &fakeStandingWatch{err: errors.New("temporarily unavailable")}
	reconciler := New(graph, nil, nil).WithStandingWatch(watch)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	decision, err := graph.StandingWatchDecisionState()
	if err != nil || decision != store.StandingWatchEnabled {
		t.Fatalf("decision after failed install = %q err=%v", decision, err)
	}
	settled, _, _ := graph.CommandBySeq(command.Seq)
	if settled.Status != store.CommandRejected {
		t.Fatalf("failed install command = %+v", settled)
	}
	watch.err = nil
	reconciler.standingWatchCheck = time.Time{}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if watch.installs != 2 || !watch.status.Installed {
		t.Fatalf("retry installs=%d status=%+v", watch.installs, watch.status)
	}
}

func createProposedStandingCharter(t *testing.T, graph *store.Store, id, sessionID string) store.Charter {
	t.Helper()
	charter, err := store.NewCharter(id, "Keep release notes current.", store.WatchSpec{
		Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "new releases", Cadence: time.Hour},
	}, "Is there a new release?", store.CharterAction{Template: "Update release notes"}, store.CharterRails{
		PerFiringBudgetUSD: 0.10, MaxFiringsPerDay: 3,
	}, store.CharterProposed, store.Ratification{
		Origin: store.OriginUser, SessionID: sessionID, Evidence: "proposed in conversation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	return charter
}
