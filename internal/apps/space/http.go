package space

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/gateway"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// The Tauri origins the desk is allowed to call from. Copied from the app
// guard: this package cannot import appapi, because appapi imports apps,
// which imports space.
func allowedOrigin(origin string) bool {
	switch origin {
	case "tauri://localhost", "http://tauri.localhost", "http://localhost:1420", "http://127.0.0.1:1420":
		return true
	}
	return false
}

// Mux serves join, sign-in, and the bridge. The bridge reads the member from
// the request context, which the middleware sets after Members.Authenticate.
func (s *Service) Mux(ctx context.Context, all *svc.Services) http.Handler {
	if all == nil {
		all = s.all
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /app/v1/space/join", s.handleJoin)
	mux.HandleFunc("POST /app/v1/space/signin/email", s.handleEmail)
	mux.HandleFunc("POST /app/v1/space/signin/code", s.handleCode)
	mux.HandleFunc("POST /app/v1/space/claim", s.handleClaim)
	mux.HandleFunc("GET /app/v1/space/signin/oauth/{provider}/callback", s.handleOAuthCallback)
	mux.HandleFunc("GET /app/v1/space/signin/oauth/{provider}", s.handleOAuthStart)
	mux.Handle(bridge.Path, gateway.NewHandler(ctx, all))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serve(mux, w, r)
	})
}

func (s *Service) serve(next http.Handler, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !allowRequestOrigin(w, r) {
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if publicSpacePath(r.URL.Path) {
		next.ServeHTTP(w, r)
		return
	}
	token := bearerToken(r)
	caller, err := s.Members().Authenticate(r.Context(), token)
	if err != nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeErrStatus(w, http.StatusUnauthorized, err)
		return
	}
	rw := w
	if r.URL.Path == bridge.Path && s.ws != nil {
		rw = s.ws.sockets.wrap(w, caller.Member)
	}
	next.ServeHTTP(rw, r.WithContext(svc.WithCaller(r.Context(), caller)))
}

func allowRequestOrigin(w http.ResponseWriter, r *http.Request) bool {
	origins, present := r.Header["Origin"]
	if !present {
		return true
	}
	if len(origins) != 1 || !allowedOrigin(origins[0]) {
		writeErrStatus(w, http.StatusForbidden, denied("this connection is only for the codeaf app"))
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origins[0])
	w.Header().Set("Access-Control-Expose-Headers", "ETag")
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	return true
}

func publicSpacePath(path string) bool {
	switch path {
	case "/app/v1/space/join", "/app/v1/space/signin/email", "/app/v1/space/signin/code", "/app/v1/space/claim":
		return true
	}
	return strings.HasPrefix(path, "/app/v1/space/signin/oauth/")
}

func bearerToken(r *http.Request) string {
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	}
	for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, part := range strings.Split(header, ",") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, bridge.BearerProtocol) {
				return strings.TrimPrefix(part, bridge.BearerProtocol)
			}
		}
	}
	return ""
}

func (s *Service) handleJoin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Invite string `json:"invite"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	row, err := s.ws.preview(r.Context(), body.Invite)
	if err != nil {
		writeErr(w, err)
		return
	}
	if s.ws.signIn == "email" && row.Email != "" {
		if err := s.ws.sendCode(r.Context(), body.Invite, row.Email); err != nil {
			writeErr(w, err)
			return
		}
	}
	signin := s.ws.signinDescription(row)
	if s.ws.signIn != "email" {
		signin.URL = s.ws.publicURL + "/app/v1/space/signin/oauth/" + s.ws.signIn + "?invite=" + url.QueryEscape(body.Invite)
	}
	writeJSON(w, http.StatusOK, joinBody{
		Space:  bridge.SpaceEntry{ID: s.ws.name, Name: s.ws.name, Address: urlHost(s.ws.publicURL), Role: row.Role},
		Invite: bridge.SpaceJoinInvite{By: row.By, Role: row.Role, Server: urlHost(s.ws.publicURL), Expires: row.Expires},
		Signin: signin,
		State:  "needs_signin",
	})
}

func (s *Service) handleEmail(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Invite string `json:"invite"`
		Email  string `json:"email"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.ws.sendCode(r.Context(), body.Invite, body.Email); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bridge.Empty{})
}

func (s *Service) handleCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	signed, err := s.ws.redeemCode(r.Context(), body.Email, body.Code)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.audit(r.Context(), signed.Member.ID, "spaces.signin", "member/"+signed.Member.ID, "@"+signed.Member.Name+" joined "+s.ws.name+" as "+roleTitle(signed.Member.Role))
	s.publish(s.ws.snapshot(r.Context()))
	writeJSON(w, http.StatusOK, sessionBody{
		Token: signed.Token, Member: signed.Member, Expires: signed.Expires,
		Space: bridge.SpaceEntry{ID: s.ws.name, Name: s.ws.name, Address: urlHost(s.ws.publicURL), Role: signed.Member.Role},
	})
}

func (s *Service) handleClaim(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SetupCode string `json:"setup_code"`
		Name      string `json:"name"`
		Handle    string `json:"handle"`
		Email     string `json:"email"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	token, member, expires, err := s.claim(r.Context(), body.SetupCode, body.Name, body.Handle, body.Email)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sessionBody{
		Token: token, Member: member, Expires: expires,
		Space: bridge.SpaceEntry{ID: s.ws.name, Name: s.ws.name, Address: urlHost(s.ws.publicURL), Role: member.Role},
	})
}

func (s *Service) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	raw, err := s.ws.beginOAuth(r.Context(), r.PathValue("provider"), r.URL.Query().Get("invite"))
	if err != nil {
		writeErr(w, err)
		return
	}
	http.Redirect(w, r, raw, http.StatusFound)
}

func (s *Service) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	signed, err := s.ws.finishOAuth(r.Context(), provider, r.URL.Query().Get("state"), r.URL.Query().Get("code"))
	if err != nil {
		writeErr(w, err)
		return
	}
	s.audit(r.Context(), signed.Member.ID, "spaces.signin", "member/"+signed.Member.ID, "@"+signed.Member.Name+" joined "+s.ws.name+" as "+roleTitle(signed.Member.Role))
	s.publish(s.ws.snapshot(r.Context()))
	target := "codeaf://space/session?space=" + url.QueryEscape(s.ws.name) + "#token=" + url.QueryEscape(signed.Token)
	http.Redirect(w, r, target, http.StatusFound)
}

func readJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		return invalid("the request body must be JSON")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, err error) {
	var be *bridge.Error
	if !errors.As(err, &be) {
		log.Printf("space: %v", err)
		be = bridge.Errorf(bridge.CodeInternal, "workspace request failed")
	}
	writeErrStatus(w, statusFor(be.Code), be)
}

func writeErrStatus(w http.ResponseWriter, status int, err error) {
	var be *bridge.Error
	if !errors.As(err, &be) {
		log.Printf("space: %v", err)
		be = bridge.Errorf(bridge.CodeInternal, "workspace request failed")
	}
	writeJSON(w, status, be)
}

func statusFor(code bridge.ErrorCode) int {
	switch code {
	case bridge.CodeConflict:
		return http.StatusConflict
	case bridge.CodeDenied:
		return http.StatusForbidden
	case bridge.CodeInvalid:
		return http.StatusUnprocessableEntity
	case bridge.CodeNotFound:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
