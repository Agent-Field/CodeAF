package outbox

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fixedClock answers a clock standing on the named UTC day.
func fixedClock(day string) func() time.Time {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return t }
}

// stepRand answers 16 fresh bytes per read, the first byte carrying step and
// the rest a counter, so every nonce a test appends is its own.
type stepRand struct {
	step byte
	n    uint64
}

func (r *stepRand) Read(p []byte) (int, error) {
	if len(p) < 16 {
		return 0, io.ErrShortBuffer
	}
	p[0] = r.step
	binary.LittleEndian.PutUint64(p[1:9], r.n)
	copy(p[9:16], "outbox+")
	r.n++
	return 16, nil
}

// shortRand answers fewer bytes than a nonce needs.
type shortRand struct{ n int }

func (r *shortRand) Read(p []byte) (int, error) {
	if r.n == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > r.n {
		n = r.n
	}
	for i := 0; i < n; i++ {
		p[i] = 0xaa
	}
	r.n -= n
	return n, nil
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("no bytes today") }

func openOutbox(t *testing.T) (*Outbox, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	o, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { o.Close() })
	return o, path
}

func appendRow(t *testing.T, o *Outbox, payload string) {
	t.Helper()
	if err := o.Append(json.RawMessage(payload)); err != nil {
		t.Fatalf("Append(%s): %v", payload, err)
	}
}

func readAll(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func fileLines(t *testing.T, path string) []string {
	t.Helper()
	text := strings.TrimSuffix(string(readAll(t, path)), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func countMarks(t *testing.T, path, kind string) int {
	t.Helper()
	return strings.Count(string(readAll(t, path)), `{"`+kind+`":"`)
}

func pendingNonces(t *testing.T, o *Outbox) []string {
	t.Helper()
	rows := o.Pending()
	nonces := make([]string, len(rows))
	for i, r := range rows {
		nonces[i] = r.Nonce
	}
	return nonces
}

func mustEqual(t *testing.T, what string, got, want []byte) {
	t.Helper()
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: got %q, want %q", what, got, want)
	}
}

// postRecord answers every POST with 200 and records the body and the
// content-type under its own lock. A seconds field sleeps first; a status
// list answers its entries in request order before falling back to 200.
type postRecord struct {
	mu      sync.Mutex
	bodies  []string
	types   []string
	headers []*http.Header
	seconds int
	status  []int
}

func (p *postRecord) handle(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	p.mu.Lock()
	p.bodies = append(p.bodies, string(b))
	p.types = append(p.types, r.Header.Get("Content-Type"))
	h := r.Header
	p.headers = append(p.headers, &h)
	// The two knobs are read under the same lock the bodies are written
	// under: the server answers on its own goroutine, and a handler reading a
	// field the test writes is a race whether or not the requests overlap.
	wait := time.Duration(p.seconds) * time.Millisecond
	status := 200
	if len(p.status) > 0 {
		status = p.status[0]
		p.status = p.status[1:]
	}
	p.mu.Unlock()
	if wait > 0 {
		time.Sleep(wait)
	}
	w.WriteHeader(status)
}

func TestOpenRefusesAnEmptyPath(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal(`Open("") answered no error`)
	}
}

func TestAFreshRunLeavesMode0600OnTheFileAnd0700OnTheDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "nested", "outbox.jsonl")
	if _, err := Open(path); err != nil {
		t.Fatalf("Open: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("file mode is %v, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if di.Mode().Perm() != 0700 {
		t.Fatalf("directory mode is %v, want 0700", di.Mode().Perm())
	}
}

func TestOpenKeepsExistingRowsAndSkipsLinesThatAreNotRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	nonce := func(n int) string { return fmt.Sprintf("%032x", n) }
	row1 := `{"schema":1,"day":"2026-09-16","nonce":"` + nonce(1) + `","payload":{"k":1}}`
	row2 := `{"schema":1,"day":"2026-09-16","nonce":"` + nonce(2) + `","payload":{"k":2}}`
	row3 := `{"schema":1,"day":"2026-09-17","nonce":"` + nonce(3) + `","payload":{"k":3}}`
	row4 := `{"schema":1,"day":"2026-09-17","nonce":"` + nonce(4) + `","payload":{"k":4}}`
	file := strings.Join([]string{
		row1,
		"not json at all",
		"null",
		"[]",
		`{"x":1}`,
		row2,
		`{"sent":"` + nonce(1) + `"}`,
		`{"dropped":"` + nonce(3) + `"}`,
		row3, // a marker may sit before the row it retires
		row4, // unterminated last line
	}, "\n")
	if err := os.WriteFile(path, []byte(file), 0600); err != nil {
		t.Fatal(err)
	}
	o, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer o.Close()
	want := []string{nonce(2), nonce(4)}
	got := pendingNonces(t, o)
	if len(got) != len(want) {
		t.Fatalf("Pending answered %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Pending[%d] nonce is %s, want %s", i, got[i], want[i])
		}
	}
}

func TestReopenKeepsPendingRowsInTheOrderTheyWereAppended(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	o, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	o.Rand = &stepRand{step: 1}
	appendRow(t, o, `{"k":1}`)
	appendRow(t, o, `{"k":2}`)
	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	o2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer o2.Close()
	got := pendingNonces(t, o2)
	if len(got) != 2 || got[0] == got[1] {
		t.Fatalf("reopen answered %v, want the two rows in order", got)
	}
	first := got[0]
	appendRow(t, o2, `{"k":3}`)
	got = pendingNonces(t, o2)
	if len(got) != 3 || got[0] != first {
		t.Fatalf("the third append disturbed the order: %v", got)
	}
}

