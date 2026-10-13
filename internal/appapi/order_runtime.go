package appapi

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/standing"
)

type orderRuntime struct {
	sync.Mutex
	wake chan struct{}
	seen map[string]uint64
	runs map[string]string
}

// The app owns a credential-bearing ticker even when every agent tab is closed.
// Publication also observes OS passes and proposals from child engines; the
// native tick flock prevents two app launches from executing the same pass.
func (s *Server) startOrderWatch() {
	a := s.options.Agents
	if a == nil {
		return
	}
	if s.inbox != nil {
		s.SetInboxSource("standing", &orderInboxSource{s: s})
	}
	s.orders.wake = make(chan struct{}, 1)
	// The server owns cancellation and joins this writer before returning from Close.
	ctx := s.inbox.ctx
	s.watchers.Add(1)
	go func() {
		defer s.watchers.Done()
		clock := time.NewTicker(time.Second)
		defer clock.Stop()
		next := time.Now()
		for {
			run := false
			select {
			case <-ctx.Done():
				return
			case <-a.done:
				return
			case <-s.orders.wake:
				run = true
			case <-clock.C:
				if !time.Now().Before(next) {
					run = true
				}
			}
			s.events.mu.Lock()
			closed := s.events.closed
			s.events.mu.Unlock()
			if closed {
				return
			}
			if run && s.options.StandingTick != nil {
				store, err := standing.Open(filepath.Join(a.home, "v3", "standing"))
				if err == nil {
					_ = s.options.StandingTick(ctx, store)
				}
				next = time.Now().Add(standing.Interval)
			}
			_ = s.PublishOrders()
		}
	}()
}

func (s *Server) wakeOrders() {
	if s.orders.wake != nil {
		select {
		case s.orders.wake <- struct{}{}:
		default:
		}
	}
}

// PublishOrders exposes a synchronous producer door for deterministic tests.
// The revision fence makes repeated samples quiet while run ids survive restart.
func (s *Server) PublishOrders() error {
	a := s.options.Agents
	if a == nil {
		return nil
	}
	s.orders.Lock()
	defer s.orders.Unlock()
	store, err := standing.Open(filepath.Join(a.home, "v3", "standing"))
	if err != nil {
		return err
	}
	items, err := store.List()
	if err != nil {
		return err
	}
	if s.orders.seen == nil {
		s.orders.seen = map[string]uint64{}
		s.orders.runs = map[string]string{}
	}
	for _, item := range items {
		if item.App == nil {
			continue
		}
		if s.orders.seen[item.ID] == uint64(item.Revision) {
			continue
		}
		s.publishOrder(item)
		for _, run := range item.App.Runs {
			key := orderAskID(item.ID, run.ID)
			if s.orders.runs[key] == run.State {
				continue
			}
			if run.State == "waiting" {
				if err := s.orderNotice(item, run.ID+"_ask", item.NeedsPerson); err != nil {
					return err
				}
				_, _ = s.events.Publish(EventOrderFired, OrderFiredEvent{Order: item.ID, Run: run.ID, Ask: "ask:" + key})
			} else if run.State == "skipped_paused" {
				if err := s.orderNotice(item, run.ID+"_skipped", run.Line); err != nil {
					return err
				}
			} else if run.State == "done" {
				text := "Standing order ran · " + item.Title()
				if run.Report != "" {
					text += "\n" + run.Report
				}
				if err := s.orderNotice(item, run.ID+"_done", text); err != nil {
					return err
				}
				ask := ""
				if run.Report != "" {
					ask = "ask:" + orderAskID(item.ID, run.ID+"_report")
				}
				_, _ = s.events.Publish(EventOrderFired, OrderFiredEvent{Order: item.ID, Run: run.ID, Ask: ask})
			}
			s.orders.runs[key] = run.State
		}
		if isOrderReminder(item) && item.RetiredWhy == "fired" {
			if err := s.orderNotice(item, "reminder", "Reminder · "+item.Title()+"\n"+item.Does.Say); err != nil {
				return err
			}
			_, _ = s.events.Publish(EventOrderFired, OrderFiredEvent{Order: item.ID, Run: "reminder"})
		}
		s.orders.seen[item.ID] = uint64(item.Revision)
	}
	return nil
}

