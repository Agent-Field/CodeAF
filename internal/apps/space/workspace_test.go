package space_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/apps"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/space"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const expiredInvite = "This invite has expired · Ask @santosh for a new one"
const lastOwner = "You're the last Owner. Make someone else Owner first, or delete acme in General."

type movingClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *movingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *movingClock) Set(at time.Time) {
	c.mu.Lock()
	c.at = at
	c.mu.Unlock()
}

func TestInviteJoinSignInConnectInfo(t *testing.T) {
	ws, all, origin, setup, codes := boot(t, space.WorkspaceOptions{})
	ctx := context.Background()
	personal := personalSidecar(t, "santosh")
	if _, err := personal.New(ctx, svc.Caller{}, bridge.SpaceNewParams{Name: "acme", Address: origin, SetupCode: setup}); err != nil {
		t.Fatal(err)
	}
	events, cancel := all.Hub.Subscribe(bridge.TopicSpaceMembers)
	defer cancel()
	owner := svc.Caller{Member: "santosh", Space: "acme", WS: svc.WorkspaceOwner}
	inv, err := ws.Members().Invite(ctx, owner, bridge.InviteParams{Email: "dana@acme.com", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		list, ok := ev.Data.(bridge.MembersListResult)
		if !ok || len(list.Invites) != 1 || list.Invites[0].Email != "dana@acme.com" {
			t.Fatalf("space/members = %#v", ev.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("space/members was not published")
	}
	activity, err := all.Audit.Activity(ctx, svc.Scope{Caller: owner}, bridge.ActivityListParams{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !hasText(activity, "@santosh invited dana@acme.com as Member") {
		t.Fatalf("activity = %+v", activity.Lines)
	}

	joined, err := personal.Join(ctx, svc.Caller{}, bridge.SpaceJoinParams{Link: inv.Link})
	if err != nil {
		t.Fatal(err)
	}
	if joined.State != "needs_signin" || joined.Signin.Kind != "email" || joined.Signin.EmailHint != "dana@acme.com" || joined.Invite.By != "santosh" {
		t.Fatalf("join = %+v", joined)
	}
	code := signInCode(t, codes.String(), "dana@acme.com")
	done, err := personal.Signin(ctx, svc.Caller{}, bridge.SpacesSigninParams{Space: "acme", Code: code})
	if err != nil {
		t.Fatal(err)
	}
	if done.State != "joined" || done.Space.Role != string(svc.WorkspaceMember) {
		t.Fatalf("signin = %+v", done)
	}
	connection, err := personal.Connect(ctx, svc.Caller{}, bridge.SpaceConnectParams{Space: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if connection.Token == "" || !strings.HasPrefix(connection.URL, "ws://") {
		t.Fatalf("connect = %+v", connection)
	}
	conn := dial(t, ctx, connection)
	var info bridge.SpaceInfo
	call(t, ctx, conn, bridge.MethodSpaceInfo, bridge.Empty{}, &info)
	if info.Kind != "workspace" || info.ID != "acme" || info.Me.ID != "dana" || info.Me.Role != string(svc.WorkspaceMember) || info.Me.Email != "dana@acme.com" {
		t.Fatalf("space.info = %+v", info)
	}

	_, err = personal.Join(ctx, svc.Caller{}, bridge.SpaceJoinParams{Link: inv.Link})
	expectBridge(t, err, bridge.CodeConflict, expiredInvite)
}

func TestExpiredInviteMessage(t *testing.T) {
	clock := &movingClock{at: time.Date(2026, 10, 13, 15, 0, 0, 0, time.UTC)}
	ws, _, origin, setup, _ := boot(t, space.WorkspaceOptions{Now: clock.Now})
	ctx := context.Background()
	personal := personalSidecar(t, "santosh")
	if _, err := personal.New(ctx, svc.Caller{}, bridge.SpaceNewParams{Name: "acme", Address: origin, SetupCode: setup}); err != nil {
		t.Fatal(err)
	}
	owner := svc.Caller{Member: "santosh", Space: "acme", WS: svc.WorkspaceOwner}
	inv, err := ws.Members().Invite(ctx, owner, bridge.InviteParams{Email: "dana@acme.com", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Set(clock.Now().Add(8 * 24 * time.Hour))
	_, err = personal.Join(ctx, svc.Caller{}, bridge.SpaceJoinParams{Link: inv.Link})
	expectBridge(t, err, bridge.CodeConflict, expiredInvite)
}

func TestLastOwnerCannotLeave(t *testing.T) {
	ws, _, origin, setup, _ := boot(t, space.WorkspaceOptions{})
	ctx := context.Background()
	personal := personalSidecar(t, "santosh")
	if _, err := personal.New(ctx, svc.Caller{}, bridge.SpaceNewParams{Name: "acme", Address: origin, SetupCode: setup}); err != nil {
		t.Fatal(err)
	}
	space.IsolateMemberGoneHooks(t)
	var gone string
	space.OnMemberGone(func(_ context.Context, member string) { gone = member })
	_, err := ws.Members().Leave(ctx, svc.Caller{Member: "santosh", Space: "acme", WS: svc.WorkspaceOwner})
	expectBridge(t, err, bridge.CodeConflict, lastOwner)
	if gone != "" {
		t.Fatalf("memberGone ran for %s", gone)
	}
}

func TestRemoveClosesTheSocket(t *testing.T) {
	ws, _, origin, setup, codes := boot(t, space.WorkspaceOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	personal := personalSidecar(t, "santosh")
	if _, err := personal.New(ctx, svc.Caller{}, bridge.SpaceNewParams{Name: "acme", Address: origin, SetupCode: setup}); err != nil {
		t.Fatal(err)
	}
	owner := svc.Caller{Member: "santosh", Space: "acme", WS: svc.WorkspaceOwner}
	connection := joinDana(t, ctx, ws, personal, owner, codes)
	conn := dial(t, ctx, connection)
	var info bridge.SpaceInfo
	call(t, ctx, conn, bridge.MethodSpaceInfo, bridge.Empty{}, &info)
	if info.Me.ID != "dana" {
		t.Fatalf("me = %+v", info.Me)
	}
	space.IsolateMemberGoneHooks(t)
	hook := make(chan error, 1)
	space.OnMemberGone(func(_ context.Context, member string) {
		if member != "dana" {
			hook <- errors.New("memberGone for " + member)
			return
		}
		write, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		raw, _ := json.Marshal(bridge.Empty{})
		env, _ := json.Marshal(bridge.Envelope{ID: "2", Type: bridge.TypeCall, Method: bridge.MethodSpaceInfo, Params: raw})
		hook <- conn.Write(write, websocket.MessageText, env)
	})
	if _, err := ws.Members().Remove(ctx, owner, bridge.MemberRef{Member: "dana"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-hook:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("memberGone did not run")
	}
	read, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	closed := false
	for !closed {
		_, _, err := conn.Read(read)
		if err != nil {
			closed = true
		}
		if read.Err() != nil {
			break
		}
	}
	if !closed {
		t.Fatal("socket stayed open after remove")
	}
}

func TestOnlyOwnerMakesOwner(t *testing.T) {
	ws, _, origin, setup, codes := boot(t, space.WorkspaceOptions{})
	ctx := context.Background()
	personal := personalSidecar(t, "santosh")
	if _, err := personal.New(ctx, svc.Caller{}, bridge.SpaceNewParams{Name: "acme", Address: origin, SetupCode: setup}); err != nil {
		t.Fatal(err)
	}
	owner := svc.Caller{Member: "santosh", Space: "acme", WS: svc.WorkspaceOwner}
	joinDana(t, ctx, ws, personal, owner, codes)
	if _, err := ws.Members().SetRole(ctx, owner, bridge.MemberRoleParams{Member: "dana", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	admin := svc.Caller{Member: "dana", Space: "acme", WS: svc.WorkspaceAdmin}
	_, err := ws.Members().SetRole(ctx, admin, bridge.MemberRoleParams{Member: "dana", Role: "owner"})
	expectBridge(t, err, bridge.CodeDenied, "only an Owner can make or unmake an Owner")
	got, err := ws.Members().SetRole(ctx, owner, bridge.MemberRoleParams{Member: "dana", Role: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != string(svc.WorkspaceOwner) {
		t.Fatalf("role = %s", got.Role)
	}
}

func TestForeignOriginIsRefused(t *testing.T) {
	ws, _, _, _, _ := boot(t, space.WorkspaceOptions{})
	req := httptest.NewRequest(http.MethodPost, "/app/v1/space/join", strings.NewReader(`{"invite":"x"}`))
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	ws.Mux(context.Background(), nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body)
	}
	req = httptest.NewRequest(http.MethodPost, "/app/v1/space/join", strings.NewReader(`{"invite":"not-a-link"}`))
	req.Header.Set("Origin", "tauri://localhost")
	rec = httptest.NewRecorder()
	ws.Mux(context.Background(), nil).ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("tauri origin refused: %s", rec.Body)
	}
}

func boot(t *testing.T, opt space.WorkspaceOptions) (*space.Service, *svc.Services, string, string, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	all := apps.Wire(ctx, home)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var setup bytes.Buffer
	codes := &bytes.Buffer{}
	if opt.SetupOut == nil {
		opt.SetupOut = &setup
	}
	if opt.Home == "" {
		opt.Home = home
	}
	if opt.Name == "" {
		opt.Name = "acme"
	}
	if opt.SignIn == "" {
		opt.SignIn = "email"
	}
	if opt.PublicURL == "" {
		opt.PublicURL = "http://" + ln.Addr().String()
	}
	if opt.Mailer == nil {
		opt.Mailer = space.LogMailer{Log: log.New(codes, "", 0)}
	}
	ws, err := space.OpenWorkspace(svc.Config{Root: filepath.Join(home, "apps"), Ctx: ctx}, all, opt)
	if err != nil {
		t.Fatal(err)
	}
	all.Spaces = ws
	all.Members = ws.Members()
	server := &http.Server{Handler: ws.Mux(ctx, all), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() {
		shut, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = server.Shutdown(shut)
	})
	return ws, all, opt.PublicURL, setupCode(t, setup.String()), codes
}

func personalSidecar(t *testing.T, handle string) *space.Service {
	t.Helper()
	home := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	all := apps.Wire(ctx, home)
	return space.OpenPersonal(svc.Config{Root: filepath.Join(home, "apps"), Ctx: ctx}, all, space.PersonalOptions{
		Keyring: space.NewFileKeyring(filepath.Join(home, "spaces.json")),
		Handle:  handle,
	})
}

func joinDana(t *testing.T, ctx context.Context, ws, personal *space.Service, owner svc.Caller, codes *bytes.Buffer) bridge.SpaceConnection {
	t.Helper()
	inv, err := ws.Members().Invite(ctx, owner, bridge.InviteParams{Email: "dana@acme.com", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := personal.Join(ctx, svc.Caller{}, bridge.SpaceJoinParams{Link: inv.Link}); err != nil {
		t.Fatal(err)
	}
	if _, err := personal.Signin(ctx, svc.Caller{}, bridge.SpacesSigninParams{Space: "acme", Code: signInCode(t, codes.String(), "dana@acme.com")}); err != nil {
		t.Fatal(err)
	}
	connection, err := personal.Connect(ctx, svc.Caller{}, bridge.SpaceConnectParams{Space: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func setupCode(t *testing.T, text string) string {
	t.Helper()
	m := regexp.MustCompile(`setup code: ([0-9a-f]+)`).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("setup code missing in %q", text)
	}
	return m[1]
}

func signInCode(t *testing.T, text, email string) string {
	t.Helper()
	m := regexp.MustCompile(`space sign-in code for ` + regexp.QuoteMeta(email) + `: (\d{6})`).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("sign-in code missing in %q", text)
	}
	return m[1]
}

func hasText(result bridge.ActivityListResult, text string) bool {
	for _, line := range result.Lines {
		if line.Text == text {
			return true
		}
	}
	return false
}

func expectBridge(t *testing.T, err error, code bridge.ErrorCode, message string) {
	t.Helper()
	var be *bridge.Error
	if !errors.As(err, &be) || be.Code != code || be.Message != message {
		t.Fatalf("err = %v, want %s %s", err, code, message)
	}
}

func dial(t *testing.T, ctx context.Context, connection bridge.SpaceConnection) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, connection.URL, &websocket.DialOptions{
		Subprotocols: []string{bridge.Subprotocol, bridge.BearerProtocol + connection.Token},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func call(t *testing.T, ctx context.Context, conn *websocket.Conn, method string, params any, dst any) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	env, err := json.Marshal(bridge.Envelope{ID: "1", Type: bridge.TypeCall, Method: method, Params: raw})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText, env); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var reply bridge.Envelope
	if err := json.Unmarshal(data, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Error != nil {
		t.Fatalf("%s: %s", reply.Error.Code, reply.Error.Message)
	}
	if dst != nil && json.Unmarshal(reply.Result, dst) != nil {
		t.Fatalf("result %s", reply.Result)
	}
}
