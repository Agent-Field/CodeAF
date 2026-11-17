package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func madeIn(title string, team *OriginTeam) Automation {
	a := routine("15m")
	// The id is set so id order is title order.
	a.ID, a.Title = "m-"+title, title
	a.Origin = Origin{SessionID: "s", Transcript: "t", Team: team}
	return a
}

// madeInProjMax is the projection budget the tests read with unless they test
// it: the wire's.
const madeInProjMax = 256 << 10

// continueAfter is the cursor that continues after every entry page covered,
// or "" when the page reached the end.
func continueAfter(t *testing.T, page MadeInPage) string {
	t.Helper()
	if page.End {
		return ""
	}
	if len(page.Entries) == 0 {
		t.Fatalf("a page that is not the end covered nothing: %+v", page)
	}
	cursor, err := page.CursorAt(page.Entries[len(page.Entries)-1].ID)
	if err != nil {
		t.Fatal(err)
	}
	return cursor
}

// walkMadeIn reads every page for teamID with limit, and answers the rows'
// titles, the summed unknown count and how many pages it took.
func walkMadeIn(t *testing.T, s *Store, teamID string, limit int) ([]string, int, int) {
	t.Helper()
	var titles []string
	seen := map[string]bool{}
	unknown, pages, cursor, last := 0, 0, "", ""
	for {
		page, err := s.MadeInTeam(context.Background(), teamID, cursor, limit, madeInProjMax)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		rows := 0
		for _, entry := range page.Entries {
			if entry.ID <= last || seen[entry.ID] {
				t.Fatalf("%s came twice or out of order after %q", entry.ID, last)
			}
			seen[entry.ID], last = true, entry.ID
			if entry.Unknown {
				unknown++
			}
			if a := entry.Row; a != nil {
				rows++
				if entry.Unknown || a.Origin.Team == nil || a.Origin.Team.ID != teamID {
					t.Fatalf("a row from team %+v came back for %s", a.Origin.Team, teamID)
				}
				titles = append(titles, a.Title)
			}
		}
		if rows > limit {
			t.Fatalf("a page of %d rows over limit %d", rows, limit)
		}
		if cursor = continueAfter(t, page); cursor == "" {
			return titles, unknown, pages
		}
		if pages > 50 {
			t.Fatal("the walk never ended")
		}
	}
}

