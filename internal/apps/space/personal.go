package space

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Signin finishes an email code, or stores a session token the OAuth deep
// link already returned. The gateway still answers spaces.signin with
// not_built until that registration calls this method.
func (s *Service) Signin(ctx context.Context, _ svc.Caller, p bridge.SpacesSigninParams) (bridge.SpaceJoinResult, error) {
	if err := s.personalOnly(); err != nil {
		return bridge.SpaceJoinResult{}, err
	}
	pend, ok := s.pending(p.Space)
	if !ok {
		return bridge.SpaceJoinResult{}, notFound("join " + p.Space + " first")
	}
	code := strings.TrimSpace(p.Code)
	if code == "" {
		if pend.Kind == "email" && pend.Email != "" {
			if err := s.post(ctx, pend.Origin+"/app/v1/space/signin/email", map[string]string{"invite": pend.Invite, "email": pend.Email}, nil); err != nil {
				return bridge.SpaceJoinResult{}, err
			}
		}
		return s.joinResult(pend, "needs_signin"), nil
	}
	if len(code) > 6 {
		return s.remember(pend, sessionBody{Token: code, Member: bridge.Member{ID: pend.Handle, Name: pend.Handle, Role: pend.Role}, Expires: time.Now().UTC().Add(sessionTTL)})
	}
	var body sessionBody
	if err := s.post(ctx, pend.Origin+"/app/v1/space/signin/code", map[string]string{"email": pend.Email, "code": code}, &body); err != nil {
		return bridge.SpaceJoinResult{}, err
	}
	return s.remember(pend, body)
}

func (s *Service) List(ctx context.Context, caller svc.Caller) (bridge.SpacesListResult, error) {
	if err := s.personalOnly(); err != nil {
		return bridge.SpacesListResult{}, err
	}
	info, err := s.Info(ctx, caller)
	if err != nil {
		return bridge.SpacesListResult{}, err
	}
	entries := []bridge.SpaceEntry{{ID: info.ID, Name: info.Name, Address: info.Address, Role: string(svc.WorkspaceOwner), Apps: info.Apps}}
	rows, err := s.kr.Load()
	if err != nil {
		return bridge.SpacesListResult{Spaces: entries}, nil
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Order < rows[j].Order })
	for _, row := range rows {
		entries = append(entries, bridge.SpaceEntry{ID: row.ID, Name: row.Name, Address: row.Address, Role: row.Role, Order: row.Order})
	}
	return bridge.SpacesListResult{Spaces: entries}, nil
}

func (s *Service) Join(ctx context.Context, _ svc.Caller, p bridge.SpaceJoinParams) (bridge.SpaceJoinResult, error) {
	if err := s.personalOnly(); err != nil {
		return bridge.SpaceJoinResult{}, err
	}
	host, id, secret, err := parseInvite(p.Link)
	if err != nil {
		return bridge.SpaceJoinResult{}, err
	}
	origin := originFor(host)
	var body joinBody
	if err := s.post(ctx, origin+"/app/v1/space/join", map[string]string{"invite": p.Link}, &body); err != nil {
		return bridge.SpaceJoinResult{}, err
	}
	pend := pendingJoin{
		ID: body.Space.ID, Name: body.Space.Name, Origin: origin, Address: host,
		Invite: id + "." + secret, Email: body.Signin.EmailHint, Kind: body.Signin.Kind,
		Role: body.Invite.Role, By: body.Invite.By, URL: body.Signin.URL, Expires: body.Invite.Expires,
	}
	if pend.ID == "" {
		pend.ID = body.Space.ID
	}
	s.savePending(pend)
	return s.joinResult(pend, body.State), nil
}

func (s *Service) Connect(_ context.Context, _ svc.Caller, p bridge.SpaceConnectParams) (bridge.SpaceConnection, error) {
	if err := s.personalOnly(); err != nil {
		return bridge.SpaceConnection{}, err
	}
	rows, err := s.kr.Load()
	if err != nil {
		return bridge.SpaceConnection{}, err
	}
	for _, row := range rows {
		if row.ID != p.Space {
			continue
		}
		if !row.Expires.IsZero() && !time.Now().Before(row.Expires) {
			return bridge.SpaceConnection{}, conflict("sign in to " + row.Name + " again")
		}
		return bridge.SpaceConnection{URL: bridgeURL(row.Origin), Token: row.Token, Expires: row.Expires}, nil
	}
	return bridge.SpaceConnection{}, notFound("join " + p.Space + " first")
}