func TestAppendWritesExactlyTheExpectedLine(t *testing.T) {
	o, path := openOutbox(t)
	o.Now = fixedClock("2026-09-17")
	o.Rand = bytes.NewReader([]byte{0x0f, 0x1e, 0x2d, 0x3c, 0x4b, 0x5a, 0x69, 0x78, 0x87, 0x96, 0xa5, 0xb4, 0xc3, 0xd2, 0xe1, 0xf0})
	appendRow(t, o, `{"k":1}`)
	want := `{"schema":1,"day":"2026-09-17","nonce":"0f1e2d3c4b5a69788796a5b4c3d2e1f0","payload":{"k":1}}` + "\n"
	mustEqual(t, "file", readAll(t, path), []byte(want))
}

func TestAppendWritesTheUTCDayWhenTheClockSitsInAnotherZone(t *testing.T) {
	o, path := openOutbox(t)
	zone := time.FixedZone("test+0530", 5*3600+1800)
	local := time.Date(2026, 9, 18, 2, 0, 0, 0, zone) // UTC: 2026-09-17 20:30
	o.Now = func() time.Time { return local }
	o.Rand = &stepRand{step: 1}
	appendRow(t, o, `{"k":1}`)
	lines := fileLines(t, path)
	if !strings.Contains(lines[0], `"day":"2026-09-17"`) {
		t.Fatalf("row day is not the UTC day: %s", lines[0])
	}
}

func TestAppendStoresThePayloadCompactedOnOneLine(t *testing.T) {
	o, path := openOutbox(t)
	o.Rand = &stepRand{step: 2}
	appendRow(t, o, "{\n  \"k\": 1,\n  \"j\": [ 1 , 2 ]\n}")
	lines := fileLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("file holds %d lines, want 1", len(lines))
	}
	if !strings.Contains(lines[0], `"payload":{"k":1,"j":[1,2]}`) {
		t.Fatalf("payload was not stored compacted: %s", lines[0])
	}
	rows := o.Pending()
	if len(rows) != 1 || string(rows[0].Payload) != `{"k":1,"j":[1,2]}` {
		t.Fatalf("Pending answered payload %v", rows)
	}
}

func TestAppendsBelowTheCapWriteOneLineEach(t *testing.T) {
	o, path := openOutbox(t)
	o.Rand = &stepRand{step: 3}
	for i := 0; i < 10; i++ {
		appendRow(t, o, fmt.Sprintf(`{"k":%d}`, i))
	}
	if lines := fileLines(t, path); len(lines) != 10 {
		t.Fatalf("file holds %d lines, want 10 and no marker lines", len(lines))
	}
}

func TestAppendRefusesPayloadsItCannotStoreAndWritesNothing(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"empty", ``},
		{"open brace", `{`},
		{"bare word", `truthy`},
		{"two values", `{"a":1} {"b":2}`},
		{"truncated array", `[1,2`},
		{"newline inside the value", "[\"a\n\"]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o, path := openOutbox(t)
			o.Rand = &stepRand{step: 4}
			appendRow(t, o, `{"k":1}`)
			before := readAll(t, path)
			err := o.Append(json.RawMessage(c.payload))
			if err == nil {
				t.Fatalf("Append accepted %q", c.payload)
			}
			mustEqual(t, "file", readAll(t, path), before)
			if got := len(o.Pending()); got != 1 {
				t.Fatalf("Pending answers %d rows after a refused append, want 1", got)
			}
		})
	}
}

func TestAppendAppliesThePayloadCapOnceCompacted(t *testing.T) {
	o, path := openOutbox(t)
	o.Rand = &stepRand{step: 5}
	// {"k":"aaa…"} compacts to a payload of len 8 + n.
	filler := strings.Repeat("a", 65528)
	if err := o.Append(json.RawMessage(`{"k":"` + filler + `"}`)); err != nil {
		t.Fatalf("a payload of exactly 64 KiB once compacted was refused: %v", err)
	}
	if err := o.Append(json.RawMessage(`{"k":"` + filler + `a"}`)); err == nil {
		t.Fatal("a payload one byte over 64 KiB once compacted was kept")
	}
	if lines := fileLines(t, path); len(lines) != 1 {
		t.Fatalf("a refused append left %d lines, want the file untouched at 1", len(lines))
	}
}

func TestAppendRefusesWhenTheNonceCannotBeRead(t *testing.T) {
	cases := []struct {
		name string
		rand io.Reader
	}{
		{"short read", &shortRand{n: 15}},
		{"failing reader", errReader{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o, path := openOutbox(t)
			appendRow(t, o, `{"k":1}`) // crypto/rand answers this one
			before := readAll(t, path)
			o.Rand = c.rand
			if err := o.Append(json.RawMessage(`{"k":2}`)); err == nil {
				t.Fatal("Append answered no error for an unreadable nonce")
			}
			mustEqual(t, "file", readAll(t, path), before)
		})
	}
}

