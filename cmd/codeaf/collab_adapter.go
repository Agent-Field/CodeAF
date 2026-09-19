package main

// Collaboration wiring at the v3 door: one wscollab.Router over collections.db,
// handed to wsapi as Collaborator, to the session as Config.Collab and
// RegisterCollabRouter, and to the surface as tui3.Collab. Nil stays absence
// when collections did not open — never a memory fallback that pretends
// deliveries were recorded.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/wsapi"
	"github.com/Agent-Field/codeaf/internal/wscollab"
)

var (
	_ wscollab.Store       = (*workspaceCollabStore)(nil)
	_ session.Collab       = (*sessionCollab)(nil)
	_ tui3.Collab          = (*tuiCollab)(nil)
	_ wsapi.Collaborator   = (*wsapiCollaborator)(nil)
	_ session.CollabRouter = (*sessionCollabRouter)(nil)
	_ wscollab.Seam        = (*agentCollabSeam)(nil)

	v3CollabMu     sync.Mutex
	v3CollabRouter *wscollab.Router
)

func setV3CollabRouter(r *wscollab.Router) {
	v3CollabMu.Lock()
	defer v3CollabMu.Unlock()
	v3CollabRouter = r
}

func currentV3CollabRouter() *wscollab.Router {
	v3CollabMu.Lock()
	defer v3CollabMu.Unlock()
	return v3CollabRouter
}

func bindV3Collab(svc *wsapi.Service) {
	if svc == nil || svc.Workspace() == nil {
		return
	}
	router, err := wscollab.New(&workspaceCollabStore{jobs: svc.Workspace()})
	if err != nil {
		return
	}
	svc.SetCollaborator(&wsapiCollaborator{router: router})
	session.RegisterCollabRouter(&sessionCollabRouter{router: router})
	session.RegisterPutAwayCollab(func(ctx context.Context, conversationID string, archived bool) error {
		if archived {
			return svc.ArchiveCoordination(ctx, conversationID)
		}
		return svc.RestoreCoordination(ctx, conversationID)
	})
	setV3CollabRouter(router)
}

func v3ChatID(sessionFile string) string {
	return filepath.Base(filepath.Dir(strings.TrimSpace(sessionFile)))
}

func conversationChatIDFrom(place session.Place, sessionFile string) string {
	if id := strings.TrimSpace(place.ID()); id != "" {
		return id
	}
	return v3ChatID(sessionFile)
}

func bindAgentCollab(agent *session.Agent, cfg session.Config) {
	router := currentV3CollabRouter()
	if router == nil || agent == nil {
		return
	}
	id := conversationChatIDFrom(cfg.Place, cfg.SessionFile)
	if id == "" {
		return
	}
	_, _ = router.Bind(context.Background(), id, &agentCollabSeam{agent: agent})
}

type agentCollabSeam struct{ agent *session.Agent }

func (s *agentCollabSeam) Recorded(id wscollab.DeliveryID) bool {
	if s == nil || s.agent == nil {
		return false
	}
	return s.agent.CollabRecorded(string(id))
}

func (s *agentCollabSeam) Append(_ context.Context, env wscollab.Envelope) error {
	if s == nil || s.agent == nil {
		return wscollab.ErrRetired
	}
	s.agent.ReceiveCollab(session.CollabLine{
		DeliveryID: string(env.ID), CauseID: env.CauseID, FromChatID: env.From,
		Body: env.Body, Origin: env.Origin, ActorID: env.ActorID,
		Pattern: env.Pattern, DiscussionID: env.Discussion,
	})
	return nil
}

func (s *agentCollabSeam) Accept(context.Context, wscollab.Envelope) string {
	if s == nil || s.agent == nil {
		return wscollab.QueueNobody
	}
	return wscollab.QueueAccepted
}

func sessionCollabOf(folders session.Folders, chatID string) session.Collab {
	wrapped, ok := folders.(*sessionFolders)
	if !ok || wrapped == nil || wrapped.svc == nil || strings.TrimSpace(chatID) == "" {
		return nil
	}
	return &sessionCollab{svc: wrapped.svc, chatID: chatID}
}

func surfaceCollab(folders session.Folders) tui3.Collab {
	wrapped, ok := folders.(*sessionFolders)
	if !ok || wrapped == nil || wrapped.svc == nil {
		return nil
	}
	return &tuiCollab{svc: wrapped.svc}
}

func attachSurfaceCollab(options *tui3.Options, folders session.Folders) {
	if options == nil {
		return
	}
	options.Collab = surfaceCollab(folders)
}

type sessionCollab struct {
	svc    *wsapi.Service
	chatID string
}

