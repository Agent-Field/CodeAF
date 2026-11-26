package data

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder collects a subscription's diffs.
type recorder struct {
	mu    sync.Mutex
	diffs []Diff
	more  chan struct{}
}

func newRecorder() *recorder { return &recorder{more: make(chan struct{}, 64)} }

func (r *recorder) take(d Diff) {
	r.mu.Lock()
	r.diffs = append(r.diffs, d)
	r.mu.Unlock()
	r.more <- struct{}{}
}

// next waits for the next diff.
func (r *recorder) next(t *testing.T) Diff {
	t.Helper()
	select {
	case <-r.more:
	case <-time.After(5 * time.Second):
		t.Fatal("no diff came")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	d := r.diffs[0]
	r.diffs = r.diffs[1:]
	return d
}

// quiet says no diff comes within a short wait.
func (r *recorder) quiet(t *testing.T, s *Store) {
	t.Helper()
	// A write to another type passes through the dispatcher after anything
	// queued before it, so once its hook has run nothing earlier is pending.
	done := make(chan struct{}, 1)
	stop := s.OnCommit(func(c Change) {
		if c.Type != "company" {
			return
		}
		select {
		case done <- struct{}{}:
		default:
		}
	})
	defer stop()
	mustCreate(t, s, person, "company", map[string]any{"name": fmt.Sprintf("flush %d", time.Now().UnixNano())})
	<-done
	select {
	case <-r.more:
		t.Fatalf("an unexpected diff: %+v", r.diffs)
	default:
	}
}

// waitIdle waits until every change committed so far has been dispatched.
func waitIdle(t *testing.T, s *Store) {
	t.Helper()
	done := make(chan struct{}, 1)
	stop := s.OnCommit(func(c Change) {
		if c.Type == "contact" {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})
	defer stop()
	mustCreate(t, s, person, "contact", map[string]any{"name": "idle"})
	<-done
}

// A LIVE QUERY IS TOLD ROW DIFFS: a matching create is an upsert, a change
// out of the query is a remove, a change to a record it never matched is
// nothing, and every diff carries the new total.
func TestSubscriptionDiffs(t *testing.T) {
	s, _ := openCRM(t)
	_, deals := seedDeals(t, s)
	waitIdle(t, s)
	rec := newRecorder()
	if _, err := s.Subscribe(Scope{App: "crm"}, SubSpec{ID: "open", Type: "deal", Query: "-stage:Lost amount>5000"}, rec.take); err != nil {
		t.Fatal(err)
	}

	made := mustCreate(t, s, person, "deal", map[string]any{"name": "New", "amount": 9000})[0]
	d := rec.next(t)
	if d.Sub != "open" || len(d.Upserts) != 1 || d.Upserts[0].ID() != made.ID() || len(d.Removes) != 0 || d.Total != 5 {
		t.Fatalf("create: %+v", d)
	}

	_, _ = s.Update(person, "deal", []string{deals["Initech · pilot"]}, map[string]any{"stage": "Lost"}, UpdateOptions{})
	d = rec.next(t)
	if len(d.Upserts) != 0 || !equal(d.Removes, []string{deals["Initech · pilot"]}) || d.Total != 4 {
		t.Fatalf("move out: %+v", d)
	}

	// Hooli is under 5000: changing it touches nothing the query sees.
	_, _ = s.Update(person, "deal", []string{deals["Hooli · first order"]}, map[string]any{"name": "Hooli"}, UpdateOptions{})
	rec.quiet(t, s)

	// Moving it over 5000 brings it in.
	_, _ = s.Update(person, "deal", []string{deals["Hooli · first order"]}, map[string]any{"amount": 6000}, UpdateOptions{})
	d = rec.next(t)
	if len(d.Upserts) != 1 || d.Upserts[0]["amount"] != 6000.0 || d.Total != 5 {
		t.Fatalf("move in: %+v", d)
	}

	// A change inside the query is an upsert with the new values.
	res, _ := s.Update(person, "deal", []string{deals["Globex · platform"]}, map[string]any{"stage": "Won"}, UpdateOptions{})
	d = rec.next(t)
	if len(d.Upserts) != 1 || d.Upserts[0]["stage"] != "Won" {
		t.Fatalf("update: %+v", d)
	}
	// Undo is a change like any other.
	_, _ = s.Undo(person, res.Batch)
	d = rec.next(t)
	if d.Upserts[0]["stage"] != "Negotiation" {
		t.Fatalf("undo: %+v", d)
	}

	_, _ = s.Delete(person, "deal", []string{made.ID()}, UpdateOptions{})
	d = rec.next(t)
	if !equal(d.Removes, []string{made.ID()}) || d.Total != 4 {
		t.Fatalf("delete: %+v", d)
	}

	// A record subscription sees its one record, and its deletion.
	one := newRecorder()
	if _, err := s.Subscribe(person, SubSpec{ID: "one", Type: "deal", Record: deals["Hooli · first order"]}, one.take); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Update(person, "deal", []string{deals["Hooli · first order"]}, map[string]any{"amount": 7000}, UpdateOptions{})
	if d := one.next(t); d.Upserts[0]["amount"] != 7000.0 {
		t.Fatalf("record: %+v", d)
	}
	rec.next(t)
	s.Unsubscribe("open")
	_, _ = s.Delete(person, "deal", []string{deals["Hooli · first order"]}, UpdateOptions{})
	if d := one.next(t); !equal(d.Removes, []string{deals["Hooli · first order"]}) {
		t.Fatalf("record delete: %+v", d)
	}
	rec.quiet(t, s)

	// A subscription an app is not allowed is refused at the start.
	_, err := s.Subscribe(Scope{App: "ghost"}, SubSpec{ID: "x", Type: "deal"}, rec.take)
	wantCode(t, err, CodeNotAllowed)
}

// ONE WRITER, MANY READERS: concurrent writes all land with distinct ids,
// concurrent reads never fail, and of many racing creates of one unique
// value exactly one wins.
func TestConcurrency(t *testing.T) {
	s, _ := openCRM(t)
	var wg sync.WaitGroup
	errs := make(chan error, 1000)
	for g := 0; g < 16; g++ {
		wg.Add(2)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				if _, err := s.Create(person, "deal", []map[string]any{{"name": fmt.Sprintf("g%d-%d", g, i), "amount": i}}); err != nil {
					errs <- err
				}
			}
		}(g)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				if _, err := s.Query(person, "deal", "amount>3 sort:-amount", "", 20); err != nil {
					errs <- err
				}
				if _, _, err := s.Count(person, "deal", "", "stage"); err != nil {
					errs <- err
				}
			}
		}()
	}
	var wins, dups int
	var mu sync.Mutex
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Create(person, "company", []map[string]any{{"name": "Only one"}})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if e, ok := AsError(err); ok && e.Code == CodeDuplicate {
				dups++
			} else {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if wins != 1 || dups != 15 {
		t.Fatalf("%d won and %d were refused", wins, dups)
	}
	page, _ := s.Query(person, "deal", "", "", 500)
	if page.Total != 400 || len(page.Rows) != 400 {
		t.Fatalf("total %d rows %d", page.Total, len(page.Rows))
	}
	seen := map[string]bool{}
	for _, r := range page.Rows {
		if seen[r.ID()] {
			t.Fatal("an id was given twice")
		}
		seen[r.ID()] = true
	}
}

