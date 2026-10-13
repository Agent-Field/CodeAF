package appapi

import (
	"context"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/teams"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var crewWorkContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/wrap-up", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).crewWorkHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/keep-going", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).crewWorkHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/finish", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).crewWorkHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/reopen", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).crewWorkHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/archive", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).crewWorkHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/bring-in-helpers", Request: "CrewHelpersRequest", Response: "HelperProposal"}, handle: (*Server).crewWorkHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/crews/{cid}/report", Request: "", Response: "CrewReport"}, handle: (*Server).crewWorkHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/crews/{cid}/log", Request: "", Response: "CrewLogResponse"}, handle: (*Server).crewWorkHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/items", Request: "CrewItemRequest", Response: "CrewItem"}, handle: (*Server).crewWorkHandler},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/crews/{cid}/items/{ref...}", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).crewWorkHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/crews/{cid}/summary-since", Request: "", Response: "CrewSummaryResponse"}, handle: (*Server).crewWorkHandler},
}

// Crew work uses the same native document as membership. History is preserved
// on finish; only unlinking removes a reference, never the referenced work.
func (s *Server) crewWorkHandler(w http.ResponseWriter, r *http.Request) {
	s.idempotent(s.crewAppsAroundWork(s.serveCrewWork))(w, r)
}
func (s *Server) serveCrewWork(w http.ResponseWriter, r *http.Request) {
	a := s.options.Agents
	if a == nil {
		fail(w, 503, "not_allowed", "the agent registry is unavailable")
		return
	}
	if strings.HasSuffix(r.URL.Path, "/bring-in-helpers") {
		s.proposeSubCrew(w, r)
		return
	}
	cid := r.PathValue("cid")
	verb := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	if r.Method == "GET" {
		if err := s.RefreshCrewWork(r.Context()); err != nil {
			crewFailure(w, err)
			return
		}
		s.readCrewWork(w, r, a, cid, verb)
		return
	}
	var body ExpectRequest
	var item CrewItemRequest
	if r.Method == "POST" && verb == "items" {
		if !decodeCrew(w, r, &item) {
			return
		}
		body.Expect = item.Expect
		if err := validateCrewItem(item); err != nil {
			crewFailure(w, err)
			return
		}
	} else if !decodeCrew(w, r, &body) {
		return
	}
	if verb == "finish" {
		// The manager writes the report through its own billed engine if there is
		// none yet. Linked resources are checked before this AI call and at close.
		version, err := s.ensureCrewReport(r.Context(), cid, body.Expect)
		if err != nil {
			s.crewWorkFailure(w, err)
			return
		}
		body.Expect = &AgentExpect{Version: &version}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		fail(w, 503, "not_allowed", "the engine is stopping")
		return
	}
	var result Crew
	var receipt DisplayEntry
	var log *teams.Entry
	var changed []string
	var reportDecision, reportPacket string
	var resolvedEnd *Ask
	err := a.disk(func(d *agentDisk) error {
		return teams.UpdateClosing(a.home, func(f *teams.File) (string, string, error) {
			err := func() error {
				i := teams.Index(f.Teams, cid)
				if i < 0 {
					return crewMissing()
				}
				t := &f.Teams[i]
				meta, err := readCrewMeta(*t)
				if err != nil {
					return err
				}
				if err = crewExpect(*t, meta, body.Expect); err != nil {
					return err
				}
				work, err := readCrewWork(*t)
				if err != nil {
					return err
				}
				if r.Method == "DELETE" {
					if t.Closed() {
						return agentError(409, "conflict", "reopen the crew before unlinking items")
					}
					found := false
					kept := []CrewItem{}
					for _, linked := range work.Items {
						if linked.Ref == r.PathValue("ref") {
							found = true
						} else {
							kept = append(kept, linked)
						}
					}
					if !found {
						return agentError(404, "not_found", "no such linked item")
					}
					work.Items = kept
					changed = []string{"items"}
				} else {
					switch verb {
					case "items":
						if t.Closed() {
							return agentError(409, "conflict", "reopen the crew before linking items")
						}
						linked := CrewItem{Ref: item.Ref, URL: item.URL}
						if linked.Ref == "" {
							linked.Ref = "url_" + teams.NewID()
						}
						for _, existing := range work.Items {
							if existing.Ref == linked.Ref || linked.URL != "" && existing.URL == linked.URL {
								return agentError(409, "conflict", "already linked to this crew")
							}
						}
						if len(work.Items) >= 256 {
							return crewBad("a crew may link at most 256 items")
						}
						work.Items = append(work.Items, linked)
						item.Ref = linked.Ref
						changed = []string{"items"}
					case "wrap-up":
						resolvedEnd = work.EndAsk
						work.EndAsk = nil
						if t.Closed() {
							return agentError(409, "conflict", "reopen the crew before wrapping up")
						}
						if meta.State == "report_ready" {
							return agentError(409, "conflict", "the report is ready; finish the crew or keep going")
						}
						if t.Wrap == nil {
							if err = f.SetWrap(cid, a.now().UTC(), 15*time.Minute); err != nil {
								return err
							}
							entry := teams.WrapUpRequest("")
							entry.ToKey = t.Manager
							entry.At = a.now().UTC()
							log = &entry
						}
						meta.State = "wrapping_up"
						changed = []string{"state"}
					case "keep-going":
						resolvedEnd = work.EndAsk
						meta.EndsAt = nil
						if t.Closed() || meta.State == "active" {
							return agentError(409, "conflict", "the crew is not waiting to finish")
						}
						if err = f.ClearWrap(cid); err != nil {
							return err
						}
						meta.State = "active"
						reportDecision = teams.OptionKeepGoing
						reportPacket = work.ReportPacket
						meta.Report = nil
						work.EndAsk = nil
						entry := teams.Entry{Kind: teams.KindDirective, From: teams.FromYou, To: teams.ToManager, ToKey: t.Manager, Text: "Keep going. The crew stays open; continue the remaining work.", At: a.now().UTC()}
						log = &entry
						changed = []string{"state", "report"}
					case "finish":
						resolvedEnd = work.EndAsk
						work.EndAsk = nil
						if t.Closed() {
							return agentError(409, "conflict", "the crew is already closed")
						}
						if err = s.checkCrewOpenItems(r.Context(), f, *t, d, a); err != nil {
							return err
						}
						if meta.Report == nil {
							return agentError(409, "conflict", "the manager's report is not ready")
						}
						for _, ending := range append([]teams.Team{*t}, f.Descendants(t.ID)...) {
							em, err := readCrewMeta(ending)
							if err != nil {
								return err
							}
							for id, member := range em.Members {
								if member.EndSpent == nil {
									spent := a.view(d.Records[id]).Spent
									if spent == nil {
										spent = &Money{USD: "0.00"}
									}
									member.EndSpent = spent
									em.Members[id] = member
								}
							}
							if ending.ID == t.ID {
								meta = em
							} else if err = writeCrewMeta(&f.Teams[teams.Index(f.Teams, ending.ID)], em); err != nil {
								return err
							}
						}
						if err = s.stopFinishedCrew(r.Context(), f, *t, d, a); err != nil {
							return err
						}
						if err = f.Close(cid, a.now().UTC(), work.ReportPacket); err != nil {
							return err
						}
						meta.State = "closed"
						reportDecision = teams.OptionClose
						reportPacket = work.ReportPacket
						at := a.now().UTC()
						meta.ClosedAt = &at
						changed = []string{"state", "report", "members"}
					case "archive":
						if !t.Closed() {
							return agentError(409, "conflict", "finish the crew before archiving it")
						}
						meta.State = "archived"
						at := a.now().UTC()
						meta.ArchivedAt = &at
						changed = []string{"state", "archived_at"}
					case "reopen":
						if !t.Closed() {
							return agentError(409, "conflict", "the crew is already open")
						}
						if err = f.Reopen(cid); err != nil {
							return agentError(409, "conflict", err.Error())
						}
						meta.State = "active"
						meta.ClosedAt = nil
						meta.ArchivedAt = nil
						meta.BudgetAsk = nil
						work.EndAsk = nil
						for _, opened := range append([]teams.Team{*t}, f.Descendants(cid)...) {
							if opened.Closed() {
								continue
							}
							om, err := readCrewMeta(opened)
							if err != nil {
								return err
							}
							om.State = "active"
							om.ClosedAt = nil
							om.ArchivedAt = nil
							for id, member := range om.Members {
								if member.EndSpent != nil {
									end, _ := crewCents(*member.EndSpent)
									start := int64(0)
									if member.StartSpent != nil {
										start, _ = crewCents(*member.StartSpent)
									}
									spent := a.view(d.Records[id]).Spent
									current := int64(0)
									if spent != nil {
										current, _ = crewCents(*spent)
									}
									if current > end {
										member.StartSpent = &Money{USD: fmt.Sprintf("%d.%02d", (start+current-end)/100, (start+current-end)%100)}
									}
									member.EndSpent = nil
								}
								om.Members[id] = member
							}
							if opened.ID == t.ID {
								meta = om
							} else if err = writeCrewMeta(&f.Teams[teams.Index(f.Teams, opened.ID)], om); err != nil {
								return err
							}
							for _, native := range opened.Members {
								id := crewAgentID(d, native.Key)
								record, ok := d.Records[id]
								if !ok {
									continue
								}
								currentDepth := 0
								if assigned, ok := f.Team(record.Agent.CrewID); ok {
									currentDepth = len(f.Ancestors(assigned.ID)) + 1
								}
								if record.Agent.CrewID == "" || currentDepth < len(f.Ancestors(opened.ID))+1 {
									record.Agent.CrewID = opened.ID
									record.Agent.RoleID = om.Members[id].RoleID
									record.Agent.Version++
									d.Records[id] = record
								}
							}
						}
						for id, m := range meta.Members {
							if m.State == "paused" {
								m.State = "idle"
								meta.Members[id] = m
							}
						}
						changed = []string{"state", "members"}
					default:
						return crewBad("unknown crew action")
					}
				}
				meta.Version++
				if err = writeCrewMeta(t, meta); err != nil {
					return err
				}
				if err = writeCrewWork(t, work); err != nil {
					return err
				}
				result, _, err = crewView(f, *t, d, a)
				at := a.now().UTC()
				receipt = DisplayEntry{Kind: "receipt", Text: "Crew " + verb, By: "you", At: &at, Link: &SourceRef{Kind: "crew", ID: cid}}
				return err
			}()
			return reportPacket, reportDecision, err
		})
	}, true)
	if err != nil {
		s.crewWorkFailure(w, err)
		return
	}

	if log != nil {
		if err = teams.AppendTraffic(a.home, cid, *log); err != nil {
			crewFailure(w, err)
			return
		}
	}
	if resolvedEnd != nil {
		_, _ = s.Events().Publish(EventAskResolved, AskEvent{Ask: resolvedEnd.ID, Kind: resolvedEnd.Kind, How: verb, By: "you"})
	}
	s.crewChanged(cid, changed...)
	_, _ = s.Events().Publish(EventTurnEntry, TurnEntryEvent{Agent: result.ManagerID, Entry: receipt})
	if r.Method == "POST" && verb == "items" {
		write(w, 201, CrewItem{Ref: item.Ref, URL: item.URL})
		return
	}
	write(w, 200, ActionResponse{OK: true, Receipt: &receipt})
}
func validateCrewItem(item CrewItemRequest) error {
	if (item.Ref == "") == (item.URL == "") {
		return crewBad("link exactly one ref or URL")
	}
	if len(item.Ref) > 512 || strings.ContainsAny(item.Ref, "\x00\r\n") {
		return crewBad("invalid item reference")
	}
	if item.URL != "" {
		u, err := url.Parse(item.URL)
		if err != nil || u.Hostname() == "" || u.User != nil || u.Scheme != "https" && u.Scheme != "http" || len(item.URL) > 4096 {
			return crewBad("link an HTTP or HTTPS URL without credentials")
		}
	}
	return nil
}

