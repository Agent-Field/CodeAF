package delegate

// One launch's lines, read within a fixed budget. A task's action log and its
// conversation log are only ever appended to, across every launch a task's
// program has had, so a read that wants the current process's work and reads
// the whole of either costs what the task's whole history costs — an hour's
// run of senior-dev, handed off a few times, is hundreds of megabytes. This is
// the read that costs the launch and no more, and SAYS SO when it could not
// read it all.
//
// HOW THE LAUNCH IS FOUND. Its producer wrote its `start` line before the
// process existed ([ActionStart]) and stamped every line after it with the
// launch ([Action.Launch]), so the read looks back from the end of a size
// taken once for the launch's start line, and reads forward from there. Lines
// of any other launch in between — an earlier run's line written late — are
// passed over by their stamp, never by their time.
//
// WHY FORWARD FROM THE LAUNCH'S FIRST LINE. A program's vocabulary may read a
// line by the lines before it (senior-dev's nudges, review's first mention of
// a stage), so one reader reads one launch from its first line ([ActionReader]),
// and a page of older actions is the same reading kept at another window. The
// index of an action within its launch therefore never changes as the launch
// goes on, which is what makes a cursor of (launch, index) exact.
//
// WHAT IS NOT KNOWN IS SAID. A launch whose lines run past the budget, a line
// that does not parse or is longer than a line may be, a read cut by its
// deadline or refused by the disk: the result is not complete, and a caller
// says no count, no "no history", no last stage and no ending from it.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ReadBudget is the most one read of a log may cost: bytes read off the disk,
// complete lines decoded. Its deadline is the caller's context.
type ReadBudget struct {
	Bytes int64
	Lines int
}

// Why a read is not complete.
const (
	// ReadCutBudget: the launch's lines run past the budget.
	ReadCutBudget = "budget"
	// ReadCutDamaged: a line that does not parse, or is longer than any line
	// codeaf writes may be, sits among the launch's lines.
	ReadCutDamaged = "damaged"
	// ReadCutUnreadable: the disk refused the read.
	ReadCutUnreadable = "unreadable"
	// ReadCutDeadline: the read's deadline passed, or its caller left.
	ReadCutDeadline = "deadline"
)

// launchWindow is how much of the log one step back reads.
const launchWindow = 64 << 10

// LaunchPage is a window of one launch's shown actions and what its lines say
// about where it stands.
type LaunchPage struct {
	// Complete says every line of the launch was read and parsed; Cut is why
	// not, one of the ReadCut words, when it is false.
	Complete bool
	Cut      string
	// Total is how many actions the launch shows, First the index within the
	// launch of Actions[0]. Total is exact only when Complete.
	Total, First int
	Actions      []Shown
	// LastStage is the stage the launch's newest stage line names, and Ending
	// the launch's last shown action when its last line is its `end` line, with
	// EndStatus that line's status. All three are the launch's own and are
	// meaningful only when Complete.
	LastStage string
	Ending    *Shown
	EndStatus string
}