func TestAppendSurvivesManyGoroutinesWithoutLosingOrMixingRows(t *testing.T) {
	o, path := openOutbox(t)
	const goroutines, per = 32, 25
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				if err := o.Append(json.RawMessage(fmt.Sprintf(`{"g":%d,"i":%d}`, g, i))); err != nil {
					t.Errorf("Append: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	lines := fileLines(t, path)
	if len(lines) != goroutines*per {
		t.Fatalf("file holds %d lines, want %d", len(lines), goroutines*per)
	}
	seen := make(map[string]bool, len(lines))
	for _, line := range lines {
		var r Row
		if err := json.Unmarshal([]byte(line), &r); err != nil || r.Nonce == "" {
			t.Fatalf("line does not parse on its own: %q (%v)", line, err)
		}
		seen[r.Nonce] = true
	}
	if len(seen) != len(lines) {
		t.Fatalf("file holds %d distinct nonces across %d rows", len(seen), len(lines))
	}
	if got := len(o.Pending()); got != len(lines) {
		t.Fatalf("Pending answers %d rows, want %d", got, len(lines))
	}
}

func TestPendingAnswersNothingForAFileItCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	o, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer o.Close()
	o.Rand = &stepRand{step: 6}
	appendRow(t, o, `{"k":1}`)
	if got := len(o.Pending()); got != 1 {
		t.Fatalf("Pending answers %d rows while the file is readable, want 1", got)
	}
	// A file that is gone, and one that turned into a directory, both read as
	// no rows, and neither fails.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := len(o.Pending()); got != 0 {
		t.Fatalf("Pending answered %d rows for a missing file, want 0", got)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := len(o.Pending()); got != 0 {
		t.Fatalf("Pending answered %d rows for an unreadable file, want 0", got)
	}
}

func TestTheCapDropsPendingRowsFromTheOldEnd(t *testing.T) {
	o, path := openOutbox(t)
	o.MaxPending = 3
	o.Rand = &stepRand{step: 7}
	nonces := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		appendRow(t, o, fmt.Sprintf(`{"k":%d}`, i))
		rows := o.Pending()
		nonces = append(nonces, rows[len(rows)-1].Nonce)
	}
	got := pendingNonces(t, o)
	if len(got) != 3 || got[0] != nonces[2] || got[2] != nonces[4] {
		t.Fatalf("Pending answers the wrong end: got %v, want %v..%v", got, nonces[2], nonces[4])
	}
	if n := countMarks(t, path, "dropped"); n != 2 {
		t.Fatalf("file holds %d dropped markers, want 2", n)
	}
	// The dropped rows stay gone across a reopen.
	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	o2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer o2.Close()
	after := pendingNonces(t, o2)
	if len(after) != 3 || after[0] != got[0] {
		t.Fatalf("a reopen resurrected dropped rows: %v", after)
	}
}

func TestTheCapDefaultsTo5000AndAppliesToNegativeValues(t *testing.T) {
	if DefaultMaxPending != 5000 {
		t.Fatalf("DefaultMaxPending is %d, want 5000", DefaultMaxPending)
	}
	for _, max := range []int{0, -1} {
		t.Run(fmt.Sprintf("MaxPending=%d", max), func(t *testing.T) {
			o, path := openOutbox(t)
			o.Rand = &stepRand{step: 7}
			o.MaxPending = max
			for i := 0; i < DefaultMaxPending+1; i++ {
				if err := o.Append(json.RawMessage(fmt.Sprintf(`{"k":%d}`, i))); err != nil {
					t.Fatalf("Append: %v", err)
				}
			}
			if got := len(o.Pending()); got != DefaultMaxPending {
				t.Fatalf("Pending answers %d rows, want %d", got, DefaultMaxPending)
			}
			if n := countMarks(t, path, "dropped"); n != 1 {
				t.Fatalf("file holds %d dropped markers, want 1", n)
			}
			got := pendingNonces(t, o)
			if got[0] == got[len(got)-1] {
				t.Fatal("the cap did not drop from the old end")
			}
		})
	}
}

func TestTheRowJustAppendedSurvivesACapOfOne(t *testing.T) {
	o, _ := openOutbox(t)
	o.MaxPending = 1
	o.Rand = &stepRand{step: 7}
	appendRow(t, o, `{"k":1}`)
	appendRow(t, o, `{"k":2}`)
	got := o.Pending()
	if len(got) != 1 || string(got[0].Payload) != `{"k":2}` {
		t.Fatalf("Pending answers %v, want just the last row", got)
	}
}

func TestSendWithAnEmptyDestIsANoop(t *testing.T) {
	o, _ := openOutbox(t)
	o.Rand = &stepRand{step: 8}
	appendRow(t, o, `{"k":1}`)
	appendRow(t, o, `{"k":2}`)
	n, err := o.Send(context.Background(), "")
	if n != 0 || err != nil {
		t.Fatalf(`Send("") answered (%d, %v), want (0, nil)`, n, err)
	}
	if got := len(o.Pending()); got != 2 {
		t.Fatalf("Pending answers %d rows after an empty dest, want 2", got)
	}
}

func TestSendWithNothingPendingMakesNoRequest(t *testing.T) {
	rec := &postRecord{}
	srv := httptest.NewServer(http.HandlerFunc(rec.handle))
	defer srv.Close()
	o, _ := openOutbox(t)
	n, err := o.Send(context.Background(), srv.URL)
	if n != 0 || err != nil {
		t.Fatalf("Send answered (%d, %v), want (0, nil)", n, err)
	}
	if len(rec.bodies) != 0 {
		t.Fatalf("a Send with nothing pending made %d requests", len(rec.bodies))
	}
}

