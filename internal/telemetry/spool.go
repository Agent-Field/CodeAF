package telemetry

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Spool limits, from the contract: a thousand lines of history, seven days of
// age, fifty events on the wire per POST, one megabyte read into memory at
// most.
const (
	MaxSpoolLines     = 1000
	MaxEventAge       = 7 * 24 * time.Hour
	MaxEventsPerPOST  = 50
	MaxSpoolReadBytes = 1 << 20
)

// noticeSeenPath is the marker that says the person has seen the notice. Until
// it exists, nothing is sent: spooling is silent, flushing is a no-op.
const noticeSeenFile = "notice_seen"

// noticeSeenPath is where NoticeShown reads and MarkNoticeShown writes.
func noticeSeenPath() string { return telemetryFile(noticeSeenFile) }

// spoolPath is the JSON-lines file events wait in.
func spoolPath() string { return telemetryFile("spool.jsonl") }

// NoticeShown reports whether the notice has been marked shown. Until it has,
// events may be spooled but are never flushed.
func NoticeShown() bool { return fileExists(noticeSeenPath()) }

// MarkNoticeShown records that the person has seen the notice, which opens the
// gate on flushing. A failure is silent for the same reason every other write
// here is: the worst case is a notice seen twice.
func MarkNoticeShown() {
	if err := writeFilePrivate(noticeSeenPath(), []byte("shown\n")); err != nil {
		oneWarning("telemetry: could not record that the notice was shown")
	}
}

// warnOnce is the one line to stderr a process may ever produce for telemetry
// failures, however many failures there are.
var warnOnce sync.Once

// oneWarning prints the process's single telemetry failure line to stderr.
func oneWarning(message string) {
	warnOnce.Do(func() {
		fmt.Fprintln(os.Stderr, message)
	})
}

// Spool appends one event to the spool file and returns immediately. It never
// blocks the caller for more than a few milliseconds: it does no network work,
// and the one file append it does is a single WriteFile on a line that was
// already marshalled. A failure here is silent.
func Spool(event Event) {
	line, err := jsonMarshal(event)
	if err != nil {
		return
	}
	go persist([]byte(line))
}

// SpoolSync appends one event and waits for the append, for tests and for a
// caller that must see the line on disk. It shares every law with Spool and is
// not for the product's hot path.
func SpoolSync(event Event) error {
	line, err := jsonMarshal(event)
	if err != nil {
		return nil
	}
	return persist(line)
}

// persist appends one marshalled event line to the spool. The file is opened
// per write: a spool of a handful of lines is a once-per-session append, and
// an append that never holds a descriptor open can never leak one.
func persist(line []byte) error {
	if err := ensureDir(); err != nil {
		return err
	}
	file, err := os.OpenFile(spoolPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return err
	}
	return truncateSpoolToCap()
}

// truncateSpoolToCap keeps the spool at the contract's line cap by dropping
// the OLDEST lines — the front of the file — once it has grown past 1000.
func truncateSpoolToCap() error {
	path := spoolPath()
	lines, err := readSpoolLines(path)
	if err != nil {
		return err
	}
	if len(lines) <= MaxSpoolLines {
		return nil
	}
	return rewriteSpool(path, lines[len(lines)-MaxSpoolLines:])
}

// spoolEntry is one parsed line of the spool file, with the wire row it
// parsed from when it parsed.
type spoolEntry struct {
	line     string
	event    jsonEvent
	ageKnown bool
}

// Flush sends every spooled event it can within the deadline the caller
// carries — callers pass a hard budget, one second at exit — POSTing up to
// fifty at a time to the endpoint. Lines that were sent are removed, lines
// older than seven days are dropped, and a failure of any kind is silent.
// The returned error is always nil; it exists so a future caller can log
// without this package ever being able to fail a run.
func Flush(ctx context.Context) error {
	if !NoticeShown() {
		return nil
	}
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(time.Second)
	}
	path := spoolPath()
	lines, err := readSpoolLines(path)
	if err != nil {
		return nil
	}
	kept := make([]spoolEntry, 0, len(lines))
	var batches [][]spoolEntry
	var batch []spoolEntry
	dropped := false
	for _, entry := range lines {
		if entry.ageKnown && ageOf(entry.event.EventTime) > MaxEventAge {
			dropped = true
			continue
		}
		kept = append(kept, entry)
		batch = append(batch, entry)
		if len(batch) == MaxEventsPerPOST {
			batches = append(batches, batch)
			batch = nil
		}
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	for _, batch := range batches {
		if !sendBatch(ctx, deadline, batch) {
			// The deadline is hard: stop mid-batch, keep everything not yet
			// confirmed sent, and say nothing.
			break
		}
		dropped = true
	}
	if dropped {
		_ = rewriteSpool(path, keptAfterSending(path, lines, kept, batches))
	}
	return nil
}

