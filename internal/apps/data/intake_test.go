package data

import (
	"bytes"
	"path/filepath"
	"sync"
	"testing"
)

func TestIntakeCommitsOneReceiptWithRecordsAndWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	s, err := Open(path, Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	const app = "crm"
	raw := []byte(`{"id":"crm","records":{"contact":{"title":"name","fields":{"name":{"type":"text","required":true},"scan":{"type":"file"}}}},"capabilities":{"records":{"contact":"rw"}}}`)
	if err := s.RegisterApp(raw); err != nil {
		t.Fatal(err)
	}
	in := IntakeDelivery{App: app, Channel: "request", Key: "request-1", Type: "contact", Run: "follow-up",
		Set: map[string]any{"name": "Ada"}, Work: &WorkItem{Title: "Review"},
		FileField: "scan", Files: []IntakeFile{{Name: "scan.pdf", MIME: "application/pdf", Bytes: []byte("%PDF-1.4\n")}}}
	start, err := s.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got, err := s.Deliver(in)
			if err != nil || got.Created != 1 || len(got.Rows) != 1 || got.WorkItem == "" {
				t.Errorf("delivery = %+v, %v", got, err)
			}
		})
	}
	wg.Wait()
	events, err := s.Outbox(start, 10)
	if err != nil || len(events) != 2 || events[0].Event != EventRecordCreated ||
		events[0].Cause != "channel:request" || events[0].ByKind != ByImport || events[1].Event != EventWorkItemCreated {
		t.Fatalf("outbox = %+v, %v", events, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	got, err := s.Deliver(in)
	if err != nil || !got.Replayed || got.Rows[0] != events[0].Record {
		t.Fatalf("replay after reopen = %+v, %v", got, err)
	}
	files, err := s.IntakeFiles(Scope{App: app, Agent: true}, "contact", got.Rows[0])
	if err != nil || len(files) != 1 || !bytes.Equal(files[0].Bytes, in.Files[0].Bytes) {
		t.Fatalf("files = %+v, %v", files, err)
	}
	in.Set = map[string]any{"name": "Different"}
	if _, err := s.Deliver(in); err == nil {
		t.Fatal("different payload reused the delivery key")
	}
}
