package main

// Execution wiring at the v3 door: one wsexec.Adapter over collections.db,
// handed to wsapi as Executor, to the session as Config.Exec and
// RegisterExecutor, and to the surface as tui3.Exec. Nil stays absence when
// collections did not open — never a memory fallback that pretends a launch
// completed.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/wsapi"
	"github.com/Agent-Field/codeaf/internal/wsexec"
)

var (
	_ wsexec.Store     = (*workspaceExecStore)(nil)
	_ wsexec.Runtime   = (*liveExecRuntime)(nil)
	_ wsapi.Executor   = (*wsapiExecBridge)(nil)
	_ session.Exec     = (*sessionExec)(nil)
	_ tui3.Exec        = (*tuiExec)(nil)
	_ session.Executor = (*sessionExec)(nil)
)

var (
	v3ExecMu      sync.Mutex
	v3ExecAdapter *wsexec.Adapter
	v3ExecRuntime = &liveExecRuntime{agents: map[string]*session.Agent{}, byRun: map[string]execLive{}}
)

type execLive struct {
	agent                    *session.Agent
	chatID, requestKey, road string
	id                       uint64
}

func setV3Executor(a *wsexec.Adapter) {
	v3ExecMu.Lock()
	defer v3ExecMu.Unlock()
	v3ExecAdapter = a
}

func currentV3Executor() *wsexec.Adapter {
	v3ExecMu.Lock()
	defer v3ExecMu.Unlock()
	return v3ExecAdapter
}

func bindV3Exec(svc *wsapi.Service) {
	if svc == nil || svc.Workspace() == nil {
		return
	}
	adapter := wsexec.Open(&workspaceExecStore{jobs: svc.Workspace()}, v3ExecRuntime)
	svc.SetExecutor(&wsapiExecBridge{inner: adapter})
	session.RegisterExecutor(&sessionExec{svc: svc})
	setV3Executor(adapter)
}

func bindAgentExec(agent *session.Agent, cfg session.Config) {
	id := conversationChatIDFrom(cfg.Place, cfg.SessionFile)
	if agent == nil || id == "" {
		return
	}
	v3ExecRuntime.bind(id, agent)
}

func sessionExecOf(folders session.Folders, chatID string) session.Exec {
	wrapped, ok := folders.(*sessionFolders)
	if !ok || wrapped == nil || wrapped.svc == nil || strings.TrimSpace(chatID) == "" {
		return nil
	}
	if wrapped.svc.Workspace() == nil {
		return nil
	}
	return &sessionExec{svc: wrapped.svc, chatID: chatID}
}

func surfaceExec(folders session.Folders) tui3.Exec {
	wrapped, ok := folders.(*sessionFolders)
	if !ok || wrapped == nil || wrapped.svc == nil {
		return nil
	}
	return &tuiExec{svc: wrapped.svc}
}

func attachSurfaceExec(options *tui3.Options, folders session.Folders) {
	if options == nil {
		return
	}
	options.Exec = surfaceExec(folders)
}

type sessionExec struct {
	svc    *wsapi.Service
	chatID string
}

func (e *sessionExec) LaunchOrJoin(ctx context.Context, grantID, brief, equivalenceKey string) (session.ExecView, error) {
	key, err := mintExecRequestKey()
	if err != nil {
		return session.ExecView{}, err
	}
	got, err := e.svc.LaunchOrJoin(ctx, wsapi.LaunchWorkRequest{
		GrantID: grantID, CoordinatorID: e.chatID, OwnerChatID: e.chatID,
		Brief: brief, EquivalenceKey: equivalenceKey, IdempotencyKey: key,
	})
	if err != nil {
		return session.ExecView{}, err
	}
	return sessionViewOf(got), nil
}

// IssuePersonGrant is the production door IssueGrant was missing: software
// stamps OriginPerson, an empty parent issuer, execute class, and this chat
// as coordinator. A model cannot mint grant_id through the schema.
func (e *sessionExec) IssuePersonGrant(ctx context.Context, brief string) (string, error) {
	if e == nil || e.svc == nil {
		return "", fmt.Errorf("%w: executor is absent", workspace.ErrInvalid)
	}
	if strings.TrimSpace(e.chatID) == "" {
		return "", fmt.Errorf("%w: grant needs a coordinator", workspace.ErrInvalid)
	}
	got, err := e.svc.IssueGrant(ctx, wsapi.GrantRequest{
		CoordinatorID: e.chatID, Goal: brief, ChatIDs: []string{e.chatID},
		ActionClasses: []string{workspace.ClassExecute},
	})
	if err != nil {
		return "", err
	}
	return got.ID, nil
}

func (e *sessionExec) Inspect(ctx context.Context, workID string) (session.ExecView, error) {
	got, err := e.svc.InspectWork(ctx, workID)
	if err != nil {
		return session.ExecView{}, err
	}
	return sessionViewOf(got), nil
}

func (e *sessionExec) Steer(ctx context.Context, workID, text, personRequestID string) error {
	view, err := e.svc.InspectWork(ctx, workID)
	if err != nil {
		return err
	}
	return e.svc.SteerWork(ctx, wsapi.SteerRevision{
		WorkID: workID, GrantID: view.GrantID, Text: text, PersonRequestID: personRequestID,
	})
}

func (e *sessionExec) PauseWork(ctx context.Context, workID string) error {
	return e.svc.PauseWork(ctx, workID)
}

