package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

const (
	ScopeSelected       = "selected"
	ScopeFolderDynamic  = "folder-dynamic"
	ActorKindChat       = "chat"
	ActorKindRole       = "role"
	ActorKindFolder     = "folder"
	ParticipantActive   = "active"
	ParticipantPaused   = "paused"
	ParticipantArchived = "archived"
	PatternDirect       = "direct"
	PatternFanout       = "fan-out"
	PatternDiscussion   = "discussion"
	RootRepresentative  = "root"
	OriginAgent         = "agent"
)

// CollabEnvelope copies wscollab.Envelope fields so this package does not
// import wscollab. SetCollaborator injects the router; nil means Deliver is
// absent, not a dummy success.
type CollabEnvelope struct {
	DeliveryID, CauseID, FromChatID, ToChatID, Pattern, Body string
	Origin, ActorID, DiscussionID, IdempotencyKey            string
}

// CollabAck copies wscollab.Receipt fields. State is pending, accepted,
// recorded, or processed — three facts, never inferred from each other.
type CollabAck struct {
	DeliveryID, CauseID, ToChatID, State string
}

// Collaborator is the injected router, the same pattern as Inventory.
type Collaborator interface {
	Deliver(ctx context.Context, env CollabEnvelope) (CollabAck, error)
	DeliverMany(ctx context.Context, causeID string, envs []CollabEnvelope) ([]CollabAck, error)
	Resume(ctx context.Context, conversationID string) ([]CollabAck, error)
}

type CoordinateRequest struct {
	CoordinatorID string
	ChatIDs       []string // marked IDs; order preserved
}

type ManageFolderRequest struct {
	CoordinatorID, FolderID string
}

type ScopeView struct {
	ID, Kind, CoordinatorID, FolderID string
	ChatIDs                           []string
	Revision                          int
}

type DeliverRequest struct {
	FromChatID, Body, Pattern, CauseID, IdempotencyKey, DiscussionID string
	ToChatIDs                                                        []string
}

type DeliverReceipt struct {
	DeliveryID, CauseID, ToChatID, State string
}

type InviteRequest struct {
	DiscussionID, SourceChatID, Role string
}

type ParticipantView struct {
	ActorID, DiscussionID, Kind, Role, SourceChatID, Status string
}

type CreateDiscussionRequest struct {
	ChatID, CoordinatorID, Title, IdempotencyKey string
	FolderIDs                                    []string
}

type DiscussionView struct {
	ChatID, Title string
	FolderIDs     []string
}

// collabRow is the participant/scope record the schema lane persists as
// workspace.Participant. THE DOOR IS OPTIONAL: a v3 *workspace.Store does not
// implement collabStore, so CoordinateSelected stays absent rather than
// inventing a second participant table.
type collabRow struct {
	ID, DiscussionID, ActorID, Kind, Role, SourceChatID, Status string
	ScopeKind, FolderID, SnapshotJSON                           string
	Origin, Actor                                               string
}

type collabStore interface {
	PutParticipant(ctx context.Context, p collabRow) (collabRow, error)
	ListParticipants(ctx context.Context, discussionID string) ([]collabRow, error)
	SetParticipantStatus(ctx context.Context, id, status string) error
}

func (s *Service) requireCollabStore() (collabStore, error) {
	if s == nil || s.store == nil {
		return nil, wrapStoreError(workspace.ErrInvalid)
	}
	c, ok := s.store.(collabStore)
	if !ok {
		return nil, fmt.Errorf("%w: collaboration store is absent", workspace.ErrInvalid)
	}
	return c, nil
}