// THE READ IS EXACT ON THE RECORDED TEAM, AND A WALK SAYS HOW MANY ROWS NEVER
// HAD ONE — legacy, never marked known, or not JSON — once each,
// across the store being closed and opened again. Known none is not unknown.
func TestMadeInTeamReadsTheRecordedTeamAndCountsUnknownOncePerWalk(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		title string
		team  *OriginTeam
	}{
		{"legacy", nil},
		{"none", &OriginTeam{Known: true}},
		{"a1", &OriginTeam{Known: true, ID: "teamA"}},
		{"a2", &OriginTeam{Known: true, ID: "teamA"}},
		{"a3", &OriginTeam{Known: true, ID: "teamA"}},
		{"b1", &OriginTeam{Known: true, ID: "teamB"}},
		{"forbidden", &OriginTeam{ID: "teamA"}},
	} {
		if _, err := s.Create(madeIn(row.title, row.team)); err != nil {
			t.Fatal(err)
		}
	}
	// A document nobody can read, and one that is JSON naming another team but
	// that this build cannot decode, are neither a team's nor silently ignored.
	for id, doc := range map[string]string{
		"zz-damaged":   `{not json`,
		"zz-undecoded": `{"title":5,"origin":{"team":{"known":true,"id":"teamB"}}}`,
	} {
		if _, err := s.db.Exec(`INSERT INTO automations (id, doc, status) VALUES (?, ?, 'active')`, id, doc); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	for _, limit := range []int{1, 2, MadeInTeamMax} {
		titles, unknown, _ := walkMadeIn(t, s, "teamA", limit)
		if fmt.Sprint(titles) != "[a1 a2 a3]" {
			t.Fatalf("limit %d: rows %v, want a1 a2 a3 in id order", limit, titles)
		}
		// The JSON row naming teamB is teamB's to refuse when teamB is read: for
		// any other team it is a recorded team that is not this one, and it is
		// never decoded here.
		if unknown != 3 {
			t.Fatalf("limit %d: unknown summed to %d, want 3 (legacy, not known, not JSON)", limit, unknown)
		}
	}
	if titles, unknown, _ := walkMadeIn(t, s, "teamC", 10); len(titles) != 0 || unknown != 3 {
		t.Fatalf("an unused team read %v with unknown %d", titles, unknown)
	}
	for _, bad := range []int{0, MadeInTeamMax + 1} {
		if _, err := s.MadeInTeam(context.Background(), "teamA", "", bad, madeInProjMax); err == nil {
			t.Fatalf("limit %d was served", bad)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.MadeInTeam(cancelled, "teamA", "", 5, madeInProjMax); err == nil {
		t.Fatal("a cancelled read answered")
	}
}

// ONE PAGE READS AT MOST [MadeInScanMax] STORED ROWS, however few match: a store
// of other teams' rows answers an empty page that says where it stopped, and
// the walk still finds the one row after them.
func TestMadeInTeamReadsABoundedWindowOfTheStore(t *testing.T) {
	s, _ := openStore(t)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := range MadeInScanMax + 5 {
		if _, err := tx.Exec(`INSERT INTO automations (id, doc, status) VALUES (?, ?, 'active')`, fmt.Sprintf("r%04d", i), `{"title":"other","origin":{"team":{"known":true,"id":"teamB"}}}`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO automations (id, doc, status) VALUES ('r9999', '{"title":"mine","origin":{"team":{"known":true,"id":"teamA"}}}', 'active')`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	first, err := s.MadeInTeam(context.Background(), "teamA", "", MadeInTeamMax, madeInProjMax)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != MadeInScanMax || first.End || first.Entries[MadeInScanMax-1].ID != fmt.Sprintf("r%04d", MadeInScanMax-1) {
		t.Fatalf("first page = %d entries, end %v; want the full window and a continuation", len(first.Entries), first.End)
	}
	for _, entry := range first.Entries {
		if entry.Row != nil || entry.Unknown {
			t.Fatalf("another team's row %s came back as %+v", entry.ID, entry)
		}
	}
	titles, unknown, pages := walkMadeIn(t, s, "teamA", MadeInTeamMax)
	if fmt.Sprint(titles) != "[mine]" || unknown != 0 || pages != 2 {
		t.Fatalf("the walk read %v, unknown %d, over %d pages", titles, unknown, pages)
	}
}

// A ROW THAT SAYS IT WAS MADE IN THE TEAM BUT CANNOT BE DECODED REFUSES THE
// PAGE: an empty or shorter list would be a list missing one of the team's own.
func TestMadeInTeamRefusesAMatchingRowItCannotRead(t *testing.T) {
	s, _ := openStore(t)
	if _, err := s.db.Exec(`INSERT INTO automations (id, doc, status) VALUES ('x1', '{"title":5,"origin":{"team":{"known":true,"id":"teamC"}}}', 'active')`); err != nil {
		t.Fatal(err)
	}
	if page, err := s.MadeInTeam(context.Background(), "teamC", "", 10, madeInProjMax); err == nil {
		t.Fatalf("an unreadable row of the team was answered as %+v", page)
	}
	// For another team the row is a recorded team that is not this one: covered,
	// not unknown, never decoded.
	if page, err := s.MadeInTeam(context.Background(), "teamA", "", 10, madeInProjMax); err != nil || len(page.Entries) != 1 || page.Entries[0].Unknown || page.Entries[0].Row != nil || !page.End {
		t.Fatalf("another team's read = %+v, %v; want the row covered and the end reached", page, err)
	}
}

// madeInStore is a store holding teamA rows m-a1..m-a4 (ids in title order).
func madeInStore(t *testing.T) *Store {
	t.Helper()
	s, _ := openStore(t)
	for _, title := range []string{"a1", "a2", "a3", "a4"} {
		if _, err := s.Create(madeIn(title, &OriginTeam{Known: true, ID: "teamA"})); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// firstOfTwo reads the first page with limit 1 and answers its cursor.
func firstOfTwo(t *testing.T, s *Store) string {
	t.Helper()
	page, err := s.MadeInTeam(context.Background(), "teamA", "", 1, madeInProjMax)
	if err != nil {
		t.Fatal(err)
	}
	cursor := continueAfter(t, page)
	if cursor == "" || len(cursor) > MadeInCursorMax {
		t.Fatalf("the first page's cursor %q", cursor)
	}
	return cursor
}

func wantStale(t *testing.T, s *Store, cursor, what string) {
	t.Helper()
	if page, err := s.MadeInTeam(context.Background(), "teamA", cursor, 1, madeInProjMax); !errors.Is(err, ErrMadeInStale) {
		t.Fatalf("after %s the walk went on: %+v, %v", what, page, err)
	}
}

// A WALK NEVER ENDS COMPLETE WHILE MISSING A ROW: an automation created below
// the cursor, one deleted, and the same done by a writer that knows nothing of
// the revision (an older build sharing the file) all make the walk start again.
func TestMadeInStaleAfterCreateBelowCursor(t *testing.T) {
	s := madeInStore(t)
	cursor := firstOfTwo(t, s)
	if _, err := s.Create(madeIn("a0", &OriginTeam{Known: true, ID: "teamA"})); err != nil {
		t.Fatal(err)
	}
	wantStale(t, s, cursor, "a create below the cursor")
}

func TestMadeInStaleAfterDelete(t *testing.T) {
	s := madeInStore(t)
	cursor := firstOfTwo(t, s)
	if err := s.Delete("m-a3"); err != nil {
		t.Fatal(err)
	}
	wantStale(t, s, cursor, "a delete")
}

func TestMadeInStaleAfterOlderWriterRawInsertDeleteAndOriginRewrite(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"a raw insert", `INSERT INTO automations (id, doc, status) VALUES ('m-a0', '{"title":"old"}', 'active')`},
		{"a raw delete", `DELETE FROM automations WHERE id = 'm-a2'`},
		{"an older update", `UPDATE automations SET doc = json_remove(doc, '$.origin.team') WHERE id = 'm-a3'`},
		{"a retargeted origin", `UPDATE automations SET doc = json_set(doc, '$.origin.team.id', 'teamB') WHERE id = 'm-a3'`},
		{"a doc that is not json", `UPDATE automations SET doc = '{damaged' WHERE id = 'm-a4'`},
		{"an id rewritten", `UPDATE automations SET id = 'm-a0' WHERE id = 'm-a4'`},
	} {
		s := madeInStore(t)
		cursor := firstOfTwo(t, s)
		if _, err := s.db.Exec(tc.raw); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		wantStale(t, s, cursor, tc.name)
	}
}

// THE CLOCK'S ORDINARY WORK NEVER STALES A WALK: a status, a wake time, a seen
// mark, a memo, a revision and a person's edit that keeps the creation team.
func TestMadeInRunAndStatusWritesDoNotStale(t *testing.T) {
	s := madeInStore(t)
	cursor := firstOfTwo(t, s)
	if _, err := s.SetStatus("m-a2", StatusPaused); err != nil {
		t.Fatal(err)
	}
	a3, err := s.Get("m-a3")
	if err != nil {
		t.Fatal(err)
	}
	a3.Title = "renamed"
	if _, err := s.Update(a3); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE automations SET next_ms = next_ms + 60000, seen = 'seen', memo = 'memo', revision = revision + 1, seq = seq + 1 WHERE id = 'm-a4'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE automations SET doc = json_set(doc, '$.title', 'retitled') WHERE id = 'm-a4'`); err != nil {
		t.Fatal(err)
	}
	page, err := s.MadeInTeam(context.Background(), "teamA", cursor, MadeInTeamMax, madeInProjMax)
	if err != nil || len(page.Entries) != 3 || !page.End {
		t.Fatalf("after ordinary writes the walk = %+v, %v; want the three rows after the cursor and the end", page, err)
	}
}

// AN EDIT NEVER MOVES WHERE AN AUTOMATION WAS MADE: a caller that drops,
// changes or invents the creation team is ignored, known or unknown.
func TestStoreUpdateKeepsTheCreationTeam(t *testing.T) {
	s, _ := openStore(t)
	known, err := s.Create(madeIn("known", &OriginTeam{Known: true, ID: "teamA"}))
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := s.Create(madeIn("unknown", nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, team := range []*OriginTeam{nil, {Known: true}, {Known: true, ID: "teamB"}} {
		known.Origin.Team = team
		if saved, err := s.Update(known); err != nil || saved.Origin.Team == nil || saved.Origin.Team.ID != "teamA" {
			t.Fatalf("an update passing %+v saved %+v, %v", team, saved.Origin.Team, err)
		}
		if got, err := s.Get(known.ID); err != nil || got.Origin.Team == nil || got.Origin.Team.ID != "teamA" || !got.Origin.Team.Known {
			t.Fatalf("after an update passing %+v the store holds %+v, %v", team, got.Origin.Team, err)
		}
		unknown.Origin.Team = team
		if _, err := s.Update(unknown); err != nil {
			t.Fatal(err)
		}
		if got, err := s.Get(unknown.ID); err != nil || got.Origin.Team != nil {
			t.Fatalf("an unknown row became %+v after an update passing %+v, %v", got.Origin.Team, team, err)
		}
	}
}

// bigDoc is a JSON document of about n bytes recording team, padded in a
// field no order's view reads, so its projection is small.
func bigDoc(n int, team string) string {
	return `{"title":"big","pad":"` + strings.Repeat("x", n) + `","origin":{"team":{"known":true,"id":"` + team + `"}}}`
}

// briefDoc is a document recording team whose brief is n bytes: a field the
// order's view reads, so its projection is large.
func briefDoc(n int, team string) string {
	return `{"title":"brief","action":{"do":"` + strings.Repeat("d", n) + `"},"origin":{"team":{"known":true,"id":"` + team + `"}}}`
}

func rawRow(t *testing.T, s *Store, id, doc string) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO automations (id, doc, status) VALUES (?, ?, 'active')`, id, doc); err != nil {
		t.Fatal(err)
	}
}

// A DOCUMENT LARGER THAN THE SCAN BUDGET — ANOTHER TEAM'S INCLUDED — IS NEVER
// SKIPPED AND NEVER THE END: the page before it ends there, and a page that
// would start at it is refused naming it, every time, with the cursor unmoved.
// This test claims nothing about the walk being complete: it cannot be, and
// the read says so rather than pretending.
func TestMadeInForeignOversizedDocRefusesAtItsPosition(t *testing.T) {
	s, _ := openStore(t)
	rawRow(t, s, "m-a0", `{"title":"small","origin":{"team":{"known":true,"id":"teamA"}}}`)
	rawRow(t, s, "m-b0", bigDoc(MadeInScanBytesMax, "teamB"))
	rawRow(t, s, "m-c0", `{"title":"after","origin":{"team":{"known":true,"id":"teamA"}}}`)
	first, err := s.MadeInTeam(context.Background(), "teamA", "", MadeInTeamMax, madeInProjMax)
	if err != nil || len(first.Entries) != 1 || first.Entries[0].ID != "m-a0" || !first.Stopped || first.StopID != "m-b0" || first.End {
		t.Fatalf("the page before the big row = %+v, %v; want m-a0 alone, stopped at m-b0", first, err)
	}
	cursor := continueAfter(t, first)
	for range 2 {
		if page, err := s.MadeInTeam(context.Background(), "teamA", cursor, MadeInTeamMax, madeInProjMax); !errors.Is(err, ErrMadeInTooLarge) || !strings.Contains(err.Error(), "m-b0") || page.End {
			t.Fatalf("the page starting at the big row = %+v, %v; want it refused by name", page, err)
		}
	}
}

// A MATCH WHOSE PROJECTION IS LARGER THAN THE BUDGET IS NEVER COPIED OUT: a
// page that would start at it is refused, and another team's read covers the
// row without projecting it.
func TestMadeInProjectionOverBudgetRefuses(t *testing.T) {
	s, _ := openStore(t)
	rawRow(t, s, "m-a0", briefDoc(300<<10, "teamA"))
	if page, err := s.MadeInTeam(context.Background(), "teamA", "", 10, madeInProjMax); !errors.Is(err, ErrMadeInTooLarge) || !strings.Contains(err.Error(), "m-a0") {
		t.Fatalf("a match over the projection budget = %+v, %v", page, err)
	}
	if page, err := s.MadeInTeam(context.Background(), "teamB", "", 10, madeInProjMax); err != nil || len(page.Entries) != 1 || page.Entries[0].Row != nil || !page.End {
		t.Fatalf("another team's read = %+v, %v", page, err)
	}
}

// THE SCAN BUDGET ENDS THE WINDOW BEFORE THE ROW THAT WOULD PASS IT, and the
// unknown rows past it are not counted on this page; the walk covers each once.
// The 819 KiB match is padded in a field no view reads, so it is carried within
// the wire's projection budget, and the stop is the scan budget's alone.
func TestMadeInScanBytesEndTheWindowBeforeTheRow(t *testing.T) {
	s, _ := openStore(t)
	third := MadeInScanBytesMax * 2 / 5
	rawRow(t, s, "m-a0", bigDoc(third, "teamA"))
	rawRow(t, s, "m-a1", `{"title":"legacy","pad":"`+strings.Repeat("y", third)+`"}`)
	rawRow(t, s, "m-a2", `{"title":"legacy too","pad":"`+strings.Repeat("z", third)+`"}`)
	first, err := s.MadeInTeam(context.Background(), "teamA", "", MadeInTeamMax, madeInProjMax)
	if err != nil || len(first.Entries) != 2 || !first.Stopped || first.StopID != "m-a2" || first.End {
		t.Fatalf("first page = %+v, %v; want two entries, stopped at m-a2", first, err)
	}
	if first.Entries[0].Row == nil || first.Entries[0].Unknown || first.Entries[1].Row != nil || !first.Entries[1].Unknown {
		t.Fatalf("first page entries = %+v", first.Entries)
	}
	_, unknown, pages := walkMadeIn(t, s, "teamA", MadeInTeamMax)
	if unknown != 2 || pages != 2 {
		t.Fatalf("the walk counted %d unknown over %d pages, want 2 over 2", unknown, pages)
	}
}

// AN ORDINARY LARGE MEMO NEVER BLOCKS A SMALL ORDER: a watch's 300 KiB memo in
// its column, and the copy of it a status change leaves in its document, are
// neither read out nor counted against the projection budget.
func TestMadeInHugeWatchMemoDoesNotBlockATinyOrder(t *testing.T) {
	s, _ := openStore(t)
	made, err := s.Create(madeIn("watch", &OriginTeam{Known: true, ID: "teamA"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSeen(made.ID, made.Revision, "yes", strings.Repeat("path\tsize:0123456789abcdef\n", (300<<10)/27)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetStatus(made.ID, StatusPaused); err != nil {
		t.Fatal(err)
	}
	var docBytes int64
	if err := s.db.QueryRow(`SELECT octet_length(doc) FROM automations WHERE id = ?`, made.ID).Scan(&docBytes); err != nil || docBytes < 300<<10 {
		t.Fatalf("the memo was not copied into the document (%d bytes, %v); the test proves nothing", docBytes, err)
	}
	page, err := s.MadeInTeam(context.Background(), "teamA", "", 10, madeInProjMax)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].Row == nil || !page.End {
		t.Fatalf("a small order with a large memo = %+v, %v", page, err)
	}
	if row := page.Entries[0].Row; row.Memo != "" || row.Seen != "" || row.Title != "watch" || row.Status != StatusPaused {
		t.Fatalf("the row read out %d memo bytes, seen %q, title %q, status %q", len(row.Memo), row.Seen, row.Title, row.Status)
	}
}

// AN ID TOO LONG FOR A CURSOR IS NEVER COPIED OUT, ENCODED OR ECHOED: the page
// ends before its row, and a page that would start at it is refused by where
// the row is, not by its id.
func TestMadeInOversizedIDStopsBeforeItAndNeverCopiesIt(t *testing.T) {
	for _, n := range []int{MadeInIDMax + 1, 64 << 10} {
		s, _ := openStore(t)
		long := "m-b" + strings.Repeat("q", n-3)
		rawRow(t, s, "m-a0", `{"title":"small","origin":{"team":{"known":true,"id":"teamA"}}}`)
		rawRow(t, s, long, `{"title":"long","origin":{"team":{"known":true,"id":"teamA"}}}`)
		first, err := s.MadeInTeam(context.Background(), "teamA", "", MadeInTeamMax, madeInProjMax)
		if err != nil || len(first.Entries) != 1 || !first.Stopped || first.StopID != "" || first.End {
			t.Fatalf("%d-byte id: first page = %+v, %v", n, first, err)
		}
		cursor := continueAfter(t, first)
		for range 2 {
			_, err := s.MadeInTeam(context.Background(), "teamA", cursor, MadeInTeamMax, madeInProjMax)
			if !errors.Is(err, ErrMadeInTooLarge) || strings.Contains(err.Error(), "qqqq") || !strings.Contains(err.Error(), "m-a0") {
				t.Fatalf("%d-byte id: the page at it = %v; want a refusal naming its position only", n, err)
			}
		}
	}
}

// A MATCH WHOSE STATUS OR WAKE TIME CANNOT BE READ REFUSES ITS TEAM'S PAGE,
// bounded: a megabyte of status is never read out. Another team's walk covers
// the same rows, because it never reads those columns.
func TestMadeInDamagedStatusOrNextRefusesOnlyItsCrew(t *testing.T) {
	for name, damage := range map[string]string{
		"status": `UPDATE automations SET status = ? WHERE id = 'm-a0'`,
		"next":   `UPDATE automations SET next_ms = 'soon' WHERE id = 'm-a0' AND ? != ''`,
	} {
		s, _ := openStore(t)
		rawRow(t, s, "m-a0", `{"title":"small","origin":{"team":{"known":true,"id":"teamA"}}}`)
		if _, err := s.db.Exec(damage, strings.Repeat("s", 1<<20)); err != nil {
			t.Fatal(err)
		}
		_, err := s.MadeInTeam(context.Background(), "teamA", "", 10, madeInProjMax)
		if !errors.Is(err, ErrMadeInDamaged) || len(err.Error()) > 512 {
			t.Fatalf("%s: a damaged match = %v", name, err)
		}
		if page, err := s.MadeInTeam(context.Background(), "teamB", "", 10, madeInProjMax); err != nil || len(page.Entries) != 1 || !page.End {
			t.Fatalf("%s: another team's walk = %+v, %v", name, page, err)
		}
	}
}

// A MISSING OR DAMAGED WALK REVISION IS AN ERROR ON EVERY PAGE, never an empty
// page and never the end; a store opened again puts the row back, and a cursor
// from before is then stale.
func TestMadeInMissingRevisionRowIsAnError(t *testing.T) {
	for name, damage := range map[string]string{
		"missing": `DELETE FROM origin_revision`,
		"text":    `UPDATE origin_revision SET rev = 'x'`,
	} {
		s := madeInStore(t)
		cursor := firstOfTwo(t, s)
		if _, err := s.db.Exec(damage); err != nil {
			t.Fatal(err)
		}
		for _, at := range []string{"", cursor} {
			if page, err := s.MadeInTeam(context.Background(), "teamA", at, 1, madeInProjMax); !errors.Is(err, ErrMadeInDamaged) || page.End {
				t.Fatalf("%s revision, cursor %q: %+v, %v", name, at, page, err)
			}
		}
		if name != "missing" {
			continue
		}
		root := s.Root()
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		again, err := Open(root)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = again.Close() })
		if _, err := again.MadeInTeam(context.Background(), "teamA", cursor, 1, madeInProjMax); !errors.Is(err, ErrMadeInStale) {
			t.Fatalf("a cursor from before the revision was put back = %v", err)
		}
	}
}

// A CURSOR IS ONE TEAM'S WALK: another team's is refused, never read as this
// one's, and so is the unreleased m1 shape.
func TestMadeInCursorIsBoundToItsCrew(t *testing.T) {
	s := madeInStore(t)
	cursor := firstOfTwo(t, s)
	if _, err := s.MadeInTeam(context.Background(), "teamB", cursor, 1, madeInProjMax); !errors.Is(err, ErrMadeInCursor) {
		t.Fatalf("teamA's cursor on teamB = %v", err)
	}
	if MadeInIDMax != (MadeInCursorMax-len("m2.")-19-1-MadeInTeamIDMax-1)*3/4 || MadeInIDMax < 16 {
		t.Fatalf("MadeInIDMax = %d", MadeInIDMax)
	}
}

// AFTER THE LIMIT-TH MATCH NOTHING MORE IS PROJECTED: a later match too large
// to carry does not stop or refuse the page that ended before it.
func TestMadeInLimitStopsProjectingLaterHits(t *testing.T) {
	s, _ := openStore(t)
	rawRow(t, s, "m-a0", `{"title":"small","origin":{"team":{"known":true,"id":"teamA"}}}`)
	rawRow(t, s, "m-a1", briefDoc(300<<10, "teamA"))
	page, err := s.MadeInTeam(context.Background(), "teamA", "", 1, madeInProjMax)
	if err != nil || len(page.Entries) != 1 || page.Stopped || page.End {
		t.Fatalf("a page at its limit = %+v, %v", page, err)
	}
}

// UNKNOWN IS COUNTED ONLY IN THE PREFIX A PAGE COVERED: a page cut by its limit
// does not count the unknown rows after its last match.
func TestMadeInUnknownCountsOnlyTheCoveredPrefix(t *testing.T) {
	s, _ := openStore(t)
	for _, row := range []struct {
		title string
		team  *OriginTeam
	}{{"a1", &OriginTeam{Known: true, ID: "teamA"}}, {"b1", nil}, {"b2", nil}} {
		if _, err := s.Create(madeIn(row.title, row.team)); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.MadeInTeam(context.Background(), "teamA", "", 1, madeInProjMax)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].Unknown || page.End {
		t.Fatalf("a page cut at its limit = %+v, %v", page, err)
	}
	if _, unknown, _ := walkMadeIn(t, s, "teamA", 1); unknown != 2 {
		t.Fatalf("the walk counted %d unknown, want 2", unknown)
	}
}

// A CURSOR NO PAGE GAVE IS REFUSED, and so is one past the bound.
func TestMadeInRefusesACursorItDidNotGive(t *testing.T) {
	s := madeInStore(t)
	for _, cursor := range []string{
		"m-a1", "m1.0.bS1hMQ", "m1.0.teamA.bS1hMQ", "m2.x.teamA.bS1hMQ", "m2.0.teamA.", "m2.-1.teamA.bS1hMQ",
		"m2.00000000000000000000.teamA.bS1hMQ", "m2.0.teamA.bS1hMQ." + strings.Repeat("a", MadeInCursorMax),
		"m2.0.teamA." + strings.Repeat("cXFx", 11),
	} {
		if _, err := s.MadeInTeam(context.Background(), "teamA", cursor, 1, madeInProjMax); !errors.Is(err, ErrMadeInCursor) {
			t.Fatalf("cursor %q was served: %v", cursor, err)
		}
	}
	if _, err := (MadeInPage{Team: "teamA", Revision: 1}).CursorAt(strings.Repeat("i", MadeInIDMax+1)); !errors.Is(err, ErrMadeInTooLarge) {
		t.Fatalf("an id too long for a cursor was answered: %v", err)
	}
}
