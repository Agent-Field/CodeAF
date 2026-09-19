package workspace

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func writeV3Fixture(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(v2CollectionsDDL + v2HistoryDDL + v3DDL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO collections(id,name,purpose,lifecycle,revision,created_at,updated_at)
 VALUES ('billing-v3','Billing','','active',1,'','')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection)
 VALUES ('billing-v3','conversation','old-chat','',NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO root_state(id,purpose,revision,updated_at) VALUES (1,'',1,'')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmtPragmas(3)); err != nil {
		t.Fatal(err)
	}
}

func TestV3ListDoesNotMigrate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v3.db")
	writeV3Fixture(t, path)
	s := openTestStore(t, path)
	if s.SchemaVersion() != 3 {
		t.Fatalf("open reported version %d", s.SchemaVersion())
	}
	got, err := s.Collections(ctx)
	if err != nil || len(got) != 1 || got[0].ID != "billing-v3" {
		t.Fatalf("v3 list: %v, %v", got, err)
	}
	people, err := s.ListParticipants(ctx, "mgmt")
	if err != nil || len(people) != 0 {
		t.Fatalf("v3 list participants: %v, %v", people, err)
	}
	queued, err := s.ListPendingDeliveries(ctx, "old-chat")
	if err != nil || len(queued) != 0 {
		t.Fatalf("v3 list deliveries: %v, %v", queued, err)
	}
	if tablesNamed(t, path, v4TableNames...) != nil {
		t.Fatalf("v3 list created v4 tables: %v", tablesNamed(t, path, v4TableNames...))
	}
	app, version := fileUserVersion(t, path)
	if app != applicationID || version != 3 {
		t.Fatalf("list migrated the file: application %d version %d", app, version)
	}
}

func TestFirstWriteMigratesV3ToV4(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v3.db")
	writeV3Fixture(t, path)
	s := openTestStore(t, path)
	if err := s.Add(ctx, "billing-v3", Ref{Kind: ConversationKind, ID: "new-chat"}); err != nil {
		t.Fatal(err)
	}
	app, version := fileUserVersion(t, path)
	if app != applicationID || version != 4 {
		t.Fatalf("first write left application %d version %d", app, version)
	}
	requireTables(t, path, v3TableNames...)
	requireTables(t, path, v4TableNames...)
	if extra := tablesNamed(t, path, laterPhaseTableNames...); len(extra) != 0 {
		t.Fatalf("wave 3 created later-phase tables: %v", extra)
	}
	guidance, err := s.PutGuidance(ctx, Guidance{Text: "keep receipts", Origin: OriginPerson})
	if err != nil || guidance.ID == "" {
		t.Fatalf("wave 2 guidance after v4: %+v, %v", guidance, err)
	}
	job, err := s.EnqueueJob(ctx, Job{CoalesceKey: "chat:1"})
	if err != nil || job.State != JobPending {
		t.Fatalf("wave 2 job after v4: %+v, %v", job, err)
	}
}

func TestActorIDsAreMintedBySoftware(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "actors.db"))
	createTestCollection(t, s, "Billing")
	first, err := s.PutParticipant(ctx, Participant{
		DiscussionID: "mgmt",
		Kind:         ActorKindRole,
		Role:         "planner",
		SourceChatID: "feature-a",
		Origin:       OriginPerson,
		ActorID:      "model-invented-id",
	})
	if err != nil || len(first.ActorID) != 32 || first.ActorID == "model-invented-id" || first.ID == "" {
		t.Fatalf("minted actor: %+v, %v", first, err)
	}
	second, err := s.PutParticipant(ctx, Participant{
		DiscussionID: "mgmt",
		Kind:         ActorKindChat,
		SourceChatID: "feature-b",
		Origin:       OriginPerson,
		ActorID:      "another-model-id",
	})
	if err != nil || second.ActorID == "another-model-id" || second.ActorID == first.ActorID {
		t.Fatalf("second actor reused a model id: %+v, %v", second, err)
	}
	again, err := s.PutParticipant(ctx, Participant{
		DiscussionID: "mgmt",
		Kind:         ActorKindRole,
		Role:         "planner",
		SourceChatID: "feature-a",
		Origin:       OriginPerson,
	})
	if err != nil || again.ActorID != first.ActorID || again.ID != first.ID {
		t.Fatalf("re-invite: %+v vs %+v, %v", again, first, err)
	}
	named, err := s.PutParticipant(ctx, Participant{
		ID:           first.ID,
		DiscussionID: "mgmt",
		Kind:         ActorKindRole,
		Role:         "reviewer",
		SourceChatID: "feature-a",
		Origin:       OriginPerson,
		ActorID:      "should-be-ignored",
	})
	if err != nil || named.ActorID != first.ActorID || named.Role != "reviewer" {
		t.Fatalf("update kept software actor: %+v, %v", named, err)
	}
	if err := s.SetParticipantStatus(ctx, first.ID, ParticipantPaused); err != nil {
		t.Fatal(err)
	}
	listed, err := s.ListParticipants(ctx, "mgmt")
	if err != nil || len(listed) != 2 {
		t.Fatalf("list: %+v, %v", listed, err)
	}
}

