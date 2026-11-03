package workstream

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

func workstreamSourcePtr[T any](v T) *T { return &v }

func workstreamSourceFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "usage.md")
	if err := os.WriteFile(p, []byte("Recorded usage.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

// WP-12 (a): all nine target forms derive titles and retain only their kind's
// fields, in added order; every duplicate returns the original row.
func TestWorkstreamSourcesEveryKind(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Kinds", workspace.Ref{Kind: workspace.FactoryJobKind, ID: "7", Factory: "f"})
	record, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "decision", Text: "Use totals.\nDetails.", Kept: true})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := s.Pages(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := workstreamSourceFile(t)
	folder := t.TempDir()
	cases := []struct {
		add   SourceAdd
		title string
	}{
		{SourceAdd{Kind: SourceFile, Path: &path}, "usage.md"},
		{SourceAdd{Kind: SourceRepo, Repo: workstreamSourcePtr("ACME/Billing")}, "ACME/Billing"},
		{SourceAdd{Kind: SourceRepo, Repo: &folder}, filepath.Base(folder)},
		{SourceAdd{Kind: SourceIssue, Repo: workstreamSourcePtr("acme/billing"), Number: workstreamSourcePtr(412)}, "acme/billing#412"},
		{SourceAdd{Kind: SourceLink, URL: workstreamSourcePtr("HTTPS://Docs.Example.com/x/?query=yes#part")}, "docs.example.com/x"},
		{SourceAdd{Kind: SourceKept, Ref: &KeptRef{Kind: "record", ID: record.ID}}, "Use totals."},
		{SourceAdd{Kind: SourceKept, Ref: &KeptRef{Kind: "page", ID: pages[0].ID}}, "Overview"},
		{SourceAdd{Kind: SourceKept, Ref: &KeptRef{Kind: "message", ID: "message", ChatID: workstreamSourcePtr("chat")}}, "Kept message"},
		{SourceAdd{Kind: SourceKept, Ref: &KeptRef{Kind: "factory_job", ID: "7", Factory: workstreamSourcePtr("f")}}, "Job 7"},
	}
	want := []SourceItem{}
	for _, tc := range cases {
		item, already, _, err := s.AddSource(ctx, w.ID, tc.add)
		if err != nil || already || item.Title != tc.title || item.Proposed || item.ReadByJobs || item.AddedBy != "you" || item.Why != "" {
			t.Fatalf("%+v: %+v %v %v", tc.add, item, already, err)
		}
		duplicate := tc.add
		if tc.add.Kind == SourceRepo && tc.add.Repo != &folder {
			duplicate.Repo = workstreamSourcePtr("acme/billing")
		}
		if tc.add.Kind == SourceIssue {
			duplicate.Repo = workstreamSourcePtr("ACME/Billing")
		}
		if tc.add.Kind == SourceLink {
			duplicate.URL = workstreamSourcePtr("https://docs.example.com/x/?query=yes#part")
		}
		again, already, change, err := s.AddSource(ctx, w.ID, duplicate)
		if err != nil || !already || !reflect.DeepEqual(item, again) || len(change.Parts) != 0 {
			t.Fatalf("duplicate: %+v %v %+v %v", again, already, change, err)
		}
		want = append(want, item)
	}
	list, err := s.Sources(ctx, w.ID)
	if err != nil || !reflect.DeepEqual(list, want) {
		t.Fatalf("round trip: %+v %v", list, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(folder); err != nil {
		t.Fatal(err)
	}
	list, err = s.Sources(ctx, w.ID)
	if err != nil || !list[0].Gone || !list[2].Gone || list[1].Gone || list[3].Gone {
		t.Fatalf("gone: %+v %v", list, err)
	}
}

// WP-12 (b), (c), (g): proposals cannot enable jobs, duplicates do not mutate
// them, and either person keep path preserves their attribution and reason.
func TestWorkstreamSourceProposalsAndKeeping(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Proposals")
	add := SourceAdd{Kind: SourceLink, URL: workstreamSourcePtr("https://example.com/guide")}
	proposal, already, _, err := s.ProposeSource(ctx, w.ID, add, "Read the guide.")
	if err != nil || already || !proposal.Proposed || proposal.ReadByJobs || proposal.AddedBy != "agent" || proposal.Why != "Read the guide." {
		t.Fatalf("proposal: %+v %v", proposal, err)
	}
	again, already, change, err := s.ProposeSource(ctx, w.ID, add, "Another reason.")
	if err != nil || !already || !reflect.DeepEqual(again, proposal) || len(change.Parts) != 0 {
		t.Fatalf("duplicate proposal: %+v %v", again, err)
	}
	if _, _, err := s.UpdateSource(ctx, proposal.ID, SourcePatch{ReadByJobs: workstreamSourcePtr(true)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("proposal jobs: %v", err)
	}
	unchanged, err := s.GetSource(ctx, proposal.ID)
	if err != nil || !reflect.DeepEqual(unchanged, proposal) {
		t.Fatalf("refusal changed proposal: %+v %v", unchanged, err)
	}
	add.Title, add.ReadByJobs = workstreamSourcePtr("The guide"), true
	kept, already, change, err := s.AddSource(ctx, w.ID, add)
	if err != nil || !already || kept.ID != proposal.ID || kept.Title != "The guide" || kept.Proposed || !kept.ReadByJobs || kept.AddedBy != "agent" || kept.Why != proposal.Why || len(change.Parts) == 0 {
		t.Fatalf("person keep: %+v %v %+v %v", kept, already, change, err)
	}
	if _, _, change, err := s.ProposeSource(ctx, w.ID, add, "Again."); !errors.Is(err, ErrInvalid) || len(change.Parts) != 0 {
		t.Fatalf("agent cannot set jobs: %v", err)
	}
	add.ReadByJobs = false
	if got, already, change, err := s.ProposeSource(ctx, w.ID, add, "Again."); err != nil || !already || got.Proposed || len(change.Parts) != 0 {
		t.Fatalf("duplicate kept: %+v %v", got, err)
	}
	for _, patch := range []SourcePatch{{Keep: workstreamSourcePtr(true)}, {Title: workstreamSourcePtr(kept.Title)}, {ReadByJobs: workstreamSourcePtr(true)}} {
		got, change, err := s.UpdateSource(ctx, kept.ID, patch)
		if err != nil || !reflect.DeepEqual(got, kept) || len(change.Parts) != 0 {
			t.Fatalf("no-op: %+v %+v %v", got, change, err)
		}
	}
}

// WP-12 (b): every invalid kind field is refused before a write and leaves
// the source list and the workstream's timestamp unchanged.
func TestWorkstreamSourcesInvalidInputsStoreNothing(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Invalid sources")
	path := workstreamSourceFile(t)
	bad := []SourceAdd{
		{Kind: SourceFile, Path: workstreamSourcePtr("relative")},
		{Kind: SourceFile, Path: workstreamSourcePtr(filepath.Dir(path) + "/../usage.md")},
		{Kind: SourceFile, Path: workstreamSourcePtr(filepath.Dir(path) + "/./usage.md")},
		{Kind: SourceFile, Path: workstreamSourcePtr(filepath.Dir(path) + "//usage.md")},
		{Kind: SourceFile, Path: workstreamSourcePtr(path + "/")},
		{Kind: SourceFile, Path: workstreamSourcePtr(path + "\x00")},
		{Kind: SourceFile, Path: workstreamSourcePtr(path + "\n")},
		{Kind: SourceFile, Path: workstreamSourcePtr(filepath.Join(t.TempDir(), "missing"))},
		{Kind: SourceFile, Path: workstreamSourcePtr("/" + strings.Repeat("x", 4096))},
		{Kind: SourceFile, Path: workstreamSourcePtr(string([]byte{'/', 255}))},
		{Kind: SourceFile, Path: &path, URL: workstreamSourcePtr("")},
		{Kind: SourceRepo, Repo: &path},
		{Kind: SourceIssue, Repo: &path, Number: workstreamSourcePtr(1)},
		{Kind: SourceKept, Ref: &KeptRef{Kind: "factory_job", ID: "7", Factory: workstreamSourcePtr("elsewhere")}},
	}
	for _, repo := range []string{"../x", "owner/..", "-owner/x", "owner/name/extra", "relative", "owner/.", "owner/"} {
		bad = append(bad, SourceAdd{Kind: SourceRepo, Repo: &repo})
	}
	for _, number := range []int{0, -1, 2147483648} {
		bad = append(bad, SourceAdd{Kind: SourceIssue, Repo: workstreamSourcePtr("owner/name"), Number: &number})
	}
	for _, url := range []string{"file:///etc/passwd", "javascript:alert(1)", "ftp://h/x", "//h/x", "https:///x", "https://user:pw@h/x", "https://h/x y", "https://h/x\n", "https://h/" + strings.Repeat("x", 2039)} {
		bad = append(bad, SourceAdd{Kind: SourceLink, URL: &url})
	}
	for _, title := range []string{"", strings.Repeat("x", 201), "two\nlines", " untrimmed ", "two\u2028lines"} {
		bad = append(bad, SourceAdd{Kind: SourceFile, Path: &path, Title: &title})
	}
	for i, add := range bad {
		_, _, change, err := s.AddSource(ctx, w.ID, add)
		if !errors.Is(err, ErrInvalid) || len(change.Parts) != 0 {
			t.Fatalf("invalid %d: %v %+v", i, err, change)
		}
	}
	for _, ref := range []*KeptRef{{Kind: "record", ID: "missing"}, {Kind: "page", ID: "missing"}} {
		if _, _, _, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceKept, Ref: ref}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown kept: %v", err)
		}
	}
	list, err := s.Sources(ctx, w.ID)
	after, getErr := s.Get(ctx, w.ID)
	if err != nil || getErr != nil || len(list) != 0 || !after.UpdatedAt.Equal(w.UpdatedAt) {
		t.Fatalf("refused sources changed state: %+v %v %v", list, err, getErr)
	}
}

// WP-12 (b), (d): kept rows must belong here and removal never deletes a
// record or page. Deleted pages cannot be kept.
func TestWorkstreamSourcesKeptOwnershipAndRemoval(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Kept")
	other := createStream(t, s, "Other")
	record, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "question", Text: "Keep?"})
	if err != nil {
		t.Fatal(err)
	}
	page, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Notes"})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []*KeptRef{{Kind: "record", ID: record.ID}, {Kind: "page", ID: page.ID}} {
		if _, _, _, err := s.AddSource(ctx, other.ID, SourceAdd{Kind: SourceKept, Ref: ref}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign ref: %v", err)
		}
		item, _, _, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceKept, Ref: ref})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.RemoveSource(ctx, item.ID); err != nil {
			t.Fatal(err)
		}
	}
	if records, err := s.Records(ctx, w.ID, "", ""); err != nil || len(records) != 1 {
		t.Fatalf("removed record: %v %v", records, err)
	}
	if _, err := s.Page(ctx, page.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.DeletePage(ctx, page.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceKept, Ref: &KeptRef{Kind: "page", ID: page.ID}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted page: %v", err)
	}
}