func TestSendPostsBatchesOfAtMost200Rows(t *testing.T) {
	rec := &postRecord{}
	srv := httptest.NewServer(http.HandlerFunc(rec.handle))
	defer srv.Close()
	o, path := openOutbox(t)
	o.Rand = &stepRand{step: 9}
	for i := 0; i < 450; i++ {
		appendRow(t, o, fmt.Sprintf(`{"k":%d}`, i))
	}
	rows := fileLines(t, path)[:450] // the rows, before any marker lines
	n, err := o.Send(context.Background(), srv.URL)
	if n != 450 || err != nil {
		t.Fatalf("Send answered (%d, %v), want (450, nil)", n, err)
	}
	if len(rec.bodies) != 3 {
		t.Fatalf("Send made %d posts, want 3", len(rec.bodies))
	}
	wantLines := []int{200, 200, 50}
	for i, want := range wantLines {
		body := rec.bodies[i]
		if got := strings.Count(body, "\n"); got != want {
			t.Fatalf("post %d carries %d lines, want %d", i, got, want)
		}
		if !strings.HasSuffix(body, "\n") {
			t.Fatalf("post %d does not end in a newline", i)
		}
		if rec.types[i] != "application/x-ndjson" {
			t.Fatalf("post %d content-type is %q", i, rec.types[i])
		}
		lo := i * 200
		hi := min(lo+200, 450)
		mustEqual(t, fmt.Sprintf("post %d body", i), []byte(body), []byte(strings.Join(rows[lo:hi], "\n")+"\n"))
	}
	if got := o.Pending(); len(got) != 0 {
		t.Fatalf("Pending answers %d rows after a full send, want 0", len(got))
	}
	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	o2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer o2.Close()
	if got := pendingNonces(t, o2); len(got) != 0 {
		t.Fatalf("the sent marks did not survive a reopen: %d rows pending", len(got))
	}
}

func TestSendReachesAnHTTPSDestination(t *testing.T) {
	rec := &postRecord{}
	srv := httptest.NewTLSServer(http.HandlerFunc(rec.handle))
	defer srv.Close()
	o, _ := openOutbox(t)
	o.Client = srv.Client()
	o.Rand = &stepRand{step: 10}
	appendRow(t, o, `{"k":1}`)
	dest := strings.Replace(srv.URL, "http://", "https://", 1)
	n, err := o.Send(context.Background(), dest)
	if n != 1 || err != nil {
		t.Fatalf("Send(%s) answered (%d, %v), want (1, nil)", dest, n, err)
	}
	if len(rec.bodies) != 1 {
		t.Fatal("the https post never arrived")
	}
}

func TestSendKeepsUnarrivedRowsAndNeverSendsAnArrivedRowTwice(t *testing.T) {
	seen := make(map[string]int)
	var mu sync.Mutex
	note := func(body string) {
		mu.Lock()
		defer mu.Unlock()
		for _, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
			var r Row
			if json.Unmarshal([]byte(line), &r) == nil && r.Nonce != "" {
				seen[r.Nonce]++
			}
		}
	}
	fail := &postRecord{status: []int{200, 500}}
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		note(string(b))
		fail.handle(w, r)
	}))
	defer srv1.Close()
	o, path := openOutbox(t)
	o.Rand = &stepRand{step: 11}
	for i := 0; i < 250; i++ {
		appendRow(t, o, fmt.Sprintf(`{"k":%d}`, i))
	}
	n, err := o.Send(context.Background(), srv1.URL)
	if n != 200 || err == nil {
		t.Fatalf("Send answered (%d, %v), want (200, an error)", n, err)
	}
	if cn := countMarks(t, path, "sent"); cn != 200 {
		t.Fatalf("file holds %d sent markers after the first batch arrived, want 200", cn)
	}
	remaining := pendingNonces(t, o)
	if len(remaining) != 50 {
		t.Fatalf("%d rows stayed pending, want 50", len(remaining))
	}
	ok := &postRecord{}
	srv2 := httptest.NewServer(http.HandlerFunc(ok.handle))
	defer srv2.Close()
	n, err = o.Send(context.Background(), srv2.URL)
	if n != 50 || err != nil {
		t.Fatalf("the resume answered (%d, %v), want (50, nil)", n, err)
	}
	for _, body := range ok.bodies {
		note(body)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 250 {
		t.Fatalf("%d distinct rows were sent, want 250", len(seen))
	}
	for nonce, count := range seen {
		// The 200 rows whose first batch answered 2xx were transmitted once
		// and never again; the 50 rows the 500 refused were transmitted
		// twice — once refused, once accepted — and never a third time.
		want := 1
		for _, n2 := range remaining {
			if n2 == nonce {
				want = 2
				break
			}
		}
		if count != want {
			t.Fatalf("nonce %s… was sent %d times, want %d", nonce[:8], count, want)
		}
	}
	if len(remaining) != 50 {
		t.Fatal("the pending list was consumed by the check")
	}
}

func TestSendSaysWhatFailedAndLeavesTheRowsPending(t *testing.T) {
	rec := &postRecord{status: []int{404}}
	srv := httptest.NewServer(http.HandlerFunc(rec.handle))
	defer srv.Close()
	o, _ := openOutbox(t)
	o.Rand = &stepRand{step: 12}
	appendRow(t, o, `{"k":1}`)
	n, err := o.Send(context.Background(), srv.URL)
	if n != 0 || err == nil {
		t.Fatalf("Send answered (%d, %v), want (0, an error)", n, err)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("the error does not say what failed: %v", err)
	}
	if got := len(o.Pending()); got != 1 {
		t.Fatalf("%d rows stayed pending after the refusal, want 1", got)
	}
}

