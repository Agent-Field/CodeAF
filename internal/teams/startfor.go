package teams

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

// startfor.go answers "what was this member started with" by identity.
//
// A HANDLE IS NOT AN IDENTITY: a person renames a member and a later member
// takes the name, so reading the log for "the start line to @web" can hand one
// member another's brief and lose its own. A membership therefore keeps the id
// of the exact line that made it ([Member.StartEntry]) and this reads that one
// line; it never parses words and never takes a conversation's first message
// for a brief.

// ReadEntry is the one entry of team teamID's log with id, from either file,
// found without reading the log whole: a page of one past the id before it.
// ok is false when the log holds no such entry (rotated away, or never there).
func ReadEntry(profileDir, teamID, id string) (Entry, bool, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return Entry{}, false, nil
	}
	if err := safeTeamID(teamID); err != nil {
		return Entry{}, false, err
	}
	path := TrafficPath(profileDir, teamID)
	got, err := forwardTraffic([]string{trafficRotated(path), path}, fmt.Sprintf("%0*d", trafficIDWidth, n-1), 1)
	if err != nil || len(got) == 0 || got[0].ID != id {
		return Entry{}, false, err
	}
	return got[0], true, nil
}

// StartFor is the manager's start line that made member m of team t, and false
// when none can be proved to be theirs.
//
// A membership that kept its line ([Member.StartEntry]) is answered by reading
// that line, and it must be a manager's start of THIS team that is not
// addressed to a different conversation; no other line is ever fetched in its
// place. Any other membership is answered only when the log makes it
// unambiguous, by the one rule that needs no reading of words: exactly one
// manager start in the whole retained log is addressed to this member's key,
// or (for a start the interface wrote before the member existed) is the only
// one ever addressed to its handle, the log still begins at its first entry,
// and no former member held that handle. Anything else is unknown, and a log
// that cannot be read through is an error, never an empty answer.
func StartFor(profileDir string, t Team, m Member) (Entry, bool, error) {
	if m.StartEntry != "" {
		e, ok, err := ReadEntry(profileDir, t.ID, m.StartEntry)
		if err != nil || !ok || !isManagerStart(e) || e.ID != m.StartEntry ||
			e.Team != "" && e.Team != t.ID || e.ToKey != "" && e.ToKey != m.Key {
			return Entry{}, false, err
		}
		return e, true, nil
	}
	if m.Key == "" || safeTeamID(t.ID) != nil {
		return Entry{}, false, nil
	}
	path := TrafficPath(profileDir, t.ID)
	// Only a candidate and a count are kept: two matches are as unknown as
	// two hundred, so the scan stops at the second.
	var (
		byKey, byHandle Entry
		nKey, nHandle   int
		first           int64
		seen            bool
	)
	for _, p := range []string{trafficRotated(path), path} {
		err := scanTrafficFile(p, func(id int64, line []byte) bool {
			if !seen {
				first, seen = id, true
			}
			if !bytes.Contains(line, []byte(`"kind":"start"`)) {
				return true
			}
			e, ok := parseTrafficLine(line)
			if !ok || !isManagerStart(e) || e.Team != "" && e.Team != t.ID {
				return true
			}
			switch {
			case e.ToKey == m.Key:
				nKey++
				byKey = e
			case m.Handle != "" && e.To == m.Handle:
				nHandle++
				byHandle = e // counted so a shared name is seen
			}
			return nKey < 2 && nHandle < 2
		})
		if err != nil {
			return Entry{}, false, err
		}
		if nKey >= 2 || nHandle >= 2 {
			break
		}
	}
	if nKey == 1 {
		return byKey, true, nil
	}
	if nKey > 0 || nHandle != 1 || !m.Started || byHandle.ToKey != "" || !seen || first > 1 {
		return Entry{}, false, nil
	}
	for _, f := range t.FormerMembers {
		if f.Handle == m.Handle {
			return Entry{}, false, nil
		}
	}
	return byHandle, true, nil
}

func isManagerStart(e Entry) bool {
	return e.Kind == KindStart && e.From == FromManager
}

// maxScanLine is the longest line one scan will hold. A line past it is
// corruption (an entry is a short record), and a scan that skipped it could
// have skipped the start line it was looking for, so it is an error.
const maxScanLine = 1 << 20

// ErrTrafficUnreadable is a log a scan cannot vouch for: a line past
// [maxScanLine], or a file far past the size rotation keeps it under.
var ErrTrafficUnreadable = errors.New("the crew's log cannot be read through")

// scanTrafficFile calls visit with the id and raw bytes of each complete line
// of one file that carries an id, one line in memory at a time, until visit
// answers false. A missing file is nothing.
//
// It reads the size the file had when it was opened and not a byte more, so
// an append during the scan is the next scan's, and it holds no line longer
// than [maxScanLine]; both bounds fail with [ErrTrafficUnreadable] instead of
// answering as though the log were short.
func scanTrafficFile(path string, visit func(id int64, line []byte) bool) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	// Rotation keeps a file near trafficRotateBytes (it can pass it by one
	// line); a file several times that was not written by this log.
	if info.Size() > 2*trafficRotateBytes+maxScanLine {
		return fmt.Errorf("%w: %s is %d bytes", ErrTrafficUnreadable, path, info.Size())
	}
	r := bufio.NewReaderSize(io.LimitReader(file, info.Size()), 64<<10)
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(line)+len(chunk) > maxScanLine {
			return fmt.Errorf("%w: a line in %s is past %d bytes", ErrTrafficUnreadable, path, maxScanLine)
		}
		line = append(line, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if len(line) > 0 && line[len(line)-1] == '\n' {
			if id, ok := lineID(line); ok && !visit(id, line) {
				return nil
			}
		}
		line = line[:0]
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// lineID reads the leading "id" of an entry line without decoding it; every
// entry is written with its id first.
func lineID(line []byte) (int64, bool) {
	const lead = `{"id":"`
	if !bytes.HasPrefix(line, []byte(lead)) {
		return 0, false
	}
	rest := line[len(lead):]
	end := bytes.IndexByte(rest, '"')
	if end <= 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(string(rest[:end]), 10, 64)
	return n, err == nil
}