// WP-12 (e), (f): create validates all sources atomically, preserves why,
// folds duplicate targets, and ignores sources on a client retry.
func TestWorkstreamCreateSourcesAreAtomic(t *testing.T) {
	s, collections, _ := testStore(t)
	ctx := context.Background()
	path := workstreamSourceFile(t)
	p := Create{Name: "Created sources", AgentID: "agent", ClientID: "client", Why: "  Exactly as sent.\n", Sources: []SourceAdd{{Kind: SourceRepo, Repo: workstreamSourcePtr("ACME/Billing"), ReadByJobs: true}, {Kind: SourceFile, Path: &path}, {Kind: SourceRepo, Repo: workstreamSourcePtr("acme/billing")}}}
	w, _, err := s.Create(ctx, p)
	if err != nil || w.Why != p.Why {
		t.Fatalf("create: %+v %v", w, err)
	}
	items, err := s.Sources(ctx, w.ID)
	if err != nil || len(items) != 2 || !items[0].ReadByJobs || items[1].ReadByJobs {
		t.Fatalf("create sources: %+v %v", items, err)
	}
	p.Sources = []SourceAdd{{Kind: SourceLink, URL: workstreamSourcePtr("https://example.com/new")}}
	again, change, err := s.Create(ctx, p)
	if err != nil || again.ID != w.ID || len(change.Parts) != 0 {
		t.Fatalf("retry: %+v %v", again, err)
	}
	items, err = s.Sources(ctx, w.ID)
	if err != nil || len(items) != 2 {
		t.Fatalf("retry added sources: %v %v", items, err)
	}
	for _, bad := range []SourceAdd{{Kind: SourceFile, Path: workstreamSourcePtr("relative")}, {Kind: SourceKept, Ref: &KeptRef{Kind: "record", ID: "missing"}}} {
		p.Name, p.ClientID, p.Sources = "Free name", "", []SourceAdd{{Kind: SourceFile, Path: &path}, bad}
		if _, _, err := s.Create(ctx, p); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad create: %v", err)
		}
	}
	rows, err := collections.Collections(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("partial create: %v %v", rows, err)
	}
	p.Sources = nil
	if _, _, err := s.Create(ctx, p); err != nil {
		t.Fatalf("name was taken: %v", err)
	}
	if err := collections.Add(ctx, w.ID, workspace.Ref{Kind: workspace.ArtifactKind, ID: path}); err != nil {
		t.Fatal(err)
	}
	items, err = s.Sources(ctx, w.ID)
	if err != nil || len(items) != 2 {
		t.Fatalf("artifact became a source: %v %v", items, err)
	}
}