func (c *sessionCollab) Deliver(ctx context.Context, to []string, body, pattern, discussionID string) ([]session.CollabReceipt, error) {
	got, err := c.svc.Deliver(ctx, wsapi.DeliverRequest{
		FromChatID: c.chatID, ToChatIDs: to, Body: body, Pattern: pattern, DiscussionID: discussionID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]session.CollabReceipt, 0, len(got))
	for _, one := range got {
		out = append(out, session.CollabReceipt{DeliveryID: one.DeliveryID, CauseID: one.CauseID, ToChatID: one.ToChatID, State: one.State})
	}
	return out, nil
}

func (c *sessionCollab) Invite(ctx context.Context, discussionID, sourceChatID, role string) error {
	_, err := c.svc.InviteToDiscussion(ctx, wsapi.InviteRequest{DiscussionID: discussionID, SourceChatID: sourceChatID, Role: role})
	return err
}

func (c *sessionCollab) InspectScope(ctx context.Context) (session.CollabScope, error) {
	got, err := c.svc.InspectScope(ctx, c.chatID)
	if err != nil {
		return session.CollabScope{}, err
	}
	return session.CollabScope{Kind: got.Kind, FolderID: got.FolderID, ChatIDs: got.ChatIDs}, nil
}

func (c *sessionCollab) CoordinateSelected(ctx context.Context, chatIDs []string) error {
	_, err := c.svc.CoordinateSelected(ctx, wsapi.CoordinateRequest{CoordinatorID: c.chatID, ChatIDs: chatIDs})
	return err
}

func (c *sessionCollab) ManageFolder(ctx context.Context, folderID string) error {
	_, err := c.svc.ManageFolder(ctx, wsapi.ManageFolderRequest{CoordinatorID: c.chatID, FolderID: folderID})
	return err
}

func (c *sessionCollab) Pause(ctx context.Context) error {
	return c.svc.PauseCoordination(ctx, c.chatID)
}

func (c *sessionCollab) Contribute(ctx context.Context, discussionID string, inv session.CollabInvocation, body string) error {
	router := currentV3CollabRouter()
	if router == nil {
		return fmt.Errorf("%w: collaborator is absent", workspace.ErrInvalid)
	}
	_, err := router.Contribute(ctx, discussionID, wscollab.Invocation{
		ID: inv.ID, ActorID: inv.ActorID, Role: inv.Role, Source: inv.Source,
	}, body)
	return err
}

type tuiCollab struct {
	svc *wsapi.Service
	mu  sync.Mutex
	ids []string
}

func (c *tuiCollab) Mark(_ context.Context, refID string) error {
	refID = strings.TrimSpace(refID)
	if refID == "" {
		return workspace.ErrInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range c.ids {
		if id == refID {
			return nil
		}
	}
	c.ids = append(c.ids, refID)
	return nil
}

func (c *tuiCollab) Unmark(_ context.Context, refID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.ids[:0]
	for _, id := range c.ids {
		if id != refID {
			out = append(out, id)
		}
	}
	c.ids = out
	return nil
}

func (c *tuiCollab) Marked(context.Context) ([]tui3.CollabMark, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]tui3.CollabMark, 0, len(c.ids))
	for _, id := range c.ids {
		out = append(out, tui3.CollabMark{RefID: id})
	}
	return out, nil
}

func (c *tuiCollab) markedIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.ids...)
}

func (c *tuiCollab) CoordinateMarked(ctx context.Context, coordinatorID string) error {
	ids := c.markedIDs()
	_, err := c.svc.CoordinateSelected(ctx, wsapi.CoordinateRequest{CoordinatorID: coordinatorID, ChatIDs: ids})
	return err
}

func (c *tuiCollab) Activity(ctx context.Context, coordinatorID string) ([]tui3.CollabActivity, error) {
	jobs := c.svc.Workspace()
	if jobs == nil {
		return nil, nil
	}
	rows, err := jobs.ListChatTraffic(ctx, coordinatorID)
	if err != nil {
		return nil, err
	}
	out := make([]tui3.CollabActivity, 0, len(rows))
	for _, d := range rows {
		kind, source := collabPaintKind(coordinatorID, d)
		if kind == "" {
			continue
		}
		out = append(out, tui3.CollabActivity{
			DeliveryID: d.ID, Pattern: d.Pattern, Body: d.Body,
			SourceRef: source, Kind: kind, ToTitle: source,
		})
	}
	return out, nil
}

func (c *tuiCollab) Participants(ctx context.Context, discussionID string) ([]tui3.CollabParticipant, error) {
	people, err := c.svc.ListParticipants(ctx, discussionID)
	if err != nil {
		return nil, err
	}
	out := make([]tui3.CollabParticipant, 0, len(people))
	for _, p := range people {
		out = append(out, tui3.CollabParticipant{Role: p.Role, SourceTitle: p.SourceChatID})
	}
	return out, nil
}

