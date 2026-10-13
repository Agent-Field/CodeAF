package appapi

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var orderContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/orders", Request: "", Response: "OrdersResponse"}, handle: (*Server).orderHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/ratify", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/not-now", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/pause", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/resume", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/stop", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/change", Request: "ChangeOrderRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/orders/pause-all", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/runs/{rid}", Request: "OrderRunRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/reminders/{oid}/change", Request: "ReminderChangeRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/reminders/{oid}/cancel", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler},
}

// Orders use the native store and its per-item flock. The registry lock only
// protects the owner's receipt and connection; it never substitutes for that flock.
func (s *Server) orderHandler(w http.ResponseWriter, r *http.Request) {
	if len(r.Header.Values("Idempotency-Key")) > 1 {
		fail(w, 422, "bad_input", "use one Idempotency-Key")
		return
	}
	s.idempotent(s.serveOrder)(w, r)
}

func (s *Server) serveOrder(w http.ResponseWriter, r *http.Request) {
	a := s.options.Agents
	if a == nil {
		fail(w, 503, "not_allowed", "the agent registry is unavailable")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		fail(w, 503, "not_allowed", "the engine is stopping")
		return
	}
	store, err := standing.Open(filepath.Join(a.home, "v3", "standing"))
	if err != nil {
		agentFailure(w, err)
		return
	}
	a.events = s.events
	if r.PathValue("id") != "" {
		s.agentOrders(w, r, a, store)
		return
	}
	oid := r.PathValue("oid")
	item, err := store.Get(oid)
	if err != nil {
		orderFailure(w, err)
		return
	}
	action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	reminder := strings.HasPrefix(r.URL.Path, "/app/v1/reminders/")
	var change ChangeOrderRequest
	var run OrderRunRequest
	var at ReminderChangeRequest
	var plain ExpectRequest
	var expect *AgentExpect
	switch {
	case r.PathValue("rid") != "":
		if !decodeCrew(w, r, &run) {
			return
		}
		expect = run.Expect
		action = "run"
	case action == "change" && reminder:
		if !decodeCrew(w, r, &at) {
			return
		}
		expect = at.Expect
	case action == "change":
		if !decodeCrew(w, r, &change) {
			return
		}
		expect = change.Expect
	default:
		if !decodeCrew(w, r, &plain) {
			return
		}
		expect = plain.Expect
	}
	var receipt DisplayEntry
	err = a.disk(func(d *agentDisk) error {
		record, ok := d.Records[item.Origin.SessionID]
		if !ok {
			return errAgentMissing
		}
		updated, e := store.ChangeApp(oid, func(current *standing.Item) error {
			if e := orderExpect(*current, expect); e != nil {
				return e
			}
			if reminder && !isOrderReminder(*current) {
				return agentError(422, "bad_input", "this order is not a reminder")
			}
			if current.Status == standing.StatusRetired || current.Status == standing.StatusDeclined {
				if action != "run" || run.Verb != "not_right" {
					return agentError(409, "conflict", "this standing order has ended")
				}
			}
			if current.App == nil {
				current.App = &standing.AppOrder{Scope: "this_chat"}
			}
			switch action {
			case "ratify":
				if current.Status != standing.StatusProposed {
					return standing.ErrAppConflict
				}
				if current.When.Kind == standing.WhenAt && !current.When.At.After(a.now()) {
					return orderPast()
				}
				current.Status = standing.StatusActive
				if current.When.Kind == standing.WhenEvery {
					next, e := standing.ParseEvery(current.When.Every)
					if e != nil {
						return e
					}
					current.NextDue = next(a.now())
				} else if current.When.Kind == standing.WhenAt {
					current.NextDue = current.When.At
				}
			case "not-now":
				if current.Status != standing.StatusProposed {
					return standing.ErrAppConflict
				}
				current.Status = standing.StatusDeclined
			case "pause":
				if current.Status != standing.StatusActive {
					return standing.ErrAppConflict
				}
				current.Status = standing.StatusPaused
			case "resume":
				if current.Status != standing.StatusPaused {
					return standing.ErrAppConflict
				}
				current.Status = standing.StatusActive
			case "stop", "cancel":
				if current.Status == standing.StatusProposed {
					return standing.ErrAppConflict
				}
				current.Status, current.RetiredWhy = standing.StatusRetired, "stopped by you"
			case "change":
				if reminder {
					if !at.At.After(a.now()) {
						return orderPast()
					}
					current.When.At, current.NextDue = at.At, at.At
					current.When.Words = at.At.Format(time.RFC3339)
				} else if e := editOrder(current, change, record.Agent.CrewID, a.now()); e != nil {
					return e
				}
			case "run":
				if !validOrderVerb(run.Verb) {
					return agentError(422, "bad_input", "unknown standing run verb")
				}
				if isOrderReminder(*current) {
					return agentError(422, "bad_input", "reminders have no trust step")
				}
				return standing.DecideAppRun(current, r.PathValue("rid"), run.Verb)
			default:
				return agentError(422, "bad_input", "unknown standing order action")
			}

			if current.Status == standing.StatusPaused || current.Status == standing.StatusRetired {
				discardPendingOrderRun(current)
			}
			return nil
		})
		if e != nil {
			return e
		}
		item = updated
		text := orderReceipt(item, action, run.Verb)
		receipt = a.turnReceipt(&record, text, &SourceRef{Kind: "order", ID: oid})
		d.Records[record.Agent.ID] = record
		return nil
	}, true)
	if err != nil {
		orderFailure(w, err)
		return
	}
	if action == "ratify" && item.When.Kind == standing.WhenFile {
		armed, e := store.Arm(item.ID)
		if e == nil {
			item = armed
		} else {
			_ = store.NoteNeedsPerson(item.ID, standing.NeedsBaselineLead+": "+e.Error())
		}
	}
	if action == "ratify" || action == "not-now" {
		a.resolveOrderProposal(item, action == "ratify")
	}
	s.publishOrder(item)
	a.publishTurn(EventTurnEntry, TurnEntryEvent{Agent: item.Origin.SessionID, Entry: receipt})
	if action == "ratify" || action == "resume" {
		s.wakeOrders()
	}
	if action == "run" {
		if run.Verb == "go" || run.Verb == "run_now" {
			s.wakeOrders()
		}
	}
	write(w, 200, ActionResponse{OK: true, Receipt: &receipt})
}

