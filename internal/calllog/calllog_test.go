package calllog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fresh points the package's one log at a temporary file and puts it back
// afterwards, so tests in this file cannot leak into each other or into the
// developer's own state root.
func fresh(t *testing.T, path string) {
	t.Helper()
	shared.mutex.Lock()
	shared.close()
	shared.path = path
	shared.resolved = path != ""
	shared.silenced = false
	shared.mutex.Unlock()
	t.Cleanup(func() {
		shared.mutex.Lock()
		shared.close()
		shared.path = ""
		shared.resolved = false
		shared.silenced = false
		shared.mutex.Unlock()
	})
}

func readLines(t *testing.T, path string) []Record {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open the log: %v", err)
	}
	defer file.Close()
	var records []Record
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("a line of the log is not a record: %v (%q)", err, line)
		}
		records = append(records, record)
	}
	return records
}

func TestEveryAppendIsOneWholeLineAndTheEmptyFieldsAreNotThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	fresh(t, path)

	Append(Record{Time: "2026-08-28T21:12:53.000Z", Tag: "compile", Model: "z-ai/glm-5.3", Status: 200})
	Append(Record{Time: "2026-08-28T21:12:59.000Z", Tag: "turn", Model: "z-ai/glm-5.3", Status: 400,
		Error: "Reasoning is mandatory", Learned: []string{"reasoning_mandatory"}})

	records := readLines(t, path)
	if len(records) != 2 {
		t.Fatalf("two calls should be two lines; got %d", len(records))
	}
	if records[0].Tag != "compile" || records[1].Learned[0] != "reasoning_mandatory" {
		t.Fatalf("the lines are not what was appended: %+v", records)
	}
	// The emptiness law reaches the file: a call with no tokens must not write
	// a zero somebody could read as a count.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"prompt_tokens", "completion_tokens", "cost", "request_body", "phase"} {
		if strings.Contains(string(raw), absent) {
			t.Errorf("%q is on a line that never had one", absent)
		}
	}
}

func TestTheLogRotatesAtTheCapAndKeepsOnePredecessor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	fresh(t, path)

	// Records big enough that a couple of hundred of them pass the cap. The
	// error field is the one that can hold a paragraph; it is clipped by its
	// writer rather than here, so the test builds the size it needs directly.
	filler := strings.Repeat("x", 256<<10)
	for written := int64(0); written < MaxBytes+int64(len(filler)); written += int64(len(filler)) {
		Append(Record{Time: "2026-08-28T21:12:53.000Z", Tag: "leaf", Error: filler})
	}

	live, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the live log should still be there: %v", err)
	}
	if live.Size() >= MaxBytes {
		t.Errorf("the live log is %d bytes, past the %d cap", live.Size(), MaxBytes)
	}
	previous, err := os.Stat(filepath.Join(dir, PreviousFileName))
	if err != nil {
		t.Fatalf("the predecessor should have been kept: %v", err)
	}
	if previous.Size() == 0 {
		t.Error("the predecessor is empty; the rotation kept the wrong file")
	}
	// And exactly two files: a rotation that kept a series would fill a disk
	// nobody was watching.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("rotation should leave two files, found %d", len(entries))
	}
}

// TestBodiesKeepHistoryPastTheOrdinaryCap is F24: AFORGE_CALL_LOG_BODIES=1
// makes each record tens of kilobytes, and MaxBytes then turns over after
// ~80 calls. A debug session that asked for the bodies must still have the
// first call after that point, on the live file, not rotated away.
func TestBodiesKeepHistoryPastTheOrdinaryCap(t *testing.T) {
	t.Setenv(BodiesEnvVar, "1")
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	fresh(t, path)

	const firstID = "first001"
	filler := strings.Repeat("x", 256<<10)
	Append(Record{
		Time: "2026-08-28T21:12:53.000Z", ID: firstID, Tag: "leaf",
		Error: filler, RequestBody: "req", ResponseBody: "res",
	})
	n := 1
	for written := int64(len(filler)); written < MaxBytes+int64(len(filler)); written += int64(len(filler)) {
		n++
		Append(Record{
			Time: "2026-08-28T21:12:53.000Z", Tag: "leaf",
			Error: filler, RequestBody: "req", ResponseBody: "res",
		})
	}
	if n < 80 {
		t.Fatalf("the fixture only wrote %d calls; need to pass the old ~80-call point", n)
	}

	live, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the live log should still be there: %v", err)
	}
	if live.Size() <= MaxBytes {
		t.Fatalf("bodies on should keep writing past the ordinary %d-byte cap; live is %d", MaxBytes, live.Size())
	}
	if _, err := os.Stat(filepath.Join(dir, PreviousFileName)); !os.IsNotExist(err) {
		t.Fatal("bodies on rotated at the ordinary cap; a debug session would have started losing history")
	}
	records := readLines(t, path)
	if len(records) != n {
		t.Fatalf("bodies on should keep all %d calls on the live file; found %d", n, len(records))
	}
	if records[0].ID != firstID {
		t.Fatalf("the first body-bearing call left the live file: got %q", records[0].ID)
	}
}

