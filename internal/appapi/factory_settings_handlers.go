package appapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"net/http"
	"net/url"
	"strings"

	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

var factorySettingsContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/factories/{id}/settings", Response: "FactorySettingsDetails"}, handle: (*Server).factorySettingsHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/factories/{id}/repos/{repo_id}", Request: "FactoryRepoRequest", Response: "ActionResponse"}, handle: (*Server).canonicalSettingsHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/factories/{id}/links/{link_id}", Request: "EmptyRequest", Response: "ActionResponse"}, handle: (*Server).canonicalSettingsHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/factories/{id}/revoke-sessions", Request: "EmptyRequest", Response: "ActionResponse"}, handle: (*Server).factorySettingsHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/factories/{id}/advanced", Request: "FactoryAdvancedRequest", Response: "ActionResponse"}, handle: (*Server).canonicalSettingsHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/factories/{id}/runner", Request: "FactoryRunnerRequest", Response: "ActionResponse"}, handle: (*Server).canonicalSettingsHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/factories/solo", Response: "FactorySettingsDetails"}, handle: (*Server).factorySettingsHandler},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/factories/solo", Request: "PatchFactorySettingsRequest", Response: "FactorySettingsDetails"}, handle: (*Server).factorySettingsHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/factories/{id}", Response: "FactorySettingsDetails"}, handle: (*Server).factorySettingsHandler},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/factories/{id}", Request: "PatchFactorySettingsRequest", Response: "FactorySettingsDetails"}, handle: (*Server).factorySettingsHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/factories/{id}/sign-out", Request: "EmptyRequest", Response: "ActionResponse"}, handle: (*Server).factorySettingsHandler},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/factories/{id}/repos/{repo_id}", Request: "FactoryRepoRequest", Response: "ActionResponse"}, handle: (*Server).factorySettingsHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/factories/{id}/links", Request: "FactoryLinkRequest", Response: "FactoryLinkResponse"}, handle: (*Server).factorySettingsHandler},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/factories/{id}/links", Request: "DeleteFactoryLinkRequest", Response: "ActionResponse"}, handle: (*Server).factorySettingsHandler},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/factories/{id}/links/{link_id}", Response: "ActionResponse"}, handle: (*Server).factorySettingsHandler},
}