func collabPaintKind(coordinatorID string, d workspace.Delivery) (kind, source string) {
	from := strings.TrimSpace(d.FromChatID)
	to := strings.TrimSpace(d.ToChatID)
	if to == coordinatorID && from != coordinatorID {
		return "reply", from
	}
	if d.Pattern == workspace.PatternFanout {
		return "sent", to
	}
	if d.Pattern == workspace.PatternDiscussion && from != coordinatorID {
		return "reply", from
	}
	return "request", to
}

type wsapiCollaborator struct{ router *wscollab.Router }

func (c *wsapiCollaborator) Deliver(ctx context.Context, env wsapi.CollabEnvelope) (wsapi.CollabAck, error) {
	got, err := c.router.Deliver(ctx, env.Origin, collabMessage(env), []string{env.ToChatID})
	if err != nil {
		return wsapi.CollabAck{}, err
	}
	if len(got) == 0 {
		return wsapi.CollabAck{}, wscollab.ErrNotFound
	}
	return collabAck(got[0]), nil
}

func (c *wsapiCollaborator) DeliverMany(ctx context.Context, causeID string, envs []wsapi.CollabEnvelope) ([]wsapi.CollabAck, error) {
	if len(envs) == 0 {
		return nil, nil
	}
	to := make([]string, 0, len(envs))
	for _, env := range envs {
		to = append(to, env.ToChatID)
	}
	msg := collabMessage(envs[0])
	msg.CauseID = causeID
	got, err := c.router.Deliver(ctx, envs[0].Origin, msg, to)
	if err != nil {
		return nil, err
	}
	out := make([]wsapi.CollabAck, 0, len(got))
	for _, one := range got {
		out = append(out, collabAck(one))
	}
	return out, nil
}