func (s *Server) agentOrders(w http.ResponseWriter, r *http.Request, a *AgentRegistry, store *standing.Store) {
	id := r.PathValue("id")
	var expect ExpectRequest
	if r.Method == "POST" && !decodeCrew(w, r, &expect) {
		return
	}
	result := OrdersResponse{Waiting: []StandingOrder{}, Active: []StandingOrder{}, Paused: []StandingOrder{}, Reminders: []StandingOrder{}}
	changed := []standing.Item{}
	var receipt DisplayEntry
	err := a.disk(func(d *agentDisk) error {
		record, ok := d.Records[id]
		if !ok {
			return errAgentMissing
		}
		if r.Method == "POST" {
			if e := turnExpectation(a.view(record), expect.Expect); e != nil {
				return e
			}
		}
		items, err := store.List()
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.Origin.SessionID != id {
				continue
			}
			if r.Method == "POST" {
				if item.Status != standing.StatusActive || isOrderReminder(item) {
					continue
				}
				updated, e := store.ChangeApp(item.ID, func(current *standing.Item) error {
					if current.Status != standing.StatusActive {
						return standing.ErrAppConflict
					}
					current.Status = standing.StatusPaused
					discardPendingOrderRun(current)
					return nil
				})
				if e != nil {
					return e
				}
				changed = append(changed, updated)
				continue
			}
			wire := orderView(item)
			if !orderMatches(wire, r.URL.Query().Get("q")) {
				continue
			}
			switch {
			case item.Status == standing.StatusProposed:
				result.Waiting = append(result.Waiting, wire)
			case isOrderReminder(item) && item.Status != standing.StatusDeclined && item.RetiredWhy != "stopped by you":
				result.Reminders = append(result.Reminders, wire)
			case item.Status == standing.StatusActive:
				result.Active = append(result.Active, wire)
			case item.Status == standing.StatusPaused:
				result.Paused = append(result.Paused, wire)
			}
		}
		if r.Method == "POST" {
			receipt = a.turnReceipt(&record, fmt.Sprintf("Paused %d standing orders · you", len(changed)), &SourceRef{Kind: "agent", ID: id})
			d.Records[id] = record
		}
		return nil
	}, r.Method == "POST")
	if err != nil {
		orderFailure(w, err)
		return
	}
	if r.Method == "GET" {
		write(w, 200, result)
		return
	}
	for _, item := range changed {
		s.publishOrder(item)
	}
	a.publishTurn(EventTurnEntry, TurnEntryEvent{Agent: id, Entry: receipt})
	write(w, 200, ActionResponse{OK: true, Receipt: &receipt})
}

func orderExpect(item standing.Item, expect *AgentExpect) error {
	if expect != nil && ((expect.State != nil && *expect.State != orderView(item).State) || (expect.Version != nil && *expect.Version != uint64(item.Revision))) {
		return agentError(409, "stale", "the standing order changed; read it again before acting")
	}
	return nil
}
func orderPast() error { return agentError(422, "bad_input", "That time has passed. Pick another.") }
func orderFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, standing.ErrNotFound) {
		fail(w, 404, "not_found", "no such standing order or run")
		return
	}
	if errors.Is(err, standing.ErrAppConflict) {
		fail(w, 409, "conflict", err.Error())
		return
	}
	var reply *agentReplyError
	if errors.As(err, &reply) || errors.Is(err, errAgentMissing) {
		agentFailure(w, err)
		return
	}
	fail(w, 500, "not_allowed", "the standing order action could not be saved")
}

