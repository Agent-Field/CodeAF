package knowledge

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// A job's journal, whose saved events the pass marks as the job's.
type jobHistory struct{}

func (jobHistory) Inventory(context.Context) ([]Journal, error) {
	return []Journal{{ID: "j", Session: "job-session", Job: true}}, nil
}
func (jobHistory) Read(context.Context, Journal, JournalCursor) (JournalBatch, error) {
	return JournalBatch{}, nil
}

func jobTag(t *testing.T, v *Vault) string {
	t.Helper()
	db, err := v.database()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tag sql.NullString
	if err = db.QueryRow("SELECT json_extract(payload,'$.job') FROM events WHERE id='e'").Scan(&tag); err != nil {
		t.Fatal(err)
	}
	return tag.String
}

// Memory off means none of this work: on a 270 MB database it cost 70% of a CPU with Memory off.
func TestMemoryOffLeavesJobEventsAloneAndMemoryOnMarksThem(t *testing.T) {
	v := Open(t.TempDir())
	if err := config.WriteMemoryEnabled(v.profile, false); err != nil {
		t.Fatal(err)
	}
	db, err := v.database()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO events(partition,id,payload,arrived) VALUES('profile','e','{\"session\":\"job-session\"}',1)"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	last := ""
	if err = v.runPass(context.Background(), jobHistory{}, func() string { return "" }, &last); err != nil {
		t.Fatal(err)
	}
	if tag := jobTag(t, v); tag != "" {
		t.Fatalf("Memory off: the pass marked the event (%q)", tag)
	}

	if err = v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if err = v.runPass(context.Background(), jobHistory{}, func() string { return "" }, &last); err != nil {
		t.Fatal(err)
	}
	if tag := jobTag(t, v); tag != "1" {
		t.Fatalf("Memory on: the event was not marked (%q)", tag)
	}
}

func sessionIndexExists(t *testing.T, v *Vault) bool {
	t.Helper()
	db, err := v.database()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='profile_session_lookup'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// An existing database gains the index on the worker's first pass with Memory on, never on an open: an open must not
// wait for the build, and with Memory off nobody needs it. The marking then seeks by session.
func TestMemoryOnBuildsTheSessionIndexOnThePassAndTheMarkingUsesIt(t *testing.T) {
	v := Open(t.TempDir())
	if err := config.WriteMemoryEnabled(v.profile, false); err != nil {
		t.Fatal(err)
	}
	last := ""
	if err := v.runPass(context.Background(), jobHistory{}, func() string { return "" }, &last); err != nil {
		t.Fatal(err)
	}
	if sessionIndexExists(t, v) {
		t.Fatal("Memory off built the index")
	}
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if err := v.runPass(context.Background(), jobHistory{}, func() string { return "" }, &last); err != nil {
		t.Fatal(err)
	}
	if !sessionIndexExists(t, v) {
		t.Fatal("Memory on did not build the index")
	}
	db, err := v.database()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+markJobEvents, "job-session")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail + "\n")
	}
	if !strings.Contains(plan.String(), "USING INDEX profile_session_lookup") {
		t.Fatalf("the marking does not seek by session:\n%s", plan.String())
	}
}

// Building the index holds the write lock for its whole length: 1.5 s on a 1 GB database on the test Mac, so about
// 5 s on a laptop three times slower. A write in that time waits; it does not fail.
func TestAWriteWaitsOutASlowIndexBuild(t *testing.T) {
	const slowBuild = 5 * time.Second
	v := Open(t.TempDir())
	builder, err := v.database()
	if err != nil {
		t.Fatal(err)
	}
	defer builder.Close()
	tx, err := builder.Begin() // _txlock=immediate: this holds the write lock, as a CREATE INDEX does.
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(slowBuild, func() { _ = tx.Commit() })

	writer, err := v.database()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	began := time.Now()
	if _, err = writer.Exec("INSERT INTO events(partition,id,payload,arrived) VALUES('profile','during','{}',1)"); err != nil {
		t.Fatalf("a write failed after %s during the build: %v", time.Since(began), err)
	}
	if time.Since(began) < slowBuild/2 {
		t.Fatal("the write did not wait for the lock, so this test proves nothing")
	}
}
