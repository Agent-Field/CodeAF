package placegraph

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestKnowledgeMigrationIsDurableAndIdempotent(t *testing.T) {
	s, path := newStore(t)
	p := mk(t, s, "Marketing")
	legacy := snap(t, s).State.clone()
	legacy.KnowsMigrated = false
	legacy.Places[0].Context = Context{Instructions: "First paragraph\r\nwith a wrap.\r\n \r\nSecond paragraph.\n\n", Sources: []Source{
		{ID: "src_text", Kind: SourceFile, Ref: "/work/voice.md", Label: "Voice", AddedBy: AddedByYou},
		{ID: "src_url", Kind: SourceURL, Ref: "https://example.test/rules.txt?x=1", AddedBy: AddedByYou},
		{ID: "src_image", Kind: SourceFile, Ref: "/work/photo.png", AddedBy: AddedByYou},
		{ID: "src_folder", Kind: SourceFolder, Ref: "/work", AddedBy: AddedByYou},
		{ID: "src_web", Kind: SourceURL, Ref: "https://example.test/", AddedBy: AddedByYou},
	}}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	got := snap(t, reopened)
	lines := got.Knowledge(p.ID)
	if len(lines) != 4 {
		t.Fatalf("lines: %+v", lines)
	}
	if lines[0].Text != "First paragraph\nwith a wrap." || lines[1].Text != "Second paragraph." || lines[0].Source.Kind != LineYouWrote {
		t.Fatalf("paragraph migration: %+v", lines)
	}
	if lines[2].Source.Kind != LineFile || lines[2].Source.Path != "/work/voice.md" || lines[3].Source.Path != legacy.Places[0].Context.Sources[1].Ref {
		t.Fatalf("source migration: %+v", lines)
	}
	if got.Places[0].Context.Instructions != "" || !reflect.DeepEqual(got.Places[0].Context.Sources, legacy.Places[0].Context.Sources) {
		t.Fatal("migration changed source references or kept instructions")
	}
	first, _ := os.ReadFile(path)
	if !bytes.Contains(first, []byte(`"instructions": ""`)) {
		t.Fatal("rollback field missing")
	}
	if _, err := Open(Options{Path: path}); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if !bytes.Equal(first, second) {
		t.Fatal("reopening rewrote migration")
	}
	if got.Revision != legacy.Revision+1 || len(reopened.Receipts()) != 0 {
		t.Fatal("migration revision or receipt incorrect")
	}
	// A rollback writer can repopulate the old field; the durable marker wins.
	got.Places[0].Context.Instructions = "Restored by an older build."
	data, _ = json.Marshal(got.State)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if len(snap(t, reopened).Lines) != 4 {
		t.Fatal("rollback text migrated twice")
	}
}

