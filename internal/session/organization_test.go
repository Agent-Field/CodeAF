package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

func organizationFixture(t *testing.T) (*Agent, *workspace.Store, workspace.Collection) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "collections.db")
	s, err := workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	group, err := s.Create(context.Background(), "Marketing")
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{config: Config{Place: Place{Dir: filepath.Join(t.TempDir(), "chat-one")}, Organization: &Organization{Path: path}}}
	if err = s.Add(context.Background(), group.ID, a.organizationSource()); err != nil {
		t.Fatal(err)
	}
	return a, s, group
}

func TestOrganizationContextRevisesAndWithdrawsAcrossChatsWithoutMemory(t *testing.T) {
	a, s, g := organizationFixture(t)
	ctx := context.Background()
	other := workspace.Ref{Kind: workspace.ConversationKind, ID: "chat-two"}
	if err := s.Add(ctx, g.ID, other); err != nil {
		t.Fatal(err)
	}
	record, err := s.CreateContext(ctx, "Launch window", "Launch on Tuesday", a.organizationSource(), []workspace.Ref{{Kind: workspace.CollectionKind, ID: g.ID}})
	if err != nil {
		t.Fatal(err)
	}
	a.refreshOrganization(ctx)
	if !strings.Contains(a.organizationText, "Tuesday") || a.config.Memory != nil {
		t.Fatal("shared context missing with memory disabled")
	}
	block, err := OrganizationContext(ctx, a.config.Organization, other)
	if err != nil || !strings.Contains(block, "Tuesday") {
		t.Fatalf("other chat: %q %v", block, err)
	}
	record, err = s.ReviseContext(ctx, record.ID, record.Revision, "Launch window", "Launch on Thursday", a.organizationSource(), record.Targets)
	if err != nil {
		t.Fatal(err)
	}
	a.refreshOrganization(ctx)
	if strings.Contains(a.organizationText, "Tuesday") || !strings.Contains(a.organizationText, "Thursday") {
		t.Fatal(a.organizationText)
	}
	if _, err = s.WithdrawContext(ctx, record.ID, record.Revision); err != nil {
		t.Fatal(err)
	}
	a.refreshOrganization(ctx)
	if !strings.Contains(a.organizationText, "no longer apply") || strings.Contains(a.organizationText, "Thursday") {
		t.Fatal(a.organizationText)
	}
}

