package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type surfaceProbe struct {
	dispatchProbe
	holds             bool
	heldScope         svc.Scope
	heldDoc, heldPart string
	holdCalls         int
	lookupErr         error
	comment           bridge.Comment
	dropped           chan string
}

type surfaceParts struct {
	svc.Parts
	p *surfaceProbe
}
type surfaceCollab struct {
	svc.Collab
	p *surfaceProbe
}

func (s surfaceParts) ListDocs(_ context.Context, scope svc.Scope, p bridge.ListDocsParams) (bridge.ListDocsResult, error) {
	s.p.save(bridge.MethodDocsList, scope, p)
	return bridge.ListDocsResult{}, nil
}
func (s surfaceParts) GetDoc(_ context.Context, scope svc.Scope, p bridge.GetDocParams) (bridge.DocInfo, error) {
	s.p.save(bridge.MethodDocsGet, scope, p)
	return bridge.DocInfo{}, nil
}
func (s surfaceParts) CreateDoc(_ context.Context, scope svc.Scope, p bridge.CreateDocParams) (bridge.DocInfo, error) {
	s.p.save(bridge.MethodDocsCreate, scope, p)
	return bridge.DocInfo{}, nil
}
func (s surfaceParts) Reorder(_ context.Context, scope svc.Scope, p bridge.ReorderDocParams) (bridge.DocInfo, error) {
	s.p.save(bridge.MethodDocsReorder, scope, p)
	return bridge.DocInfo{}, nil
}
func (s surfaceParts) Get(_ context.Context, scope svc.Scope, p bridge.GetPartParams) (bridge.Part, error) {
	s.p.save(bridge.MethodPartsGet, scope, p)
	return bridge.Part{}, nil
}
func (s surfaceParts) Write(_ context.Context, scope svc.Scope, p bridge.WritePartParams) (bridge.Part, error) {
	s.p.save(bridge.MethodPartsWrite, scope, p)
	return bridge.Part{}, nil
}
func (s surfaceParts) Add(_ context.Context, scope svc.Scope, p bridge.AddPartParams) (bridge.DocInfo, error) {
	s.p.save(bridge.MethodPartsAdd, scope, p)
	return bridge.DocInfo{}, nil
}
func (s surfaceParts) Remove(_ context.Context, scope svc.Scope, p bridge.RemovePartParams) (bridge.DocInfo, error) {
	s.p.save(bridge.MethodPartsRemove, scope, p)
	return bridge.DocInfo{}, nil
}
func (s surfaceParts) Versions(_ context.Context, scope svc.Scope, p bridge.ListPartVersionsParams) (bridge.ListPartVersionsResult, error) {
	s.p.save(bridge.MethodPartsVersions, scope, p)
	return bridge.ListPartVersionsResult{}, nil
}
func (s surfaceParts) Diff(_ context.Context, scope svc.Scope, p bridge.DiffPartParams) (bridge.DiffPartResult, error) {
	s.p.save(bridge.MethodPartsDiff, scope, p)
	return bridge.DiffPartResult{}, nil
}
func (s surfaceParts) Restore(_ context.Context, scope svc.Scope, p bridge.RestorePartParams) (bridge.Part, error) {
	s.p.save(bridge.MethodPartsRestore, scope, p)
	return bridge.Part{}, nil
}
func (s surfaceParts) SetShare(_ context.Context, scope svc.Scope, p bridge.SetShareParams) (bridge.Share, error) {
	s.p.save(bridge.MethodShareSet, scope, p)
	return bridge.Share{}, nil
}
func (s surfaceParts) OpenShare(_ context.Context, scope svc.Scope, p bridge.OpenShareParams) (bridge.DocInfo, error) {
	s.p.save(bridge.MethodShareOpen, scope, p)
	return bridge.DocInfo{}, nil
}
func (s surfaceCollab) Lock(_ context.Context, scope svc.Scope, p bridge.LockPartParams) (bridge.Lock, error) {
	s.p.save(bridge.MethodPartsLock, scope, p)
	return bridge.Lock{}, nil
}
func (s surfaceCollab) Takeover(_ context.Context, scope svc.Scope, p bridge.TakeoverPartParams) (bridge.Lock, error) {
	s.p.save(bridge.MethodPartsTakeover, scope, p)
	return bridge.Lock{}, nil
}
func (s surfaceCollab) Answer(_ context.Context, scope svc.Scope, p bridge.AnswerPartParams) (bridge.Lock, error) {
	s.p.save(bridge.MethodPartsAnswer, scope, p)
	return bridge.Lock{}, nil
}
func (s surfaceCollab) Release(_ context.Context, scope svc.Scope, p bridge.ReleasePartParams) (bridge.Empty, error) {
	s.p.save(bridge.MethodPartsRelease, scope, p)
	return bridge.Empty{}, nil
}
func (s surfaceCollab) SetPresence(_ context.Context, scope svc.Scope, p bridge.SetPresenceParams) (bridge.Empty, error) {
	s.p.save(bridge.MethodPresenceSet, scope, p)
	return bridge.Empty{}, nil
}
func (s surfaceCollab) ListComments(_ context.Context, scope svc.Scope, p bridge.ListCommentsParams) (bridge.ListCommentsResult, error) {
	s.p.save(bridge.MethodCommentsList, scope, p)
	return bridge.ListCommentsResult{}, nil
}
func (s surfaceCollab) AddComment(_ context.Context, scope svc.Scope, p bridge.AddCommentParams) (bridge.Comment, error) {
	s.p.save(bridge.MethodCommentsAdd, scope, p)
	return bridge.Comment{}, nil
}
func (s surfaceCollab) ResolveComment(_ context.Context, scope svc.Scope, p bridge.ResolveCommentParams) (bridge.Comment, error) {
	s.p.save(bridge.MethodCommentsResolve, scope, p)
	return bridge.Comment{}, nil
}
func (s surfaceCollab) Comment(_ context.Context, scope svc.Scope, p bridge.SendCommentParams) (bridge.Comment, error) {
	s.p.save(bridge.MethodCommentsSend, scope, p)
	return s.p.comment, s.p.lookupErr
}