// WP-12 Fix 1 Major 2 (e): an identical client retry returns its first row
// even after an initial source disappears; a new client still validates it.
func TestWorkstreamMajor2CreateRetryE(t *testing.T) {
	s, collections, _ := testStore(t)
	ctx := context.Background()
	path := workstreamSourceFile(t)
	p := Create{Name: "Retry with removed source", AgentID: "agent", ClientID: "retry-removed", Sources: []SourceAdd{{Kind: SourceFile, Path: &path}}}
	w, _, err := s.Create(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	before, err := s.Sources(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, change, err := s.Create(ctx, p)
	if err != nil || !reflect.DeepEqual(again, w) || change.ID != "" || len(change.Parts) != 0 {
		t.Fatalf("retry: %+v %+v %v", again, change, err)
	}
	after, err := s.Sources(ctx, w.ID)
	if err != nil || len(after) != 1 || !reflect.DeepEqual(after, before) {
		t.Fatalf("retry changed sources: %+v %v", after, err)
	}
	p.ClientID, p.Name = "new-client", "New client name"
	if _, _, err := s.Create(ctx, p); !errors.Is(err, ErrInvalid) {
		t.Fatalf("new client bypassed validation: %v", err)
	}
	rows, err := collections.Collections(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("new client wrote rows: %+v %v", rows, err)
	}
	p.Sources = nil
	if _, _, err := s.Create(ctx, p); err != nil {
		t.Fatalf("refusal reserved name or client: %v", err)
	}
}

// WP-12 Fix 1 Minor 1 (a), (b): the normalized URL obeys the byte bound and
// a fitting multibyte URL remains an idempotent target when re-added as returned.
func TestWorkstreamMinor1LinkBoundAB(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Normalized URL bounds")
	tooLong := "https://h/" + strings.Repeat("é", 1019)
	if len(tooLong) != 2048 {
		t.Fatal("fixture must be exactly 2048 input bytes")
	}
	_, _, change, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceLink, URL: &tooLong})
	if !errors.Is(err, ErrInvalid) || change.ID != "" || len(change.Parts) != 0 {
		t.Fatalf("expanded URL accepted: %+v %v", change, err)
	}
	items, err := s.Sources(ctx, w.ID)
	if err != nil || len(items) != 0 {
		t.Fatalf("invalid URL stored: %+v %v", items, err)
	}
	unchanged, err := s.Get(ctx, w.ID)
	if err != nil || !unchanged.UpdatedAt.Equal(w.UpdatedAt) {
		t.Fatalf("invalid URL touched workstream: %+v %v", unchanged, err)
	}
	fits := "https://h/" + strings.Repeat("é", 339) + "abcd"
	want := "https://h/" + strings.Repeat("%C3%A9", 339) + "abcd"
	item, already, _, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceLink, URL: &fits})
	if err != nil || already || item.URL != want || len(item.URL) != 2048 {
		t.Fatalf("fitting URL: %+v %v %v", item, already, err)
	}
	again, already, change, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceLink, URL: &item.URL})
	if err != nil || !already || !reflect.DeepEqual(again, item) || change.ID != "" || len(change.Parts) != 0 {
		t.Fatalf("returned URL retry: %+v %v %+v %v", again, already, change, err)
	}
}