func TestOffWritesNothingAtAll(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvVar, OffValue)
	fresh(t, "")
	Open(dir)

	Append(Record{Time: "2026-08-28T21:12:53.000Z", Tag: "turn"})

	if path := Path(); path != "" {
		t.Fatalf("off should resolve to no path; got %q", path)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("off wrote something: %v %v", entries, err)
	}
}

func TestAPinnedPathIsWhereTheLogGoes(t *testing.T) {
	redirect := filepath.Join(t.TempDir(), "somewhere", "else.jsonl")
	t.Setenv(EnvVar, redirect)
	fresh(t, "")
	Open(filepath.Join(t.TempDir(), "profile"))

	Append(Record{Time: "2026-08-28T21:12:53.000Z", Tag: "turn"})

	if Path() != redirect {
		t.Fatalf("the pin should outrank the profile; got %q", Path())
	}
	if records := readLines(t, redirect); len(records) != 1 {
		t.Fatalf("the redirected log should hold one call; got %d", len(records))
	}
}

func TestWithNoPinTheLogSitsUnderTheProfileBesideTheQuirksMemo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvVar, "")
	if got, want := PathFor(dir), filepath.Join(dir, DirName, FileName); got != want {
		t.Fatalf("the profile's log is at %q, want %q", got, want)
	}
	// And with no profile at all it falls back to the state root, which is the
	// same fallback the quirks memo makes.
	previous := homeJoin
	homeJoin = func(elements ...string) string {
		return filepath.Join(append([]string{"/state/root"}, elements...)...)
	}
	defer func() { homeJoin = previous }()
	if got, want := PathFor(""), filepath.Join("/state/root", DirName, FileName); got != want {
		t.Fatalf("the fallback log is at %q, want %q", got, want)
	}
}

func TestAWriteThatCannotHappenFallsSilentOnceAndSaysSoOnce(t *testing.T) {
	// A directory where the file should be: every write fails, forever.
	dir := t.TempDir()
	blocked := filepath.Join(dir, "calls.jsonl")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	fresh(t, blocked)
	var complaints strings.Builder
	previous := stderr
	stderr = &complaints
	defer func() { stderr = previous }()

	for range 5 {
		Append(Record{Time: "2026-08-28T21:12:53.000Z", Tag: "turn"})
	}

	said := complaints.String()
	if strings.Count(said, "\n") != 1 {
		t.Fatalf("a broken log should complain exactly once; it said:\n%s", said)
	}
	if !strings.Contains(said, blocked) {
		t.Errorf("the complaint should name the path it could not write: %q", said)
	}
}

func TestBodiesAreOffUnlessSomebodyAsks(t *testing.T) {
	t.Setenv(BodiesEnvVar, "")
	if Bodies() {
		t.Error("bodies should be off with nothing set")
	}
	for _, off := range []string{"0", "false", "off"} {
		t.Setenv(BodiesEnvVar, off)
		if Bodies() {
			t.Errorf("%q should not switch bodies on", off)
		}
	}
	t.Setenv(BodiesEnvVar, "1")
	if !Bodies() {
		t.Error("bodies should be on when asked for")
	}
}

func TestAnErrorIsClippedRatherThanOwningTheWholeLine(t *testing.T) {
	if got := ClipError("  short  "); got != "short" {
		t.Errorf("a short message should come through whole: %q", got)
	}
	long := ClipError(strings.Repeat("y", MaxErrorChars*2))
	if len([]rune(long)) != MaxErrorChars+1 || !strings.HasSuffix(long, "…") {
		t.Errorf("a long message should be clipped and marked: %d runes", len([]rune(long)))
	}
}

func TestAPairingTokenIsShortAndDifferentEveryTime(t *testing.T) {
	first, second := NewID(), NewID()
	if len(first) != 8 || first == second {
		t.Fatalf("ids should be eight characters and unique: %q %q", first, second)
	}
}

