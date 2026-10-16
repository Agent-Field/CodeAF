package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ledgerAt points the app at a ledger of these lines, in this zone, for one
// test.
func ledgerAt(t *testing.T, zone *time.Location, lines []session.UsageLine, extra string) string {
	t.Helper()
	var b strings.Builder
	for _, line := range lines {
		data, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	b.WriteString(extra)
	path := filepath.Join(t.TempDir(), session.UsageLedgerName)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	oldPath, oldZone := appLedgerPath, appLedgerZone
	appLedgerPath = func() string { return path }
	appLedgerZone = func() *time.Location { return zone }
	t.Cleanup(func() { appLedgerPath, appLedgerZone = oldPath, oldZone })
	return path
}

// appLedgerFixture is twelve thousand calls across two local days: 40 runs
// that each use two models, and 8 000 calls with no run, a third of them
// unpriced, with a workspace whose name needs quoting.
func appLedgerFixture(zone *time.Location) []session.UsageLine {
	base := time.Date(2026, 10, 14, 6, 0, 0, 0, zone)
	var lines []session.UsageLine
	for r := 0; r < 40; r++ {
		for c := 0; c < 100; c++ {
			at := base.Add(time.Duration(r)*30*time.Minute + time.Duration(c)*time.Second)
			lines = append(lines, session.UsageLine{
				Run: fmt.Sprintf("run-%02d", r), At: at, Day: at.Format("2006-01-02"),
				Model: []string{"m/one", "m/two"}[c%2], Calls: 1, Input: 4, Output: 2, USD: 1.0 / 256,
				Session: "conv0000000000001", Workspace: "/work/a \"quoted\",\nfolder",
			})
		}
	}
	for i := 0; i < 8000; i++ {
		at := base.Add(time.Duration(i) * 15 * time.Second)
		line := session.UsageLine{At: at, Day: at.Format("2006-01-02"), Model: "m/aux", Calls: 1, Input: 1, Output: 1, Session: "conv0000000000002", Role: "title"}
		if i%3 != 0 {
			line.USD = 1.0 / 512
		} else {
			line.Unbilled = true
		}
		lines = append(lines, line)
	}
	return lines
}

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return records
}