// ReadLaunchActions reads one launch's lines of the action log in dir through
// read, which is handed every one of the launch's lines from its first, in
// order. It keeps the newest limit shown actions before index before — the
// newest of the whole launch when before is negative — within budget and ctx.
//
// A log that is not there is a launch that has written nothing, which is
// complete and empty. An error is never answered: a refusal of the disk is a
// page that is not complete, because a caller has nothing truer to say.
func ReadLaunchActions(ctx context.Context, dir, launch string, read ActionReader, before, limit int, budget ReadBudget) LaunchPage {
	if !ValidLaunch(launch) || limit <= 0 {
		return LaunchPage{Cut: ReadCutUnreadable}
	}
	file, err := os.Open(filepath.Join(dir, ActionsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return LaunchPage{Complete: true, First: max(before, 0)}
	}
	if err != nil {
		return LaunchPage{Cut: ReadCutUnreadable}
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return LaunchPage{Cut: ReadCutUnreadable}
	}
	size := info.Size()
	from, cut := launchStart(ctx, file, size, launch, budget.Bytes)
	if cut != "" {
		return LaunchPage{Cut: cut}
	}
	page := LaunchPage{Complete: true}
	var window []Shown
	lastShownIsEnd := false
	lines := 0
	err = eachLine(ctx, io.NewSectionReader(file, from, size-from), func(line []byte, ok bool) bool {
		if lines++; budget.Lines > 0 && lines > budget.Lines {
			page.Complete, page.Cut = false, ReadCutBudget
			return false
		}
		var action Action
		if !ok || json.Unmarshal(line, &action) != nil || action.Kind == "" {
			page.Complete, page.Cut = false, ReadCutDamaged
			return true
		}
		if action.Launch != launch || action.Kind == ActionStart {
			return true
		}
		if action.Kind == ActionStage && action.Stage != "" {
			page.LastStage = action.Stage
		}
		shown, shows := read(action)
		lastShownIsEnd = shows && action.Kind == ActionEnd
		if lastShownIsEnd {
			page.EndStatus = action.Status
		}
		if !shows {
			return true
		}
		index := page.Total
		page.Total++
		if before < 0 || index < before {
			window = append(window, shown)
			if len(window) > limit {
				window = window[1:]
			}
		}
		if lastShownIsEnd {
			ending := shown
			page.Ending = &ending
		}
		return true
	})
	if err != nil && page.Complete {
		page.Complete, page.Cut = false, ReadCutUnreadable
		if ctx.Err() != nil {
			page.Cut = ReadCutDeadline
		}
	}
	if !lastShownIsEnd {
		page.Ending, page.EndStatus = nil, ""
	}
	end := page.Total
	if before >= 0 && before < end {
		end = before
	}
	page.First = end - len(window)
	page.Actions = append([]Shown(nil), window...)
	return page
}

// CountLaunchCalls is how many of one launch's calls in the conversation log
// in dir reached a model: each call once, as its latest record says, and
// codeaf's own refusals left out. Known is false — and the count means
// nothing — when the log is larger than the budget, a line of it does not
// parse, or it could not be read: a call count is a count of every call or it
// is not one. A log that is not there is a launch that has made no call.
func CountLaunchCalls(ctx context.Context, dir, launch string, budget ReadBudget) (calls int, known bool) {
	if !ValidLaunch(launch) {
		return 0, false
	}
	file, err := os.Open(filepath.Join(dir, ConversationFile))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, true
	}
	if err != nil {
		return 0, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || (budget.Bytes > 0 && info.Size() > budget.Bytes) {
		return 0, false
	}
	refused := map[int]bool{}
	lines, damaged := 0, false
	err = eachLine(ctx, io.NewSectionReader(file, 0, info.Size()), func(line []byte, ok bool) bool {
		if lines++; budget.Lines > 0 && lines > budget.Lines {
			damaged = true
			return false
		}
		var turn struct {
			Seq     *int   `json:"seq"`
			Launch  string `json:"launch"`
			Refused string `json:"refused"`
		}
		if !ok || json.Unmarshal(line, &turn) != nil || turn.Seq == nil {
			damaged = true
			return false
		}
		if turn.Launch == launch {
			refused[*turn.Seq] = turn.Refused != ""
		}
		return true
	})
	if err != nil || damaged {
		return 0, false
	}
	for _, wasRefused := range refused {
		if !wasRefused {
			calls++
		}
	}
	return calls, true
}

// launchStart is the offset of the launch's start line in a log of size bytes,
// found by reading back from the end; the start of the log when there is none,
// which is a launch whose start line the disk refused, read from the first
// line so that none of its own is missed. Cut says why it could not be found.
//
// THE START LINE IS FOUND BY ITS BYTES. Its producer marshals it with nothing
// between its kind and its launch, and a string inside any other line has its
// quotes escaped, so `"kind":"start","launch":"<id>"` appears in the log only
// as the start line itself; the line it sits in is still parsed before it is
// believed.
func launchStart(ctx context.Context, file io.ReaderAt, size int64, launch string, most int64) (int64, string) {
	needle := []byte(`"kind":"` + ActionStart + `","launch":"` + launch + `"`)
	buf := make([]byte, launchWindow+len(needle))
	end := size
	for end > 0 {
		if ctx.Err() != nil {
			return 0, ReadCutDeadline
		}
		start := max(end-launchWindow, 0)
		if most > 0 && size-start > most {
			return 0, ReadCutBudget
		}
		// The window reaches past its end by the needle's length, so a needle cut
		// by the edge between two windows is found whole in the earlier one.
		n := int(min(end+int64(len(needle)), size) - start)
		if _, err := file.ReadAt(buf[:n], start); err != nil && !errors.Is(err, io.EOF) {
			return 0, ReadCutUnreadable
		}
		for at := bytes.LastIndex(buf[:n], needle); at >= 0; at = bytes.LastIndex(buf[:at], needle) {
			if from, ok := startLineAt(file, start+int64(at), launch); ok {
				return from, ""
			}
		}
		end = start
	}
	return 0, ""
}

// startLineAt is the offset of the line holding offset at, when that line is
// the launch's start line.
func startLineAt(file io.ReaderAt, at int64, launch string) (int64, bool) {
	// A start line is short: its time, its kind and its launch.
	const most = 256
	from := max(at-most, 0)
	buf := make([]byte, 2*most)
	n, err := file.ReadAt(buf, from)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, false
	}
	head, tail := buf[:at-from], buf[at-from:n]
	begin := bytes.LastIndexByte(head, '\n') + 1
	if begin == 0 && from > 0 {
		return 0, false
	}
	stop := bytes.IndexByte(tail, '\n')
	if stop < 0 {
		return 0, false
	}
	var action Action
	line := buf[begin : int(at-from)+stop]
	if json.Unmarshal(line, &action) != nil || action.Kind != ActionStart || action.Launch != launch {
		return 0, false
	}
	return from + int64(begin), true
}

// eachLine hands every newline-ended line of r to fn, ok false for a line
// longer than [maxLineBytes] (handed without its bytes), until fn says stop.
// A last line with no newline is a write still under way and is not handed
// on. ctx is asked between lines.
func eachLine(ctx context.Context, r io.Reader, fn func(line []byte, ok bool) bool) error {
	reader := bufio.NewReaderSize(r, launchWindow)
	var long []byte
	over := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			if !over {
				if len(long)+len(chunk) > maxLineBytes {
					over, long = true, nil
				} else {
					long = append(long, chunk...)
				}
			}
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		line, ok := chunk[:len(chunk)-1], true
		if over || long != nil {
			if !over && len(long)+len(line) <= maxLineBytes {
				line = append(long, line...)
			} else {
				line, ok = nil, false
			}
			long, over = nil, false
		}
		if ok && len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if !fn(line, ok) {
			return nil
		}
	}
}
