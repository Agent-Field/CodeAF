package wsapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

func TestOpenConflictDiscussionReusesOneRoomAndInvitesAncestorsAndRoot(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	child, billing, security := billingSecurityConflict(t, svc)

	first, err := svc.OpenConflictDiscussion(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if first.ChatID == "" || first.Reused || first.Exhausted {
		t.Fatalf("first open %+v", first)
	}
	assertConflictRoster(t, first.Participants, child, billing.ID, security.ID)

	second, err := svc.OpenConflictDiscussion(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if second.ChatID != first.ChatID || !second.Reused {
		t.Fatalf("reuse %+v vs %+v", second, first)
	}
	assertConflictRoster(t, second.Participants, child, billing.ID, security.ID)

	other := "otherotherother0"
	if err := svc.AddPlacement(ctx, billing.ID, conv(other), workspace.Provenance{Origin: workspace.OriginPerson}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddPlacement(ctx, security.ID, conv(other), workspace.Provenance{Origin: workspace.OriginPerson}); err != nil {
		t.Fatal(err)
	}
	shared, err := svc.OpenConflictDiscussion(ctx, other)
	if err != nil || shared.ChatID != first.ChatID {
		t.Fatalf("same parent conflict must reuse one discussion: %+v, %v", shared, err)
	}
}

func TestOpenConflictDiscussionIsAbsentWhenGuidanceAgrees(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	billing := createFolder(t, svc, "Billing")
	if _, err := svc.InstructFolder(ctx, InstructRequest{
		ScopeID: billing.ID, Text: "invoices stay in billing",
		Provenance: workspace.Provenance{Origin: workspace.OriginPerson},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddPlacement(ctx, billing.ID, conv("child"), workspace.Provenance{Origin: workspace.OriginPerson}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.OpenConflictDiscussion(ctx, "child")
	if err != nil || got.ChatID != "" || len(got.Participants) != 0 {
		t.Fatalf("agreeing guidance must not open a room: %+v, %v", got, err)
	}
}

func TestConflictDiscussionStopsAfterFiniteRounds(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	svc.SetCollaborator(&recordingCollaborator{})
	child, _, _ := billingSecurityConflict(t, svc)
	got, err := svc.OpenConflictDiscussion(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.putConflictBudget(ctx, got.ChatID, conflictBudget{Rounds: conflictRoundCap}); err != nil {
		t.Fatal(err)
	}
	again, err := svc.OpenConflictDiscussion(ctx, child)
	if err != nil || !again.Exhausted || again.ChatID != got.ChatID {
		t.Fatalf("exhausted reopen %+v, %v", again, err)
	}
	_, err = svc.Deliver(ctx, DeliverRequest{
		FromChatID: child, Body: "keep looping", DiscussionID: got.ChatID, ToChatIDs: []string{got.ChatID},
	})
	if err == nil || !strings.Contains(err.Error(), "reaches the person") {
		t.Fatalf("exhausted deliver must reach the person, not loop: %v", err)
	}
}

func TestSQLiteOpenConflictDiscussionPersistsOneRoom(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	child, billing, security := billingSecurityConflict(t, svc)
	first, err := svc.OpenConflictDiscussion(ctx, child)
	if err != nil || first.ChatID == "" {
		t.Fatalf("sqlite open %+v, %v", first, err)
	}
	assertConflictRoster(t, first.Participants, child, billing.ID, security.ID)
	second, err := svc.OpenConflictDiscussion(ctx, child)
	if err != nil || second.ChatID != first.ChatID || !second.Reused {
		t.Fatalf("sqlite reuse %+v, %v", second, err)
	}
}

func TestRootRepresentativeCannotExpandGrantWithoutPersonAuthority(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	_, err := svc.IssueGrant(ctx, GrantRequest{
		CoordinatorID: RootRepresentative, Goal: "take execute",
		ActionClasses: []string{workspace.ClassExecute},
	})
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), errNeedPerson) {
		t.Fatalf("root without issuer must reach the person: %v", err)
	}

	person, err := svc.IssueGrant(ctx, GrantRequest{
		CoordinatorID: "mgmt", Goal: "read only",
		ActionClasses: []string{workspace.ClassRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.IssueGrant(ctx, GrantRequest{
		CoordinatorID: RootRepresentative, Goal: "also execute", Issuer: person.ID,
		ActionClasses: []string{workspace.ClassExecute},
	})
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "expand") {
		t.Fatalf("root expand: %v", err)
	}
	got, err := svc.IssueGrant(ctx, GrantRequest{
		CoordinatorID: RootRepresentative, Goal: "read as delegated", Issuer: person.ID,
		ActionClasses: []string{workspace.ClassRead},
	})
	if err != nil || got.Issuer != person.ID || !containsID(got.ActionClasses, workspace.ClassRead) {
		t.Fatalf("delegated root read %+v, %v", got, err)
	}
}

func billingSecurityConflict(t *testing.T, svc *Service) (string, Folder, Folder) {
	t.Helper()
	ctx := context.Background()
	billing := createFolder(t, svc, "Billing")
	security := createFolder(t, svc, "Security")
	person := workspace.Provenance{Origin: workspace.OriginPerson}
	if _, err := svc.InstructFolder(ctx, InstructRequest{
		ScopeID: billing.ID, Text: "always mail receipt links in the clear", Provenance: person,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InstructFolder(ctx, InstructRequest{
		ScopeID: security.ID, Text: "never mail raw URLs", Provenance: person,
	}); err != nil {
		t.Fatal(err)
	}
	const child = "childchildchild1"
	if err := svc.AddPlacement(ctx, billing.ID, conv(child), person); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddPlacement(ctx, security.ID, conv(child), person); err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.EffectiveGuidance(ctx, child)
	if err != nil || !loaded.Conflict {
		t.Fatalf("fixture must conflict: %+v, %v", loaded, err)
	}
	return child, billing, security
}

func assertConflictRoster(t *testing.T, people []ParticipantView, child, billing, security string) {
	t.Helper()
	seen := map[string]int{}
	for _, p := range people {
		seen[p.SourceChatID]++
		if p.ActorID == "" {
			t.Fatal("actor id must be minted by software")
		}
	}
	if seen[billing] != 1 || seen[security] != 1 || seen[RootRepresentative] != 1 {
		t.Fatalf("want one billing, one security, one root; got %v from %+v", seen, people)
	}
	if seen[child] != 1 {
		t.Fatalf("triggering chat must join without becoming boss: %v", seen)
	}
	if seen["billing"] != 0 {
		t.Fatalf("folder names must not replace folder ids: %v", seen)
	}
}
