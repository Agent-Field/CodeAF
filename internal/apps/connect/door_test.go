package connect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type grantStub struct {
	svc.Grants
	credential svc.Credential
	err        error
	calls      int
	scope      svc.Scope
	service    string
	sheet      *bridge.AllowSheet
}

func (g *grantStub) Resolve(_ context.Context, scope svc.Scope, service string) (svc.Credential, error) {
	g.calls++
	g.scope = scope
	g.service = service
	return g.credential, g.err
}

func (g *grantStub) AllowSheet(context.Context, svc.Scope, bridge.AllowSheetParams) (bridge.AllowSheet, error) {
	if g.sheet == nil {
		return bridge.AllowSheet{}, bridge.NotBuilt("connect.allow.sheet")
	}
	return *g.sheet, nil
}

type adapterStub struct {
	calls      int
	credential svc.Credential
	err        error
}

func (a *adapterStub) Call(_ context.Context, c svc.Credential, _ string, _ map[string]any) (any, error) {
	a.calls++
	a.credential = c
	return "real reply", a.err
}

type capsStub struct {
	calls int
	err   error
}

func (c *capsStub) Allow(context.Context, svc.Scope, string, json.RawMessage) error {
	c.calls++
	return c.err
}

type waitingRuns struct {
	svc.Runs
	changed   []bridge.ConnectorWait
	stopped   []bool
	errForRun map[string]error
	err       error
}

func (r *waitingRuns) ConnectorChanged(_ context.Context, w bridge.ConnectorWait, stop bool) error {
	r.changed = append(r.changed, w)
	r.stopped = append(r.stopped, stop)
	if err := r.errForRun[w.RunID]; err != nil {
		return err
	}
	return r.err
}