// WP-12 (c): the Read by jobs toggle persists through reopening the file.
func TestWorkstreamSourcesToggleSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "collections.db")
	collections, err := workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := New(collections, Options{})
	w := createStream(t, s, "Persistence")
	item, _, _, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceLink, URL: workstreamSourcePtr("https://example.com")})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []bool{true, false, true} {
		if _, _, err := s.UpdateSource(ctx, item.ID, SourcePatch{ReadByJobs: &flag}); err != nil {
			t.Fatal(err)
		}
		list, err := s.Sources(ctx, w.ID)
		if err != nil || len(list) != 1 || list[0].ReadByJobs != flag {
			t.Fatalf("toggle: %v %v", list, err)
		}
	}
	if err := collections.Close(); err != nil {
		t.Fatal(err)
	}
	collections, err = workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer collections.Close()
	s = New(collections, Options{})
	list, err := s.Sources(ctx, w.ID)
	if err != nil || len(list) != 1 || !list[0].ReadByJobs {
		t.Fatalf("reopen: %+v %v", list, err)
	}
}

// WP-12 (h): done is read-only for every source write, and reopen restores them.
func TestWorkstreamSourcesDoneAndReopen(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Done sources")
	add := SourceAdd{Kind: SourceLink, URL: workstreamSourcePtr("https://example.com")}
	item, _, _, err := s.AddSource(ctx, w.ID, add)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.MarkDone(ctx, w.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.AddSource(ctx, w.ID, add); !errors.Is(err, ErrConflict) {
		t.Fatalf("add done: %v", err)
	}
	if _, _, err := s.UpdateSource(ctx, item.ID, SourcePatch{Title: workstreamSourcePtr("New")}); !errors.Is(err, ErrConflict) {
		t.Fatalf("update done: %v", err)
	}
	if _, _, err := s.RemoveSource(ctx, item.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("remove done: %v", err)
	}
	if _, _, _, err := s.ProposeSource(ctx, w.ID, add, "Read it."); !errors.Is(err, ErrDone) {
		t.Fatalf("propose done: %v", err)
	}
	list, err := s.Sources(ctx, w.ID)
	if err != nil || len(list) != 1 || !reflect.DeepEqual(item, list[0]) {
		t.Fatalf("done list: %v %v", list, err)
	}
	if _, _, err := s.Reopen(ctx, w.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.UpdateSource(ctx, item.ID, SourcePatch{Title: workstreamSourcePtr("New")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RemoveSource(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.ProposeSource(ctx, w.ID, add, "Read it."); err != nil {
		t.Fatal(err)
	}
}

// WP-12 (b): the 2,001st source refuses atomically, while duplicates still work.
func TestWorkstreamSourcesLimit(t *testing.T) {
	s, collections, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Source limit")
	if err := collections.Write(ctx, func(tx *sql.Tx) error {
		statement, err := tx.Prepare("INSERT INTO workstream_sources(id,workstream_id,kind,target,title,url,proposed,added_by,added_at) VALUES (?,?,'link',?,'Example',?,1,'agent',100)")
		if err != nil {
			return err
		}
		defer statement.Close()
		for i := 0; i < SourcesMax; i++ {
			url := fmt.Sprintf("https://example.com/%d", i)
			if _, err := statement.Exec(fmt.Sprintf("limit-%d", i), w.ID, url, url); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	add := SourceAdd{Kind: SourceLink, URL: workstreamSourcePtr("https://example.com/extra")}
	if _, _, _, err := s.AddSource(ctx, w.ID, add); !errors.Is(err, ErrLimit) {
		t.Fatalf("source limit: %v", err)
	}
	if _, _, _, err := s.ProposeSource(ctx, w.ID, add, "Read it."); !errors.Is(err, ErrLimit) {
		t.Fatalf("proposal limit: %v", err)
	}
	add.URL = workstreamSourcePtr("https://example.com/0")
	item, already, _, err := s.AddSource(ctx, w.ID, add)
	if err != nil || !already || item.Proposed {
		t.Fatalf("keep at limit: %+v %v", item, err)
	}
}

// WP-12 (a), (b): derived titles cut at a UTF-8 boundary, supplied titles and
// file base names preserve internal spaces, and kept ids enforce byte bounds.
func TestWorkstreamSourceTitlesAndKeptRefBounds(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Source text bounds")
	record, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "decision", Text: strings.Repeat("界", 67) + "\nDetails."})
	if err != nil {
		t.Fatal(err)
	}
	item, _, _, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceKept, Ref: &KeptRef{Kind: "record", ID: record.ID}})
	if err != nil || item.Title != strings.Repeat("界", 66) || !utf8.ValidString(item.Title) {
		t.Fatalf("derived cut: %+v %v", item, err)
	}
	path := filepath.Join(t.TempDir(), "two  spaces.md")
	if err := os.WriteFile(path, []byte("Words"), 0600); err != nil {
		t.Fatal(err)
	}
	item, _, _, err = s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceFile, Path: &path})
	if err != nil || item.Title != "two  spaces.md" {
		t.Fatalf("base title: %+v %v", item, err)
	}
	item, _, err = s.UpdateSource(ctx, item.ID, SourcePatch{Title: workstreamSourcePtr("Two  spaces")})
	if err != nil || item.Title != "Two  spaces" {
		t.Fatalf("given title: %+v %v", item, err)
	}
	for _, length := range []int{0, 129} {
		ref := &KeptRef{Kind: "message", ID: strings.Repeat("x", length), ChatID: workstreamSourcePtr("chat")}
		if _, _, _, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceKept, Ref: ref}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("kept id %d: %v", length, err)
		}
		ref.ID = "message"
		ref.ChatID = workstreamSourcePtr(strings.Repeat("x", length))
		if _, _, _, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceKept, Ref: ref}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("kept chat %d: %v", length, err)
		}
	}
	if _, _, _, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceKept, Ref: &KeptRef{Kind: "message", ID: strings.Repeat("x", 128), ChatID: workstreamSourcePtr(strings.Repeat("c", 128))}}); err != nil {
		t.Fatal(err)
	}
}

