package workstream

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

type workstreamWriteSnapshot struct {
	stream  Workstream
	records []Record
	pages   []Page
	history map[string][]PageVersion
}

func workstreamSnapshotWrites(t *testing.T, s *Store, id string) workstreamWriteSnapshot {
	t.Helper()
	ctx := context.Background()
	w, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	records, err := s.Records(ctx, id, "", "")
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := s.Pages(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	out := workstreamWriteSnapshot{stream: w, records: records, history: map[string][]PageVersion{}}
	for _, node := range nodes {
		p, err := s.Page(ctx, node.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		out.pages = append(out.pages, p)
		history, err := s.PageHistory(ctx, node.ID, 50)
		if err != nil {
			t.Fatal(err)
		}
		out.history[node.ID] = history
	}
	return out
}

func TestWorkstreamDoneRefusesAgentWritesInsideTheirTransactions(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Done writes")
	p, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Notes", Owner: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.MarkDone(ctx, w.ID, "Finished"); err != nil {
		t.Fatal(err)
	}
	before := workstreamSnapshotWrites(t, s, w.ID)
	*now = now.Add(time.Hour)
	who := Attribution{By: "agent"}
	writes := []struct {
		name string
		call func() (Change, error)
	}{
		{"record", func() (Change, error) {
			_, c, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "decision", Text: "Late proposal", By: "agent"})
			return c, err
		}},
		{"create", func() (Change, error) {
			_, c, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Late page", Owner: "agent", Attribution: who})
			return c, err
		}},
		{"save", func() (Change, error) {
			_, c, err := s.SavePage(ctx, p.ID, p.Version, json.RawMessage(emptyDoc), who)
			return c, err
		}},
		{"restore", func() (Change, error) {
			_, c, err := s.RestorePage(ctx, p.ID, p.Version, p.Version, who)
			return c, err
		}},
	}
	for _, write := range writes {
		t.Run(write.name, func(t *testing.T) {
			c, err := write.call()
			if !errors.Is(err, ErrDone) || !reflect.DeepEqual(c, Change{}) {
				t.Fatalf("done write: %+v %v", c, err)
			}
			if after := workstreamSnapshotWrites(t, s, w.ID); !reflect.DeepEqual(before, after) {
				t.Fatal("a refused write changed records, pages, history or the workstream")
			}
		})
	}
}

func TestWorkstreamDoneStillAllowsThePersonsPageWrites(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Person pages")
	if _, _, err := s.MarkDone(ctx, w.ID, "Finished"); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Hour)
	p, c, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Person notes"})
	if err != nil || c.ID != w.ID || p.Owner != "you" || p.UpdatedBy != "you" {
		t.Fatalf("person create: %+v %+v %v", p, c, err)
	}
	p, c, err = s.SavePage(ctx, p.ID, p.Version, json.RawMessage(emptyDoc), Attribution{})
	if err != nil || c.ID != w.ID || p.Version != 2 || p.UpdatedBy != "you" {
		t.Fatalf("person save: %+v %+v %v", p, c, err)
	}
	history, err := s.PageHistory(ctx, p.ID, 50)
	if err != nil || len(history) != 2 || history[0].SavedBy != "you" {
		t.Fatalf("person history: %+v %v", history, err)
	}
}

func TestWorkstreamDoneStillAllowsThePersonsRecords(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Person records")
	if _, _, err := s.MarkDone(ctx, w.ID, "Finished"); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Hour)
	r, c, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "decision", Text: "Person decision", Kept: true})
	if err != nil || c.ID != w.ID || r.By != "you" || r.State != "kept" {
		t.Fatalf("person record: %+v %+v %v", r, c, err)
	}
	after, err := s.Get(ctx, w.ID)
	if err != nil || !after.UpdatedAt.Equal(*now) || after.DoneAt.IsZero() {
		t.Fatalf("person write timestamp: %+v %v", after, err)
	}
}