func TestARefusedRowLeavesTheOutboxAndTheRestOfItsBatchSends(t *testing.T) {
	// The relay refuses a batch on the first line it cannot validate and
	// answers which line that was, so the client can retire exactly that row
	// and send the rows around it rather than hold every row behind it
	// pending on every run to come.
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		lines := strings.Count(string(b), "\n")
		mu.Unlock()
		if lines == 3 {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"line 2: role must be worker, high or mastermind"}`))
			return
		}
		w.WriteHeader(202)
	}))
	defer srv.Close()
	o, path := openOutbox(t)
	o.Rand = &stepRand{step: 14}
	appendRow(t, o, `{"k":"worker"}`)
	appendRow(t, o, `{"role":"scout"}`)
	appendRow(t, o, `{"k":"mastermind"}`)
	bad := pendingNonces(t, o)[1]
	n, err := o.Send(context.Background(), srv.URL)
	if n != 2 || err != nil {
		t.Fatalf("Send answered (%d, %v), want (2, nil)", n, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("the send made %d posts, want one per refused line", len(bodies))
	}
	if strings.Contains(bodies[1], bad) {
		t.Fatal("the retried batch carried the refused row again")
	}
	if cn := countMarks(t, path, "sent"); cn != 2 {
		t.Fatalf("file holds %d sent markers, want 2", cn)
	}
	if cn := countMarks(t, path, "dropped"); cn != 1 {
		t.Fatalf("file holds %d dropped markers, want 1", cn)
	}
	want := `{"dropped":"` + bad + `"}`
	found := false
	for _, line := range fileLines(t, path) {
		if line == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("no dropped marker names the refused row, want %s", want)
	}
	if got := o.Pending(); len(got) != 0 {
		t.Fatalf("%d rows stayed pending after the refused row was retired, want 0", len(got))
	}
	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	o2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer o2.Close()
	if got := pendingNonces(t, o2); len(got) != 0 {
		t.Fatalf("the dropped mark did not survive a reopen: %d rows pending", len(got))
	}
}

func TestAQuotaRefusalKeepsEveryRowPending(t *testing.T) {
	rec := &postRecord{status: []int{429}}
	srv := httptest.NewServer(http.HandlerFunc(rec.handle))
	defer srv.Close()
	o, path := openOutbox(t)
	o.Rand = &stepRand{step: 15}
	for i := 0; i < 3; i++ {
		appendRow(t, o, fmt.Sprintf(`{"k":%d}`, i))
	}
	n, err := o.Send(context.Background(), srv.URL)
	if n != 0 || err == nil {
		t.Fatalf("Send answered (%d, %v), want (0, an error)", n, err)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("the error does not say what failed: %v", err)
	}
	if got := len(o.Pending()); got != 3 {
		t.Fatalf("%d rows stayed pending after the quota refusal, want 3", got)
	}
	if cn := countMarks(t, path, "sent") + countMarks(t, path, "dropped"); cn != 0 {
		t.Fatalf("file holds %d markers after a quota refusal, want none", cn)
	}
}

func TestSendAnswersARefusedConnection(t *testing.T) {
	o, _ := openOutbox(t)
	o.Rand = &stepRand{step: 12}
	appendRow(t, o, `{"k":1}`)
	n, err := o.Send(context.Background(), "http://127.0.0.1:1/")
	if n != 0 || err == nil {
		t.Fatalf("Send answered (%d, %v), want (0, an error)", n, err)
	}
	if got := len(o.Pending()); got != 1 {
		t.Fatalf("%d rows stayed pending, want 1", got)
	}
}

func TestSendLivesInsideTheBudget(t *testing.T) {
	rec := &postRecord{seconds: 900}
	srv := httptest.NewServer(http.HandlerFunc(rec.handle))
	defer srv.Close()
	o, _ := openOutbox(t)
	o.Rand = &stepRand{step: 13}
	o.Budget = 150 * time.Millisecond
	for i := 0; i < 5; i++ {
		appendRow(t, o, fmt.Sprintf(`{"k":%d}`, i))
	}
	n, err := o.Send(context.Background(), srv.URL)
	if n != 0 || err == nil {
		t.Fatalf("Send answered (%d, %v), want (0, an error)", n, err)
	}
	if got := len(o.Pending()); got != 5 {
		t.Fatalf("%d rows stayed pending, want 5", got)
	}
}

func TestSendLivesInsideTheContextWhenItEndsFirst(t *testing.T) {
	rec := &postRecord{seconds: 900}
	srv := httptest.NewServer(http.HandlerFunc(rec.handle))
	defer srv.Close()
	o, _ := openOutbox(t)
	o.Rand = &stepRand{step: 14}
	o.Budget = time.Minute
	o.Rand = &stepRand{step: 14}
	appendRow(t, o, `{"k":1}`)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := o.Send(ctx, srv.URL); err == nil {
		t.Fatal("Send outlived its context")
	}
	if got := len(o.Pending()); got != 1 {
		t.Fatalf("%d rows stayed pending, want 1", got)
	}
}

func TestSendWithACanceledContextMakesNoRequest(t *testing.T) {
	rec := &postRecord{}
	srv := httptest.NewServer(http.HandlerFunc(rec.handle))
	defer srv.Close()
	o, _ := openOutbox(t)
	o.Rand = &stepRand{step: 15}
	appendRow(t, o, `{"k":1}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n, err := o.Send(ctx, srv.URL)
	if n != 0 || err == nil {
		t.Fatalf("Send answered (%d, %v), want (0, an error)", n, err)
	}
	if len(rec.bodies) != 0 {
		t.Fatal("a canceled context still made a request")
	}
}