func editOrder(it *standing.Item, ask ChangeOrderRequest, crew string, now time.Time) error {
	if ask.Title == nil && ask.Trigger == nil && ask.Does == nil && ask.Wont == nil && ask.Scope == nil && ask.BudgetPerRun == nil {
		return agentError(422, "bad_input", "change needs at least one field")
	}
	if ask.Title != nil {
		title := strings.TrimSpace(*ask.Title)
		if title == "" || utf8.RuneCountInString(title) > 200 || strings.ContainsAny(title, "\n\r\x00") {
			return agentError(422, "bad_input", "title needs one line of 1 to 200 characters")
		}
		it.Brief.Title = title
	}
	if ask.Scope != nil {
		switch StandingScope(*ask.Scope) {
		case ScopeThisChat, ScopeEverywhere, ScopeNotHere:
			it.App.CrewID = ""
		case ScopeThisCrew:
			if crew == "" {
				return agentError(422, "bad_input", "This crew needs an agent in a crew")
			}
			it.App.CrewID = crew
		default:
			return agentError(422, "bad_input", "unknown standing order scope")
		}
		it.App.Scope = *ask.Scope
	}
	if ask.Does != nil {
		if strings.TrimSpace(*ask.Does) == "" {
			return agentError(422, "bad_input", "It does needs words")
		}
		if it.Does.Kind == standing.ActionTask {
			it.Does.Brief = *ask.Does
		} else {
			it.Does.Say = *ask.Does
		}
		it.App.ApprovedRuns = 0
	}
	if ask.Wont != nil {
		it.App.Wont = *ask.Wont
		it.App.ApprovedRuns = 0
	}
	if ask.BudgetPerRun != nil {
		if !agentMoney.MatchString(ask.BudgetPerRun.USD) {
			return agentError(422, "bad_input", "budget per run needs USD with two decimal places")
		}
		usd, err := strconv.ParseFloat(ask.BudgetPerRun.USD, 64)
		if err != nil || usd <= 0 || usd > 1000000 {
			return agentError(422, "bad_input", "budget per run must be positive and at most $1000000.00")
		}
		it.Rails.PerRunUSD = usd
		if strings.HasPrefix(it.NeedsPerson, "Budget per run reached") {
			it.NeedsPerson = ""
		}
	}
	if ask.Trigger != nil {
		trigger := *ask.Trigger
		when := standing.When{Words: trigger.Source, Hint: trigger.Filter}
		switch trigger.Kind {
		case "schedule":
			if _, err := standing.ParseEvery(trigger.Source); err != nil {
				return agentError(422, "bad_input", err.Error())
			}
			when.Kind, when.Every = standing.WhenEvery, trigger.Source
		case "once":
			at, err := time.Parse(time.RFC3339, trigger.Source)
			if err != nil {
				return agentError(422, "bad_input", "a once trigger needs an RFC3339 time")
			}
			if !at.After(now) {
				return orderPast()
			}
			when.Kind, when.At = standing.WhenAt, at
		case "event":
			switch {
			case strings.HasPrefix(trigger.Source, "file:"):
				when.Kind, when.Glob = standing.WhenFile, strings.TrimPrefix(trigger.Source, "file:")
			case strings.HasPrefix(trigger.Source, "command:"):
				when.Kind, when.Probe.Command, when.ProbeEvery = standing.WhenProbe, strings.TrimPrefix(trigger.Source, "command:"), standing.Interval
			default:
				return agentError(422, "bad_input", "an event trigger needs file:<glob> or command:<probe>; ask the agent to compile other conditions")
			}
		default:
			return agentError(422, "bad_input", "unknown standing trigger kind")
		}
		it.When, it.Fingerprint, it.Positive = when, "", ""
		it.NextDue = now
		if when.Kind == standing.WhenEvery {
			next, _ := standing.ParseEvery(when.Every)
			it.NextDue = next(now)
		}
		if when.Kind == standing.WhenAt {
			it.NextDue = when.At
		}
		it.App.ApprovedRuns = 0
	}
	// Editing the action consumes any old authorization; the person must approve
	// a new trigger rather than execute old evidence with a newly widened brief.
	if ask.Does != nil || ask.Wont != nil || ask.Trigger != nil {
		if pending := it.AppWaitingRun(); pending != nil {
			pending.State, pending.Evidence = "changed", ""
		}
		it.NeedsPerson = ""
	}
	if err := it.Validate(); err != nil {
		return agentError(422, "bad_input", err.Error())
	}
	return nil
}