type crewOpenItemsError struct{ count int }

func (e *crewOpenItemsError) Error() string { return fmt.Sprintf("%d items aren't done", e.count) }
func (s *Server) crewWorkFailure(w http.ResponseWriter, err error) {
	var open *crewOpenItemsError
	if errors.As(err, &open) {
		write(w, 409, map[string]any{"error": open.Error(), "reason": "conflict", "request_id": fmt.Sprintf("%08x", requestSequence.Add(1)), "open_items": open.count})
		return
	}
	crewFailure(w, err)
}
func (s *Server) checkCrewOpenItems(ctx context.Context, f *teams.File, t teams.Team, d *agentDisk, a *AgentRegistry) error {
	source, err := s.crewItemSource(a.home)
	if err != nil {
		return err
	}
	count := 0
	seen := map[string]bool{}
	for _, crew := range append([]teams.Team{t}, f.Descendants(t.ID)...) {
		if crew.Closed() {
			continue
		}
		work, err := readCrewWork(crew)
		if err != nil {
			return err
		}
		meta, err := readCrewMeta(crew)
		if err != nil {
			return err
		}
		if meta.Report != nil {
			for _, open := range meta.Report.LeftOpen {
				if open.HandOff == nil {
					count++
				}
			}
		}
		for _, item := range work.Items {
			done := false
			if source != nil {
				done, err = source.CrewItemDone(ctx, item)
				if err != nil {
					return err
				}
			}
			if !done {
				count++
			}
		}
		for _, m := range crew.Members {
			id := crewAgentID(d, m.Key)
			if seen[id] {
				continue
			}
			seen[id] = true
			record, ok := d.Records[id]
			if !ok {
				continue
			}
			live, err := a.connect(ctx, record)
			if err != nil {
				return err
			}
			for _, task := range live.conn.Engine.PlanTasks() {
				switch task.Status {
				case "done", "completed", "declined", "handed_off":
				default:
					count++
				}
			}
		}
	}
	if count > 0 {
		return &crewOpenItemsError{count: count}
	}
	return nil
}
func (s *Server) stopFinishedCrew(ctx context.Context, f *teams.File, t teams.Team, d *agentDisk, a *AgentRegistry) error {
	ids := map[string]bool{t.ID: true}
	for _, child := range f.Descendants(t.ID) {
		ids[child.ID] = true
	}
	stopped := map[string]bool{}
	for _, crew := range append([]teams.Team{t}, f.Descendants(t.ID)...) {
		for _, m := range crew.Members {
			id := crewAgentID(d, m.Key)
			if stopped[id] {
				continue
			}
			stopped[id] = true
			record, ok := d.Records[id]
			if !ok {
				continue
			}
			elsewhere := ""
			for _, other := range f.Teams {
				if !ids[other.ID] && !other.Closed() && other.Holds(m.Key) {
					elsewhere = other.ID
				}
			}
			if elsewhere != "" {
				other, _ := f.Team(elsewhere)
				om, err := readCrewMeta(other)
				if err != nil {
					return err
				}
				record.Agent.CrewID = elsewhere
				record.Agent.RoleID = om.Members[id].RoleID
				record.Agent.Version++
				d.Records[id] = record
				continue
			}
			live, err := a.connect(ctx, record)
			if err != nil {
				return err
			}
			if err = live.conn.Engine.StopWork(); err != nil {
				return err
			}
			live.running = false
			live.generation++
			record.Agent.CrewID = ""
			record.Agent.RoleID = ""
			record.Agent.State = "idle"
			record.Agent.Version++
			d.Records[id] = record
		}
	}
	return nil
}
func (s *Server) readCrewWork(w http.ResponseWriter, r *http.Request, a *AgentRegistry, cid, verb string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	err := a.disk(func(d *agentDisk) error {
		f, err := teams.Snapshot(a.home)
		if err != nil {
			return err
		}
		t, ok := f.Team(cid)
		if !ok {
			return crewMissing()
		}
		meta, err := readCrewMeta(t)
		if err != nil {
			return err
		}
		w.Header().Set("ETag", fmt.Sprintf("\"%d\"", crewRevision(t)))
		if verb == "report" {
			if meta.Report == nil {
				return agentError(409, "conflict", "the manager's report is not ready")
			}
			write(w, 200, meta.Report)
			return nil
		}
		entries, err := teams.ReadTraffic(a.home, cid, "", 0)
		if err != nil {
			return err
		}
		query := strings.ToLower(r.URL.Query().Get("q"))
		if verb == "summary-since" {
			at, err := time.Parse(time.RFC3339Nano, r.URL.Query().Get("at"))
			if err != nil {
				return crewBad("at must be an RFC3339 time")
			}
			if at.After(a.now()) {
				return crewBad("at cannot be in the future")
			}
			summary := CrewSummaryResponse{Since: at, Items: []SourceRef{}}
			if a.now().Sub(at) >= time.Hour {
				var text strings.Builder
				for _, e := range entries {
					if e.At.After(at) {
						fmt.Fprintf(&text, "%s → %s · %s\n", e.From, e.To, e.Text)
					}
				}
				summary.Text = strings.TrimSpace(text.String())
				if summary.Text != "" {
					summary.Items = append(summary.Items, SourceRef{Kind: "crew", ID: cid})
				}
			}
			write(w, 200, summary)
			return nil
		}
		before := r.URL.Query().Get("before")
		if before != "" {
			if len(before) != 12 {
				return crewBad("before must be a log cursor")
			}
			if _, err := strconv.ParseUint(before, 10, 64); err != nil {
				return crewBad("before must be a log cursor")
			}
		}
		response := CrewLogResponse{Entries: []CrewLogEntry{}}
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			if before != "" && e.ID >= before || !strings.Contains(strings.ToLower(e.From+" "+e.To+" "+e.Text), query) {
				continue
			}
			if len(response.Entries) == 100 {
				break
			}
			at := e.At
			response.Entries = append(response.Entries, CrewLogEntry{Crew: cid, From: e.From, To: e.To, Text: e.Text, At: &at})
			response.Before = e.ID
		}
		write(w, 200, response)
		return nil
	}, false)
	if err != nil {
		crewFailure(w, err)
	}
}