func TestOrganizationDoesNotInheritUnsharedAncestorContext(t *testing.T) {
	a, s, g := organizationFixture(t)
	ctx := context.Background()
	parent, err := s.Create(ctx, "Company")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Add(ctx, parent.ID, workspace.Ref{Kind: workspace.CollectionKind, ID: g.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateContext(ctx, "Private planning", "Unshared ancestor fact", a.organizationSource(), []workspace.Ref{{Kind: workspace.CollectionKind, ID: parent.ID}}); err != nil {
		t.Fatal(err)
	}
	a.refreshOrganization(ctx)
	if a.organizationText != "" {
		t.Fatalf("ancestor context leaked: %s", a.organizationText)
	}
}

func TestSharedContextToolUsesRuntimeSourceAndWorkerCannotChangeIt(t *testing.T) {
	a, s, g := organizationFixture(t)
	args, _ := json.Marshal(map[string]any{"action": "create", "title": "Decision", "text": "Descriptive finding", "source": map[string]string{"kind": "conversation", "id": "someone-else"}, "targets": []workspace.Ref{{Kind: workspace.CollectionKind, ID: g.ID}}})
	result, failed, err := a.sharedContextTool(context.Background(), args)
	if err != nil || failed {
		t.Fatalf("%s %v", result, err)
	}
	var record workspace.ContextRecord
	if err = json.Unmarshal([]byte(result), &record); err != nil {
		t.Fatal(err)
	}
	if record.Source != a.organizationSource() {
		t.Fatalf("spoofed source: %+v", record.Source)
	}
	a.config.InTask = true
	withdraw, _ := json.Marshal(map[string]any{"action": "withdraw", "id": record.ID, "revision": record.Revision})
	_, failed, err = a.sharedContextTool(context.Background(), withdraw)
	if err != nil || !failed {
		t.Fatal("worker changed shared context")
	}
	kept, err := s.Context(context.Background(), record.ID)
	if err != nil || kept.Withdrawn {
		t.Fatal("refused mutation changed record")
	}
}

func TestOrganizationContextFailureDoesNotLeaveOldSnapshotCurrent(t *testing.T) {
	a, _, _ := organizationFixture(t)
	a.organizationText = "old snapshot"
	a.config.Organization.Path = filepath.Join(t.TempDir(), "a-directory")
	// A nonexistent store is an empty reading and must retire previously held context.
	a.refreshOrganization(context.Background())
	if !strings.Contains(a.organizationText, "no longer apply") {
		t.Fatal(a.organizationText)
	}
}

func TestOrganizationWorkerScopeUsesBothTaskAndOwningConversation(t *testing.T) {
	a, s, g := organizationFixture(t)
	ctx := context.Background()
	owner := a.organizationSource()
	a.config.InTask, a.config.taskID, a.config.rootSession = true, 7, owner.ID
	task := a.organizationSource()
	if task != (workspace.Ref{Kind: workspace.TaskKind, ID: "7", SessionID: owner.ID}) {
		t.Fatal(task)
	}
	for _, row := range []struct {
		text   string
		target workspace.Ref
	}{
		{"TASK_SCOPE", task}, {"OWNER_SCOPE", owner},
		{"GROUP_SCOPE", workspace.Ref{Kind: workspace.CollectionKind, ID: g.ID}},
		{"FOREIGN_SCOPE", workspace.Ref{Kind: workspace.TaskKind, ID: "7", SessionID: "another-chat"}},
	} {
		if _, err := s.CreateContext(ctx, row.text, row.text, owner, []workspace.Ref{row.target}); err != nil {
			t.Fatal(err)
		}
	}
	a.refreshOrganization(ctx)
	for _, expected := range []string{"TASK_SCOPE", "OWNER_SCOPE", "GROUP_SCOPE"} {
		if !strings.Contains(a.organizationText, expected) {
			t.Fatal(a.organizationText)
		}
	}
	if strings.Contains(a.organizationText, "FOREIGN_SCOPE") {
		t.Fatal(a.organizationText)
	}
}

func TestOrganizationSnapshotIsBoundedAndLeavesSystemPrefixIntact(t *testing.T) {
	a, s, _ := organizationFixture(t)
	ctx := context.Background()
	for i := 0; i < organizationContextLimit+2; i++ {
		if _, err := s.CreateContext(ctx, fmt.Sprint("Finding ", i), strings.Repeat("界", organizationTextLimit+50), a.organizationSource(), []workspace.Ref{a.organizationSource()}); err != nil {
			t.Fatal(err)
		}
	}
	a.messages = append(a.messages, textMessage("system", "IMMUTABLE SYSTEM"), textMessage("user", "continue"))
	before := messageContentText(a.messages[0])
	a.refreshOrganization(ctx)
	a.landVolatileLocked()
	if messageContentText(a.messages[0]) != before {
		t.Fatal("snapshot changed the cache prefix")
	}
	if !strings.HasPrefix(messageContentText(a.messages[len(a.messages)-1]), volatileNoteOpening) {
		t.Fatal("snapshot did not land in the volatile tail")
	}
	if strings.Count(a.organizationText, `"truncated":true`) != organizationContextLimit || !strings.Contains(a.organizationText, "more records") {
		t.Fatal(a.organizationText)
	}
	if strings.Contains(a.organizationText, strings.Repeat("界", organizationTextLimit+1)) {
		t.Fatal("snapshot exceeded the per-record bound")
	}
}

func TestOrganizationReadsUnicodeWindowsAtOneRevision(t *testing.T) {
	a, s, _ := organizationFixture(t)
	ctx := context.Background()
	text := strings.Repeat("界", organizationReadRunes) + "OLD-END"
	r, err := s.CreateContext(ctx, "Long finding", text, a.organizationSource(), []workspace.Ref{a.organizationSource()})
	if err != nil {
		t.Fatal(err)
	}
	call := func(revision, offset int) string {
		t.Helper()
		args, _ := json.Marshal(map[string]any{"action": "read", "id": r.ID, "revision": revision, "text_offset": offset})
		out, failed, err := a.sharedContextTool(ctx, args)
		if failed || err != nil {
			t.Fatalf("%s %v", out, err)
		}
		return out
	}
	var first struct {
		Text     string
		Revision int
		Next     int `json:"next_text_offset"`
	}
	if err := json.Unmarshal([]byte(call(0, 0)), &first); err != nil {
		t.Fatal(err)
	}
	if len([]rune(first.Text)) != organizationReadRunes || first.Next != organizationReadRunes || first.Revision != 1 {
		t.Fatalf("invalid text window: %d %d %d", len([]rune(first.Text)), first.Next, first.Revision)
	}
	if _, err = s.ReviseContext(ctx, r.ID, 1, r.Title, "NEW", r.Source, r.Targets); err != nil {
		t.Fatal(err)
	}
	if out := call(first.Revision, first.Next); !strings.Contains(out, "OLD-END") || strings.Contains(out, "NEW") {
		t.Fatal(out)
	}
}

func TestOrganizationRequiresSavedSourceAndLeavesAbsentToolsOffBelt(t *testing.T) {
	a := &Agent{}
	if len(a.organizationTools()) != 0 {
		t.Fatal("organization verbs on an unwired belt")
	}
	path := filepath.Join(t.TempDir(), "collections.db")
	a.config.Organization = &Organization{Path: path}
	if ref := a.organizationSource(); ref.Kind != "" {
		t.Fatal(ref)
	}
	_, failed, err := a.sharedContextTool(context.Background(), json.RawMessage(`{"action":"create","title":"finding","text":"value","targets":[{"kind":"conversation","id":"other"}]}`))
	if err != nil || !failed {
		t.Fatal("unsaved source accepted")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refusal provisioned a store: %v", err)
	}
	a.config.Organization.Path = ""
	if len(a.organizationTools()) != 0 {
		t.Fatal("empty path enables tools")
	}
}

func TestOrganizationStoreErrorRetiresEarlierSnapshot(t *testing.T) {
	a, _, _ := organizationFixture(t)
	a.organizationText = "old context"
	a.config.Organization.Path = t.TempDir()
	a.refreshOrganization(context.Background())
	if !strings.Contains(a.organizationText, "could not be read") || strings.Contains(a.organizationText, "old context") {
		t.Fatal(a.organizationText)
	}
}

func TestOrganizationNeverUsedConversationStaysEmptyAndReopenRetractsMembership(t *testing.T) {
	a, s, g := organizationFixture(t)
	ctx := context.Background()
	a.messages = append(a.messages, textMessage("system", "system"), textMessage("user", "hello"), textMessage("assistant", "hello"))
	a.refreshOrganization(ctx)
	if a.organizationText != "" {
		t.Fatalf("ordinary conversation acquired an empty snapshot: %s", a.organizationText)
	}
	if _, err := s.CreateContext(ctx, "Known finding", "OLD CLAIM", a.organizationSource(), []workspace.Ref{{Kind: workspace.CollectionKind, ID: g.ID}}); err != nil {
		t.Fatal(err)
	}
	a.refreshOrganization(ctx)
	if !strings.Contains(a.organizationText, "OLD CLAIM") {
		t.Fatal(a.organizationText)
	}
	if err := s.Remove(ctx, g.ID, a.organizationSource()); err != nil {
		t.Fatal(err)
	}
	reopened := &Agent{config: a.config, messages: a.messages}
	reopened.refreshOrganization(ctx)
	if reopened.organizationText != organizationRetired {
		t.Fatalf("reopen retained a removed scope: %s", reopened.organizationText)
	}
}

func TestOrganizationReopenRetractsRetargetedRecord(t *testing.T) {
	a, s, _ := organizationFixture(t)
	ctx := context.Background()
	r, err := s.CreateContext(ctx, "Finding", "OLD", a.organizationSource(), []workspace.Ref{a.organizationSource()})
	if err != nil {
		t.Fatal(err)
	}
	// There need not be a saved exposure marker: revision history is sufficient
	// to answer that this explicit target used to receive the record.
	if _, err = s.ReviseContext(ctx, r.ID, 1, r.Title, "NEW", r.Source, []workspace.Ref{{Kind: workspace.ConversationKind, ID: "other"}}); err != nil {
		t.Fatal(err)
	}
	a.refreshOrganization(ctx)
	if a.organizationText != organizationRetired {
		t.Fatal(a.organizationText)
	}
}

func TestOrganizationCollectionToolsResolvePageAndDefaultCurrentRef(t *testing.T) {
	a, s, g := organizationFixture(t)
	ctx := context.Background()
	for i := 0; i < organizationPageSize; i++ {
		if err := s.Add(ctx, g.ID, workspace.Ref{Kind: workspace.ConversationKind, ID: fmt.Sprintf("extra-%03d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	seen := 0
	a.config.Organization.Resolve = func(_ context.Context, supplied *workspace.Store, refs []workspace.Ref) ([]workspace.ResolvedRef, error) {
		if supplied == nil {
			t.Fatal("resolver lost the store")
		}
		seen = len(refs)
		rows := make([]workspace.ResolvedRef, len(refs))
		for i, r := range refs {
			rows[i] = workspace.ResolvedRef{Ref: r, Title: "owner title", Available: true}
		}
		return rows, nil
	}
	raw, _ := json.Marshal(map[string]any{"action": "show", "id": g.ID})
	out, failed, err := a.collectionsTool(ctx, raw)
	if failed || err != nil || seen != organizationPageSize || !strings.Contains(out, `"next_offset":25`) {
		t.Fatalf("%s %v seen=%d", out, err, seen)
	}
	raw, _ = json.Marshal(map[string]any{"action": "remove", "id": g.ID})
	_, failed, err = a.collectionsTool(ctx, raw)
	if failed || err != nil {
		t.Fatal(err)
	}
	groups, err := s.CollectionsFor(ctx, a.organizationSource())
	if err != nil || len(groups) != 0 {
		t.Fatalf("remove did not default to this chat: %+v %v", groups, err)
	}
	a.config.InTask = true
	raw, _ = json.Marshal(map[string]any{"action": "add", "id": g.ID})
	_, failed, err = a.collectionsTool(ctx, raw)
	if !failed || err != nil {
		t.Fatal("worker reorganized a collection")
	}
	groups, err = s.CollectionsFor(ctx, a.organizationSource())
	if err != nil || len(groups) != 0 {
		t.Fatal("refused worker mutation changed membership")
	}
}

func TestSharedContextRevisionCannotAccidentallyClearOmittedTargets(t *testing.T) {
	a, s, _ := organizationFixture(t)
	ctx := context.Background()
	r, err := s.CreateContext(ctx, "Finding", "BEFORE", a.organizationSource(), []workspace.Ref{a.organizationSource()})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"action": "revise", "id": r.ID, "revision": 1, "title": r.Title, "text": "AFTER"})
	out, failed, err := a.sharedContextTool(ctx, raw)
	if err != nil || !failed || !strings.Contains(out, "complete targets") {
		t.Fatalf("%s %v", out, err)
	}
	unchanged, err := s.Context(ctx, r.ID)
	if err != nil || unchanged.Revision != 1 || unchanged.Text != "BEFORE" || len(unchanged.Targets) != 1 {
		t.Fatalf("omission cleared applicability: %+v %v", unchanged, err)
	}
	raw, _ = json.Marshal(map[string]any{"action": "revise", "id": r.ID, "revision": 1, "title": r.Title, "text": "AFTER", "targets": []workspace.Ref{}})
	_, failed, err = a.sharedContextTool(ctx, raw)
	if err != nil || failed {
		t.Fatal("explicit clearing refused")
	}
	cleared, err := s.Context(ctx, r.ID)
	if err != nil || cleared.Revision != 2 || len(cleared.Targets) != 0 {
		t.Fatalf("explicit clearing failed: %+v %v", cleared, err)
	}
}

func TestOrganizationFindNameDoesNotSilentlySearchMembership(t *testing.T) {
	a, _, g := organizationFixture(t)
	out, failed, err := a.collectionsTool(context.Background(), json.RawMessage(`{"action":"find","name":"MARKET"}`))
	if failed || err != nil || !strings.Contains(out, g.ID) {
		t.Fatalf("%s %v", out, err)
	}
}