func TestSendAppendsTheRowsToFileDestination(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "missing", "sub", "out.ndjson")
	o, path := openOutbox(t)
	o.Rand = &stepRand{step: 16}
	for i := 0; i < 3; i++ {
		appendRow(t, o, fmt.Sprintf(`{"k":%d}`, i))
	}
	rows := fileLines(t, path)
	n, err := o.Send(context.Background(), dest)
	if n != 3 || err != nil {
		t.Fatalf("Send answered (%d, %v), want (3, nil)", n, err)
	}
	sent := fileLines(t, dest)
	if len(sent) != 3 {
		t.Fatalf("destination holds %d lines, want 3", len(sent))
	}
	for i := range sent {
		if sent[i] != rows[i] {
			t.Fatalf("destination line %d is %q, want the row %q", i, sent[i], rows[i])
		}
	}
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat dest: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("destination mode is %v, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(dest))
	if err != nil {
		t.Fatalf("stat dest dir: %v", err)
	}
	if di.Mode().Perm() != 0700 {
		t.Fatalf("destination directory mode is %v, want 0700", di.Mode().Perm())
	}
	if got := len(o.Pending()); got != 0 {
		t.Fatalf("%d rows stayed pending after a file send, want 0", got)
	}
	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	o2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer o2.Close()
	if got := pendingNonces(t, o2); len(got) != 0 {
		t.Fatalf("a reopen still answers %d rows, want none", len(got))
	}
}

func TestSendAcceptsTheFileScheme(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out.ndjson")
	o, _ := openOutbox(t)
	o.Rand = &stepRand{step: 17}
	appendRow(t, o, `{"k":1}`)
	n, err := o.Send(context.Background(), "file://"+dest)
	if n != 1 || err != nil {
		t.Fatalf("Send answered (%d, %v), want (1, nil)", n, err)
	}
	if lines := fileLines(t, dest); len(lines) != 1 {
		t.Fatalf("destination holds %d lines, want 1", len(lines))
	}
}

func TestSendAppendsToFileAcrossSends(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out.ndjson")
	o, _ := openOutbox(t)
	o.Rand = &stepRand{step: 17}
	appendRow(t, o, `{"k":1}`)
	appendRow(t, o, `{"k":2}`)
	if n, err := o.Send(context.Background(), dest); n != 2 || err != nil {
		t.Fatalf("first send answered (%d, %v)", n, err)
	}
	appendRow(t, o, `{"k":3}`)
	if n, err := o.Send(context.Background(), dest); n != 1 || err != nil {
		t.Fatalf("second send answered (%d, %v)", n, err)
	}
	if lines := fileLines(t, dest); len(lines) != 3 || !strings.Contains(lines[2], `"k":3`) {
		t.Fatalf("destination holds %v", lines)
	}
}

func TestKeepLeavesDeclinedRowsUnsentAndGone(t *testing.T) {
	rec := &postRecord{}
	srv := httptest.NewServer(http.HandlerFunc(rec.handle))
	defer srv.Close()
	o, path := openOutbox(t)
	o.Rand = &stepRand{step: 18}
	asked := 0
	o.Keep = func(payload json.RawMessage) bool {
		asked++
		s := string(payload)
		return !strings.Contains(s, `"k":2`) && !strings.Contains(s, `"k":4`)
	}
	for i := 1; i <= 4; i++ {
		appendRow(t, o, fmt.Sprintf(`{"k":%d}`, i))
	}
	n, err := o.Send(context.Background(), srv.URL)
	if n != 2 || err != nil {
		t.Fatalf("Send answered (%d, %v), want (2, nil)", n, err)
	}
	if asked != 4 {
		t.Fatalf("Keep was asked %d times, want 4", asked)
	}
	body := strings.Join(rec.bodies, "")
	if strings.Contains(body, `"k":2`) || strings.Contains(body, `"k":4`) {
		t.Fatal("a declined payload reached the destination")
	}
	if got := len(o.Pending()); got != 0 {
		t.Fatalf("%d rows stayed pending, want 0 — a declined row leaves the outbox", got)
	}
	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	o2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer o2.Close()
	if got := pendingNonces(t, o2); len(got) != 0 {
		t.Fatalf("a declined row came back after a reopen: %d rows", len(got))
	}
}

func TestKeepDecliningEverythingSendsNothing(t *testing.T) {
	rec := &postRecord{}
	srv := httptest.NewServer(http.HandlerFunc(rec.handle))
	defer srv.Close()
	o, _ := openOutbox(t)
	o.Rand = &stepRand{step: 19}
	o.Keep = func(json.RawMessage) bool { return false }
	appendRow(t, o, `{"k":1}`)
	n, err := o.Send(context.Background(), srv.URL)
	if n != 0 || err != nil {
		t.Fatalf("Send answered (%d, %v), want (0, nil)", n, err)
	}
	if len(rec.bodies) != 0 {
		t.Fatal("a Send that declines everything still made a request")
	}
	if got := len(o.Pending()); got != 0 {
		t.Fatalf("%d rows stayed pending, want 0", got)
	}
}

func TestRowsAppendedAfterASendArePendingLikeAnyOther(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out.ndjson")
	o, _ := openOutbox(t)
	o.Rand = &stepRand{step: 19}
	appendRow(t, o, `{"k":1}`)
	if n, err := o.Send(context.Background(), dest); n != 1 || err != nil {
		t.Fatalf("first send answered (%d, %v)", n, err)
	}
	appendRow(t, o, `{"k":2}`)
	got := o.Pending()
	if len(got) != 1 || string(got[0].Payload) != `{"k":2}` {
		t.Fatalf("Pending answers %v, want the fresh row", got)
	}
}

