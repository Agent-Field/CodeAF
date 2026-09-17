package telemetry

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
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

// The sending-file protocol. A flush does not send from spool.jsonl: it
// renames the spool to a process-unique spool.sending.<pid>.<random> first,
// so two codeaf processes flushing at once each own a file and neither can
// delete the other's unsent events. Whatever a flush cannot send it appends
// back to spool.jsonl, one Write per line on an O_APPEND descriptor, and then
// removes its sending file. A process that dies mid-flush leaves its sending
// file behind, and the next flush to start more than ten minutes later — far
// past any flush's one-second budget — adopts it by appending its lines back.
const (
	sendingNamePrefix = "spool.sending."
	sendingGlob       = "spool.sending.*"
	sendingStaleAfter = 10 * time.Minute
)

// persistSizeLimit is the one cheap guard persist keeps: past one megabyte of
// spool on disk the oldest half is dropped, so a process that never flushes
// still cannot grow the file without bound. Every other cap belongs to Flush.
const persistSizeLimit = 1 << 20

// spoolMu guards every in-process touch of the spool files: the append in
// persist, the rename-and-send cycle in Flush, the read behind SpoolContents.
// Several codeaf processes run at once, and the sending-file protocol above
// is what keeps them apart; this mutex is what keeps one process's own
// goroutines apart.
var spoolMu sync.Mutex

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

// Spool appends one event to the spool file and returns immediately. It
// never blocks the caller for more than a few milliseconds: it does no network
// work, and its one append runs on its own goroutine through guard.Go, the
// repo's one door for fire-and-forget work. A failure here is silent.
func Spool(event Event) {
	if !enabledFor() {
		return
	}
	line, err := jsonMarshal(event)
	if err != nil {
		return
	}
	guard.Go("telemetry.spool", func() { _ = persist(line) })
}

// SpoolSync appends one event and waits for the append, for tests and for a
// caller that must see the line on disk. It shares every law with Spool and is
// not for the product's hot path.
func SpoolSync(event Event) error {
	if !enabledFor() {
		return nil
	}
	line, err := jsonMarshal(event)
	if err != nil {
		return nil
	}
	return persist(line)
}

// persist appends one marshalled event line to the spool. The file is opened
// per write — an append that never holds a descriptor open can never leak one
// — and it is never re-read or rewritten here: the thousand-line cap and the
// seven-day drop belong to Flush, which owns the file while it sends. The one
// rewrite persist may do is the size guard, and only past one megabyte.
func persist(line []byte) error {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	if err := ensureDir(); err != nil {
		return err
	}
	path := spoolPath()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(line, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() <= persistSizeLimit {
		return nil
	}
	lines, err := readSpoolLines(path)
	if err != nil || len(lines) < 2 {
		return nil
	}
	return rewriteSpool(path, lines[len(lines)/2:])
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
//
// The spool is first renamed to a process-unique sending file, so a flush
// owns the lines it is sending: another process appending to spool.jsonl
// mid-flush cannot be deleted unsent, and this process's unsent lines are
// appended back rather than rewritten around the other's.
func Flush(ctx context.Context) error {
	if !enabledFor() {
		return nil
	}
	if !NoticeShown() {
		return nil
	}
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(time.Second)
	}
	spoolMu.Lock()
	defer spoolMu.Unlock()
	adoptStaleSendingFiles()
	path := spoolPath()
	if !fileExists(path) {
		// An empty spool is a no-op: no rename, so no sending file is left
		// behind for a later flush to adopt.
		return nil
	}
	sending := sendingPath()
	if err := os.Rename(path, sending); err != nil {
		return nil
	}
	entries, err := readSpoolLines(sending)
	if err != nil {
		// The lines are safe where they are; a flush ten minutes on adopts
		// them rather than losing them.
		return nil
	}
	// The contract's caps, applied to the file this flush owns: age first,
	// the event allowlist next, the thousand-line cap last so that when the
	// cap bites it is the oldest lines that go.
	var survivors []spoolEntry
	for _, entry := range entries {
		if entry.ageKnown && ageOf(entry.event.EventTime) > MaxEventAge {
			continue
		}
		if !allowlistedEvents[entry.event.EventName] {
			// An event name the contract does not define can never be sent
			// validly, so it is discarded rather than posted or kept forever.
			continue
		}
		survivors = append(survivors, entry)
	}
	if len(survivors) > MaxSpoolLines {
		survivors = survivors[len(survivors)-MaxSpoolLines:]
	}
	var batches [][]spoolEntry
	for start := 0; start < len(survivors); start += MaxEventsPerPOST {
		end := start + MaxEventsPerPOST
		if end > len(survivors) {
			end = len(survivors)
		}
		batches = append(batches, survivors[start:end])
	}
	// sent marks, by index into survivors, the events whose batch the relay
	// confirmed. Counting the confirmed batches here — never assuming the
	// whole flush succeeded — is the whole law: a batch the relay refused
	// leaves its events spooled for the next flush.
	sent := make(map[int]bool)
	offset := 0
	for _, batch := range batches {
		if !sendBatch(ctx, deadline, batch) {
			// The deadline is hard: stop mid-batch, keep everything not yet
			// confirmed sent, and say nothing.
			break
		}
		for i := range batch {
			sent[offset+i] = true
		}
		offset += len(batch)
	}
	var kept []spoolEntry
	for index, entry := range survivors {
		if !sent[index] {
			kept = append(kept, entry)
		}
	}
	if len(kept) > 0 {
		if err := appendLines(spoolPath(), kept); err != nil {
			// The unsent lines are still in the sending file; leave it for a
			// later flush to adopt rather than dropping them.
			return nil
		}
	}
	_ = os.Remove(sending)
	return nil
}

// sendingPath names the file this process flushes from: unique per process,
// and unique per flush, so two flushes from one process cannot collide either.
func sendingPath() string {
	return filepath.Join(telemetryDir(), fmt.Sprintf("%s%d.%s", sendingNamePrefix, os.Getpid(), randomHex(8)))
}

// appendLines appends entries to the spool file, one Write per line, on a
// descriptor opened with O_APPEND: two processes appending at once interleave
// whole lines, never overwriting each other's.
func appendLines(path string, entries []spoolEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if err := ensureDir(); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := file.Write([]byte(entry.line + "\n")); err != nil {
			file.Close()
			return err
		}
	}
	return file.Close()
}

