package resident

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// pinnedReconciler builds a resident on the v2 surface through the same door
// the real surface uses. The environment read is part of the seam, so a test
// that set the field directly would prove the policy and not the selection.
func pinnedReconciler(t *testing.T, graph *store.Store) *Reconciler {
	t.Helper()
	t.Setenv(chatV2Env, "1")
	return New(graph, nil, nil)
}

// spliceHeadlessJob is a top-level job with no conversation behind it at all —
// a charter firing, a repair splice, anything the system started for itself.
func spliceHeadlessJob(t *testing.T, graph *store.Store, id string) {
	t.Helper()
	err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: id, Brief: "Survey the field", Stage: 1},
	}}, store.Provenance{Origin: store.OriginSelf, Intent: "survey the field"})
	if err != nil {
		t.Fatalf("splice %s: %v", id, err)
	}
}

func failNode(t *testing.T, graph *store.Store, id, reason string) {
	t.Helper()
	claim, won, err := graph.Claim(id, "worker")
	if err != nil || !won {
		t.Fatalf("claim %s: won=%v err=%v", id, won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	if err := graph.Fail(claim, reason); err != nil {
		t.Fatalf("fail %s: %v", id, err)
	}
}

func messageBodies(t *testing.T, graph *store.Store, sessionID string) []string {
	t.Helper()
	messages, err := graph.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatalf("messages %s: %v", sessionID, err)
	}
	bodies := make([]string, 0, len(messages))
	for _, message := range messages {
		bodies = append(bodies, message.Body)
	}
	return bodies
}

func TestTheRoomPolicyIsResolvedOnceAtConstruction(t *testing.T) {
	graph := openStore(t)
	if legacy := New(graph, nil, nil); legacy.roomPolicy() != roomsLegacy {
		t.Fatalf("an unflagged resident left the legacy route: %v", legacy.rooms)
	}
	t.Setenv(chatV2Env, "1")
	pinned := New(graph, nil, nil)
	if pinned.roomPolicy() != roomsOwnerPinned {
		t.Fatalf("%s=1 did not select owner-pinned rooms: %v", chatV2Env, pinned.rooms)
	}
	// Once, and only once: the environment moving under a running resident must
	// not re-address work that is already in flight.
	t.Setenv(chatV2Env, "")
	if pinned.roomPolicy() != roomsOwnerPinned {
		t.Fatalf("the policy was re-derived from the environment: %v", pinned.rooms)
	}
	if New(graph, nil, nil).roomPolicy() != roomsLegacy {
		t.Fatalf("an emptied flag did not fall back to legacy")
	}
	// A Reconciler built as a literal — every test fixture in this package, and
	// every embedding — is on the shipped route by construction.
	if (&Reconciler{}).roomPolicy() != roomsLegacy {
		t.Fatalf("the zero value is not the legacy policy")
	}
	if (*Reconciler)(nil).roomPolicy() != roomsLegacy {
		t.Fatalf("a nil resident does not degrade to legacy")
	}
}

// The legacy journey inverted: under rooms the overnight answer waits in the
// room that asked for it, and the window somebody happens to have open is not
// handed somebody else's deliverable.
func TestOwnerPinnedDeliverableWaitsInTheRoomThatCommissionedIt(t *testing.T) {
	graph := openStore(t)
	spliceOvernightJob(t, graph, "overnight", "yesterday")
	reconciler := pinnedReconciler(t, graph)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "yesterday", store.SeenDetached); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "today", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	landNode(t, graph, "overnight", "Three posts worth your time.")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	owner := messageBodies(t, graph, "yesterday")
	if len(owner) != 1 || owner[0] != "Three posts worth your time." {
		t.Fatalf("the commissioning room did not keep its answer: %+v", owner)
	}
	if attached := messageBodies(t, graph, "today"); len(attached) != 0 {
		t.Fatalf("the deliverable leaked into the attached room: %+v", attached)
	}
}

// A job nobody commissioned has no room to pin to. It must still be heard: the
// fallback is the legacy address, because the alternative is a deliverable that
// exists, is journaled, and is spoken into nothing.
func TestOwnerPinnedDeliverableWithNoRoomOfItsOwnFallsBackToTheAttachedRoom(t *testing.T) {
	graph := openStore(t)
	spliceHeadlessJob(t, graph, "headless")
	reconciler := pinnedReconciler(t, graph)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "today", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	landNode(t, graph, "headless", "It is surveyed.")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	attached := messageBodies(t, graph, "today")
	if len(attached) != 1 || attached[0] != "It is surveyed." {
		t.Fatalf("a deliverable with no owner was dropped: %+v", attached)
	}
}

// The same fixture on the shipped route: legacy says nothing for a node with no
// originating room, and this locks that in — the pinned fallback above is a
// change the flag makes and the default surface never sees.
func TestLegacyStillSaysNothingForAJobWithNoRoomOfItsOwn(t *testing.T) {
	graph := openStore(t)
	spliceHeadlessJob(t, graph, "headless")
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "today", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	landNode(t, graph, "headless", "It is surveyed.")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attached := messageBodies(t, graph, "today"); len(attached) != 0 {
		t.Fatalf("legacy announced an ownerless job: %+v", attached)
	}
}