func TestKnowledgeWritesUndoAndSnapshotIsolation(t *testing.T) {
	s, path := newStore(t)
	p := mk(t, s, "Marketing")
	line, add, err := s.AddLine(Line{PlaceID: p.ID, Text: "Lead with the free seat.", Source: LineSource{Kind: LineSaidInChat, ChatID: "chat_1", At: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}})
	if err != nil {
		t.Fatal(err)
	}
	original := line
	line.Text = "Lead with usage pricing."
	line.LastUsedAt = line.CreatedAt.Add(time.Hour)
	edit, err := s.UpdateLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(edit.ID); err != nil {
		t.Fatal(err)
	}
	if got := snap(t, s).Knowledge(p.ID); !reflect.DeepEqual(got, []Line{original}) {
		t.Fatalf("undo edit: %+v", got)
	}
	if _, err := s.Undo(add.ID); err != nil {
		t.Fatal(err)
	}
	if len(snap(t, s).Lines) != 0 {
		t.Fatal("undo add retained line")
	}
	line, _, err = s.AddLine(original)
	if err != nil {
		t.Fatal(err)
	}
	view := snap(t, s)
	view.Lines[0].Text = "mutated copy"
	reopened, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if snap(t, reopened).Lines[0] != line {
		t.Fatal("snapshot mutation changed persisted line")
	}
	del, err := s.DeleteLine(line.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(del.ID); err != nil {
		t.Fatal(err)
	}
	if snap(t, s).Lines[0] != line {
		t.Fatal("undo delete lost provenance")
	}
	if rc, err := s.UpdateLine(line); err != nil || !rc.Noop() {
		t.Fatal("unchanged line should be a noop", err)
	}
}

func TestKnowledgeReplacementMergeDeleteAndUndo(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	b := mk(t, s, "B")
	old, _, err := s.AddLine(Line{PlaceID: a.ID, Text: "Old", Source: LineSource{Kind: LineYouWrote}})
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := s.AddLine(Line{PlaceID: a.ID, Text: "New", Source: LineSource{Kind: LineLearned, Answers: 3}})
	if err != nil {
		t.Fatal(err)
	}
	old.ReplacedBy = next.ID
	old.ReplacedAt = next.CreatedAt
	ok(t)(s.UpdateLine(old))
	cyclic := next
	cyclic.ReplacedBy = old.ID
	cyclic.ReplacedAt = old.CreatedAt
	if _, err := s.UpdateLine(cyclic); !errors.Is(err, ErrInvalid) {
		t.Fatal("cycle accepted", err)
	}
	deletion, err := s.DeleteLine(next.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap(t, s).Lines[0].ReplacedBy != "" {
		t.Fatal("deleted replacement left dangling edge")
	}
	if _, err := s.Undo(deletion.ID); err != nil {
		t.Fatal(err)
	}
	_, merge, err := s.MergePlaces(a.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap(t, s).Knowledge(b.ID)) != 2 {
		t.Fatal("merge lost knowledge")
	}
	if _, err := s.Undo(merge.ID); err != nil {
		t.Fatal(err)
	}
	_, del, err := s.DeletePlace(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap(t, s).Lines) != 0 {
		t.Fatal("deleted place retained orphan knowledge")
	}
	if _, err := s.Undo(del.ID); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snap(t, s).Knowledge(a.ID), []Line{old, next}) {
		t.Fatal("undo lost knowledge")
	}
}

func TestKnowledgeRejectsInvalidWritesAtomically(t *testing.T) {
	s, path := newStore(t)
	p := mk(t, s, "A")
	before, _ := os.ReadFile(path)
	for _, line := range []Line{
		{PlaceID: p.ID, Text: " ", Source: LineSource{Kind: LineYouWrote}},
		{PlaceID: p.ID, Text: "Fact", Source: LineSource{Kind: "invented"}},
		{PlaceID: p.ID, Text: "Fact", Source: LineSource{Kind: LineFile}},
		{PlaceID: p.ID, Text: "Fact", Source: LineSource{Kind: LineLearned, Answers: -1}},
		{PlaceID: "missing", Text: "Fact", Source: LineSource{Kind: LineYouWrote}},
	} {
		if _, rc, err := s.AddLine(line); err == nil || !rc.Noop() {
			t.Fatal("invalid write accepted", line, err)
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed write changed file")
	}
}

func TestKnowledgeMigrationFailureKeepsOriginalBytes(t *testing.T) {
	s, path := newStore(t)
	mk(t, s, "A")
	legacy := snap(t, s).State
	legacy.KnowsMigrated = false
	legacy.Places[0].Context.Instructions = "Keep this paragraph."
	original, _ := json.Marshal(legacy)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	s.beforeRename = func() error { return errors.New("injected replace failure") }
	if _, err := s.Snapshot(); err == nil {
		t.Fatal("migration should fail")
	}
	bytesAfter, _ := os.ReadFile(path)
	if !bytes.Equal(original, bytesAfter) {
		t.Fatal("failed migration changed original bytes")
	}
	s.beforeRename = nil
	got := snap(t, s)
	if len(got.Lines) != 1 || got.Lines[0].Text != "Keep this paragraph." {
		t.Fatal("retry lost or duplicated paragraph")
	}
}

func TestKnowledgeUndoRefusesAnotherWindowChange(t *testing.T) {
	s, path := newStore(t)
	p := mk(t, s, "A")
	_, rc, err := s.AddLine(Line{PlaceID: p.ID, Text: "First", Source: LineSource{Kind: LineYouWrote}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = other.AddLine(Line{PlaceID: p.ID, Text: "Second", Source: LineSource{Kind: LineYouWrote}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(rc.ID); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale undo accepted", err)
	}
	if len(snap(t, s).Lines) != 2 {
		t.Fatal("stale undo lost another window's line")
	}
}