// keptAfterSending removes the lines whose batches were confirmed sent. It is
// a pure function of the original lines, the parsed survivors and the batches
// that went out, so a flush that sent nothing still rewrites the file with the
// age drops applied.
func keptAfterSending(path string, lines []spoolEntry, kept []spoolEntry, batches [][]spoolEntry) []spoolEntry {
	sent := map[int]bool{}
	// Count forward: batch i is the i-th group of MaxEventsPerPOST survivors,
	// which is exactly how the batches above were cut.
	for i := range batches {
		for j := range batches[i] {
			sent[i*MaxEventsPerPOST+j] = true
		}
	}
	if len(sent) == 0 {
		return kept
	}
	var survivors []spoolEntry
	for index, entry := range kept {
		if !sent[index] {
			survivors = append(survivors, entry)
		}
	}
	return survivors
}

// sendBatch POSTs one batch. It answers true only when the relay answered
// 2xx within the deadline; any error, any other status, and the batch stays.
func sendBatch(ctx context.Context, deadline time.Time, batch []spoolEntry) bool {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false
	}
	batchCtx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	body, err := jsonMarshal(map[string]any{
		"schema_version": schemaVersion,
		"events":         batchEvents(batch),
	})
	if err != nil {
		return false
	}
	request, err := http.NewRequestWithContext(batchCtx, http.MethodPost, Endpoint(), bytes.NewReader(body))
	if err != nil {
		return false
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	ok := response.StatusCode >= 200 && response.StatusCode < 300
	return ok
}

// batchEvents returns the wire rows of one batch, in order.
func batchEvents(batch []spoolEntry) []jsonEvent {
	out := make([]jsonEvent, len(batch))
	for i, entry := range batch {
		out[i] = entry.event
	}
	return out
}

// httpClient is the one client. No timeout of its own: the deadline lives in
// the request context, where the caller's hard budget is.
var httpClient = &http.Client{}

// readSpoolLines reads the spool as entries, skipping and reporting lines it
// cannot parse. A spool that does not exist is an empty spool.
func readSpoolLines(path string) ([]spoolEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	var entries []spoolEntry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxSpoolReadBytes)
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		entry := spoolEntry{line: raw}
		if err := json.Unmarshal([]byte(raw), &entry.event); err == nil {
			entry.ageKnown = true
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

// rewriteSpool replaces the spool file with the given lines atomically.
func rewriteSpool(path string, entries []spoolEntry) error {
	if err := ensureDir(); err != nil {
		return err
	}
	var out bytes.Buffer
	for _, entry := range entries {
		out.WriteString(entry.line)
		out.WriteByte('\n')
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, out.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// SpoolContents returns the spooled events as raw JSON rows, oldest first, in
// the order they would be sent. Show formats them for a person.
func SpoolContents() []json.RawMessage {
	lines, err := readSpoolLines(spoolPath())
	if err != nil {
		return nil
	}
	out := make([]json.RawMessage, 0, len(lines))
	for _, entry := range lines {
		out = append(out, json.RawMessage(entry.line))
	}
	return out
}

// Show returns the spool as pretty JSON — the exact answer a future
// `codeaf telemetry show` prints, so a person can read everything that has
// not left yet.
func Show() string {
	contents := SpoolContents()
	if len(contents) == 0 {
		return "[]"
	}
	var out bytes.Buffer
	out.WriteString("[\n")
	for i, raw := range contents {
		out.WriteString("  ")
		out.Write(raw)
		if i < len(contents)-1 {
			out.WriteByte(',')
		}
		out.WriteByte('\n')
	}
	out.WriteString("]")
	return out.String()
}