// WP-12 (a), (c), (d), (f): sources retain identity, flags and order, without
// becoming members or changing the things they point at.
func TestWorkstreamSourcesRoundTripAndRemove(t *testing.T) {
	s, collections, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Sources")
	before, err := collections.Members(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := workstreamSourceFile(t)
	file, already, change, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceFile, Path: &path, ReadByJobs: true})
	if err != nil || already || file.Title != "usage.md" || file.AddedBy != "you" || file.Proposed || !file.ReadByJobs || len(change.Parts) == 0 {
		t.Fatalf("file: %+v %v %+v %v", file, already, change, err)
	}
	repo, _, _, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceRepo, Repo: workstreamSourcePtr("ACME/Billing")})
	if err != nil {
		t.Fatal(err)
	}
	duplicate, already, change, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceRepo, Repo: workstreamSourcePtr("acme/billing")})
	if err != nil || !already || duplicate.ID != repo.ID || len(change.Parts) != 0 {
		t.Fatalf("duplicate: %+v %v %+v %v", duplicate, already, change, err)
	}
	for _, flag := range []bool{false, true} {
		if _, _, err := s.UpdateSource(ctx, file.ID, SourcePatch{ReadByJobs: &flag}); err != nil {
			t.Fatal(err)
		}
		list, err := s.Sources(ctx, w.ID)
		if err != nil || len(list) != 2 || list[0].ReadByJobs != flag || list[1].ID != repo.ID {
			t.Fatalf("list: %+v %v", list, err)
		}
	}
	after, err := collections.Members(ctx, w.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("sources changed members: %+v %v", after, err)
	}
	removed, _, err := s.RemoveSource(ctx, file.ID)
	if err != nil || !reflect.DeepEqual(removed, file) {
		t.Fatalf("remove: %+v %v", removed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "Recorded usage.\n" {
		t.Fatalf("source removal changed file: %q %v", data, err)
	}
}