func TestSendRefusesNonsenseDestinationsWithoutPanicking(t *testing.T) {
	cases := []string{
		"http://",
		"https://",
		"http://[::1",
		"file://",
		"\x00",
		"file://\x00",
	}
	for _, dest := range cases {
		t.Run(fmt.Sprintf("%q", dest), func(t *testing.T) {
			o, _ := openOutbox(t)
			o.Rand = &stepRand{step: 20}
			appendRow(t, o, `{"k":1}`)
			n, err := o.Send(context.Background(), dest)
			if err == nil || n != 0 {
				t.Fatalf("Send(%q) answered (%d, %v), want (0, an error)", dest, n, err)
			}
			if got := len(o.Pending()); got != 1 {
				t.Fatalf("%d rows stayed pending after the refusal, want 1", got)
			}
		})
	}
}

func TestCloseIsIdempotentAndAClosedOutboxRefusesWork(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out.ndjson")
	o, _ := openOutbox(t)
	o.Rand = &stepRand{step: 21}
	appendRow(t, o, `{"k":1}`)
	appendRow(t, o, `{"k":2}`)
	if err := o.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := o.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := o.Append(json.RawMessage(`{"k":3}`)); err == nil {
		t.Fatal("a closed outbox accepted an append")
	}
	if n, err := o.Send(context.Background(), dest); err == nil || n != 0 {
		t.Fatalf("a closed outbox sent (%d, %v)", n, err)
	}
	if got := len(o.Pending()); got != 2 {
		t.Fatalf("Pending answers %d rows after Close, want 2", got)
	}
}

func TestFieldsSetAfterOpenAreUsed(t *testing.T) {
	o, path := openOutbox(t)
	o.Now = fixedClock("2026-09-17")
	o.Rand = &stepRand{step: 21}
	o.MaxPending = 2
	for i := 0; i < 4; i++ {
		appendRow(t, o, fmt.Sprintf(`{"k":%d}`, i))
	}
	got := o.Pending()
	if len(got) != 2 {
		t.Fatalf("the cap set after Open was ignored: %d rows pending", len(got))
	}
	lines := fileLines(t, path)
	if !strings.Contains(lines[0], `"day":"2026-09-17"`) {
		t.Fatalf("the clock set after Open was ignored: %s", lines[0])
	}
	if !strings.Contains(lines[0], `"nonce":"15`) { // stepRand answers the step as its first byte
		t.Fatalf("the nonce reader set after Open was ignored: %s", lines[0])
	}
}

func TestAppendWithTheDefaultClockAndRand(t *testing.T) {
	o, _ := openOutbox(t)
	before := time.Now().UTC().Format("2006-01-02")
	for i := 0; i < 20; i++ {
		appendRow(t, o, fmt.Sprintf(`{"k":%d}`, i))
	}
	after := time.Now().UTC().Format("2006-01-02")
	for i, r := range o.Pending() {
		if len(r.Nonce) != 32 {
			t.Fatalf("row %d nonce is %d characters, want 32", i, len(r.Nonce))
		}
		if _, err := hex.DecodeString(r.Nonce); err != nil {
			t.Fatalf("row %d nonce is not lowercase hex: %v", i, err)
		}
		if r.Nonce != strings.ToLower(r.Nonce) {
			t.Fatalf("row %d nonce is not lowercase", i)
		}
		if r.Day != before && r.Day != after {
			t.Fatalf("row %d day is %s, want the UTC day (%s or %s)", i, r.Day, before, after)
		}
		if r.Schema != 1 {
			t.Fatalf("row %d schema is %d, want 1", i, r.Schema)
		}
	}
}

// A nonce is what the caller's own Rand made of sixteen bytes, so a source
// that answers with the same bytes twice writes the same nonce twice. Each
// marker still retires exactly one row.
func TestRowsSharingANonceAreRetiredOneMarkerAtATime(t *testing.T) {
	o, _ := openOutbox(t)
	o.Rand = bytes.NewReader(bytes.Repeat([]byte{3}, 4096))
	o.MaxPending = 3
	for i := 0; i < 5; i++ {
		appendRow(t, o, fmt.Sprintf(`{"i":%d}`, i))
	}
	pending := o.Pending()
	if len(pending) != 3 {
		t.Fatalf("pending = %d rows, want the three the cap leaves", len(pending))
	}
	for i, r := range pending {
		if want := fmt.Sprintf(`{"i":%d}`, i+2); string(r.Payload) != want {
			t.Fatalf("pending[%d] = %s, want %s: the cap drops from the old end", i, r.Payload, want)
		}
	}
}

// A partial send over a shared nonce leaves the rows it never reached, rather
// than retiring them with the batch that did arrive.
func TestAPartialSendOverASharedNonceKeepsTheRowsItDidNotReach(t *testing.T) {
	post := &postRecord{}
	srv := httptest.NewServer(http.HandlerFunc(post.handle))
	defer srv.Close()

	o, _ := openOutbox(t)
	o.Rand = bytes.NewReader(bytes.Repeat([]byte{5}, 4096))
	o.Client = srv.Client()
	for i := 0; i < 250; i++ {
		appendRow(t, o, fmt.Sprintf(`{"i":%d}`, i))
	}
	post.status = []int{200, 500}

	n, err := o.Send(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("a refused second batch returned no error")
	}
	if n != 200 {
		t.Fatalf("Send = %d, want the 200 rows the first batch carried", n)
	}
	if got := len(o.Pending()); got != 50 {
		t.Fatalf("pending = %d, want the 50 rows the refused batch never delivered", got)
	}
}