// OrderAsks gives Inbox the durable waiting set without borrowing its handler.
// Delivery scope affects chat addresses; every order still has an Inbox copy.
func (a *AgentRegistry) OrderAsks() ([]Ask, error) {
	store, err := standing.Open(filepath.Join(a.home, "v3", "standing"))
	if err != nil {
		return nil, err
	}
	items, err := store.List()
	if err != nil {
		return nil, err
	}
	out := []Ask{}
	for _, item := range items {
		if item.App == nil {
			continue
		}
		out = append(out, orderReportAsks(item)...)
		kind, run, title := "order_ask", "", item.Title()
		options := []AskOption{}
		if item.Status == standing.StatusProposed {
			run = "proposal"
			kind = "proposal"
			options = []AskOption{{ID: "ratify", Label: "Ratify"}, {ID: "not_now", Label: "Not now"}}
		} else if item.Status == standing.StatusActive && item.AppWaitingRun() != nil && item.AppWaitingRun().State == "waiting" {
			run = item.AppWaitingRun().ID
			title = item.NeedsPerson
			options = []AskOption{{ID: "go", Label: "Go ahead"}, {ID: "skip", Label: "Skip this one"}, {ID: "pause", Label: "Pause order"}}
		} else if item.Status == standing.StatusActive && item.NeedsPerson != "" {
			kind, run, title = "budget", "budget", item.NeedsPerson
			options = []AskOption{{ID: "raise", Label: "Raise the budget per run"}, {ID: "pause", Label: "Pause order"}}
		} else if isOrderReminder(item) && item.RetiredWhy == "fired" {
			kind, run = "reminder", "reminder"
		} else {
			continue
		}
		source := SourceRef{Kind: "order", ID: item.ID}
		out = append(out, Ask{ID: orderAskID(item.ID, run), Kind: kind, Source: source, Path: []SourceRef{{Kind: "agent", ID: item.Origin.SessionID}}, Title: title, Options: options, State: "waiting", Before: []DisplayEntry{{Kind: "notice", Text: item.Title()}, {Kind: "notice", Text: item.When.Words}}})
	}
	return out, nil
}

// Notifications are saved as engine entries without waking a model. Per-chat
// identities prevent polling or a restart from adding another receipt.
func (s *Server) orderNotice(item standing.Item, run, text string) error {
	if item.App.Scope == "not_here" {
		return nil
	}
	a := s.options.Agents
	a.mu.Lock()
	defer a.mu.Unlock()
	type notice struct {
		agent string
		entry DisplayEntry
	}
	entries := []notice{}
	err := a.disk(func(d *agentDisk) error {
		for id, record := range d.Records {
			if record.Agent.State == "archived" {
				continue
			}
			switch item.App.Scope {
			case "this_chat":
				if id != item.Origin.SessionID {
					continue
				}
			case "this_crew":
				if record.Agent.CrewID != item.App.CrewID {
					continue
				}
			}
			receiptID := orderAskID(item.ID, run)
			seen := false
			for _, saved := range record.Receipts {
				if saved.Entry.ID == receiptID {
					seen = true
					break
				}
			}
			if seen {
				continue
			}
			entry := a.turnReceipt(&record, text, &SourceRef{Kind: "order", ID: item.ID})
			entry.ID, entry.By, entry.Kind = receiptID, "codeaf", "notice"
			record.Receipts[len(record.Receipts)-1].Entry = entry
			d.Records[id] = record
			entries = append(entries, notice{id, entry})
		}
		return nil
	}, true)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		_, _ = s.events.Publish(EventTurnEntry, TurnEntryEvent{Agent: entry.agent, Entry: entry.entry})
	}
	return nil
}

// Completed reports remain readable in Inbox even when scope suppresses every
// chat delivery. Per-person Done belongs to the app; Not right is an engine
// decision that consumes the report and resets this order's trust.
func orderReportAsks(item standing.Item) []Ask {
	out := []Ask{}
	for _, run := range item.App.Runs {
		if run.State != "done" || run.Report == "" {
			continue
		}
		source := SourceRef{Kind: "order", ID: item.ID}
		title := "Standing order ran · " + item.Title()
		out = append(out, Ask{ID: orderAskID(item.ID, run.ID+"_report"), Kind: "order_ask", Source: source, Path: []SourceRef{{Kind: "agent", ID: item.Origin.SessionID}}, Title: title, Options: []AskOption{{ID: "not_right", Label: "Not right"}}, State: "waiting", Before: []DisplayEntry{{Kind: "notice", Text: title}, {Kind: "notice", Text: run.Report}}})
	}
	return out
}