func (s surfaceCollab) Holds(scope svc.Scope, doc, part string) bool {
	s.p.heldScope, s.p.heldDoc, s.p.heldPart = scope, doc, part
	s.p.holdCalls++
	return s.p.holds
}
func (s surfaceCollab) Dropped(member string) {
	select {
	case s.p.dropped <- member:
	default:
	}
}

type surfaceAgent struct {
	svc.Agent
	params bridge.AgentSendParams
	scope  svc.Scope
	calls  int
	err    error
}

func (a *surfaceAgent) Send(_ context.Context, scope svc.Scope, p bridge.AgentSendParams) (bridge.AgentSendResult, error) {
	a.calls++
	a.scope, a.params = scope, p
	return bridge.AgentSendResult{Thread: "thread-1", Turn: "turn-1"}, a.err
}

func TestSurfaceDispatch(t *testing.T) {
	scope := svc.Scope{App: "slides", Data: svc.DataSample, Caller: svc.Caller{Member: "mona", Role: svc.RoleUser}}
	tests := []struct {
		method string
		params any
	}{
		{bridge.MethodDocsList, bridge.ListDocsParams{Surface: bridge.SurfaceCanvas}},
		{bridge.MethodDocsGet, bridge.GetDocParams{Doc: "decks/q4"}},
		{bridge.MethodDocsCreate, bridge.CreateDocParams{Doc: "decks/q4", Surface: bridge.SurfaceCanvas, Title: "Q4", Parts: []bridge.CreateDocPart{{Part: "slides/1.json", Text: "{}"}}}},
		{bridge.MethodDocsReorder, bridge.ReorderDocParams{Doc: "decks/q4", Parts: []string{"slides/2.json", "slides/1.json"}}},
		{bridge.MethodPartsGet, bridge.GetPartParams{Doc: "decks/q4", Part: "slides/1.json", Version: 2}},
		{bridge.MethodPartsLock, bridge.LockPartParams{Doc: "decks/q4", Part: "slides/1.json"}},
		{bridge.MethodPartsTakeover, bridge.TakeoverPartParams{Doc: "decks/q4", Part: "slides/1.json"}},
		{bridge.MethodPartsAnswer, bridge.AnswerPartParams{Doc: "decks/q4", Part: "slides/1.json", Keep: true}},
		{bridge.MethodPartsRelease, bridge.ReleasePartParams{Doc: "decks/q4", Part: "slides/1.json"}},
		{bridge.MethodPartsWrite, bridge.WritePartParams{Doc: "decks/q4", Part: "slides/1.json", Text: "updated", BaseVersion: 2, Kind: bridge.PartVersionKindNudge, Label: "shorter", Extra: &bridge.PartExtra{Formulas: map[string]string{"C5": "=SUM(C2:C4)"}}}},
		{bridge.MethodPartsAdd, bridge.AddPartParams{Doc: "decks/q4", Part: "slides/2.json", Text: "{}"}},
		{bridge.MethodPartsRemove, bridge.RemovePartParams{Doc: "decks/q4", Part: "slides/2.json"}},
		{bridge.MethodPartsVersions, bridge.ListPartVersionsParams{Doc: "decks/q4", Part: "slides/1.json"}},
		{bridge.MethodPartsDiff, bridge.DiffPartParams{Doc: "decks/q4", Part: "slides/1.json", From: 1, To: 2}},
		{bridge.MethodPartsRestore, bridge.RestorePartParams{Doc: "decks/q4", Part: "slides/1.json", Version: 1}},
		{bridge.MethodPresenceSet, bridge.SetPresenceParams{Doc: "decks/q4", Part: "slides/1.json"}},
		{bridge.MethodCommentsList, bridge.ListCommentsParams{Doc: "decks/q4", Part: "slides/1.json", State: bridge.CommentStateOpen}},
		{bridge.MethodCommentsAdd, bridge.AddCommentParams{Doc: "decks/q4", Part: "slides/1.json", Anchor: bridge.CommentAnchor{Box: "chart"}, Text: "annual numbers", For: bridge.CommentForAgent}},
		{bridge.MethodCommentsResolve, bridge.ResolveCommentParams{ID: "comment-1"}},
		{bridge.MethodCommentsSend, bridge.SendCommentParams{ID: "comment-1"}},
		{bridge.MethodShareSet, bridge.SetShareParams{Doc: "decks/q4", On: true}},
		{bridge.MethodShareOpen, bridge.OpenShareParams{Slug: "q4-board"}},
	}
	if len(tests) != len(surfaceMethods) {
		t.Fatalf("dispatch coverage=%d methods=%d", len(tests), len(surfaceMethods))
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			p := &surfaceProbe{holds: true, comment: bridge.Comment{Doc: "decks/q4", Part: "slides/1.json", Text: "annual numbers"}}
			agent := &surfaceAgent{}
			all := &svc.Services{Parts: surfaceParts{p: p}, Collab: surfaceCollab{p: p}, Agent: agent}
			entry, ok := lookupMethod(tt.method)
			if !ok || entry.Global {
				t.Fatal("missing app-scoped dispatch")
			}
			raw, err := json.Marshal(tt.params)
			if err != nil {
				t.Fatal(err)
			}
			result, err := entry.Handle(context.Background(), all, scope, raw)
			if err != nil {
				t.Fatal(err)
			}
			if p.method != tt.method || p.scope != scope || !reflect.DeepEqual(p.params, tt.params) {
				t.Fatalf("dispatch=%+v params=%+v", p, tt.params)
			}
			if tt.method == bridge.MethodPartsWrite {
				if p.holdCalls != 1 || p.heldScope != scope || p.heldDoc != "decks/q4" || p.heldPart != "slides/1.json" {
					t.Fatalf("lock check=%+v", p)
				}
			} else if p.holdCalls != 0 {
				t.Fatal("unexpected lock check")
			}
			if tt.method == bridge.MethodCommentsSend {
				want := bridge.AgentSendParams{Text: "Document decks/q4, part slides/1.json:\nannual numbers", Record: "decks/q4/slides/1.json"}
				if agent.calls != 1 || agent.params != want || agent.scope != scope {
					t.Fatalf("agent=%+v", agent)
				}
				if result != (bridge.AgentSendResult{Thread: "thread-1", Turn: "turn-1"}) {
					t.Fatalf("result=%+v", result)
				}
			} else if agent.calls != 0 {
				t.Fatal("unexpected agent send")
			}
			p.method = ""
			if _, err := entry.Handle(context.Background(), all, scope, json.RawMessage(`[`)); err == nil {
				t.Fatal("malformed params accepted")
			}
			if p.method != "" {
				t.Fatal("malformed params reached service")
			}
		})
	}
}