func testDoor(t *testing.T) (*Service, *grantStub, *adapterStub, *svc.Services, *time.Time) {
	t.Helper()
	clock := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	grants := &grantStub{credential: svc.Credential{ID: "connection-1", Service: "gmail", Account: "mona@example.test", Level: "personal", Member: "mona"}}
	all := &svc.Services{Hub: realtime.New(svc.Config{}, nil), Connect: svc.ConnectServices{Grants: grants}}
	s := NewWithClock(svc.Config{Root: filepath.Join(t.TempDir(), "apps")}, all, func() time.Time { return clock })
	t.Cleanup(func() { _ = s.Close() })
	adapter := &adapterStub{}
	for name := range s.adapters {
		s.adapters[name] = adapter
	}
	return s, grants, adapter, all, &clock
}
func testScope(mode svc.DataMode) svc.Scope {
	return svc.Scope{App: "crm", Data: mode, Caller: svc.Caller{Member: "mona", Role: svc.RoleUser}}
}
func TestNonRealScopesAlwaysUseFakes(t *testing.T) {
	for _, mode := range []svc.DataMode{svc.DataSample, "draft", "future", ""} {
		for _, service := range []string{"gmail", "calendar", "slack", "mcp", "mcp:custom", "github", "stripe", "airtable", "hubspot"} {
			t.Run(string(mode)+"/"+service, func(t *testing.T) {
				s, g, a, all, _ := testDoor(t)
				events, cancel := all.Hub.Subscribe(bridge.TopicFakes("crm"))
				defer cancel()
				ctx := WithInvocation(context.Background(), "run-1", "read")
				reply, err := s.Call(ctx, testScope(mode), service+".read", map[string]any{"query": "team"})
				if err != nil || reply == nil || g.calls != 0 || a.calls != 0 {
					t.Fatalf("reply=%v err=%v grants=%d adapters=%d", reply, err, g.calls, a.calls)
				}
				calls, err := s.Fakes(ctx, testScope(mode), bridge.ConnectFakesParams{RunID: "run-1"})
				if err != nil || len(calls.Calls) != 1 {
					t.Fatalf("calls=%+v err=%v", calls, err)
				}
				call := calls.Calls[0]
				if !call.Fake || call.Summary != "team" || call.VerbLabel != "Would read" {
					t.Fatalf("call=%+v", call)
				}
				select {
				case event := <-events:
					if !reflect.DeepEqual(event.Data, call) {
						t.Fatalf("event=%+v", event)
					}
				default:
					t.Fatal("no fake event")
				}
			})
		}
	}
}
func TestRealResolvesBeforeAdapter(t *testing.T) {
	s, g, a, _, _ := testDoor(t)
	scope := testScope(svc.DataReal)
	reply, err := s.Call(context.Background(), scope, "gmail.read", nil)
	if err != nil || reply != "real reply" || g.calls != 1 || a.calls != 1 || a.credential != g.credential || g.scope != scope || g.service != "gmail" {
		t.Fatalf("reply=%v err=%v grant=%+v adapter=%+v", reply, err, g, a)
	}
	calls, _ := s.Fakes(context.Background(), scope, bridge.ConnectFakesParams{})
	if len(calls.Calls) != 0 {
		t.Fatal(calls)
	}
}
func TestOutboundRequiresRunPermit(t *testing.T) {
	for _, mode := range []svc.DataMode{svc.DataReal, svc.DataSample, "draft"} {
		for _, tool := range []string{"gmail.send", "slack.post", "slack.reply", "stripe.pay", "stripe.charge", "calendar.create", "github.create"} {
			t.Run(string(mode)+"/"+tool, func(t *testing.T) {
				s, g, a, _, _ := testDoor(t)
				_, err := s.Call(context.Background(), testScope(mode), tool, nil)
				var typed *bridge.Error
				if !errors.As(err, &typed) || typed.Code != bridge.CodeDenied || g.calls != 0 || a.calls != 0 {
					t.Fatalf("err=%v grants=%d adapter=%d", err, g.calls, a.calls)
				}
				if _, err := s.Call(WithPermit(context.Background()), testScope(mode), tool, map[string]any{"subject": "Team plan", "to": "Priya"}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
func TestWaitPersistenceResumeAndDeadline(t *testing.T) {
	for _, reason := range []string{bridge.ReasonExpired, bridge.ReasonNeedsAllow} {
		for _, stop := range []bool{false, true} {
			t.Run(fmt.Sprint(reason, "/", stop), func(t *testing.T) {
				s, g, a, all, clock := testDoor(t)
				g.err = connectionError(reason)
				events, cancel := all.Hub.Subscribe(bridge.TopicConnectWaits("mona"))
				defer cancel()
				ctx := WithInvocation(context.Background(), "run-1", "read")
				_, err := s.Call(ctx, testScope(svc.DataReal), "gmail.read", nil)
				var waitErr *WaitError
				if !errors.As(err, &waitErr) || a.calls != 0 {
					t.Fatalf("err=%v adapters=%d", err, a.calls)
				}
				original := waitErr.Wait
				if original.StopsAt != clock.Add(waitLifetime).Format(time.RFC3339Nano) {
					t.Fatal(original)
				}
				*clock = clock.Add(time.Hour)
				_, err = s.Call(ctx, testScope(svc.DataReal), "gmail.read", nil)
				if !errors.As(err, &waitErr) || waitErr.Wait != original {
					t.Fatalf("wait changed: %+v", waitErr)
				}
				select {
				case <-events:
				default:
					t.Fatal("no wait event")
				}
				select {
				case <-events:
					t.Fatal("duplicate open event")
				default:
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				result, err := s.Waits(ctx, svc.Caller{Member: "mona"})
				if err != nil || len(result.Waits) != 1 || result.Waits[0] != original {
					t.Fatalf("waits=%+v err=%v", result, err)
				}
				others, _ := s.Waits(ctx, svc.Caller{Member: "priya"})
				if len(others.Waits) != 0 {
					t.Fatal(others)
				}
				hook := &waitingRuns{}
				all.Runs = hook
				if stop {
					*clock = clock.Add(waitLifetime)
					if err := s.Sweep(ctx); err != nil {
						t.Fatal(err)
					}
				} else {
					g.err = nil
					if err := s.Resume("gmail", "mona"); err != nil {
						t.Fatal(err)
					}
				}
				result, err = s.Waits(ctx, svc.Caller{Member: "mona"})
				if err != nil || len(result.Waits) != 0 || len(hook.changed) != 1 || hook.stopped[0] != stop {
					t.Fatalf("waits=%+v hook=%+v err=%v", result, hook, err)
				}
				select {
				case <-events:
				default:
					t.Fatal("no clear event")
				}
			})
		}
	}
}
func TestFailedResumeKeepsOriginalDeadline(t *testing.T) {
	s, g, _, all, clock := testDoor(t)
	g.err = connectionError(bridge.ReasonExpired)
	ctx := WithInvocation(context.Background(), "run-1", "read")
	_, _ = s.Call(ctx, testScope(svc.DataReal), "gmail.read", nil)
	before, _ := s.Waits(ctx, svc.Caller{Member: "mona"})
	*clock = clock.Add(time.Hour)
	all.Runs = &waitingRuns{err: bridge.Errorf(bridge.CodeConflict, "connection is still waiting")}
	if err := s.Resume("gmail", "mona"); err == nil {
		t.Fatal("failed resume accepted")
	}
	after, _ := s.Waits(ctx, svc.Caller{Member: "mona"})
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
}
func TestAdapterExpiryBecomesWait(t *testing.T) {
	s, _, a, _, _ := testDoor(t)
	a.err = errors.New("Slack has to be connected again")
	_, err := s.Call(context.Background(), testScope(svc.DataReal), "slack.read", nil)
	var wait *WaitError
	if !errors.As(err, &wait) || wait.Wait.Reason != bridge.ReasonExpired {
		t.Fatal(err)
	}
}

func TestCapsDenialPrecedesEveryConnectorPath(t *testing.T) {
	for _, mode := range []svc.DataMode{svc.DataReal, svc.DataSample, "draft"} {
		t.Run(string(mode), func(t *testing.T) {
			s, g, a, all, _ := testDoor(t)
			check := &capsStub{err: bridge.Errorf(bridge.CodeDenied, "not allowed")}
			all.Caps = check
			_, err := s.Call(context.Background(), testScope(mode), "gmail.read", nil)
			if err != check.err || check.calls != 1 || g.calls != 0 || a.calls != 0 {
				t.Fatalf("err=%v caps=%d grants=%d adapters=%d", err, check.calls, g.calls, a.calls)
			}
			calls, _ := s.Fakes(context.Background(), testScope(mode), bridge.ConnectFakesParams{})
			if len(calls.Calls) != 0 {
				t.Fatal(calls)
			}
		})
	}
}
func TestResolveRefusalsDoNotWait(t *testing.T) {
	for _, err := range []error{bridge.Errorf(bridge.CodeDenied, "not allowed"), bridge.Errorf(bridge.CodeNotFound, "no account"), connectionError("other"), errors.New("disk failed")} {
		t.Run(err.Error(), func(t *testing.T) {
			s, g, a, _, _ := testDoor(t)
			g.err = err
			_, got := s.Call(context.Background(), testScope(svc.DataReal), "gmail.read", nil)
			if got != err || a.calls != 0 {
				t.Fatalf("err=%v adapters=%d", got, a.calls)
			}
			waits, _ := s.Waits(context.Background(), svc.Caller{Member: "mona"})
			if len(waits.Waits) != 0 {
				t.Fatal(waits)
			}
		})
	}
}

func TestConcurrentReconnectNotificationsWakeOnce(t *testing.T) {
	s, g, _, all, _ := testDoor(t)
	g.err = connectionError(bridge.ReasonExpired)
	ctx := WithInvocation(context.Background(), "run-1", "read")
	_, _ = s.Call(ctx, testScope(svc.DataReal), "gmail.read", nil)
	hook := &waitingRuns{}
	all.Runs = hook
	start := make(chan struct{})
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { <-start; done <- s.Resume("gmail", "mona") }()
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if len(hook.changed) != 1 {
		t.Fatalf("wakes=%d, want 1", len(hook.changed))
	}
}

func TestNeedsAllowPublishesOneSheet(t *testing.T) {
	for _, reason := range []string{bridge.ReasonNeedsAllow, bridge.ReasonExpired} {
		t.Run(reason, func(t *testing.T) {
			s, g, _, all, _ := testDoor(t)
			g.err = connectionError(reason)
			g.sheet = &bridge.AllowSheet{App: "crm", Member: "mona", Service: "gmail", Title: "CRM may read email as Mona"}
			events, cancel := all.Hub.Subscribe(bridge.TopicConnectAllow("mona"))
			defer cancel()
			ctx := WithInvocation(context.Background(), "run-1", "read")
			for i := 0; i < 2; i++ {
				_, _ = s.Call(ctx, testScope(svc.DataReal), "gmail.read", nil)
			}
			if reason == bridge.ReasonNeedsAllow {
				select {
				case event := <-events:
					if !reflect.DeepEqual(event.Data, *g.sheet) {
						t.Fatalf("event=%+v", event)
					}
				default:
					t.Fatal("no Allow sheet")
				}
			}
			select {
			case event := <-events:
				t.Fatalf("unexpected sheet=%+v", event)
			default:
			}
		})
	}
}

func TestSweepContinuesWhenAnotherConnectionCannotResume(t *testing.T) {
	s, g, _, all, clock := testDoor(t)
	g.err = connectionError(bridge.ReasonExpired)
	ctx := context.Background()
	now := *clock
	_, _ = s.Call(WithInvocation(ctx, "new-wait", "read"), testScope(svc.DataReal), "gmail.read", nil)
	*clock = now.Add(-waitLifetime)
	_, _ = s.Call(WithInvocation(ctx, "due-wait", "read"), testScope(svc.DataReal), "gmail.read", nil)
	*clock = now
	g.err = nil
	hook := &waitingRuns{errForRun: map[string]error{"new-wait": errors.New("still expired")}}
	all.Runs = hook
	if err := s.Sweep(ctx); err == nil {
		t.Fatal("resume failure was hidden")
	}
	waits, err := s.Waits(ctx, svc.Caller{Member: "mona"})
	if err != nil || len(waits.Waits) != 1 || waits.Waits[0].RunID != "new-wait" || len(hook.changed) != 2 || !hook.stopped[1] {
		t.Fatalf("waits=%+v hook=%+v err=%v", waits, hook, err)
	}
}
