package wsapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

type recordingCollaborator struct {
	mu   sync.Mutex
	envs []CollabEnvelope
	seq  int
}

func (r *recordingCollaborator) Deliver(_ context.Context, env CollabEnvelope) (CollabAck, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	if env.DeliveryID == "" {
		env.DeliveryID = fmt.Sprintf("d%d", r.seq)
	}
	if env.CauseID == "" {
		env.CauseID = fmt.Sprintf("c%d", r.seq)
	}
	r.envs = append(r.envs, env)
	return CollabAck{DeliveryID: env.DeliveryID, CauseID: env.CauseID, ToChatID: env.ToChatID, State: "pending"}, nil
}

func (r *recordingCollaborator) DeliverMany(ctx context.Context, causeID string, envs []CollabEnvelope) ([]CollabAck, error) {
	out := make([]CollabAck, 0, len(envs))
	for i, env := range envs {
		if causeID != "" {
			env.CauseID = causeID
		}
		if env.CauseID == "" {
			env.CauseID = "shared"
		}
		envs[i] = env
		ack, err := r.Deliver(ctx, env)
		if err != nil {
			return nil, err
		}
		out = append(out, ack)
	}
	return out, nil
}

func (r *recordingCollaborator) Resume(context.Context, string) ([]CollabAck, error) {
	return []CollabAck{}, nil
}

func (r *recordingCollaborator) envelopes() []CollabEnvelope {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CollabEnvelope, len(r.envs))
	copy(out, r.envs)
	return out
}