func TestSurfaceWriteRequiresCallerLock(t *testing.T) {
	p := &surfaceProbe{}
	all := &svc.Services{Parts: surfaceParts{p: p}, Collab: surfaceCollab{p: p}}
	_, err := surfaceMethods[bridge.MethodPartsWrite].Handle(context.Background(), all, svc.Scope{App: "slides", Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, Data: svc.DataSample}, json.RawMessage(`{"doc":"decks/q4","part":"slides/1.json"}`))
	var wire *bridge.Error
	if !errors.As(err, &wire) || wire.Code != bridge.CodeConflict {
		t.Fatalf("error=%v", err)
	}
	if p.method != "" || p.holdCalls != 1 {
		t.Fatal("unlocked write reached Parts")
	}
}

func TestSurfaceCommentSendPropagatesErrors(t *testing.T) {
	for _, at := range []string{"lookup", "agent"} {
		t.Run(at, func(t *testing.T) {
			want := bridge.Errorf(bridge.CodeNotFound, "missing comment")
			p := &surfaceProbe{}
			agent := &surfaceAgent{}
			if at == "lookup" {
				p.lookupErr = want
			} else {
				agent.err = want
			}
			all := &svc.Services{Collab: surfaceCollab{p: p}, Agent: agent}
			_, err := sendComment(context.Background(), all, svc.Scope{App: "slides", Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, Data: svc.DataSample}, json.RawMessage(`{"id":"comment-1"}`))
			if err != want {
				t.Fatalf("error=%v", err)
			}
			if at == "lookup" && agent.calls != 0 {
				t.Fatal("failed lookup reached agent")
			}
		})
	}
}

func TestSurfaceSocketCloseDropsMember(t *testing.T) {
	resetCallerResolver(t)
	SetCallerResolver(func(context.Context, *http.Request, string) svc.Caller {
		return svc.Caller{Member: "mona", Role: svc.RoleUser}
	})
	all := testServices(t)
	p := &surfaceProbe{dropped: make(chan string, 1)}
	all.Collab = surfaceCollab{p: p}
	RegisterCollabDisconnect(all.Collab)
	all.Parts = surfaceParts{p: p}
	ctx, url := testServer(t, all)
	client := dialSocket(t, ctx, url)
	id := client.send(bridge.TypeCall, "crm", bridge.MethodDocsGet, "", bridge.GetDocParams{Doc: "decks/q4"})
	if reply := client.read(); reply.ID != id || reply.Type != bridge.TypeResult {
		t.Fatalf("reply=%+v", reply)
	}
	_ = client.conn.CloseNow()
	select {
	case member := <-p.dropped:
		if member != "mona" {
			t.Fatalf("dropped member=%q", member)
		}
	case <-ctx.Done():
		t.Fatal("WebSocket close did not drop collaboration")
	}
}