func (s *Service) New(ctx context.Context, _ svc.Caller, p bridge.SpaceNewParams) (bridge.SpaceJoinResult, error) {
	if err := s.personalOnly(); err != nil {
		return bridge.SpaceJoinResult{}, err
	}
	origin := strings.TrimRight(strings.TrimSpace(p.Address), "/")
	if origin == "" {
		return bridge.SpaceJoinResult{}, invalid("address is required")
	}
	handle := s.handle
	if handle == "" {
		handle = localHandle()
	}
	var body sessionBody
	err := s.post(ctx, origin+"/app/v1/space/claim", map[string]string{
		"setup_code": p.SetupCode, "name": p.Name, "handle": handle,
	}, &body)
	if err != nil {
		return bridge.SpaceJoinResult{}, err
	}
	pend := pendingJoin{
		ID: body.Space.ID, Name: body.Space.Name, Origin: origin, Address: urlHost(origin),
		Kind: "email", Role: body.Member.Role, Handle: body.Member.ID,
	}
	if pend.ID == "" {
		pend.ID = strings.ToLower(p.Name)
		pend.Name = pend.ID
	}
	return s.remember(pend, body)
}

func (s *Service) Forget(_ context.Context, _ svc.Caller, p bridge.SpaceRef) (bridge.Empty, error) {
	if err := s.personalOnly(); err != nil {
		return bridge.Empty{}, err
	}
	rows, err := s.kr.Load()
	if err != nil {
		return bridge.Empty{}, err
	}
	kept := rows[:0]
	for _, row := range rows {
		if row.ID != p.Space {
			kept = append(kept, row)
		}
	}
	if err := s.kr.Save(kept); err != nil {
		return bridge.Empty{}, err
	}
	s.pendMu.Lock()
	delete(s.pend, p.Space)
	s.pendMu.Unlock()
	return bridge.Empty{}, nil
}

func (s *Service) pending(id string) (pendingJoin, bool) {
	s.pendMu.Lock()
	defer s.pendMu.Unlock()
	row, ok := s.pend[id]
	return row, ok
}

func (s *Service) savePending(row pendingJoin) {
	s.pendMu.Lock()
	s.pend[row.ID] = row
	s.pendMu.Unlock()
}

func (s *Service) joinResult(pend pendingJoin, state string) bridge.SpaceJoinResult {
	if state == "" {
		state = "needs_signin"
	}
	return bridge.SpaceJoinResult{
		Space:  bridge.SpaceEntry{ID: pend.ID, Name: pend.Name, Address: pend.Address, Role: pend.Role},
		Invite: bridge.SpaceJoinInvite{By: pend.By, Role: pend.Role, Server: pend.Address, Expires: pend.Expires},
		Signin: bridge.SpaceSignin{Kind: pend.Kind, URL: pend.URL, EmailHint: pend.Email},
		State:  state,
	}
}

func (s *Service) remember(pend pendingJoin, body sessionBody) (bridge.SpaceJoinResult, error) {
	rows, err := s.kr.Load()
	if err != nil {
		return bridge.SpaceJoinResult{}, err
	}
	role := body.Member.Role
	if role == "" {
		role = pend.Role
	}
	handle := body.Member.ID
	if handle == "" {
		handle = pend.Handle
	}
	row := savedSpace{
		ID: pend.ID, Name: pend.Name, Address: pend.Address, Origin: pend.Origin,
		Role: role, Handle: handle, Token: body.Token, Expires: body.Expires,
	}
	found := false
	for i := range rows {
		if rows[i].ID == row.ID {
			row.Order = rows[i].Order
			rows[i] = row
			found = true
			break
		}
	}
	if !found {
		row.Order = len(rows) + 1
		rows = append(rows, row)
	}
	if err := s.kr.Save(rows); err != nil {
		return bridge.SpaceJoinResult{}, err
	}
	s.pendMu.Lock()
	delete(s.pend, pend.ID)
	s.pendMu.Unlock()
	pend.Role = role
	pend.Handle = handle
	return s.joinResult(pend, "joined"), nil
}

type sessionBody struct {
	Token   string            `json:"token"`
	Member  bridge.Member     `json:"member"`
	Expires time.Time         `json:"expires"`
	Space   bridge.SpaceEntry `json:"space"`
}

type joinBody struct {
	Space  bridge.SpaceEntry      `json:"space"`
	Invite bridge.SpaceJoinInvite `json:"invite"`
	Signin bridge.SpaceSignin     `json:"signin"`
	State  string                 `json:"state"`
}

func (s *Service) post(ctx context.Context, rawURL string, body any, dst any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var be bridge.Error
		if json.Unmarshal(payload, &be) == nil && be.Code != "" {
			return &be
		}
		return bridge.Errorf(bridge.CodeInternal, "the workspace refused the request")
	}
	if dst == nil || len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	return json.Unmarshal(payload, dst)
}