// A CSV IMPORT'S DRY RUN IS THE IMPORT, ROLLED BACK: the preview and the real
// import agree row for row, refs resolve by name, and the import is one
// batch that undoes whole.
func TestImportCSV(t *testing.T) {
	s, _ := openCRM(t)
	mustCreate(t, s, person, "company", map[string]any{"name": "Northwind"})
	path := filepath.Join(t.TempDir(), "contacts.csv")
	csv := "\xef\xbb\xbfName,E-mail,Phone,Company,Notes\n" +
		"Ana Ruiz,ana@northwind.example,+1 555 0100,northwind,x\n" +
		"Bo,not-an-email,,Northwind,\n" +
		"Cy,cy@x.example,,Nowhere,\n" +
		",,,,\n" +
		"Di,di@x.example,,,\n"
	if err := os.WriteFile(path, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	mapping := map[string]string{"Name": "name", "E-mail": "email", "Phone": "phone", "Company": "company", "Notes": ""}
	dry, err := s.ImportCSV(person, "contact", ImportOptions{Path: path, Mapping: mapping, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Created != 2 || dry.Failed != 2 || dry.Batch != "" || len(dry.Rows) != 2 {
		t.Fatalf("dry run: %+v", dry)
	}
	if dry.Problems[0].Line != 3 || dry.Problems[0].Field != "email" || dry.Problems[1].Line != 4 || dry.Problems[1].Field != "company" {
		t.Fatalf("problems: %+v", dry.Problems)
	}
	page, _ := s.Query(person, "contact", "", "", 0)
	if page.Total != 0 {
		t.Fatal("a dry run wrote records")
	}
	real, err := s.ImportCSV(person, "contact", ImportOptions{Path: path, Mapping: mapping})
	if err != nil {
		t.Fatal(err)
	}
	if real.Created != dry.Created || real.Failed != dry.Failed || real.Batch == "" {
		t.Fatalf("real: %+v", real)
	}
	page, _ = s.Query(person, "contact", "company:Northwind", "", 0)
	if page.Total != 1 || page.Rows[0]["name"] != "Ana Ruiz" {
		t.Fatalf("imported: %+v", page.Rows)
	}
	if _, err := s.Undo(person, real.Batch); err != nil {
		t.Fatal(err)
	}
	page, _ = s.Query(person, "contact", "", "", 0)
	if page.Total != 0 {
		t.Fatal("undo left imported records")
	}
	// With no mapping, headers that name fields map themselves.
	auto, err := s.ImportCSV(person, "contact", ImportOptions{Path: path, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if auto.Mapping["Name"] != "name" || auto.Mapping["Phone"] != "phone" || auto.Mapping["E-mail"] != "" {
		t.Fatalf("auto mapping %v", auto.Mapping)
	}
	_, err = s.ImportCSV(person, "contact", ImportOptions{Path: path, Mapping: map[string]string{"Nope": "name"}})
	wantCode(t, err, CodeInvalid)
	_, err = s.ImportCSV(Scope{App: "ghost"}, "contact", ImportOptions{Path: path})
	wantCode(t, err, CodeNotAllowed)
	// Not even the app that owns the type may read a file into it.
	_, err = s.ImportCSV(Scope{App: "crm"}, "contact", ImportOptions{Path: "/etc/passwd", DryRun: true})
	wantCode(t, err, CodeNotAllowed)
}

// A DRAFT HAS ITS OWN SAMPLE FILE WITH THE SAME API: seeded rows point at
// each other by id or by name, a used type's shape comes from the person's
// data and none of their records do, and closing the draft lets its folder go.
func TestDraftSample(t *testing.T) {
	home := t.TempDir()
	p := NewPlace(home, Options{NoRefresh: true})
	defer p.Close()
	main, err := p.Main()
	if err != nil {
		t.Fatal(err)
	}
	if err := main.RegisterApp(fixture(t, "crm-app.json")); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, main, person, "contact", map[string]any{"name": "Private Person"})
	changes := make(chan Change, 16)
	p.OnCommit(func(c Change) { changes <- c })

	contact := "01JD8Z0000000000000000000A"
	err = p.SeedSample("d1", fixture(t, "hiring-app.json"), map[string][]map[string]any{
		"candidate": {{"name": "Sam", "contact": contact}, {"name": "Lee"}},
		"contact":   {{"id": contact, "name": "Sam Contact", "role_applied": "Designer"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := p.Store("d1")
	if err != nil {
		t.Fatal(err)
	}
	page, err := draft.Query(Scope{App: "hiring"}, "candidate", "contact:\"Sam Contact\"", "", 0)
	if err != nil || page.Total != 1 || page.Rows[0]["name"] != "Sam" {
		t.Fatalf("draft query: %+v %v", page, err)
	}
	page, _ = draft.Query(person, "contact", "", "", 0)
	if page.Total != 1 {
		t.Fatal("the person's records reached a sample")
	}
	for c := range changes {
		if c.Draft == "d1" {
			break
		}
		if c.Draft != "" {
			t.Fatalf("change draft %q", c.Draft)
		}
	}
	_, err = p.Store("never")
	wantCode(t, err, CodeNotFound)
	_, err = p.Store("../escape")
	wantCode(t, err, CodeInvalid)
	if err := p.CloseDraft("d1"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(p.DraftDir("d1")); err != nil {
		t.Fatal(err)
	}
	if _, err := draft.Create(person, "contact", []map[string]any{{"name": "late"}}); err == nil {
		t.Fatal("a closed draft took a write")
	}
	if !strings.HasPrefix(p.MainPath(), home) {
		t.Fatal("the data file is outside the home")
	}
}