func (e *sessionExec) StopWork(ctx context.Context, workID string) error {
	return e.svc.StopWork(ctx, workID)
}

func (e *sessionExec) Observe(ctx context.Context, workID string) (session.ExecResult, error) {
	got, err := e.svc.ObserveWork(ctx, workID)
	if err != nil {
		return session.ExecResult{}, err
	}
	return session.ExecResult{WorkID: got.WorkID, RunInstanceID: got.RunInstanceID, State: got.State, Detail: got.Detail}, nil
}

func sessionViewOf(v wsapi.WorkView) session.ExecView {
	return session.ExecView{
		WorkID: v.WorkID, RequestKey: v.RequestKey, RunInstanceID: v.RunInstanceID,
		Road: v.Road, State: v.State, Joined: v.Joined,
	}
}

func mintExecRequestKey() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

type tuiExec struct{ svc *wsapi.Service }

func (e *tuiExec) LaunchState(ctx context.Context, conversationID string) ([]tui3.ExecWork, error) {
	jobs := e.svc.Workspace()
	if jobs == nil {
		return nil, fmt.Errorf("%w: executor is absent", workspace.ErrInvalid)
	}
	rows, err := jobs.ListBindingsForChat(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	out := make([]tui3.ExecWork, 0, len(rows))
	for _, row := range rows {
		out = append(out, execWorkOf(row, conversationID))
	}
	return out, nil
}

func (e *tuiExec) PauseCoordination(ctx context.Context, coordinatorID string) error {
	return e.svc.PauseCoordination(ctx, coordinatorID)
}

func (e *tuiExec) StopWork(ctx context.Context, workID string) error {
	return e.svc.StopWork(ctx, workID)
}

func execWorkOf(row workspace.ExecutionBinding, chatID string) tui3.ExecWork {
	title := strings.TrimSpace(row.WorkID)
	if title == "" {
		title = row.RequestKey
	}
	joined := row.JoinedBy(chatID)
	source := row.OwnerChatID
	if source == "" {
		source = row.CoordinatorID
	}
	if joined {
		source = chatID
	}
	workID := row.WorkID
	if workID == "" {
		workID = row.RequestKey
	}
	return tui3.ExecWork{WorkID: workID, Title: title, State: row.State, Road: row.Road, SourceRef: source, Joined: joined}
}

type wsapiExecBridge struct{ inner *wsexec.Adapter }

func (b *wsapiExecBridge) LaunchOrJoin(ctx context.Context, req wsapi.LaunchRequest) (wsapi.WorkView, error) {
	got, err := b.inner.LaunchOrJoin(ctx, wsexec.LaunchRequest{
		RequestKey: req.RequestKey, EquivalenceKey: req.EquivalenceKey, GrantID: req.GrantID,
		CoordinatorID: req.CoordinatorID, OwnerChatID: req.OwnerChatID, Brief: req.Brief,
		GrantRev: req.GrantRev, AssignmentRev: req.AssignmentRev,
	})
	return wsapiViewOf(got), err
}

func (b *wsapiExecBridge) Inspect(ctx context.Context, workID string) (wsapi.WorkView, error) {
	got, err := b.inner.Inspect(ctx, workID)
	return wsapiViewOf(got), err
}

func (b *wsapiExecBridge) Steer(ctx context.Context, rev wsapi.SteerRevision) error {
	return b.inner.Steer(ctx, wsexec.SteerRevision{
		WorkID: rev.WorkID, GrantID: rev.GrantID, Text: rev.Text, PersonRequestID: rev.PersonRequestID, GrantRev: rev.GrantRev,
	})
}

func (b *wsapiExecBridge) PauseWork(ctx context.Context, workID string) error {
	return b.inner.PauseWork(ctx, workID)
}

func (b *wsapiExecBridge) StopWork(ctx context.Context, workID string) error {
	return b.inner.StopWork(ctx, workID)
}

func (b *wsapiExecBridge) Observe(ctx context.Context, workID string) (wsapi.ResultView, error) {
	got, err := b.inner.Observe(ctx, workID)
	return wsapi.ResultView{WorkID: got.WorkID, RunInstanceID: got.RunInstanceID, State: got.State, Detail: got.Detail}, err
}

func (b *wsapiExecBridge) Recover(ctx context.Context, requestKey string) (wsapi.WorkView, error) {
	got, err := b.inner.Recover(ctx, requestKey)
	return wsapiViewOf(got), err
}

func wsapiViewOf(v wsexec.WorkView) wsapi.WorkView {
	return wsapi.WorkView{
		WorkID: v.WorkID, RequestKey: v.RequestKey, RunInstanceID: v.RunInstanceID,
		Road: v.Road, State: v.State, OwnerChatID: v.OwnerChatID, GrantID: v.GrantID, Joined: v.Joined,
	}
}

func recoverUnboundWork(ctx context.Context, jobs *workspace.Store) {
	if jobs == nil {
		return
	}
	adapter := currentV3Executor()
	if adapter == nil {
		adapter = wsexec.Open(&workspaceExecStore{jobs: jobs}, v3ExecRuntime)
	}
	rows, err := jobs.ListUnboundBindings(ctx)
	if err != nil {
		return
	}
	for _, row := range rows {
		_, _ = adapter.Recover(ctx, row.RequestKey)
	}
}