// adoptStaleSendingFiles appends back the lines of any spool.sending.* file
// older than ten minutes — the sending file of a process that died mid-flush
// — and removes it. Ten minutes is far past any flush's one-second budget, so
// a live flush's file is never adopted out from under it.
func adoptStaleSendingFiles() {
	matches, err := filepath.Glob(filepath.Join(telemetryDir(), sendingGlob))
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-sendingStaleAfter)
	for _, orphan := range matches {
		info, err := os.Stat(orphan)
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		entries, err := readSpoolLines(orphan)
		if err != nil {
			continue
		}
		if err := appendLines(spoolPath(), entries); err != nil {
			continue
		}
		// Removed only after the lines are safely in the spool: a removal
		// that raced ahead of the append would lose events, while a file left
		// behind can only be adopted again.
		_ = os.Remove(orphan)
	}
}

// sendBatch POSTs one batch. It answers true only when the relay answered
// 2xx within the deadline; any error, any other status, and the batch stays.
func sendBatch(ctx context.Context, deadline time.Time, batch []spoolEntry) bool {
	// A test binary never reaches the production relay. The only state in
	// which a send under `go test` could land on DefaultEndpoint is an unset
	// CODEAF_TELEMETRY_ENDPOINT — a test that forgot its relay — so refuse
	// before any request is built, and refuse the way a failed send is
	// refused: the batch stays in the spool and Flush says nothing. This
	// check sits below forceLadderForTest, which reaches sendBatch only
	// through the enabledFor gate and cannot reopen it; on a production
	// binary it costs the one bool underGoTest answers.
	if underGoTest() && Endpoint() == DefaultEndpoint {
		return false
	}
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

// batchEvents returns the wire rows of one batch, in order, with the prop
// allowlist re-applied: a line spooled by an older build must not send a prop
// key the contract dropped since.
func batchEvents(batch []spoolEntry) []jsonEvent {
	out := make([]jsonEvent, len(batch))
	for i, entry := range batch {
		row := entry.event
		if allowed := allowedProps[row.EventName]; len(allowed) > 0 {
			clean := make(map[string]any, len(row.Props))
			for key, value := range row.Props {
				if allowed[key] {
					clean[key] = value
				}
			}
			row.Props = clean
		}
		out[i] = row
	}
	return out
}

// allowlistedEvents is the event-name half of the contract's allowlist, for
// the flush loop's one look per line.
var allowlistedEvents = map[string]bool{
	"first_run": true, "session_started": true, "session_ended": true, "fault": true,
}

// httpClient is the one client, and the package's one send seam: production
// declares it here and never reassigns it, and a test may swap it for a
// recording client or transport and restore it with t.Cleanup. No timeout of
// its own: the deadline lives in the request context, where the caller's hard
// budget is.
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

// rewriteSpool replaces the spool file with the given lines atomically. Only
// the persist size guard reaches it; a flush never rewrites the spool, it
// appends back.
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
	spoolMu.Lock()
	defer spoolMu.Unlock()
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
