package wsapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

const (
	// conflictBudgetRole marks the durable budget row on the conflict
	// discussion. ScopeKind stays empty: workspace only allows selected and
	// folder-dynamic, and a new kind would be a schema freeze break.
	conflictBudgetRole = "conflict"

	// conflictRoundCap is two turns per ancestor level (parents, then Root)
	// plus slack. It is a loop fence, not a ban on parent join after two turns.
	conflictRoundCap = 6
	conflictTimeCap  = 30 * time.Minute
)

// errNeedPerson is the software checkpoint when a conflict cannot be settled
// with the authority on hand. THE MODEL DOES NOT OVERRIDE IT: missing
// authority reaches the person rather than expanding Root or looping forever.
const errNeedPerson = "missing authority reaches the person"

// ConflictDiscussionView is one conflict room: a stable discussion id, the
// folders that disagree, and the roster after ancestor+Root invites.
type ConflictDiscussionView struct {
	ChatID       string
	FolderIDs    []string
	Participants []ParticipantView
	Reused       bool
	Exhausted    bool
}

type conflictBudget struct {
	Rounds  int   `json:"rounds"`
	Started int64 `json:"started"`
}

// OpenConflictDiscussion opens or reuses one room when EffectiveGuidance
// Conflict is set. SOFTWARE DOES THIS: a model is not asked to invite parents
// after two turns, and the first parent to answer is not the boss. Distinct
// ancestor SourceChatIDs are invited once, including one Root.
func (s *Service) OpenConflictDiscussion(ctx context.Context, conversationID string) (ConflictDiscussionView, error) {
	if err := s.ready(ctx); err != nil {
		return ConflictDiscussionView{}, err
	}
	loaded, err := s.EffectiveGuidance(ctx, conversationID)
	if err != nil {
		return ConflictDiscussionView{}, err
	}
	if !loaded.Conflict {
		return ConflictDiscussionView{}, nil
	}
	folders := conflictFolderIDs(loaded.Items)
	if len(folders) == 0 {
		return ConflictDiscussionView{}, nil
	}
	chatID := conflictDiscussionID(folders)
	view := ConflictDiscussionView{ChatID: chatID, FolderIDs: folders}
	existing, err := s.ListParticipants(ctx, chatID)
	if err != nil {
		return view, err
	}
	view.Reused = len(existing) > 0
	exhausted, err := s.conflictExhausted(ctx, chatID)
	if err != nil {
		return view, err
	}
	if exhausted {
		view.Exhausted = true
		view.Participants = existing
		return view, nil
	}
	if err := s.ensureConflictRoom(ctx, conversationID, chatID, folders, loaded.Items); err != nil {
		return view, err
	}
	people, err := s.ListParticipants(ctx, chatID)
	view.Participants = people
	return view, err
}

func (s *Service) ensureConflictRoom(ctx context.Context, conversationID, chatID string, folders []string, items []GuidanceItem) error {
	if _, err := s.CreateDiscussion(ctx, CreateDiscussionRequest{
		ChatID: chatID, CoordinatorID: conversationID, Title: "conflict",
		IdempotencyKey: chatID, FolderIDs: folders,
	}); err != nil {
		return err
	}
	if err := s.inviteConflictAncestors(ctx, chatID, items); err != nil {
		return err
	}
	return s.ensureConflictBudget(ctx, chatID)
}

func (s *Service) inviteConflictAncestors(ctx context.Context, discussionID string, items []GuidanceItem) error {
	for _, req := range conflictInvites(items) {
		if _, err := s.InviteToDiscussion(ctx, req.withDiscussion(discussionID)); err != nil {
			return err
		}
	}
	return nil
}

func (req InviteRequest) withDiscussion(id string) InviteRequest {
	req.DiscussionID = id
	return req
}