func (c *wsapiCollaborator) Resume(ctx context.Context, conversationID string) ([]wsapi.CollabAck, error) {
	got, err := c.router.Resume(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	out := make([]wsapi.CollabAck, 0, len(got))
	for _, one := range got {
		out = append(out, collabAck(one))
	}
	return out, nil
}

func collabMessage(env wsapi.CollabEnvelope) wscollab.Message {
	from := strings.TrimSpace(env.FromChatID)
	if from == "" {
		from = "coordinator"
	}
	return wscollab.Message{
		CauseID: env.CauseID, From: from, ActorID: env.ActorID,
		Body: env.Body, Discussion: env.DiscussionID,
	}
}

func collabAck(got wscollab.Receipt) wsapi.CollabAck {
	state := got.Queue
	if got.Processed {
		state = workspace.DeliveryProcessed
	} else if got.Recorded {
		state = workspace.DeliveryRecorded
	}
	return wsapi.CollabAck{DeliveryID: string(got.ID), CauseID: got.CauseID, ToChatID: got.To, State: state}
}

type sessionCollabRouter struct{ router *wscollab.Router }

func (r *sessionCollabRouter) Deliver(ctx context.Context, to []string, body, pattern, discussionID string) ([]session.CollabReceipt, error) {
	_ = pattern
	got, err := r.router.Deliver(ctx, wscollab.OriginAgent, wscollab.Message{
		From: "coordinator", Body: body, Discussion: discussionID,
	}, to)
	if err != nil {
		return nil, err
	}
	return sessionReceipts(got), nil
}

func (r *sessionCollabRouter) Invite(context.Context, string, string, string) error {
	return nil
}

func (r *sessionCollabRouter) Resume(ctx context.Context, conversationID string) ([]session.CollabReceipt, error) {
	got, err := r.router.Resume(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	return sessionReceipts(got), nil
}

func sessionReceipts(got []wscollab.Receipt) []session.CollabReceipt {
	out := make([]session.CollabReceipt, 0, len(got))
	for _, one := range got {
		out = append(out, session.CollabReceipt{DeliveryID: string(one.ID), CauseID: one.CauseID, ToChatID: one.To, State: one.Queue})
	}
	return out
}

// workspaceCollabStore persists envelopes on collections.db. Discussion and
// invocation rows have no v4 table, so they stay on this process until a later
// schema; Deliver/Resume use the durable deliveries table (J23).
type workspaceCollabStore struct {
	jobs *workspace.Store
	mu   sync.Mutex
	disc map[string]wscollab.Discussion
	inv  map[string]wscollab.Invocation
}

func (s *workspaceCollabStore) Put(ctx context.Context, env wscollab.Envelope) error {
	_, err := s.jobs.PutDelivery(ctx, workspace.Delivery{
		ID: string(env.ID), CauseID: env.CauseID, FromChatID: env.From, ToChatID: env.To,
		Pattern: env.Pattern, Body: env.Body, Origin: env.Origin, ActorID: env.ActorID,
		DiscussionID: env.Discussion, State: workspace.DeliveryPending,
	})
	return err
}

func (s *workspaceCollabStore) Get(ctx context.Context, id wscollab.DeliveryID) (wscollab.Record, error) {
	d, err := s.jobs.GetDelivery(ctx, string(id))
	if err != nil {
		return wscollab.Record{}, err
	}
	return deliveryRecord(d), nil
}

func (s *workspaceCollabStore) SetQueue(ctx context.Context, id wscollab.DeliveryID, queue string) error {
	if queue != wscollab.QueueAccepted {
		return nil
	}
	return s.advanceTo(ctx, string(id), workspace.DeliveryAccepted)
}

func (s *workspaceCollabStore) SetRecorded(ctx context.Context, id wscollab.DeliveryID) error {
	return s.advanceTo(ctx, string(id), workspace.DeliveryRecorded)
}

func (s *workspaceCollabStore) SetProcessed(ctx context.Context, id wscollab.DeliveryID) error {
	return s.advanceTo(ctx, string(id), workspace.DeliveryProcessed)
}

func (s *workspaceCollabStore) advanceTo(ctx context.Context, id, want string) error {
	for range 4 {
		d, err := s.jobs.GetDelivery(ctx, id)
		if err != nil {
			return err
		}
		if collabAckRank(d.State) >= collabAckRank(want) {
			return nil
		}
		next := collabAckNext(d.State)
		if next == "" {
			return nil
		}
		if _, err := s.jobs.AckDelivery(ctx, id, next); err != nil {
			return err
		}
	}
	return workspace.ErrInvalid
}

func collabAckRank(state string) int {
	switch state {
	case workspace.DeliveryPending:
		return 0
	case workspace.DeliveryAccepted:
		return 1
	case workspace.DeliveryRecorded:
		return 2
	case workspace.DeliveryProcessed:
		return 3
	default:
		return -1
	}
}

func collabAckNext(state string) string {
	switch state {
	case workspace.DeliveryPending:
		return workspace.DeliveryAccepted
	case workspace.DeliveryAccepted:
		return workspace.DeliveryRecorded
	case workspace.DeliveryRecorded:
		return workspace.DeliveryProcessed
	default:
		return ""
	}
}

func (s *workspaceCollabStore) Pending(ctx context.Context, conversationID string) ([]wscollab.Envelope, error) {
	rows, err := s.jobs.ListPendingDeliveries(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	out := make([]wscollab.Envelope, 0, len(rows))
	for _, d := range rows {
		out = append(out, deliveryEnvelope(d))
	}
	return out, nil
}

func (s *workspaceCollabStore) Archived(ctx context.Context, conversationID string) (bool, error) {
	people, err := s.jobs.ListParticipants(ctx, conversationID)
	if err != nil {
		return false, err
	}
	for _, row := range people {
		if row.Status == workspace.ParticipantArchived {
			return true, nil
		}
	}
	return false, nil
}

func (s *workspaceCollabStore) PutDiscussion(_ context.Context, d wscollab.Discussion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disc == nil {
		s.disc = map[string]wscollab.Discussion{}
	}
	s.disc[d.ID] = d
	return nil
}

func (s *workspaceCollabStore) GetDiscussion(_ context.Context, id string) (wscollab.Discussion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.disc[id]
	if !ok {
		return wscollab.Discussion{}, wscollab.ErrNotFound
	}
	return d, nil
}

func (s *workspaceCollabStore) PutInvocation(_ context.Context, inv wscollab.Invocation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inv == nil {
		s.inv = map[string]wscollab.Invocation{}
	}
	s.inv[inv.ID] = inv
	return nil
}

func (s *workspaceCollabStore) GetInvocation(_ context.Context, id string) (wscollab.Invocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.inv[id]
	if !ok {
		return wscollab.Invocation{}, wscollab.ErrNotFound
	}
	return inv, nil
}

func deliveryEnvelope(d workspace.Delivery) wscollab.Envelope {
	return wscollab.Envelope{
		ID: wscollab.DeliveryID(d.ID), CauseID: d.CauseID, Pattern: d.Pattern,
		Origin: d.Origin, ActorID: d.ActorID, From: d.FromChatID, To: d.ToChatID,
		Discussion: d.DiscussionID, Body: d.Body, Record: d.Body,
	}
}

func deliveryRecord(d workspace.Delivery) wscollab.Record {
	rec := wscollab.Record{Envelope: deliveryEnvelope(d)}
	rec.Recorded = d.State == workspace.DeliveryRecorded || d.State == workspace.DeliveryProcessed
	rec.Processed = d.State == workspace.DeliveryProcessed
	if d.State == workspace.DeliveryPending {
		rec.Queue = wscollab.QueuePending
	} else {
		rec.Queue = wscollab.QueueAccepted
	}
	return rec
}