func TestFanOutSharesCauseAndKeepsPerRecipientIDs(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "fanout.db"))
	createTestCollection(t, s, "Inbox")
	first, err := s.PutDelivery(ctx, Delivery{
		Pattern:    PatternFanout,
		FromChatID: "mgmt",
		ToChatID:   "feature-b",
		Body:       "use the shared header",
		Origin:     OriginPerson,
	})
	if err != nil || first.ID == "" || first.CauseID == "" || first.State != DeliveryPending {
		t.Fatalf("first fan-out: %+v, %v", first, err)
	}
	second, err := s.PutDelivery(ctx, Delivery{
		CauseID:    first.CauseID,
		Pattern:    PatternFanout,
		FromChatID: "mgmt",
		ToChatID:   "feature-c",
		Body:       "use the shared header",
		Origin:     OriginPerson,
	})
	if err != nil || second.ID == first.ID || second.CauseID != first.CauseID {
		t.Fatalf("second fan-out: %+v vs %+v, %v", second, first, err)
	}
	got, err := s.ListDeliveries(ctx, first.CauseID)
	if err != nil || len(got) != 2 {
		t.Fatalf("cause list: %+v, %v", got, err)
	}
	direct, err := s.PutDelivery(ctx, Delivery{
		ID:             "mailbox/feature-a@1:ask",
		Pattern:        PatternDirect,
		FromChatID:     "mgmt",
		ToChatID:       "feature-a",
		IdempotencyKey: "mgmt:feature-a:ask-1",
		Body:           "how far is A?",
	})
	if err != nil || direct.ID != "mailbox/feature-a@1:ask" || direct.Pattern != PatternDirect {
		t.Fatalf("direct mailbox id: %+v, %v", direct, err)
	}
	again, err := s.PutDelivery(ctx, Delivery{
		ID:       "mailbox/feature-a@1:ask",
		ToChatID: "feature-a",
		Body:     "duplicate",
	})
	if err != nil || again.ID != direct.ID || again.Body != direct.Body {
		t.Fatalf("delivery id reuse: %+v, %v", again, err)
	}
	keyed, err := s.PutDelivery(ctx, Delivery{
		ToChatID:       "feature-a",
		IdempotencyKey: "mgmt:feature-a:ask-1",
		Body:           "also duplicate",
	})
	if err != nil || keyed.ID != direct.ID {
		t.Fatalf("idempotency: %+v, %v", keyed, err)
	}
}

func TestDeliveryStatesStayDistinctAndOfflineStaysPending(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "offline.db")
	s := openTestStore(t, path)
	createTestCollection(t, s, "Inbox")
	queued, err := s.PutDelivery(ctx, Delivery{
		ToChatID:       "feature-a",
		Body:           "offline line",
		IdempotencyKey: "offline-1",
	})
	if err != nil || queued.State != DeliveryPending {
		t.Fatalf("queue: %+v, %v", queued, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTestStore(t, path)
	listed, err := s.ListPendingDeliveries(ctx, "feature-a")
	if err != nil || len(listed) != 1 || listed[0].State != DeliveryPending || listed[0].ID != queued.ID {
		t.Fatalf("reopen pending: %+v, %v", listed, err)
	}
	_, err = s.AckDelivery(ctx, queued.ID, DeliveryRecorded)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "state") {
		t.Fatalf("skip recorded: %v", err)
	}
	accepted, err := s.AckDelivery(ctx, queued.ID, DeliveryAccepted)
	if err != nil || accepted.State != DeliveryAccepted || accepted.AcceptedAt == "" || accepted.RecordedAt != "" {
		t.Fatalf("accepted: %+v, %v", accepted, err)
	}
	recorded, err := s.AckDelivery(ctx, queued.ID, DeliveryRecorded)
	if err != nil || recorded.State != DeliveryRecorded || recorded.RecordedAt == "" || recorded.ProcessedAt != "" {
		t.Fatalf("recorded: %+v, %v", recorded, err)
	}
	processed, err := s.AckDelivery(ctx, queued.ID, DeliveryProcessed)
	if err != nil || processed.State != DeliveryProcessed || processed.ProcessedAt == "" {
		t.Fatalf("processed: %+v, %v", processed, err)
	}
	same, err := s.AckDelivery(ctx, queued.ID, DeliveryProcessed)
	if err != nil || same.State != DeliveryProcessed {
		t.Fatalf("idempotent processed: %+v, %v", same, err)
	}
	_, err = s.AckDelivery(ctx, queued.ID, DeliveryAccepted)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "state") {
		t.Fatalf("backward ack: %v", err)
	}
	got, err := s.GetDelivery(ctx, queued.ID)
	if err != nil || got.State != DeliveryProcessed || got.AcceptedAt == "" || got.RecordedAt == "" {
		t.Fatalf("get: %+v, %v", got, err)
	}
}