// CoordinateSelected writes a frozen snapshot of marked ids on the
// coordinator's participant row. A sibling filed later does not join (A16).
func (s *Service) CoordinateSelected(ctx context.Context, req CoordinateRequest) (ScopeView, error) {
	if err := s.ready(ctx); err != nil {
		return ScopeView{}, err
	}
	if req.CoordinatorID == "" {
		return ScopeView{}, fmt.Errorf("%w: coordinator id is empty", workspace.ErrInvalid)
	}
	ids := copyIDs(req.ChatIDs)
	raw, err := json.Marshal(ids)
	if err != nil {
		return ScopeView{}, err
	}
	row, err := s.writeCoordinatorScope(ctx, req.CoordinatorID, ScopeSelected, "", string(raw))
	if err != nil {
		return ScopeView{}, err
	}
	return ScopeView{ID: row.ID, Kind: ScopeSelected, CoordinatorID: req.CoordinatorID, ChatIDs: ids}, nil
}

// ManageFolder writes folder-dynamic scope. InspectScope resolves current
// descendants, including chats filed after this call (A16).
func (s *Service) ManageFolder(ctx context.Context, req ManageFolderRequest) (ScopeView, error) {
	if err := s.ready(ctx); err != nil {
		return ScopeView{}, err
	}
	if req.CoordinatorID == "" || req.FolderID == "" {
		return ScopeView{}, fmt.Errorf("%w: manage-folder needs a coordinator and a folder", workspace.ErrInvalid)
	}
	folder, err := s.lookup(ctx, req.FolderID)
	if err != nil {
		return ScopeView{}, err
	}
	row, err := s.writeCoordinatorScope(ctx, req.CoordinatorID, ScopeFolderDynamic, req.FolderID, "")
	if err != nil {
		return ScopeView{}, err
	}
	ids, err := s.descendantChatIDs(ctx, req.FolderID)
	if err != nil {
		return ScopeView{}, err
	}
	return ScopeView{
		ID: row.ID, Kind: ScopeFolderDynamic, CoordinatorID: req.CoordinatorID,
		FolderID: req.FolderID, ChatIDs: ids, Revision: folder.Revision,
	}, nil
}

// InspectScope returns the live selected snapshot or current folder descendants.
func (s *Service) InspectScope(ctx context.Context, coordinatorID string) (ScopeView, error) {
	if err := s.ready(ctx); err != nil {
		return ScopeView{}, err
	}
	row, err := s.coordinatorRow(ctx, coordinatorID)
	if err != nil {
		return ScopeView{}, err
	}
	view := ScopeView{ID: row.ID, Kind: row.ScopeKind, CoordinatorID: coordinatorID, FolderID: row.FolderID}
	if row.ScopeKind == ScopeFolderDynamic {
		ids, err := s.descendantChatIDs(ctx, row.FolderID)
		if err != nil {
			return ScopeView{}, err
		}
		view.ChatIDs = ids
		if folder, err := s.lookup(ctx, row.FolderID); err == nil {
			view.Revision = folder.Revision
		}
		return view, nil
	}
	view.ChatIDs = parseSnapshot(row.SnapshotJSON)
	return view, nil
}

// Deliver hands one message to one or many recipients through the injected
// collaborator. Nil collaborator is absence, not an invented recorded line.
func (s *Service) Deliver(ctx context.Context, req DeliverRequest) ([]DeliverReceipt, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if s.collab == nil {
		return nil, fmt.Errorf("%w: collaborator is absent", workspace.ErrInvalid)
	}
	if err := validateDeliver(req); err != nil {
		return nil, err
	}
	return s.sendEnvelopes(ctx, deliverEnvelopes(req))
}

// InviteToDiscussion records one representative. Actor IDs are minted by the
// store. A second invite of the same SourceChatID is a no-op, including Root (A17).
func (s *Service) InviteToDiscussion(ctx context.Context, req InviteRequest) (ParticipantView, error) {
	if err := s.ready(ctx); err != nil {
		return ParticipantView{}, err
	}
	if req.DiscussionID == "" || req.SourceChatID == "" {
		return ParticipantView{}, fmt.Errorf("%w: invite needs a discussion and a source chat", workspace.ErrInvalid)
	}
	c, err := s.requireCollabStore()
	if err != nil {
		return ParticipantView{}, err
	}
	if existing, ok, err := s.existingInvite(ctx, c, req); err != nil || ok {
		return existing, err
	}
	stored, err := c.PutParticipant(ctx, inviteRow(req))
	if err != nil {
		return ParticipantView{}, wrapStoreError(err)
	}
	return participantView(stored), nil
}