func orderView(it standing.Item) StandingOrder {
	out := StandingOrder{ID: it.ID, AgentID: it.Origin.SessionID, Title: it.Title(), Does: it.Does.Say, Wont: it.Grant, Scope: "this_chat", Trust: "asks_first", BudgetPerRun: Money{USD: fmt.Sprintf("%.2f", it.Rails.PerRunUSD)}, State: string(it.Status), Version: uint64(it.Revision), Runs: []StandingRun{}}
	if it.Does.Kind == standing.ActionTask {
		out.Does = it.Does.Brief
	}
	if it.Status == standing.StatusRetired {
		out.State = "stopped"
		if it.RetiredWhy == "fired" {
			out.State = "fired"
		}
	}
	if !it.LastFired.IsZero() {
		out.LastRunAt = &it.LastFired
	}
	out.Trigger = OrderTrigger{Kind: "event", Source: it.When.Words, Filter: it.When.Hint}
	switch it.When.Kind {
	case standing.WhenAt:
		out.Trigger.Kind, out.Trigger.Source = "once", it.When.At.Format(time.RFC3339)
	case standing.WhenEvery:
		out.Trigger.Kind, out.Trigger.Source = "schedule", it.When.Every
	case standing.WhenFile:
		out.Trigger.Source = "file:" + it.When.Glob
	case standing.WhenProbe:
		if it.When.Probe.Command != "" {
			out.Trigger.Source = "command:" + it.When.Probe.Command
		}
	}
	if it.App != nil {
		out.Scope, out.Trust, out.ApprovedRuns, out.Wont, out.Skipped = it.App.Scope, it.AppTrust(), it.App.ApprovedRuns, it.App.Wont, it.App.Skipped
		for _, run := range it.App.Runs {
			out.Runs = append(out.Runs, StandingRun{ID: run.ID, At: run.At, State: run.State, Line: run.Line})
		}
	}
	return out
}
func orderMatches(order StandingOrder, query string) bool {
	for _, term := range strings.Fields(strings.ToLower(query)) {
		field, value, scoped := strings.Cut(term, ":")
		if scoped {
			switch field {
			case "state":
				if value != order.State {
					return false
				}
			case "scope":
				if value != order.Scope {
					return false
				}
			case "trust":
				if value != order.Trust {
					return false
				}
			default:
				return false
			}
		} else if !strings.Contains(strings.ToLower(order.Title+" "+order.Does+" "+order.Trigger.Source), term) {
			return false
		}
	}
	return true
}
func orderReceipt(item standing.Item, action, verb string) string {
	word := map[string]string{"ratify": "Ratified", "not-now": "Declined", "pause": "Paused", "resume": "Resumed", "stop": "Stopped", "cancel": "Cancelled", "change": "Changed"}[action]
	if action == "run" {
		word = map[string]string{"go": "Approved run", "run_now": "Approved run now", "skip": "Skipped this run", "pause": "Paused order", "not_right": "Not right · trust reset"}[verb]
	}
	return word + " · " + item.Title() + " · you"
}
func (s *Server) publishOrder(item standing.Item) {
	view := orderView(item)
	_, _ = s.events.Publish(EventOrderChanged, OrderChangedEvent{Agent: view.AgentID, Order: view.ID, State: view.State, Trust: view.Trust, ApprovedRuns: view.ApprovedRuns})
}
func orderAskID(order, run string) string { return "order_" + order + "_" + run }

// The saved card remains ratifiable after restart. A live proposer also gets
// its canonical answer so its blocked stand tool can continue with the result.
func (a *AgentRegistry) resolveOrderProposal(item standing.Item, approved bool) {
	live := a.live[item.Origin.SessionID]
	if live == nil {
		return
	}
	door, ok := live.conn.Engine.(interface {
		OpenQuestions() []session.Question
		ResolveStanding(uint64, session.StandingAnswer)
	})
	if !ok {
		return
	}
	for _, q := range door.OpenQuestions() {
		if q.Kind == session.QuestionStanding && q.Subject.Ref == item.ID {
			door.ResolveStanding(q.ID, session.StandingAnswer{Approved: approved})
			return
		}
	}
}

func validOrderVerb(verb string) bool {
	switch verb {
	case "go", "run_now", "skip", "pause", "not_right":
		return true
	}
	return false
}

// A pause or stop consumes the pending trigger so resume cannot revive an old
// authorization. A later trigger has to earn its own decision.
func discardPendingOrderRun(item *standing.Item) {
	if run := item.AppWaitingRun(); run != nil {
		run.State, run.Evidence = "skipped_paused", ""
		item.App.Skipped++
	}
	item.NeedsPerson = ""
}

func isOrderReminder(item standing.Item) bool {
	return item.When.Kind == standing.WhenAt && item.Does.Kind == standing.ActionSay
}