// A marker that could not be written leaves its rows pending: a row gone from
// the pending set and present in the file is a row that comes back at the next
// Open having been reported delivered.
func TestASendWhoseMarkerCannotBeWrittenKeepsTheRowsPending(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	o, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer o.Close()
	appendRow(t, o, `{"i":0}`)

	sink := filepath.Join(dir, "sink.jsonl")
	// The handle goes away between the delivery and the marker, which is what
	// a Close racing an in-flight Send does.
	o.Keep = func(json.RawMessage) bool {
		o.Close()
		return true
	}
	if _, err := o.Send(context.Background(), sink); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := len(o.Pending()); got != 1 {
		t.Fatalf("pending = %d, want the row whose marker never reached the file", got)
	}
}

// seedRetiredRows writes an outbox file holding retired rows each followed by
// its sent marker, two pending rows and a torn last line, and answers the two
// pending row lines in file order. The count sits well above the floor at
// which a Send rewrites the file, so the trigger is certain.
func seedRetiredRows(t *testing.T, path string, retired int) (string, string) {
	t.Helper()
	nonce := func(n int) string { return fmt.Sprintf("%032x", n) }
	var lines []string
	for i := 0; i < retired; i++ {
		lines = append(lines,
			`{"schema":1,"day":"2026-09-16","nonce":"`+nonce(i)+`","payload":{"k":`+fmt.Sprintf("%d", i)+`}}`,
			`{"sent":"`+nonce(i)+`"}`)
	}
	const base = 1 << 20
	keep1 := `{"schema":1,"day":"2026-09-17","nonce":"` + nonce(base) + `","payload":{"k":"keep one"}}`
	keep2 := `{"schema":1,"day":"2026-09-17","nonce":"` + nonce(base+1) + `","payload":{"k":"keep two"}}`
	torn := `{"schema":1,"day":"2026-09-17","nonce":"` + nonce(base+2) + `","payload":{"k":"torn"`
	lines = append(lines, keep1, keep2, torn)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0600); err != nil {
		t.Fatal(err)
	}
	return keep1, keep2
}

// A file that has grown mostly into rows a marker retired is rewritten by a
// Send down to the rows still pending: the markers and the retired rows are
// gone, a torn last line is dropped rather than kept, and Pending still
// answers the rows in order.
func TestSendCompactsAFileGrownMostlyIntoRetiredRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	keep1, keep2 := seedRetiredRows(t, path, 1500)
	o, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer o.Close()

	// A destination that refuses reaches the seam — a Send compacts before its
	// first POST — and leaves every row pending for this test to read back.
	if _, err := o.Send(context.Background(), "http://127.0.0.1:1/"); err == nil {
		t.Fatal("a Send to a refused destination answered no error")
	}

	got := fileLines(t, path)
	want := []string{keep1, keep2}
	if len(got) != len(want) {
		t.Fatalf("the compacted file holds %d lines, want the %d pending rows: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d is %q, want the pending row %q", i, got[i], want[i])
		}
	}
	rows := o.Pending()
	if len(rows) != 2 || string(rows[0].Payload) != `{"k":"keep one"}` || string(rows[1].Payload) != `{"k":"keep two"}` {
		t.Fatalf("Pending answered %v, want the two rows in order", rows)
	}
}

// Rows appended after a compaction are read back across a reopen, exactly as
// rows appended to a file that was never compacted are.
func TestRowsAppendedAfterACompactionSurviveAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	seedRetiredRows(t, path, 1500)
	o, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	o.Rand = &stepRand{step: 40}
	if _, err := o.Send(context.Background(), "http://127.0.0.1:1/"); err == nil {
		t.Fatal("a Send to a refused destination answered no error")
	}
	if got := len(fileLines(t, path)); got != 2 {
		t.Fatalf("the file holds %d lines after the compaction, want 2", got)
	}
	appendRow(t, o, `{"k":"after"}`)
	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	o2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer o2.Close()
	rows := o2.Pending()
	if len(rows) != 3 || string(rows[2].Payload) != `{"k":"after"}` {
		t.Fatalf("a reopen after a compaction answered %v, want the two rows and the fresh one", rows)
	}
}

// A batch sent over http carries the outbox's install nonce as
// X-Codeaf-Install, one header per post; an outbox with none set sends no
// header, and a file destination is never asked for one.
func TestSendCarriesTheInstallNonceAsAHeader(t *testing.T) {
	var rec postRecord
	srv := httptest.NewServer(http.HandlerFunc(rec.handle))
	defer srv.Close()

	o, _ := openOutbox(t)
	o.Rand = &stepRand{step: 11}
	o.Install = "0123456789abcdef0123456789abcdef"
	appendRow(t, o, `{"k":1}`)
	n, err := o.Send(context.Background(), srv.URL)
	if n != 1 || err != nil {
		t.Fatalf("Send answered (%d, %v), want (1, nil)", n, err)
	}
	if got := rec.headers[0].Get("X-Codeaf-Install"); got != o.Install {
		t.Fatalf("the batch rode with X-Codeaf-Install %q, want the outbox's nonce %q", got, o.Install)
	}

	rec = postRecord{}
	srv2 := httptest.NewServer(http.HandlerFunc(rec.handle))
	defer srv2.Close()
	o2, _ := openOutbox(t)
	o2.Rand = &stepRand{step: 12}
	appendRow(t, o2, `{"k":1}`)
	if _, err := o2.Send(context.Background(), srv2.URL); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := rec.headers[0].Get("X-Codeaf-Install"); got != "" {
		t.Fatalf("an outbox with no nonce sent the header anyway: %q", got)
	}
}