// CreateDiscussion files an already-minted chat. Session owns the transcript
// id; this method only records participants and optional dual placement (P8).
func (s *Service) CreateDiscussion(ctx context.Context, req CreateDiscussionRequest) (DiscussionView, error) {
	if err := s.ready(ctx); err != nil {
		return DiscussionView{}, err
	}
	if req.ChatID == "" {
		return DiscussionView{}, fmt.Errorf("%w: discussion chat id is empty", workspace.ErrInvalid)
	}
	if _, err := s.requireCollabStore(); err != nil {
		return DiscussionView{}, err
	}
	if err := s.fileDiscussion(ctx, req); err != nil {
		return DiscussionView{}, err
	}
	if req.CoordinatorID != "" {
		if _, err := s.InviteToDiscussion(ctx, InviteRequest{DiscussionID: req.ChatID, SourceChatID: req.CoordinatorID}); err != nil {
			return DiscussionView{}, err
		}
	}
	return DiscussionView{ChatID: req.ChatID, Title: req.Title, FolderIDs: copyIDs(req.FolderIDs)}, nil
}

// ListParticipants is the discussion roster. Empty is nothing, never a count.
func (s *Service) ListParticipants(ctx context.Context, discussionID string) ([]ParticipantView, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	c, err := s.requireCollabStore()
	if err != nil {
		return nil, err
	}
	people, err := c.ListParticipants(ctx, discussionID)
	if err != nil {
		return nil, wrapStoreError(err)
	}
	out := make([]ParticipantView, 0, len(people))
	for _, row := range people {
		out = append(out, participantView(row))
	}
	return out, nil
}

// PauseCoordination sets the coordinator participant paused. It stops new
// autonomous decisions; it does not cancel work already in flight.
func (s *Service) PauseCoordination(ctx context.Context, coordinatorID string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	c, err := s.requireCollabStore()
	if err != nil {
		return err
	}
	row, err := s.coordinatorRow(ctx, coordinatorID)
	if err != nil {
		return err
	}
	return wrapStoreError(c.SetParticipantStatus(ctx, row.ID, ParticipantPaused))
}

func (s *Service) writeCoordinatorScope(ctx context.Context, coordinatorID, kind, folderID, snapshot string) (collabRow, error) {
	c, err := s.requireCollabStore()
	if err != nil {
		return collabRow{}, err
	}
	row := collabRow{
		DiscussionID: coordinatorID, Kind: ActorKindChat, SourceChatID: coordinatorID,
		Status: ParticipantActive, ScopeKind: kind, FolderID: folderID, SnapshotJSON: snapshot,
		Origin: workspace.OriginPerson,
	}
	if existing, found, err := s.findCoordinator(ctx, c, coordinatorID); err != nil {
		return collabRow{}, err
	} else if found {
		row.ID, row.ActorID, row.Role = existing.ID, existing.ActorID, existing.Role
	}
	stored, err := c.PutParticipant(ctx, row)
	return stored, wrapStoreError(err)
}

func (s *Service) coordinatorRow(ctx context.Context, coordinatorID string) (collabRow, error) {
	c, err := s.requireCollabStore()
	if err != nil {
		return collabRow{}, err
	}
	row, ok, err := s.findCoordinator(ctx, c, coordinatorID)
	if err != nil {
		return collabRow{}, err
	}
	if !ok {
		return collabRow{}, wrapStoreError(workspace.ErrNotFound)
	}
	return row, nil
}

func (s *Service) findCoordinator(ctx context.Context, c collabStore, coordinatorID string) (collabRow, bool, error) {
	people, err := c.ListParticipants(ctx, coordinatorID)
	if err != nil {
		return collabRow{}, false, wrapStoreError(err)
	}
	for _, row := range people {
		if row.SourceChatID == coordinatorID && row.ScopeKind != "" {
			return row, true, nil
		}
	}
	for _, row := range people {
		if row.ScopeKind != "" {
			return row, true, nil
		}
	}
	return collabRow{}, false, nil
}

