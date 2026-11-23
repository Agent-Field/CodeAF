package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

type teamStartReceiptConv struct {
	*fakeConversation
	mu  sync.Mutex
	ids []string
}

func (c *teamStartReceiptConv) SubmitMessage(ctx context.Context, m session.Submission) (<-chan session.Event, error) {
	c.mu.Lock()
	c.ids = append(c.ids, m.ClientID)
	c.mu.Unlock()
	return c.fakeConversation.SubmitMessage(ctx, m)
}

func (c *teamStartReceiptConv) ComposedKnown() bool { return true }

func (c *teamStartReceiptConv) SubmitComposed(ctx context.Context, m session.Submission) (session.SubmissionReceipt, <-chan session.Event, error) {
	events, err := c.SubmitMessage(ctx, m)
	return session.SubmissionReceipt{MessageID: "msg-day-cap", Delivery: session.DeliveryText}, events, err
}

// A team start's goal refused by the day cap and then run by raise_today: is
// it still correlatable by the client_id the start answered with?
func TestCrewStartDayCapRaiseKeepsClientID(t *testing.T) {
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	h := newStartHarness(t)
	var rc *teamStartReceiptConv
	inner := h.b.open
	h.b.open = func(ask appOpenAsk) (*appConn, error) {
		conn, err := inner(ask)
		if err == nil {
			rc = &teamStartReceiptConv{fakeConversation: conn.Conv.(*fakeConversation)}
			conn.Conv = rc
		}
		return conn, err
	}
	h.b.today = func(time.Time) float64 { return 40 }
	cap := 40.0
	if _, err := h.b.SetSettings(appwire.SettingsPatch{DayCapUSD: &appwire.Figure{USD: &cap}}); err != nil {
		t.Fatal(err)
	}
	got, err := h.b.CrewStart(appwire.CrewCreate{Name: "D", StartManager: true, ManagerStartsMembers: true, RequestID: "d-1", Goal: "capped goal", ClientID: "c-day"})
	if err != nil {
		t.Fatal(err)
	}
	i := h.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		o, ok := data.(appwire.AskOpened)
		return ok && o.Ask.Subkind == appwire.SubkindBudget
	})
	h.rec.mu.Lock()
	ask := h.rec.data[i].(appwire.AskOpened).Ask
	h.rec.mu.Unlock()
	if err := h.b.AnswerAsk(got.Start.Manager, ask.ID, appwire.OptionRaiseToday, "60"); err != nil {
		t.Fatal(err)
	}
	var user appwire.Entry
	h.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.EntryAdded)
		if ok && d.Entry.Kind == appwire.EntryUser && d.Entry.Agent == got.Start.Manager {
			user = d.Entry
			return true
		}
		return false
	})
	var ids []string
	if rc != nil {
		rc.mu.Lock()
		ids = append(ids, rc.ids...)
		rc.mu.Unlock()
	}
	t.Logf("after raise: entry client_id=%q, conversation saw client ids %q, sends=%d", user.ClientID, ids, reviewGoalSends(h, "capped goal"))
	if user.ClientID != "c-day" {
		t.Errorf("the goal ran after raise_today without the start's client_id (entry %q, host saw %q)", user.ClientID, ids)
	}
	if len(ids) != 1 || ids[0] != "c-day" || reviewGoalSends(h, "capped goal") != 1 {
		t.Fatalf("the actual composed hand-over must retain the id exactly once: ids=%q sends=%d", ids, reviewGoalSends(h, "capped goal"))
	}
}