// THE PREVIEW AND THE FILE ARE ONE QUESTION: the same filter pages through the
// same rows the export writes, and both say the same totals — runs one row
// each, every unattributed call its own row, the unpriced counted not free.
func TestAppSpendLedgerPreviewAndExportAgree(t *testing.T) {
	zone := time.FixedZone("EDT", -4*3600)
	ledgerAt(t, zone, appLedgerFixture(zone), "")
	b := &appBackend{}
	for _, filter := range []appwire.SpendLedgerFilter{
		{From: "2026-10-14", To: "2026-10-15"},
		{From: "2026-10-14", To: "2026-10-14", Model: "m/two"},
		{From: "2026-10-14", To: "2026-10-15", Kind: appwire.SpendKindAgent, Subject: "conv0000000000002"},
	} {
		var keys []string
		var totals appwire.SpendLedgerTotals
		cursor := ""
		for {
			page, err := b.SpendLedger(appwire.SpendLedgerAsk{Filter: filter, Limit: 500, Cursor: cursor})
			if err != nil {
				t.Fatal(err)
			}
			totals = page.Totals
			for _, row := range page.Rows {
				keys = append(keys, row.Key)
				if row.Grain == appwire.SpendGrainRun && (row.Records != 100 && filter.Model == "" || len(row.Models) == 0) {
					t.Fatalf("run row %+v", row)
				}
				if _, offset := row.Time.Zone(); offset != -4*3600 {
					t.Fatalf("a row's time is not in the asker's zone: %v", row.Time)
				}
			}
			if page.Next == "" {
				break
			}
			cursor = page.Next
		}
		dest := filepath.Join(t.TempDir(), "spend.csv")
		out, err := b.ExportSpendLedger(appwire.SpendLedgerExportAsk{Filter: filter, Destination: dest})
		if err != nil {
			t.Fatal(err)
		}
		records := readCSV(t, dest)
		if out.Rows != len(records)-1 || out.Rows != len(keys) || out.Rows != totals.Rows {
			t.Fatalf("%+v: export %d rows (%d records), pages %d, totals %d", filter, out.Rows, len(records)-1, len(keys), totals.Rows)
		}
		if a, _ := json.Marshal(out.Totals); string(a) != string(ledgerJSON(json.Marshal(totals))) {
			t.Fatalf("%+v: export totals %+v, page totals %+v", filter, out.Totals, totals)
		}
		info, _ := os.Stat(dest)
		if info.Mode().Perm() != 0o600 || info.Size() != out.Bytes {
			t.Fatalf("mode %v size %d answered %d", info.Mode(), info.Size(), out.Bytes)
		}
		col := map[string]int{}
		for i, name := range records[0] {
			col[name] = i
		}
		exported := map[string]bool{}
		for _, record := range records[1:] {
			exported[record[col["key"]]] = true
			if record[col["grain"]] == "call" && record[col["unpriced_records"]] == "1" && record[col["cost_usd"]] != "" {
				t.Fatalf("an unpriced call has a cost: %q", record)
			}
			if record[col["grain"]] == "run" && record[col["workspace"]] != "/work/a \"quoted\",\nfolder" {
				t.Fatalf("a quoted workspace came back as %q", record[col["workspace"]])
			}
			if _, err := time.Parse(time.RFC3339, record[col["time"]]); err != nil || !strings.HasSuffix(record[col["time"]], "-04:00") {
				t.Fatalf("time %q", record[col["time"]])
			}
		}
		for _, key := range keys {
			if !exported[key] {
				t.Fatalf("%+v: %s is in the preview and not the file", filter, key)
			}
		}
	}
	all, err := b.SpendLedger(appwire.SpendLedgerAsk{Filter: appwire.SpendLedgerFilter{From: "2026-10-14", To: "2026-10-15"}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if tot := all.Totals; tot.Runs != 40 || tot.UnknownRunRecords != 8000 || tot.Records != 12000 || tot.UnpricedRecords != 2667 || tot.UnbilledRecords != 2667 ||
		tot.RunCostUSD == nil || *tot.RunCostUSD != 4000.0/256 || tot.UnknownRunCostUSD == nil || *tot.UnknownRunCostUSD != 5333.0/512 {
		t.Fatalf("totals %+v", tot)
	}
}

// THE FILE IS WHOLE OR NOT THERE: an existing file is kept unless the caller
// said overwrite, a reading that fails leaves the destination's bytes as they
// were with no temporary file beside them, and an overwrite replaces it whole.
func TestAppSpendLedgerExportIsAtomic(t *testing.T) {
	zone := time.UTC
	ledger := ledgerAt(t, zone, appLedgerFixture(zone)[:500], "")
	b := &appBackend{}
	dir := t.TempDir()
	dest := filepath.Join(dir, "spend.csv")
	if err := os.WriteFile(dest, []byte("the person's own file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ask := appwire.SpendLedgerExportAsk{Filter: appwire.SpendLedgerFilter{From: "2026-10-14", To: "2026-10-14"}, Destination: dest}
	_, err := b.ExportSpendLedger(ask)
	if wireErrorCode(err) != appwire.CodeBadParams || !strings.Contains(err.Error(), "overwrite") {
		t.Fatalf("an existing file without overwrite: %v", err)
	}
	keptIntact(t, dir, dest, "the person's own file\n")

	// A ledger that cannot be read — a folder where the file should be.
	appLedgerPath = func() string { return dir }
	ask.Overwrite = true
	if _, err := b.ExportSpendLedger(ask); err == nil {
		t.Fatal("an unreadable ledger exported")
	}
	keptIntact(t, dir, dest, "the person's own file\n")

	// Too many readers at once.
	appLedgerPath = func() string { return ledger }
	slots := fillScanSlots(t)
	_, err = b.ExportSpendLedger(ask)
	slots()
	if wireErrorCode(err) != appwire.CodeBusy {
		t.Fatalf("busy: %v", err)
	}
	keptIntact(t, dir, dest, "the person's own file\n")

	out, err := b.ExportSpendLedger(ask)
	if err != nil || out.Rows == 0 {
		t.Fatalf("%v %+v", err, out)
	}
	records := readCSV(t, dest)
	if len(records) != out.Rows+1 || records[0][0] != "time" {
		t.Fatalf("the replaced file holds %d records", len(records))
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("the folder holds %d entries after an export", len(entries))
	}

	for _, bad := range []string{filepath.Join(dir, "missing", "spend.csv"), dir} {
		_, err := b.ExportSpendLedger(appwire.SpendLedgerExportAsk{Filter: ask.Filter, Destination: bad, Overwrite: true})
		if wireErrorCode(err) != appwire.CodeBadParams {
			t.Fatalf("%s: %v", bad, err)
		}
	}
}

// fillScanSlots takes every ledger scan slot by running scans that block in
// their callback, and answers the function that lets them go.
func fillScanSlots(t *testing.T) func() {
	t.Helper()
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			_, _, _ = session.EachUsageLedgerEntry(appLedgerPath(), session.UsageQuery{}, session.DefaultUsageLedgerBudget, func(session.UsageEntry) error {
				select {
				case started <- struct{}{}:
				default:
				}
				<-release
				return errors.New("released")
			})
			done <- struct{}{}
		}()
	}
	<-started
	<-started
	return func() {
		close(release)
		<-done
		<-done
	}
}

func keptIntact(t *testing.T, dir, dest, want string) {
	t.Helper()
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != want {
		t.Fatalf("the destination changed: %q %v", got, err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != filepath.Base(dest) {
			t.Fatalf("%s was left beside the destination", e.Name())
		}
	}
}

func wireErrorCode(err error) string {
	var wire *appwire.Error
	if errors.As(err, &wire) {
		return wire.Code
	}
	return ""
}

// WHAT THE ASKER CAN FIX IS bad_params: a cursor from another question, and a
// range holding more runs than one answer may hold.
func TestAppSpendLedgerRefusals(t *testing.T) {
	ledgerAt(t, time.UTC, appLedgerFixture(time.UTC), "")
	b := &appBackend{}
	page, err := b.SpendLedger(appwire.SpendLedgerAsk{Filter: appwire.SpendLedgerFilter{From: "2026-10-14", To: "2026-10-15"}, Limit: 5})
	if err != nil || page.Next == "" {
		t.Fatalf("%v %q", err, page.Next)
	}
	_, err = b.SpendLedger(appwire.SpendLedgerAsk{Filter: appwire.SpendLedgerFilter{From: "2026-10-14", To: "2026-10-14"}, Limit: 5, Cursor: page.Next})
	if wireErrorCode(err) != appwire.CodeBadParams {
		t.Fatalf("another range's cursor: %v", err)
	}
	for _, err := range []error{session.ErrUsageLedgerTooLarge, session.ErrUsageLedgerChanged, session.ErrUsageLedgerFigures} {
		if wireErrorCode(appLedgerError(err)) != appwire.CodeBadParams {
			t.Fatalf("%v is not the asker's to fix", err)
		}
	}
}

// A NAME IS JOINED, NEVER GUESSED: a conversation's title is the held
// agent's, a task's needs the conversation it ran in and that conversation
// held open, and a thing nothing holds a title for has none.
func TestAppSpendLedgerNamesAreJoinedNotInferred(t *testing.T) {
	b := &appBackend{agents: map[string]*appAgent{
		"c1": {view: appwire.Agent{ID: "c1", Title: "Release notes"}, live: &appLive{tasks: map[string]appwire.Task{appTaskID("c1", 7): {Title: "Fix the build"}}}},
		"c2": {view: appwire.Agent{ID: "c2", Title: "Closed one"}},
	}}
	names := b.ledgerNamer()
	names.automation = func(id string) string { return map[string]string{"nightly": "Nightly triage"}[id] }
	for _, c := range []struct {
		entry session.UsageEntry
		want  string
	}{
		{session.UsageEntry{Kind: session.SubjectConversation, Subject: "c1"}, "Release notes"},
		{session.UsageEntry{Kind: session.SubjectConversation, Subject: "c9"}, ""},
		{session.UsageEntry{Kind: session.SubjectTask, Subject: "7", Root: "c1"}, "Fix the build"},
		{session.UsageEntry{Kind: session.SubjectTask, Subject: "7"}, ""},
		{session.UsageEntry{Kind: session.SubjectTask, Subject: "7", Root: "c2"}, ""},
		{session.UsageEntry{Kind: session.SubjectTask, Subject: "x", Root: "c1"}, ""},
		{session.UsageEntry{Kind: session.SubjectAutomation, Subject: "nightly"}, "Nightly triage"},
		{session.UsageEntry{Subject: "c1"}, ""},
	} {
		if got := names.of(c.entry); got != c.want {
			t.Fatalf("%+v named %q, want %q", c.entry, got, c.want)
		}
	}
}

// NAMING IS PAID PER ROW ANSWERED, NEVER PER THING THE MACHINE HOLDS: a page
// of one asks one name, an export asks each automation once until the cache
// is full and never keeps more than it, and nothing else is read.
func TestAppSpendLedgerNamesAreBoundedLookups(t *testing.T) {
	zone := time.UTC
	at := time.Date(2026, 10, 14, 6, 0, 0, 0, zone)
	var lines []session.UsageLine
	for i := 0; i < 3*appLedgerNameCache; i++ {
		for range 2 {
			lines = append(lines, session.UsageLine{At: at.Add(time.Duration(i) * time.Second), Automation: fmt.Sprintf("auto%04d", i), USD: 1.0 / 1024})
		}
	}
	ledgerAt(t, zone, lines, "")
	b := &appBackend{}
	asked := map[string]int{}
	namer := b.ledgerNamer()
	namer.automation = func(id string) string { asked[id]++; return "title " + id }
	filter := appwire.SpendLedgerFilter{From: "2026-10-14", To: "2026-10-14"}
	query, err := appLedgerQuery(filter)
	if err != nil {
		t.Fatal(err)
	}
	page, err := session.QueryUsageLedger(appLedgerPath(), query, "", 1, session.DefaultUsageLedgerBudget)
	if err != nil {
		t.Fatal(err)
	}
	if row := appLedgerRow(page.Entries[0], namer, zone); row.Name != "title "+row.Subject || len(asked) != 1 {
		t.Fatalf("a page of one asked %d names: %+v", len(asked), row)
	}
	clear(asked)
	namer = b.ledgerNamer()
	namer.automation = func(id string) string { asked[id]++; return "title " + id }
	rows := 0
	if _, _, err := session.EachUsageLedgerEntry(appLedgerPath(), query, session.DefaultUsageLedgerBudget, func(e session.UsageEntry) error {
		rows++
		if appLedgerRow(e, namer, zone).Name != "title "+e.Subject {
			t.Fatalf("row %s unnamed", e.Key)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(namer.automations) != appLedgerNameCache || rows != len(lines) {
		t.Fatalf("the cache holds %d after %d rows", len(namer.automations), rows)
	}
	for id, n := range asked {
		cached := id < fmt.Sprintf("auto%04d", appLedgerNameCache)
		if cached && n != 1 || !cached && n != 2 {
			t.Fatalf("%s asked %d times", id, n)
		}
	}
}
func ledgerJSON(data []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return data
}

// A FILE CREATED AT THE DESTINATION WHILE THE EXPORT WAS WRITING SURVIVES IT,
// on a folder that takes links and on one that does not: the first refuses as
// "already exists", the second refuses outright rather than look and rename,
// and neither leaves its temporary file behind.
func TestAppSpendLedgerExportNeverReplacesAConcurrentFile(t *testing.T) {
	ledgerAt(t, time.UTC, appLedgerFixture(time.UTC)[:200], "")
	b := &appBackend{}
	filter := appwire.SpendLedgerFilter{From: "2026-10-14", To: "2026-10-14"}
	oldLink := appLinkFile
	t.Cleanup(func() { appLinkFile = oldLink })
	for name, link := range map[string]func(string, string) error{
		"links":    os.Link,
		"no links": func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} },
	} {
		dir := t.TempDir()
		dest := filepath.Join(dir, "spend.csv")
		appLinkFile = func(from, to string) error {
			if err := os.WriteFile(to, []byte("theirs\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return link(from, to)
		}
		_, err := b.ExportSpendLedger(appwire.SpendLedgerExportAsk{Filter: filter, Destination: dest})
		if wireErrorCode(err) != appwire.CodeBadParams {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "no links" && !strings.Contains(err.Error(), "choose another folder") {
			t.Fatalf("%s: the refusal does not say what to do: %v", name, err)
		}
		keptIntact(t, dir, dest, "theirs\n")
	}

	// On a folder without links and nothing at the destination, it is still
	// refused, and nothing is there after.
	dir := t.TempDir()
	dest := filepath.Join(dir, "spend.csv")
	appLinkFile = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.EXDEV} }
	if _, err := b.ExportSpendLedger(appwire.SpendLedgerExportAsk{Filter: filter, Destination: dest}); wireErrorCode(err) != appwire.CodeBadParams {
		t.Fatalf("no links, nothing there: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a refused export left %d entries", len(entries))
	}
}

// THE TEMPORARY FILE IS REMOVED AFTER A LINK PLACES THE EXPORT, and a removal
// that fails does not undo the export it followed.
func TestAppSpendLedgerExportRemovesItsTemporaryName(t *testing.T) {
	ledgerAt(t, time.UTC, appLedgerFixture(time.UTC)[:200], "")
	b := &appBackend{}
	oldRemove := appRemoveFile
	t.Cleanup(func() { appRemoveFile = oldRemove })
	var removed []string
	appRemoveFile = func(path string) error {
		removed = append(removed, path)
		os.Remove(path)
		return errors.New("refused")
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "spend.csv")
	out, err := b.ExportSpendLedger(appwire.SpendLedgerExportAsk{Filter: appwire.SpendLedgerFilter{From: "2026-10-14", To: "2026-10-14"}, Destination: dest})
	if err != nil || out.Rows == 0 {
		t.Fatalf("%v %+v", err, out)
	}
	if len(removed) != 1 || !strings.HasPrefix(filepath.Base(removed[0]), ".codeaf-ledger-") {
		t.Fatalf("removed %q", removed)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("the folder holds %d entries", len(entries))
	}
}

// A RANGE WHOSE FIGURES CANNOT BE ADDED UP IS REFUSED BY BOTH DOORS, by name,
// and the export's destination is left exactly as it was.
func TestAppSpendLedgerRefusesFiguresItCannotAdd(t *testing.T) {
	at := time.Date(2026, 10, 14, 6, 0, 0, 0, time.UTC)
	ledgerAt(t, time.UTC, []session.UsageLine{{At: at, USD: math.MaxFloat64}, {At: at, Run: "r", USD: math.MaxFloat64}}, "")
	b := &appBackend{}
	filter := appwire.SpendLedgerFilter{From: "2026-10-14", To: "2026-10-14"}
	if _, err := b.SpendLedger(appwire.SpendLedgerAsk{Filter: filter, Limit: 10}); wireErrorCode(err) != appwire.CodeBadParams || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("page: %v", err)
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "spend.csv")
	if err := os.WriteFile(dest, []byte("kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ExportSpendLedger(appwire.SpendLedgerExportAsk{Filter: filter, Destination: dest, Overwrite: true}); wireErrorCode(err) != appwire.CodeBadParams {
		t.Fatalf("export: %v", err)
	}
	keptIntact(t, dir, dest, "kept\n")
}