func conflictInvites(items []GuidanceItem) []InviteRequest {
	out := make([]InviteRequest, 0, len(items)+1)
	seen := map[string]struct{}{RootRepresentative: {}}
	for _, item := range items {
		id := strings.TrimSpace(item.ScopeID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		role := strings.TrimSpace(item.Name)
		if role == "" {
			role = id
		}
		out = append(out, InviteRequest{SourceChatID: id, Role: role})
	}
	out = append(out, InviteRequest{SourceChatID: RootRepresentative, Role: "root"})
	return out
}

func conflictFolderIDs(items []GuidanceItem) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		id := strings.TrimSpace(item.ScopeID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func conflictDiscussionID(folderIDs []string) string {
	sum := sha256.New()
	_, _ = sum.Write([]byte("j24-conflict\n"))
	for _, id := range folderIDs {
		_, _ = sum.Write([]byte(id))
		_, _ = sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil)[:8])
}

func (s *Service) ensureConflictBudget(ctx context.Context, discussionID string) error {
	if _, ok, err := s.conflictBudgetRow(ctx, discussionID); err != nil || ok {
		return err
	}
	return s.putConflictBudget(ctx, discussionID, conflictBudget{Started: s.conflictNowUnix()})
}

func (s *Service) noteConflictRound(ctx context.Context, discussionID string) error {
	if strings.TrimSpace(discussionID) == "" {
		return nil
	}
	budget, ok, err := s.conflictBudgetRow(ctx, discussionID)
	if err != nil || !ok {
		return err
	}
	budget.Rounds++
	return s.putConflictBudget(ctx, discussionID, budget)
}

func (s *Service) refuseIfConflictExhausted(ctx context.Context, discussionID string) error {
	if strings.TrimSpace(discussionID) == "" {
		return nil
	}
	exhausted, err := s.conflictExhausted(ctx, discussionID)
	if err != nil {
		return err
	}
	if exhausted {
		return fmt.Errorf("%w: unresolved conflict reaches the person", workspace.ErrInvalid)
	}
	return nil
}

func (s *Service) conflictExhausted(ctx context.Context, discussionID string) (bool, error) {
	budget, ok, err := s.conflictBudgetRow(ctx, discussionID)
	if err != nil || !ok {
		return false, err
	}
	if budget.Rounds >= conflictRoundCap {
		return true, nil
	}
	now := s.conflictNowUnix()
	if budget.Started == 0 || now == 0 {
		return false, nil
	}
	return now-budget.Started >= int64(conflictTimeCap/time.Second), nil
}

func (s *Service) conflictBudgetRow(ctx context.Context, discussionID string) (conflictBudget, bool, error) {
	c, err := s.requireCollabStore()
	if err != nil {
		return conflictBudget{}, false, err
	}
	people, err := c.ListParticipants(ctx, discussionID)
	if err != nil {
		return conflictBudget{}, false, wrapStoreError(err)
	}
	for _, row := range people {
		if isConflictBudgetRow(row, discussionID) {
			return parseConflictBudget(row.SnapshotJSON), true, nil
		}
	}
	return conflictBudget{}, false, nil
}

func (s *Service) putConflictBudget(ctx context.Context, discussionID string, budget conflictBudget) error {
	c, err := s.requireCollabStore()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(budget)
	if err != nil {
		return err
	}
	row := workspace.Participant{
		DiscussionID: discussionID, Kind: ActorKindChat, SourceChatID: discussionID,
		Role: conflictBudgetRole, Status: ParticipantActive, SnapshotJSON: string(raw),
		Origin: OriginAgent,
	}
	if existing, found, err := s.findConflictBudgetRow(ctx, c, discussionID); err != nil {
		return err
	} else if found {
		row.ID, row.ActorID = existing.ID, existing.ActorID
	}
	_, err = c.PutParticipant(ctx, row)
	return wrapStoreError(err)
}

func (s *Service) findConflictBudgetRow(ctx context.Context, c collabStore, discussionID string) (workspace.Participant, bool, error) {
	people, err := c.ListParticipants(ctx, discussionID)
	if err != nil {
		return workspace.Participant{}, false, wrapStoreError(err)
	}
	for _, row := range people {
		if isConflictBudgetRow(row, discussionID) {
			return row, true, nil
		}
	}
	return workspace.Participant{}, false, nil
}

func isConflictBudgetRow(row workspace.Participant, discussionID string) bool {
	return row.Role == conflictBudgetRole && row.SourceChatID == discussionID
}

func parseConflictBudget(raw string) conflictBudget {
	var budget conflictBudget
	if raw == "" {
		return budget
	}
	_ = json.Unmarshal([]byte(raw), &budget)
	return budget
}

func (s *Service) conflictNowUnix() int64 {
	if s == nil || s.now == nil {
		return 0
	}
	stamp := s.now()
	if stamp.IsZero() {
		return 0
	}
	return stamp.Unix()
}

func (s *Service) isRootCoordinator(ctx context.Context, coordinatorID string) bool {
	if coordinatorID == RootRepresentative {
		return true
	}
	row, err := s.coordinatorRow(ctx, coordinatorID)
	if err != nil {
		return false
	}
	return row.SourceChatID == RootRepresentative
}

func (s *Service) refuseRootGrant(ctx context.Context, req GrantRequest) error {
	if !s.isRootCoordinator(ctx, req.CoordinatorID) {
		return nil
	}
	if strings.TrimSpace(req.Issuer) == "" {
		return fmt.Errorf("%w: %s", workspace.ErrInvalid, errNeedPerson)
	}
	return nil
}
