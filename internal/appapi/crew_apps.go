package appapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/crewlink"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// appsTransport keeps the service handle with this server's guarded handler,
// so independent servers need no process-wide registry or cleanup hook.
type appsTransport struct {
	http.Handler
	services *svc.Services
}

// SetAppsServices binds apps before serving requests and returns the same handle
// for Mount. Later crew handlers reach their peers through AppsServices.
func (s *Server) SetAppsServices(all *svc.Services) *svc.Services {
	if bound, ok := s.guarded.(*appsTransport); ok {
		bound.services = all
	} else {
		s.guarded = &appsTransport{Handler: s.guarded, services: all}
	}
	return all
}

func (s *Server) AppsServices() *svc.Services {
	if bound, ok := s.guarded.(*appsTransport); ok {
		return bound.services
	}
	return nil
}

// crewLinkState is the finish door. svc.CrewLinks has no room for it; the
// concrete store and the handler fakes both implement it.
type crewLinkState interface {
	Finish(context.Context, string) error
	Reopen(context.Context, string) error
}

type crewViewBody struct {
	CrewResponse
	Items []any `json:"items"`
}

func (s *Server) writeCrewView(w http.ResponseWriter, r *http.Request, body CrewResponse) error {
	items, err := s.mergeRecordItems(r, body.ID, body.Items)
	if err != nil {
		return err
	}
	write(w, 200, crewViewBody{CrewResponse: body, Items: items})
	return nil
}

func (s *Server) crewAppsAroundWork(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.serveCrewRecordLink(w, r) {
			return
		}
		verb := crewPathVerb(r)
		if verb != "finish" && verb != "reopen" && !(r.Method == http.MethodGet && verb == "report") {
			next(w, r)
			return
		}
		rec := &replyWriter{header: make(http.Header)}
		next(rec, r)
		if rec.code/100 == 2 {
			if verb == "report" {
				s.enrichCrewReport(r, rec)
			} else if err := s.syncCrewLinks(r, verb); err != nil {
				rec.Reset()
				rec.code = 0
				writeLinkError(rec, err)
			}
		}
		flushReply(w, rec)
	}
}