// A leg of somebody's task failing is that task's news. Under rooms it is
// narrated where the task lives — reached through the node's own parent, since
// a spliced child carries no session of its own — and never into whichever room
// is open.
func TestOwnerPinnedFailureNarratesIntoTheOwningTaskRoom(t *testing.T) {
	graph := openStore(t)
	spliceOvernightJob(t, graph, "job", "owner")
	if err := graph.Splice("job", store.Subtree{Nodes: []store.NodeSpec{
		{ID: "leg", Brief: "read the archive", Stage: 1},
	}}, store.Provenance{Origin: store.OriginSelf, Intent: "read the archive"}); err != nil {
		t.Fatal(err)
	}
	reconciler := pinnedReconciler(t, graph)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "today", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	failNode(t, graph, "leg", "the archive is offline")

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	told := false
	for _, body := range messageBodies(t, graph, "owner") {
		if strings.Contains(body, "the archive is offline") {
			told = true
		}
	}
	if !told {
		t.Fatalf("the task's room never heard about its own failure: %+v",
			messageBodies(t, graph, "owner"))
	}
	if attached := messageBodies(t, graph, "today"); len(attached) != 0 {
		t.Fatalf("subtree narration leaked into the attached room: %+v", attached)
	}
}

// A question belongs to the task that raised it. Under rooms, an unanswered one
// waits where the work is, and the rescue that adopts orphans into whatever
// window is open does not fire against it.
func TestOwnerPinnedQuestionStaysInItsTaskRoomWhileAnotherIsAttached(t *testing.T) {
	graph := openStore(t)
	spliceOvernightJob(t, graph, "job", "owner")
	reconciler := pinnedReconciler(t, graph)
	question := askTaskQuestion(t, reconciler, "owner", "job")
	if _, err := graph.TouchSeen("tui", "owner", store.SeenDetached); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "today", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	held, found, err := graph.AgentQuestionBySeq(question.Seq)
	if err != nil || !found || held.SessionID != "owner" {
		t.Fatalf("the task's question was taken out of its room: %+v found=%t err=%v", held, found, err)
	}
	if attached := messageBodies(t, graph, "today"); len(attached) != 0 {
		t.Fatalf("the question was re-asked in the attached room: %+v", attached)
	}
}

// The same fixture on the shipped route, which is the whole reason the policy
// is a seam: today a node-backed question really is adopted by whoever is home.
func TestLegacyStillCarriesATaskQuestionIntoTheAttachedRoom(t *testing.T) {
	graph := openStore(t)
	spliceOvernightJob(t, graph, "job", "owner")
	reconciler := New(graph, nil, nil)
	question := askTaskQuestion(t, reconciler, "owner", "job")
	if _, err := graph.TouchSeen("tui", "owner", store.SeenDetached); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "today", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	carried, found, err := graph.AgentQuestionBySeq(question.Seq)
	if err != nil || !found || carried.SessionID != "today" {
		t.Fatalf("legacy stopped carrying an orphan: %+v found=%t err=%v", carried, found, err)
	}
}

// A question filed under a session that is not its task's room goes home, not
// to whoever is attached: the owner is read from the node, so a stale address
// on the question itself cannot decide where its task's request is answered.
func TestOwnerPinnedCarriesAStrandedQuestionHomeRatherThanToWhoeverIsAttached(t *testing.T) {
	graph := openStore(t)
	spliceOvernightJob(t, graph, "job", "owner")
	reconciler := pinnedReconciler(t, graph)
	question := askTaskQuestion(t, reconciler, "stale", "job")
	if _, err := graph.TouchSeen("tui", "today", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	carried, found, err := graph.AgentQuestionBySeq(question.Seq)
	if err != nil || !found || carried.SessionID != "owner" {
		t.Fatalf("the stranded question did not go home: %+v found=%t err=%v", carried, found, err)
	}
	if attached := messageBodies(t, graph, "today"); len(attached) != 0 {
		t.Fatalf("the question was re-asked in the attached room: %+v", attached)
	}
	reposted := 0
	for _, body := range messageBodies(t, graph, "owner") {
		if strings.Contains(body, "Which airport?") {
			reposted++
		}
	}
	if reposted != 1 {
		t.Fatalf("the question was not re-asked in its own room exactly once: %d", reposted)
	}
}

// The compiler's askback — "which airport?", asked before any work exists — has
// no task to belong to. Pinning cannot address it, so the legacy rescue still
// does; a question nobody can see is a request that was silently abandoned,
// and that is true on both routes.
func TestOwnerPinnedStillRescuesAQuestionNoTaskOwns(t *testing.T) {
	graph := openStore(t)
	reconciler := pinnedReconciler(t, graph)
	question := askTaskQuestion(t, reconciler, "last-night", "")
	if _, err := graph.TouchSeen("tui", "this-morning", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	carried, found, err := graph.AgentQuestionBySeq(question.Seq)
	if err != nil || !found || carried.SessionID != "this-morning" {
		t.Fatalf("an ownerless question was left unanswerable: %+v found=%t err=%v", carried, found, err)
	}
}

func askTaskQuestion(t *testing.T, reconciler *Reconciler, sessionID, nodeID string) store.AgentQuestion {
	t.Helper()
	question, err := reconciler.AskQuestion(store.AgentQuestion{
		SessionID: sessionID, Text: "Which airport?", OriginNodeID: nodeID,
		Urgency: store.QuestionBlocking,
	})
	if err != nil {
		t.Fatalf("ask question: %v", err)
	}
	if question.Status != store.QuestionAsked {
		t.Fatalf("blocking question was not asked: %+v", question)
	}
	return question
}
