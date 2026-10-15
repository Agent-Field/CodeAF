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
// that line, and it must be a manager's start that is not addressed to a
// different conversation. Any other membership is answered only when the log
// makes it unambiguous, by the one rule that needs no reading of words: exactly
// one manager start in the whole retained log is addressed to this member's
// key, or (for a start the interface wrote before the member existed) is the
// only one ever addressed to its handle, the log still begins at its first
// entry, and no former member held that handle. Anything else is unknown.
func StartFor(profileDir string, t Team, m Member) (Entry, bool, error) {
	if m.StartEntry != "" {
		e, ok, err := ReadEntry(profileDir, t.ID, m.StartEntry)
		if err != nil || !ok || !isManagerStart(e) || e.ToKey != "" && e.ToKey != m.Key {
			return Entry{}, false, err
		}
		return e, true, nil
	}
	if m.Key == "" || safeTeamID(t.ID) != nil {
		return Entry{}, false, nil
	}
	path := TrafficPath(profileDir, t.ID)
	var (
		byKey, byHandle []Entry
		first           int64
		seen            bool
	)
	for _, p := range []string{trafficRotated(path), path} {
		err := scanTrafficFile(p, func(id int64, line []byte) {
			if !seen {
				first, seen = id, true
			}
			if !bytes.Contains(line, []byte(`"kind":"start"`)) {
				return
			}
			e, ok := parseTrafficLine(line)
			if !ok || !isManagerStart(e) {
				return
			}
			switch {
			case e.ToKey == m.Key:
				byKey = append(byKey, e)
			case m.Handle != "" && e.To == m.Handle:
				byHandle = append(byHandle, e) // counted so a shared name is seen
			}
		})
		if err != nil {
			return Entry{}, false, err
		}
	}
	if len(byKey) == 1 {
		return byKey[0], true, nil
	}
	if len(byKey) > 0 || len(byHandle) != 1 || !m.Started || byHandle[0].ToKey != "" || !seen || first > 1 {
		return Entry{}, false, nil
	}
	for _, f := range t.FormerMembers {
		if f.Handle == m.Handle {
			return Entry{}, false, nil
		}
	}
	return byHandle[0], true, nil
}

func isManagerStart(e Entry) bool {
	return e.Kind == KindStart && e.From == FromManager
}

// scanTrafficFile calls visit with the id and raw bytes of each complete line
// of one file that carries an id, one line in memory at a time. A missing file
// is nothing. It is bounded by the rotation size, never by the team's age.
func scanTrafficFile(path string, visit func(id int64, line []byte)) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	r := bufio.NewReader(file)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			if id, ok := lineID(line); ok {
				visit(id, line)
			}
		}
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