func (s *Server) factorySettingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == "" && r.URL.Path == "/app/v1/factories/solo" {
		r.SetPathValue("id", "solo")
	}
	s.idempotent(s.serveFactorySettings)(w, r)
}
func (s *Server) serveFactorySettings(w http.ResponseWriter, r *http.Request) {
	f := s.options.Factories
	if f == nil {
		fail(w, 503, "not_allowed", "factory connections are unavailable")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		fail(w, 503, "not_allowed", "the engine is stopping")
		return
	}
	rows, err := f.connections(nil)
	if err != nil {
		agentFailure(w, err)
		return
	}
	var row factoryConnection
	for _, old := range rows {
		if old.ID == r.PathValue("id") {
			row = old
		}
	}
	if row.ID == "" {
		fail(w, 404, "not_found", "factory not found")
		return
	}
	source := f.source(row)
	var reply any = ActionResponse{OK: true}
	status := 200
	switch {
	case r.Method == "GET":
		reply, err = f.settingsView(r.Context(), row)
	case strings.HasSuffix(r.URL.Path, "/sign-out"):
		var body EmptyRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		if row.Mode == "solo" {
			fail(w, 409, "not_allowed", "a solo factory belongs to this machine")
			return
		}
		if source.Token != "" {
			var me factoryapi.Me
			err = source.request(r.Context(), "GET", "/v1/me", nil, &me)
			if err == nil && me.SessionID != "" {
				err = source.request(r.Context(), "DELETE", "/v1/me/sessions/"+url.PathEscape(me.SessionID), nil, nil)
			}
		}
		if err == nil {
			if f.Credentials != nil {
				f.Credentials.SetFactorySession(row.URL, "")
			}
			s.SetInboxSource(row.ID, nil)
			delete(f.sourceTokens, row.ID)
		}
	case strings.HasSuffix(r.URL.Path, "/revoke-sessions"):
		var body EmptyRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		err = source.request(r.Context(), "POST", "/v1/access/revoke-sessions", EmptyRequest{}, nil)
	case r.PathValue("repo_id") != "":
		var body FactoryRepoRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		repo := r.PathValue("repo_id")
		if body.Enabled == nil || !store.RepoName(repo) {
			fail(w, 422, "bad_input", "needs a repository owner/name and enabled true or false")
			return
		}
		owner, name, _ := strings.Cut(repo, "/")
		err = source.request(r.Context(), "PUT", "/v1/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name), map[string]bool{"on": *body.Enabled}, nil)
	case r.Method == "POST":
		var body FactoryLinkRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		var created struct {
			Link factoryapi.AccessLink `json:"link"`
		}
		err = source.request(r.Context(), "POST", "/v1/access/links", factoryapi.CreateAccessLink{Login: body.Login, Name: body.Name, ExpiresAt: body.ExpiresAt}, &created)
		if err == nil {
			name := created.Link.Name
			if name == "" {
				name = "Team link"
			}
			detail := ""
			if !created.Link.CreatedAt.IsZero() {
				detail = "made " + created.Link.CreatedAt.Format("2 Jan")
			}
			if !created.Link.ExpiresAt.IsZero() {
				detail += " · expires " + created.Link.ExpiresAt.Format("2 Jan")
			}
			reply = FactoryLinkResponse{ID: created.Link.ID, URL: created.Link.URL, Name: name, Detail: detail}
			status = 201
		}
	case r.Method == "DELETE":
		link := r.PathValue("link_id")
		if link == "" {
			var body DeleteFactoryLinkRequest
			if !decodeFactory(w, r, &body) {
				return
			}
			link = body.ID
		}
		if link == "" || strings.ContainsAny(link, "/\\\r\n\x00") {
			fail(w, 422, "bad_input", "needs an access link id")
			return
		}
		err = source.request(r.Context(), "DELETE", "/v1/access/links/"+url.PathEscape(link), nil, nil)
	default:
		var body PatchFactorySettingsRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		if body.RunnerTimeoutSeconds == nil && body.KeepOutputDays == nil && body.KeepOutputMB == nil {
			fail(w, 422, "bad_input", "needs at least one runner setting")
			return
		}
		if body.RunnerTimeoutSeconds != nil && (*body.RunnerTimeoutSeconds < factory.MinRunnerTimeoutSeconds || *body.RunnerTimeoutSeconds > factory.MaxRunnerTimeoutSeconds) || body.KeepOutputDays != nil && (*body.KeepOutputDays < factory.MinKeepOutputDays || *body.KeepOutputDays > factory.MaxKeepOutputDays) || body.KeepOutputMB != nil && (*body.KeepOutputMB < factory.MinKeepOutputMB || *body.KeepOutputMB > factory.MaxKeepOutputMB) {
			fail(w, 422, "bad_input", fmt.Sprintf("runner timeout must be %d–%d seconds, output age %d–%d days and output size %d–%d MB", factory.MinRunnerTimeoutSeconds, factory.MaxRunnerTimeoutSeconds, factory.MinKeepOutputDays, factory.MaxKeepOutputDays, factory.MinKeepOutputMB, factory.MaxKeepOutputMB))
			return
		}
		patch := map[string]json.RawMessage{}
		if body.KeepOutputDays != nil {
			patch["factory.keep_output_days"], _ = json.Marshal(*body.KeepOutputDays)
		}
		if body.KeepOutputMB != nil {
			patch["factory.keep_output_mb"], _ = json.Marshal(*body.KeepOutputMB)
		}
		if len(patch) > 0 && s.options.ProfileDir == "" {
			fail(w, 404, "not_found", "this engine has no app settings")
			return
		}
		if body.RunnerTimeoutSeconds != nil {
			err = source.request(r.Context(), "PATCH", "/v1/settings", factoryapi.PatchFactorySettings{RunnerTimeoutSeconds: body.RunnerTimeoutSeconds}, nil)
		}
		if err == nil && len(patch) > 0 {
			s.settingsState.Lock()
			err = config.ApplyAppSettings(s.options.ProfileDir, patch, "")
			if err == nil {
				s.settingsChanged(patch)
			}
			s.settingsState.Unlock()
		}
		if err == nil {
			reply, err = f.settingsView(r.Context(), row)
		}
	}
	if err != nil {
		agentFailure(w, err)
		return
	}
	if r.Method != "GET" {
		f.publish(row, f.view(r.Context(), row).State)
		if f.Changed != nil {
			f.Changed()
		}
	}
	write(w, status, reply)
}

// A signed-out factory keeps its connection identity; operator-only directories stay absent.
func (f *FactoryRegistry) settingsView(ctx context.Context, row factoryConnection) (FactorySettingsDetails, error) {
	address := row.URL
	if row.Mode == "solo" {
		address = f.URL
	}
	u, _ := url.Parse(address)
	out := FactorySettingsDetails{ID: row.ID, Mode: row.Mode, Host: u.Host, Attached: true, Repos: []FactorySettingsRepo{}, Links: []FactorySettingsLink{}, People: []FactorySettingsPerson{}}
	source := f.source(row)
	if row.Mode == "team" && source.Token == "" {
		return out, nil
	}
	var me factoryapi.Me
	if err := source.request(ctx, "GET", "/v1/me", nil, &me); err != nil {
		return out, err
	}
	out.Login, out.SignedIn, out.Operator = me.Handle, true, me.Operator
	var settings factoryapi.FactorySettings
	if err := source.request(ctx, "GET", "/v1/settings", nil, &settings); err != nil {
		return out, err
	}
	out.RunnerTimeoutSeconds, out.KeepOutputDays, out.KeepOutputMB = settings.RunnerTimeoutSeconds, settings.KeepOutputDays, settings.KeepOutputMB
	if f.server.options.ProfileDir != "" {
		values, _, err := config.AppSettingsAt(f.server.options.ProfileDir)
		if err != nil {
			return out, err
		}
		raw, _ := json.Marshal(values["factory.keep_output_days"])
		_ = json.Unmarshal(raw, &out.KeepOutputDays)
		raw, _ = json.Marshal(values["factory.keep_output_mb"])
		_ = json.Unmarshal(raw, &out.KeepOutputMB)
	}
	if !me.Operator {
		return out, nil
	}
	var queue factoryapi.Queue
	if err := source.request(ctx, "GET", "/v1/queue", nil, &queue); err != nil {
		return out, err
	}
	for _, repo := range queue.Repos {
		out.Repos = append(out.Repos, FactorySettingsRepo{ID: repo.Name, Names: []string{repo.Name}, Enabled: repo.On || repo.Watched})
	}
	if row.Mode == "team" {
		var links factoryapi.AccessLinks
		if err := source.request(ctx, "GET", "/v1/access/links", nil, &links); err != nil {
			return out, err
		}
		for _, link := range links.Links {
			if link.Revoked {
				continue
			}
			detail := ""
			if !link.CreatedAt.IsZero() {
				detail = "made " + link.CreatedAt.Format("2 Jan")
			}
			if !link.ExpiresAt.IsZero() {
				detail += " · expires " + link.ExpiresAt.Format("2 Jan")
			}
			name := link.Name
			if name == "" {
				name = "Team link"
			}
			out.Links = append(out.Links, FactorySettingsLink{link.ID, name, detail})
		}
		var people factoryapi.People
		if err := source.request(ctx, "GET", "/v1/people", nil, &people); err != nil {
			return out, err
		}
		for _, person := range people.People {
			name := person.Name
			if name == "" {
				name = person.Handle
			}
			initial := ""
			for _, r := range name {
				initial = strings.ToUpper(string(r))
				break
			}
			out.People = append(out.People, FactorySettingsPerson{ID: person.Handle, Name: name, Initial: initial})
		}
	}
	return out, nil
}