// The log's in-memory half: the newest finished call is readable in-process
// with the file off, a start row is not an answer, and a row with no model
// cannot say who answered.
func TestLastIsTheNewestFinishedCallEvenWithTheFileOff(t *testing.T) {
	t.Setenv(EnvVar, OffValue)
	Open(t.TempDir())
	Append(Record{Time: "2026-08-28T23:00:00.000-04:00", Phase: "start", Model: "a/one"})
	if call, found := Last(); found && call.Model == "a/one" && call.At.Year() == 2026 && call.Tag == "" && call.Node == "" {
		t.Fatalf("a start row was read as an answer: %+v", call)
	}
	Append(Record{Time: "2026-08-28T23:00:01.000-04:00", Model: "a/one", Tag: "leaf", Node: "task-2", Status: 200})
	Append(Record{Time: "2026-08-28T23:00:02.000-04:00", Status: 200})
	call, found := Last()
	if !found || call.Model != "a/one" || call.Tag != "leaf" || call.Node != "task-2" {
		t.Fatalf("Last() = %+v, %v; want the finished leaf call", call, found)
	}
	if call.At.Format(timeLayout) != "2026-08-28T23:00:01.000-04:00" {
		t.Fatalf("At = %s, want the end row's own time", call.At.Format(timeLayout))
	}
}

// ── THE LANE, AND THE WAIT IT MADE ──────────────────────────────────────────

// TestALaneRowSeparatesTheQueueFromTheWriting pins the four field names on the
// wire and the one distinction they exist for: `ms` is the whole call and
// `ttft_ms` is the half of it a person feels, and a row that carries both is
// the only kind that can tell a queue apart from a slow writer.
func TestALaneRowSeparatesTheQueueFromTheWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", FileName)
	fresh(t, path)
	Append(Record{
		Time: "2026-08-30T10:00:00.000Z", ID: "abcd1234", Tag: "turn",
		Model: "deepseek/deepseek-v4-flash", Served: "CoreWeave", Lane: "Cloudflare",
		Status: 200, Millis: 4768, TTFTms: 768, DeadlineMs: 1200, Hedged: true,
		Finish: "stop",
	})

	rows := readLines(t, path)
	if len(rows) != 1 {
		t.Fatalf("wrote %d rows, want 1", len(rows))
	}
	row := rows[0]
	if row.TTFTms != 768 || row.DeadlineMs != 1200 {
		t.Fatalf("the timings came back as %+v", row)
	}
	// The two names are the whole point: the preference asked for one machine
	// and another answered, and a log that kept only one of them could not say
	// so.
	if row.Lane != "Cloudflare" || row.Served != "CoreWeave" {
		t.Fatalf("asked for %q and served by %q", row.Lane, row.Served)
	}
	if !row.Hedged {
		t.Fatal("the row forgot it was a rescue")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	for _, field := range []string{`"ttft_ms":768`, `"deadline_ms":1200`, `"lane":"Cloudflare"`, `"hedged":true`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("the line does not carry %s: %s", field, raw)
		}
	}
}

// TestACallWithNoLaneNamesNone is the emptiness law, which this file states in
// its own type comment and which is what makes four new fields safe to add to
// a format a person greps: an endpoint that is not a router names no lane, a
// call that was not streamed has no first-token wait, and a request nobody
// armed a watch on has no deadline. All four are then absent, and a reader may
// take anything they DO find as something that was measured.
func TestACallWithNoLaneNamesNone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", FileName)
	fresh(t, path)
	Append(Record{
		Time: "2026-08-30T10:00:00.000Z", ID: "abcd1234", Tag: "turn",
		Model: "sim/model", Served: "quicksilver", Status: 200, Millis: 900, Finish: "stop",
	})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	for _, word := range []string{"ttft_ms", "deadline_ms", "lane", "hedged"} {
		if strings.Contains(string(raw), word) {
			t.Fatalf("the row names %q with nothing to say: %s", word, raw)
		}
	}
}

// TestARowWrittenBeforeLanesExistedStillDecodes holds the wire compatible: the
// log rotates rather than being rewritten, so the file a person opens today
// holds rows from before these fields existed and every one of them must come
// back whole and believing nothing.
func TestARowWrittenBeforeLanesExistedStillDecodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", FileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("make the directory: %v", err)
	}
	const old = `{"ts":"2026-08-25T13:11:00.000Z","id":"abcd1234","tag":"turn",` +
		`"model":"opus-4.1","served":"quicksilver","status":200,"ms":1400,"finish":"stop",` +
		`"prompt_tokens":1200,"completion_tokens":340,"cost":0.42}`
	if err := os.WriteFile(path, []byte(old+"\n"), 0o644); err != nil {
		t.Fatalf("write the old row: %v", err)
	}
	rows := readLines(t, path)
	if len(rows) != 1 {
		t.Fatalf("read %d rows, want 1", len(rows))
	}
	row := rows[0]
	if row.Served != "quicksilver" || row.Millis != 1400 || row.Cost != 0.42 {
		t.Fatalf("the old row lost its figures: %+v", row)
	}
	if row.TTFTms != 0 || row.DeadlineMs != 0 || row.Lane != "" || row.Hedged {
		t.Fatalf("a row from before lanes existed came back believing something: %+v", row)
	}
}
