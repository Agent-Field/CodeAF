package parts

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type fakeHub struct{ events []svc.Event }

func (h *fakeHub) Publish(topic string, data any) {
	h.events = append(h.events, svc.Event{Topic: topic, Data: data})
}
func (*fakeHub) Subscribe(string) (<-chan svc.Event, func()) { return make(chan svc.Event), func() {} }

type fakeCollab struct {
	svc.Collab
	locks, releases int
	err             error
}

func (c *fakeCollab) Lock(context.Context, svc.Scope, bridge.LockPartParams) (bridge.Lock, error) {
	c.locks++
	return bridge.Lock{}, c.err
}
func (c *fakeCollab) Release(context.Context, svc.Scope, bridge.ReleasePartParams) (bridge.Empty, error) {
	c.releases++
	return bridge.Empty{}, nil
}
func setup(t *testing.T) (*Service, svc.Scope, *fakeHub) {
	t.Helper()
	h := &fakeHub{}
	return New(svc.Config{Root: t.TempDir()}, &svc.Services{Hub: h}), svc.Scope{App: "slides", Data: svc.DataReal, Caller: svc.Caller{Member: "raj", Role: svc.RoleEditor}}, h
}
func requireCode(t *testing.T, err error, code bridge.ErrorCode) {
	t.Helper()
	var wire *bridge.Error
	if !errors.As(err, &wire) || wire.Code != code {
		t.Fatalf("error=%v, want %s", err, code)
	}
}
func textDoc(t *testing.T, s *Service, scope svc.Scope, doc string) {
	t.Helper()
	_, err := s.CreateDoc(context.Background(), scope, bridge.CreateDocParams{Doc: doc, Surface: bridge.SurfaceText, Title: "Q4 board deck", Parts: []bridge.CreateDocPart{{Part: "section-1.md", Text: "# Team\nJan"}, {Part: "section-2.md", Text: "# Risks\nNone"}}})
	if err != nil {
		t.Fatal(err)
	}
}
func TestVersionChain(t *testing.T) {
	s, scope, hub := setup(t)
	ctx := context.Background()
	textDoc(t, s, scope, "decks/q4")
	collab := &fakeCollab{}
	s.all.Collab = collab
	operations := []struct {
		kind        bridge.PartVersionKind
		text, label string
		agent       bool
	}{
		{bridge.PartVersionKindNudge, "# Team\nFeb", "Jan → Feb", false},
		{bridge.PartVersionKindAsk, "# Hiring plan\nThree roles", "shorter hiring plan", true},
	}
	for i, op := range operations {
		scope.Caller.Agent = op.agent
		part, err := s.Write(ctx, scope, bridge.WritePartParams{Doc: "decks/q4", Part: "section-1.md", BaseVersion: i + 1, Kind: op.kind, Text: op.text, Label: op.label})
		if err != nil {
			t.Fatal(err)
		}
		if part.Version != i+2 || part.ByAgent != op.agent || part.UpdatedBy != "raj" || part.Text != op.text || part.UpdatedAt == "" {
			t.Fatalf("part=%+v", part)
		}
	}
	scope.Caller.Agent = false
	restored, err := s.Restore(ctx, scope, bridge.RestorePartParams{Doc: "decks/q4", Part: "section-1.md", Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Version != 4 || restored.Text != "# Team\nJan" {
		t.Fatalf("restore=%+v", restored)
	}
	if collab.locks != 1 || collab.releases != 1 {
		t.Fatalf("lock calls=%+v", collab)
	}
	history, err := s.Versions(ctx, scope, bridge.ListPartVersionsParams{Doc: "decks/q4", Part: "section-1.md"})
	if err != nil {
		t.Fatal(err)
	}
	kinds := []bridge.PartVersionKind{bridge.PartVersionKindRestore, bridge.PartVersionKindAsk, bridge.PartVersionKindNudge, bridge.PartVersionKindCreate}
	for i, v := range history.Versions {
		if v.Version != 4-i || v.Kind != kinds[i] {
			t.Fatalf("history=%+v", history)
		}
	}
	if len(history.Versions) != 4 || history.Versions[0].Label != "Restore v1" || !history.Versions[1].ByAgent {
		t.Fatalf("history=%+v", history)
	}
	diff, err := s.Diff(ctx, scope, bridge.DiffPartParams{Doc: "decks/q4", Part: "section-1.md", From: 1, To: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := []bridge.DiffLine{{Op: " ", Text: "# Team"}, {Op: "-", Text: "Jan"}, {Op: "+", Text: "Feb"}}
	if !reflect.DeepEqual(diff.Lines, want) || diff.FromText != "# Team\nJan" || diff.ToText != "# Team\nFeb" {
		t.Fatalf("diff=%+v", diff)
	}
	other, err := s.Get(ctx, scope, bridge.GetPartParams{Doc: "decks/q4", Part: "section-2.md"})
	if err != nil || other.Version != 1 {
		t.Fatalf("other=%+v err=%v", other, err)
	}
	body, err := os.ReadFile(filepath.Join(s.cfg.Root, scope.App, "content/decks/q4/section-1.md"))
	if err != nil || string(body) != restored.Text {
		t.Fatalf("body=%q err=%v", body, err)
	}
	changes := []int{}
	for _, ev := range hub.events {
		if ev.Topic != bridge.TopicDoc(scope.App, "decks/q4") {
			t.Fatalf("topic=%s", ev.Topic)
		}
		doc := ev.Data.(bridge.DocEvent)
		if doc.Kind == bridge.DocEventKindPart && doc.Part == "section-1.md" {
			changes = append(changes, doc.Version)
			if doc.By != "raj" {
				t.Fatalf("event=%+v", doc)
			}
		}
	}
	if !reflect.DeepEqual(changes, []int{1, 2, 3, 4}) {
		t.Fatalf("events=%v", changes)
	}
	// A fresh service sees the durable history, without reseeding.
	fresh := New(s.cfg, &svc.Services{})
	part, err := fresh.Get(ctx, scope, bridge.GetPartParams{Doc: "decks/q4", Part: "section-1.md"})
	if err != nil || part.Version != 4 {
		t.Fatalf("fresh=%+v err=%v", part, err)
	}
}
func TestStaleBaseAndInvalidWriteHaveNoEffects(t *testing.T) {
	s, scope, hub := setup(t)
	ctx := context.Background()
	textDoc(t, s, scope, "q4")
	initial := len(hub.events)
	for _, base := range []int{0, 2, 100} {
		_, err := s.Write(ctx, scope, bridge.WritePartParams{Doc: "q4", Part: "section-1.md", BaseVersion: base, Kind: bridge.PartVersionKindNudge, Text: "stale"})
		requireCode(t, err, bridge.CodeConflict)
	}
	_, err := s.Write(ctx, scope, bridge.WritePartParams{Doc: "q4", Part: "section-1.md", BaseVersion: 1, Kind: bridge.PartVersionKindNudge, Text: string([]byte{0xff})})
	requireCode(t, err, bridge.CodeInvalid)
	p, err := s.Get(ctx, scope, bridge.GetPartParams{Doc: "q4", Part: "section-1.md"})
	if err != nil || p.Version != 1 || p.Text != "# Team\nJan" || len(hub.events) != initial {
		t.Fatalf("part=%+v err=%v events=%v", p, err, hub.events)
	}
}
func TestDocumentOrderAndRemovedHistory(t *testing.T) {
	s, scope, _ := setup(t)
	ctx := context.Background()
	textDoc(t, s, scope, "q4")
	doc, err := s.Add(ctx, scope, bridge.AddPartParams{Doc: "q4", Part: "section-3.md", Text: "# Budget"})
	if err != nil || len(doc.Parts) != 3 {
		t.Fatalf("add=%+v %v", doc, err)
	}
	order := []string{"section-3.md", "section-1.md", "section-2.md"}
	doc, err = s.Reorder(ctx, scope, bridge.ReorderDocParams{Doc: "q4", Parts: order})
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range order {
		if doc.Parts[i].Part != id || doc.Parts[i].Version != 1 || doc.Parts[i].Lock != nil || doc.Parts[i].CommentsOpen != 0 {
			t.Fatalf("doc=%+v", doc)
		}
	}
	for _, bad := range [][]string{{"section-1.md"}, {"section-1.md", "section-1.md", "section-2.md"}} {
		_, err = s.Reorder(ctx, scope, bridge.ReorderDocParams{Doc: "q4", Parts: bad})
		requireCode(t, err, bridge.CodeInvalid)
	}
	doc, err = s.Remove(ctx, scope, bridge.RemovePartParams{Doc: "q4", Part: "section-1.md"})
	if err != nil || len(doc.Parts) != 2 {
		t.Fatalf("remove=%+v err=%v", doc, err)
	}
	_, err = s.Get(ctx, scope, bridge.GetPartParams{Doc: "q4", Part: "section-1.md"})
	requireCode(t, err, bridge.CodeNotFound)
	old, err := s.Get(ctx, scope, bridge.GetPartParams{Doc: "q4", Part: "section-1.md", Version: 1})
	if err != nil || old.Text != "# Team\nJan" {
		t.Fatalf("old=%+v err=%v", old, err)
	}
	versions, err := s.Versions(ctx, scope, bridge.ListPartVersionsParams{Doc: "q4", Part: "section-1.md"})
	if err != nil || len(versions.Versions) != 1 {
		t.Fatalf("versions=%+v %v", versions, err)
	}
	doc, err = s.Add(ctx, scope, bridge.AddPartParams{Doc: "q4", Part: "section-1.md", Text: "# New"})
	if err != nil || doc.Parts[2].Version != 2 {
		t.Fatalf("re-add=%+v err=%v", doc, err)
	}
	list, err := s.ListDocs(ctx, scope, bridge.ListDocsParams{Surface: bridge.SurfaceText})
	if err != nil || len(list.Docs) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	list, err = s.ListDocs(ctx, scope, bridge.ListDocsParams{Surface: bridge.SurfaceCanvas})
	if err != nil || len(list.Docs) != 0 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}
func TestShareOnOffAndLatest(t *testing.T) {
	s, scope, _ := setup(t)
	ctx := context.Background()
	textDoc(t, s, scope, "q4")
	var stable string
	for _, on := range []bool{true, false, true} {
		share, err := s.SetShare(ctx, scope, bridge.SetShareParams{Doc: "q4", On: on})
		if err != nil {
			t.Fatal(err)
		}
		if stable == "" {
			stable = share.Slug
		}
		if share.Slug != stable || share.Slug != "q4-board-deck" || share.URL != "codeaf://d/q4-board-deck" || share.On != on {
			t.Fatalf("share=%+v", share)
		}
		doc, err := s.OpenShare(ctx, scope, bridge.OpenShareParams{Slug: share.Slug})
		if !on {
			requireCode(t, err, bridge.CodeNotFound)
		} else if err != nil || doc.Doc != "q4" {
			t.Fatalf("doc=%+v err=%v", doc, err)
		}
	}
	_, err := s.Write(ctx, scope, bridge.WritePartParams{Doc: "q4", Part: "section-1.md", BaseVersion: 1, Kind: bridge.PartVersionKindNudge, Text: "# New"})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := s.OpenShare(ctx, scope, bridge.OpenShareParams{Slug: stable})
	if err != nil || doc.Parts[0].Version != 2 {
		t.Fatalf("doc=%+v err=%v", doc, err)
	}
	textDoc(t, s, scope, "other")
	second, err := s.SetShare(ctx, scope, bridge.SetShareParams{Doc: "other", On: true})
	if err != nil || second.Slug == stable {
		t.Fatalf("collision=%+v err=%v", second, err)
	}
	_, err = s.OpenShare(ctx, scope, bridge.OpenShareParams{Slug: "missing"})
	requireCode(t, err, bridge.CodeNotFound)
}
func TestRootsAndExplicitSeed(t *testing.T) {
	s, scope, _ := setup(t)
	ctx := context.Background()
	list, err := s.ListDocs(ctx, scope, bridge.ListDocsParams{})
	if err != nil || len(list.Docs) != 0 {
		t.Fatalf("empty=%+v %v", list, err)
	}
	files := spec.Files{"decks/q4/doc.json": []byte(`{"surface":"text","title":"Real","parts":["section-1.md"]}`), "decks/q4/section-1.md": []byte("# Real"), "decks/q4/theme.json": []byte(`{}`)}
	if err = s.Seed(scope.App, svc.DataReal, files); err != nil {
		t.Fatal(err)
	}
	files["decks/q4/section-1.md"] = []byte("# Sample")
	if err = s.Seed(scope.App, svc.DataSample, files); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		data         svc.DataMode
		folder, text string
	}{{svc.DataReal, "content", "# Real"}, {svc.DataSample, "sample-content", "# Sample"}, {"", "sample-content", "# Sample"}} {
		t.Run(string(tt.data), func(t *testing.T) {
			scope.Data = tt.data
			part, err := s.Get(ctx, scope, bridge.GetPartParams{Doc: "decks/q4", Part: "section-1.md"})
			if err != nil || part.Version != 1 || part.Text != tt.text {
				t.Fatalf("part=%+v err=%v", part, err)
			}
			body, err := os.ReadFile(filepath.Join(s.cfg.Root, scope.App, tt.folder, "decks/q4/section-1.md"))
			if err != nil || string(body) != tt.text {
				t.Fatalf("body=%q err=%v", body, err)
			}
			if _, err = os.Stat(filepath.Join(s.cfg.Root, scope.App, tt.folder, "decks/q4/theme.json")); err != nil {
				t.Fatal(err)
			}
		})
	}
	scope.Data = svc.DataSample
	_, err = s.Write(ctx, scope, bridge.WritePartParams{Doc: "decks/q4", Part: "section-1.md", Text: "# Sample v2", Kind: bridge.PartVersionKindAsk, BaseVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	scope.Data = svc.DataReal
	part, err := s.Get(ctx, scope, bridge.GetPartParams{Doc: "decks/q4", Part: "section-1.md"})
	if err != nil || part.Version != 1 {
		t.Fatalf("real=%+v err=%v", part, err)
	}
	db, err := sql.Open("sqlite", filepath.Join(s.cfg.Root, scope.App, "parts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var journal string
	if err = db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
		t.Fatalf("journal=%s err=%v", journal, err)
	}
}
func TestConcurrentBaseVersion(t *testing.T) {
	s, scope, _ := setup(t)
	ctx := context.Background()
	textDoc(t, s, scope, "q4")
	// Independent service instances rely on SQLite's write transaction as well.
	other := New(s.cfg, &svc.Services{})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, service := range []*Service{s, other} {
		wg.Add(1)
		go func(s *Service) {
			defer wg.Done()
			_, err := s.Write(ctx, scope, bridge.WritePartParams{Doc: "q4", Part: "section-1.md", Text: "# Edit", Kind: bridge.PartVersionKindNudge, BaseVersion: 1})
			results <- err
		}(service)
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			requireCode(t, err, bridge.CodeConflict)
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}