func (s *Server) serveCrewRecordLink(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodDelete && strings.HasPrefix(r.PathValue("ref"), "app:") {
		s.deleteRecordLink(w, r)
		return true
	}
	if r.Method != http.MethodPost || crewPathVerb(r) != "items" {
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil {
		fail(w, 422, "bad_input", "needs one JSON object of at most 32 KiB")
		return true
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var probe struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(raw, &probe) != nil || probe.Kind != "record" {
		return false
	}
	s.addRecordLink(w, r, raw)
	return true
}

func (s *Server) addRecordLink(w http.ResponseWriter, r *http.Request, raw []byte) {
	var body struct {
		Kind   string       `json:"kind"`
		App    string       `json:"app"`
		Type   string       `json:"type"`
		ID     string       `json:"id"`
		Expect *AgentExpect `json:"expect,omitempty"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&body) != nil || body.Kind != "record" {
		fail(w, 422, "bad_input", "invalid action fields")
		return
	}
	if body.App == "" || body.Type == "" || body.ID == "" {
		fail(w, 422, "bad_input", "a record link needs app, type and id")
		return
	}
	links := s.crewLinks()
	if links == nil {
		fail(w, 503, "not_allowed", "record links are unavailable")
		return
	}
	cid := r.PathValue("cid")
	if err := s.crewReady(cid, body.Expect, false); err != nil {
		crewFailure(w, err)
		return
	}
	link, err := links.Add(r.Context(), svc.Scope{App: body.App, Caller: appsCaller()}, bridge.AddLinkParams{Crew: cid, Type: body.Type, ID: body.ID})
	if err != nil {
		writeLinkError(w, err)
		return
	}
	s.crewChanged(cid, "items")
	write(w, 201, s.recordItem(r.Context(), link))
}

func (s *Server) deleteRecordLink(w http.ResponseWriter, r *http.Request) {
	app, typ, id, ok := crewlink.ParseRecordRef(r.PathValue("ref"))
	if !ok {
		fail(w, 422, "bad_input", "invalid item reference")
		return
	}
	links := s.crewLinks()
	if links == nil {
		fail(w, 503, "not_allowed", "record links are unavailable")
		return
	}
	var body ExpectRequest
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil {
		fail(w, 422, "bad_input", "needs one JSON object of at most 32 KiB")
		return
	}
	if len(bytes.TrimSpace(raw)) != 0 && json.Unmarshal(raw, &body) != nil {
		fail(w, 422, "bad_input", "this action needs one JSON object")
		return
	}
	cid := r.PathValue("cid")
	if err := s.crewReady(cid, body.Expect, true); err != nil {
		crewFailure(w, err)
		return
	}
	if err := links.Remove(r.Context(), svc.Scope{App: app, Caller: appsCaller()}, bridge.RemoveLinkParams{Crew: cid, Type: typ, ID: id}); err != nil {
		writeLinkError(w, err)
		return
	}
	at := s.crewClock()
	receipt := DisplayEntry{Kind: "receipt", Text: "Crew items", By: "you", At: &at, Link: &SourceRef{Kind: "crew", ID: cid}}
	s.crewChanged(cid, "items")
	write(w, 200, ActionResponse{OK: true, Receipt: &receipt})
}

func (s *Server) mergeRecordItems(r *http.Request, cid string, base []CrewItem) ([]any, error) {
	items := make([]any, 0, len(base))
	for _, item := range base {
		items = append(items, item)
	}
	records, err := s.recordItems(r, cid)
	if err != nil {
		return nil, err
	}
	for _, item := range records {
		items = append(items, item)
	}
	return items, nil
}

func (s *Server) recordItems(r *http.Request, cid string) ([]bridge.CrewRecordItem, error) {
	links := s.crewLinks()
	if links == nil || cid == "" {
		return nil, nil
	}
	result, err := links.ForCrew(r.Context(), appsCaller(), bridge.LinksForCrewParams{Crew: cid})
	if err != nil {
		return nil, err
	}
	items := []bridge.CrewRecordItem{}
	for _, link := range result.Links {
		if link.State == bridge.CrewLinkUnlinked {
			continue
		}
		items = append(items, s.recordItem(r.Context(), link))
	}
	return items, nil
}

func (s *Server) recordItem(ctx context.Context, link bridge.CrewLink) bridge.CrewRecordItem {
	item := bridge.CrewRecordItem{
		Kind: "record", Ref: crewlink.RecordRef(link.App, link.Type, link.ID),
		App: link.App, AppName: link.AppName, Type: link.Type, ID: link.ID,
		Label: link.Label, State: link.State, Access: link.Access,
		Href: crewlink.RecordHref(link.App, link.Type, link.ID),
	}
	if link.Access != bridge.CrewLinkAccessOK {
		return item
	}
	label, detail := s.liveRecord(ctx, link)
	if label != "" {
		item.Label = label
	}
	item.Detail = detail
	return item
}

func (s *Server) liveRecord(ctx context.Context, link bridge.CrewLink) (string, string) {
	all := s.AppsServices()
	if all == nil || all.Records == nil {
		return "", ""
	}
	scope := svc.Scope{App: link.App, Caller: appsCaller(), Data: svc.DataSample}
	if all.Store != nil {
		if detail, err := all.Store.Get(ctx, link.App); err == nil && detail.State == bridge.StateKept {
			scope.Data = svc.DataReal
		}
	}
	rec, err := all.Records.Get(ctx, scope, bridge.GetRecordParams{Type: link.Type, ID: link.ID})
	if err != nil {
		return "", ""
	}
	var def *spec.Bundle
	if all.Store != nil {
		def, _ = all.Store.Definition(ctx, link.App)
	}
	return crewlink.Present(def, rec)
}

func (s *Server) enrichCrewReport(r *http.Request, rec *replyWriter) {
	var report CrewReport
	if json.Unmarshal(rec.Bytes(), &report) != nil || strings.Contains(report.Markdown, "## Everything it touched") {
		return
	}
	items, err := s.recordItems(r, r.PathValue("cid"))
	if err != nil || len(items) == 0 {
		return
	}
	report.Markdown += recordTouchSection(items)
	rec.Reset()
	_ = json.NewEncoder(rec).Encode(report)
}

func recordTouchSection(items []bridge.CrewRecordItem) string {
	var b strings.Builder
	b.WriteString("\n## Everything it touched\n\n")
	for _, item := range items {
		line := item.Label
		if item.Detail != "" {
			line = joinRecordLine(line, item.Detail)
		}
		name := item.AppName
		if name == "" {
			name = item.App
		}
		b.WriteString("- " + joinRecordLine(line, name+" · View") + "\n")
	}
	return b.String()
}

func joinRecordLine(left, right string) string {
	if left == "" {
		return right
	}
	return left + " · " + right
}

func (s *Server) syncCrewLinks(r *http.Request, verb string) error {
	all := s.AppsServices()
	if all == nil {
		return nil
	}
	door, ok := all.Crews.Links.(crewLinkState)
	if !ok {
		return nil
	}
	for _, id := range s.crewFamily(r.PathValue("cid")) {
		var err error
		switch verb {
		case "finish":
			err = door.Finish(r.Context(), id)
		case "reopen":
			err = door.Reopen(r.Context(), id)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) crewFamily(cid string) []string {
	ids := []string{}
	if cid != "" {
		ids = append(ids, cid)
	}
	a := s.options.Agents
	if a == nil || cid == "" {
		return ids
	}
	file, err := teams.Snapshot(a.home)
	if err != nil || file == nil {
		return ids
	}
	for _, child := range file.Descendants(cid) {
		ids = append(ids, child.ID)
	}
	return ids
}

func (s *Server) crewReady(cid string, expect *AgentExpect, unlinking bool) error {
	a := s.options.Agents
	if a == nil {
		return agentError(503, "not_allowed", "the agent registry is unavailable")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	file, err := teams.Snapshot(a.home)
	if err != nil {
		return err
	}
	team, ok := file.Team(cid)
	if !ok {
		return crewMissing()
	}
	meta, err := readCrewMeta(team)
	if err != nil {
		return err
	}
	if err := crewExpect(team, meta, expect); err != nil {
		return err
	}
	if !team.Closed() {
		return nil
	}
	if unlinking {
		return agentError(409, "conflict", "reopen the crew before unlinking items")
	}
	return agentError(409, "conflict", "reopen the crew before linking items")
}

func (s *Server) crewLinks() svc.CrewLinks {
	all := s.AppsServices()
	if all == nil {
		return nil
	}
	return all.Crews.Links
}

func (s *Server) crewClock() time.Time {
	if a := s.options.Agents; a != nil && a.now != nil {
		return a.now().UTC()
	}
	return time.Now().UTC()
}

func appsCaller() svc.Caller {
	return svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}
}

func writeLinkError(w http.ResponseWriter, err error) {
	var bridgeErr *bridge.Error
	if !errors.As(err, &bridgeErr) {
		crewFailure(w, err)
		return
	}
	status, reason := 500, "not_allowed"
	switch bridgeErr.Code {
	case bridge.CodeInvalid:
		status, reason = 422, "bad_input"
	case bridge.CodeNotFound:
		status, reason = 404, "not_found"
	case bridge.CodeDenied:
		status, reason = 409, "not_allowed"
	case bridge.CodeConflict:
		status, reason = 409, "conflict"
	}
	message := bridgeErr.Message
	if message == "" {
		message = "the crew action could not be saved"
	}
	fail(w, status, reason, message)
}

func crewPathVerb(r *http.Request) string {
	path := strings.TrimSuffix(r.URL.Path, "/")
	return path[strings.LastIndex(path, "/")+1:]
}

func flushReply(w http.ResponseWriter, rec *replyWriter) {
	for key, values := range rec.header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	code := rec.code
	if code == 0 {
		code = http.StatusOK
	}
	w.WriteHeader(code)
	_, _ = w.Write(rec.Bytes())
}