func (s *Service) existingInvite(ctx context.Context, c collabStore, req InviteRequest) (ParticipantView, bool, error) {
	people, err := c.ListParticipants(ctx, req.DiscussionID)
	if err != nil {
		return ParticipantView{}, false, wrapStoreError(err)
	}
	for _, row := range people {
		if row.SourceChatID == req.SourceChatID && row.Status != ParticipantArchived {
			return participantView(row), true, nil
		}
	}
	return ParticipantView{}, false, nil
}

func (s *Service) fileDiscussion(ctx context.Context, req CreateDiscussionRequest) error {
	ref := conversationRef(req.ChatID)
	p := workspace.Provenance{Origin: workspace.OriginPerson}
	for _, folderID := range req.FolderIDs {
		if err := s.AddPlacement(ctx, folderID, ref, p); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) descendantChatIDs(ctx context.Context, folderID string) ([]string, error) {
	ids := map[string]struct{}{}
	if err := s.collectConversations(ctx, folderID, map[string]struct{}{}, ids); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Service) sendEnvelopes(ctx context.Context, envs []CollabEnvelope) ([]DeliverReceipt, error) {
	if len(envs) == 1 {
		ack, err := s.collab.Deliver(ctx, envs[0])
		if err != nil {
			return nil, err
		}
		return []DeliverReceipt{receiptOf(ack)}, nil
	}
	acks, err := s.collab.DeliverMany(ctx, envs[0].CauseID, envs)
	if err != nil {
		return nil, err
	}
	out := make([]DeliverReceipt, 0, len(acks))
	for _, ack := range acks {
		out = append(out, receiptOf(ack))
	}
	return out, nil
}

func validateDeliver(req DeliverRequest) error {
	if req.Body == "" || len(req.ToChatIDs) == 0 {
		return fmt.Errorf("%w: deliver needs a body and a recipient", workspace.ErrInvalid)
	}
	for _, id := range req.ToChatIDs {
		if id == "" {
			return fmt.Errorf("%w: recipient is empty", workspace.ErrInvalid)
		}
	}
	return nil
}

func deliverEnvelopes(req DeliverRequest) []CollabEnvelope {
	pattern := deliverPattern(req)
	out := make([]CollabEnvelope, 0, len(req.ToChatIDs))
	for _, to := range req.ToChatIDs {
		out = append(out, CollabEnvelope{
			CauseID: req.CauseID, FromChatID: req.FromChatID, ToChatID: to, Pattern: pattern,
			Body: req.Body, Origin: OriginAgent, DiscussionID: req.DiscussionID, IdempotencyKey: req.IdempotencyKey,
		})
	}
	return out
}

func deliverPattern(req DeliverRequest) string {
	if req.Pattern != "" {
		return req.Pattern
	}
	if req.DiscussionID != "" {
		return PatternDiscussion
	}
	if len(req.ToChatIDs) > 1 {
		return PatternFanout
	}
	return PatternDirect
}

func inviteRow(req InviteRequest) collabRow {
	kind := ActorKindChat
	if req.Role != "" {
		kind = ActorKindRole
	}
	if req.SourceChatID == RootRepresentative {
		kind = ActorKindFolder
	}
	return collabRow{
		DiscussionID: req.DiscussionID, Kind: kind, Role: req.Role, SourceChatID: req.SourceChatID,
		Status: ParticipantActive, Origin: OriginAgent,
	}
}

func participantView(row collabRow) ParticipantView {
	return ParticipantView{
		ActorID: row.ActorID, DiscussionID: row.DiscussionID, Kind: row.Kind,
		Role: row.Role, SourceChatID: row.SourceChatID, Status: row.Status,
	}
}

func receiptOf(ack CollabAck) DeliverReceipt {
	return DeliverReceipt{DeliveryID: ack.DeliveryID, CauseID: ack.CauseID, ToChatID: ack.ToChatID, State: ack.State}
}

func copyIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func parseSnapshot(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return []string{}
	}
	return ids
}