func TestSelectedSnapshotDoesNotGrowWhenASiblingIsFiled(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	billing := createFolder(t, svc, "Billing")
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := svc.AddPlacement(ctx, billing.ID, conv(id), workspace.Provenance{Reason: "file"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.CoordinateSelected(ctx, CoordinateRequest{CoordinatorID: "mgmt", ChatIDs: []string{"a", "b", "c", "d"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != ScopeSelected || !sameStrings(got.ChatIDs, []string{"a", "b", "c", "d"}) {
		t.Fatalf("selected %+v", got)
	}
	if err := svc.AddPlacement(ctx, billing.ID, conv("e"), workspace.Provenance{Reason: "later"}); err != nil {
		t.Fatal(err)
	}
	inspect, err := svc.InspectScope(ctx, "mgmt")
	if err != nil {
		t.Fatal(err)
	}
	if inspect.Kind != ScopeSelected || containsID(inspect.ChatIDs, "e") || len(inspect.ChatIDs) != 4 {
		t.Fatalf("selected snapshot grew: %+v", inspect)
	}
}

func TestManageFolderIncludesALaterDescendantOnce(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	billing := createFolder(t, svc, "Billing")
	receipts := createFolder(t, svc, "Receipts")
	if err := svc.AddPlacement(ctx, billing.ID, collectionRef(receipts.ID), workspace.Provenance{Reason: "nest"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := svc.AddPlacement(ctx, billing.ID, conv(id), workspace.Provenance{Reason: "file"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.CoordinateSelected(ctx, CoordinateRequest{CoordinatorID: "mgmt", ChatIDs: []string{"a", "b", "c", "d"}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddPlacement(ctx, billing.ID, conv("e"), workspace.Provenance{Reason: "fifth"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddPlacement(ctx, receipts.ID, conv("e"), workspace.Provenance{Reason: "also nested"}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ManageFolder(ctx, ManageFolderRequest{CoordinatorID: "mgmt", FolderID: billing.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != ScopeFolderDynamic || got.FolderID != billing.ID || !containsID(got.ChatIDs, "e") {
		t.Fatalf("dynamic %+v", got)
	}
	count := 0
	for _, id := range got.ChatIDs {
		if id == "e" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("shared descendant counted twice: %v", got.ChatIDs)
	}
	inspect, err := svc.InspectScope(ctx, "mgmt")
	if err != nil || inspect.Kind != ScopeFolderDynamic || !containsID(inspect.ChatIDs, "e") {
		t.Fatalf("inspect after manage %+v, %v", inspect, err)
	}
}

func TestDeliverOneOrManyAndNilCollaboratorIsAbsent(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	got, err := svc.Deliver(ctx, DeliverRequest{FromChatID: "mgmt", Body: "status?", ToChatIDs: []string{"a"}})
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "absent") || got != nil {
		t.Fatalf("nil collaborator must be absent, not a dummy receipt: %v, %v", got, err)
	}

	router := &recordingCollaborator{}
	svc.SetCollaborator(router)
	direct, err := svc.Deliver(ctx, DeliverRequest{FromChatID: "mgmt", Body: "how far is A?", ToChatIDs: []string{"a"}})
	if err != nil || len(direct) != 1 || direct[0].ToChatID != "a" || direct[0].State != "pending" {
		t.Fatalf("direct %+v, %v", direct, err)
	}
	if router.envelopes()[0].Pattern != PatternDirect || router.envelopes()[0].Origin != OriginAgent {
		t.Fatalf("direct envelope %+v", router.envelopes()[0])
	}

	fanout, err := svc.Deliver(ctx, DeliverRequest{
		FromChatID: "mgmt", Body: "use the shared header", CauseID: "cause-1", ToChatIDs: []string{"b", "c"},
	})
	if err != nil || len(fanout) != 2 || fanout[0].CauseID != "cause-1" || fanout[1].CauseID != "cause-1" {
		t.Fatalf("fan-out %+v, %v", fanout, err)
	}
	if fanout[0].DeliveryID == fanout[1].DeliveryID {
		t.Fatalf("fan-out must mint one id per recipient: %+v", fanout)
	}
	envs := router.envelopes()
	if envs[1].Pattern != PatternFanout || envs[2].Pattern != PatternFanout {
		t.Fatalf("fan-out pattern %+v", envs)
	}

	joint, err := svc.Deliver(ctx, DeliverRequest{
		FromChatID: "mgmt", Body: "both positions", DiscussionID: "conflict", ToChatIDs: []string{"room"},
	})
	if err != nil || len(joint) != 1 {
		t.Fatalf("joint %+v, %v", joint, err)
	}
	if router.envelopes()[3].Pattern != PatternDiscussion {
		t.Fatalf("joint pattern %+v", router.envelopes()[3])
	}
}

func TestInviteDedupesAncestorsAndOneRoot(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	for _, req := range []InviteRequest{
		{DiscussionID: "conflict", SourceChatID: "billing", Role: "billing"},
		{DiscussionID: "conflict", SourceChatID: "security", Role: "security"},
		{DiscussionID: "conflict", SourceChatID: "parent-a", Role: "parent"},
		{DiscussionID: "conflict", SourceChatID: "parent-a", Role: "parent-again"},
		{DiscussionID: "conflict", SourceChatID: RootRepresentative, Role: "root"},
		{DiscussionID: "conflict", SourceChatID: RootRepresentative, Role: "root-again"},
	} {
		if _, err := svc.InviteToDiscussion(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	people, err := svc.ListParticipants(ctx, "conflict")
	if err != nil || len(people) != 4 {
		t.Fatalf("want billing, security, one parent, one root; got %+v, %v", people, err)
	}
	seen := map[string]int{}
	for _, p := range people {
		seen[p.SourceChatID]++
		if p.ActorID == "" {
			t.Fatal("actor id must be minted by software")
		}
	}
	if seen["parent-a"] != 1 || seen[RootRepresentative] != 1 {
		t.Fatalf("dedupe failed: %v", seen)
	}
}

func TestCreateDiscussionPlacementDoesNotMergeFolders(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	billing := createFolder(t, svc, "Billing")
	security := createFolder(t, svc, "Security")
	if err := svc.AddPlacement(ctx, billing.ID, conv("invoice"), workspace.Provenance{Reason: "billing chat"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddPlacement(ctx, security.ID, conv("audit"), workspace.Provenance{Reason: "security chat"}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.CreateDiscussion(ctx, CreateDiscussionRequest{
		ChatID: "talk", CoordinatorID: "mgmt", Title: "shared talk", IdempotencyKey: "talk-1",
		FolderIDs: []string{billing.ID, security.ID},
	})
	if err != nil || got.ChatID != "talk" {
		t.Fatalf("create %+v, %v", got, err)
	}
	again, err := svc.CreateDiscussion(ctx, CreateDiscussionRequest{
		ChatID: "talk", CoordinatorID: "mgmt", Title: "shared talk", IdempotencyKey: "talk-1",
		FolderIDs: []string{billing.ID, security.ID},
	})
	if err != nil || again.ChatID != "talk" {
		t.Fatalf("retry %+v, %v", again, err)
	}
	if ids := placementIDs(t, svc, conv("talk")); !sameStrings(ids, []string{billing.ID, security.ID}) {
		t.Fatalf("discussion folders %v", ids)
	}
	if ids := placementIDs(t, svc, conv("invoice")); !sameStrings(ids, []string{billing.ID}) {
		t.Fatalf("billing must not absorb security's chat: %v", ids)
	}
	if ids := placementIDs(t, svc, conv("audit")); !sameStrings(ids, []string{security.ID}) {
		t.Fatalf("security must not absorb billing's chat: %v", ids)
	}
}

func TestPauseCoordinationDoesNotDropScope(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	if _, err := svc.CoordinateSelected(ctx, CoordinateRequest{CoordinatorID: "mgmt", ChatIDs: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	router := &recordingCollaborator{}
	svc.SetCollaborator(router)
	if _, err := svc.Deliver(ctx, DeliverRequest{FromChatID: "mgmt", Body: "already queued", ToChatIDs: []string{"a"}}); err != nil {
		t.Fatalf("deliver before pause: %v", err)
	}
	if err := svc.PauseCoordination(ctx, "mgmt"); err != nil {
		t.Fatal(err)
	}
	inspect, err := svc.InspectScope(ctx, "mgmt")
	if err != nil || !sameStrings(inspect.ChatIDs, []string{"a", "b"}) {
		t.Fatalf("pause must not clear the snapshot: %+v, %v", inspect, err)
	}
	people, err := svc.ListParticipants(ctx, "mgmt")
	if err != nil || len(people) != 1 || people[0].Status != ParticipantPaused {
		t.Fatalf("paused roster %+v, %v", people, err)
	}
	if _, err := svc.Deliver(ctx, DeliverRequest{FromChatID: "mgmt", Body: "new after pause", ToChatIDs: []string{"a"}}); err == nil || !strings.Contains(err.Error(), "paused") {
		t.Fatalf("pause must refuse new deliver: %v", err)
	}
	if _, err := svc.InviteToDiscussion(ctx, InviteRequest{DiscussionID: "mgmt", SourceChatID: "planner-chat", Role: "planner"}); err == nil || !strings.Contains(err.Error(), "paused") {
		t.Fatalf("pause must refuse new invite: %v", err)
	}
	if _, err := router.Resume(ctx, "a"); err != nil {
		t.Fatalf("pause must not block resume of pending: %v", err)
	}
	if len(router.envelopes()) != 1 {
		t.Fatalf("new deliver after pause was stored: %+v", router.envelopes())
	}
}

func TestOpenStoreCoordinatesAndLeavesDeliverAbsentWithoutCollaborator(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	got, err := svc.CoordinateSelected(ctx, CoordinateRequest{CoordinatorID: "mgmt", ChatIDs: []string{"a"}})
	if err != nil || got.Kind != ScopeSelected || !sameStrings(got.ChatIDs, []string{"a"}) {
		t.Fatalf("production store must coordinate: %+v, %v", got, err)
	}
	receipt, err := svc.Deliver(ctx, DeliverRequest{FromChatID: "mgmt", Body: "hello", ToChatIDs: []string{"a"}})
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "absent") || receipt != nil {
		t.Fatalf("nil collaborator on Open must not invent a receipt: %v, %v", receipt, err)
	}
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
