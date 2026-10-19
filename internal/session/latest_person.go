package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"
)

// LatestPersonMessageAt reads backwards to the newest surviving person message.
// It retains one bounded journal line and one read block, never a display record.
// A candidate is accepted only after its preceding compaction or the file start:
// the marker's window may identify it as a rewritten copy of an older message.
func LatestPersonMessageAt(transcript string) (time.Time, bool, error) {
	file, err := os.Open(transcript)
	if err != nil {
		return time.Time{}, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return time.Time{}, false, err
	}
	scan := personJournalScan{file: file, end: info.Size()}
	return scan.latest()
}

// The line ceiling matches readJournal's accepted record size. Refusing a larger
// line keeps recovery conservative without retaining an unbounded tool payload.
const personJournalBlockBytes = 32 << 10

type personJournalScan struct {
	file  io.ReaderAt
	end   int64
	block [personJournalBlockBytes]byte
	line  []byte
	// A region is the surviving messages between compaction markers. Counting
	// backwards locates the candidate against Window without retaining an index.
	dropped, messages, candidateAfter int
	candidate                         time.Time
	// visited and retained measure work and BYTES, including the scratch capacity.
	visited, retained int
}

func (s *personJournalScan) latest() (time.Time, bool, error) {
	for s.end > 0 {
		start := max(int64(0), s.end-personJournalBlockBytes)
		n, err := s.file.ReadAt(s.block[:s.end-start], start)
		if err != nil && err != io.EOF {
			return time.Time{}, false, err
		}
		s.end = start
		for i := n - 1; i >= 0; i-- {
			if s.block[i] != '\n' {
				if len(s.line) >= journalLineMax {
					return time.Time{}, false, errors.New("session: journal line exceeds person query bound")
				}
				if len(s.line) == cap(s.line) {
					grown := make([]byte, len(s.line), min(journalLineMax, max(personJournalBlockBytes, cap(s.line)*2)))
					copy(grown, s.line)
					s.line = grown
				}
				s.line = append(s.line, s.block[i])
				s.retained = max(s.retained, cap(s.line)+len(s.block))
				continue
			}
			if at, done, err := s.visit(); done || err != nil {
				return at, !at.IsZero(), err
			}
		}
	}
	if at, done, err := s.visit(); done || err != nil {
		return at, !at.IsZero(), err
	}
	return s.result()
}

func (s *personJournalScan) visit() (time.Time, bool, error) {
	for i, j := 0, len(s.line)-1; i < j; i, j = i+1, j-1 {
		s.line[i], s.line[j] = s.line[j], s.line[i]
	}
	line := bytes.TrimSpace(s.line)
	defer func() { s.line = s.line[:0] }()
	if len(line) == 0 {
		return time.Time{}, false, nil
	}
	s.visited++
	// Content and tool payloads are deliberately absent: only a candidate person's
	// line and compaction marker need reconstruction through replay's rules.
	var mark struct {
		Type, Role string
		Dropped    int
	}
	if json.Unmarshal(line, &mark) != nil {
		return time.Time{}, false, nil
	}
	switch mark.Type {
	case "rewind":
		s.dropped += max(0, mark.Dropped)
	case "compaction":
		var entry sessionEntry
		if json.Unmarshal(line, &entry) != nil {
			return time.Time{}, false, nil
		}
		overlap := compactionOverlap(entry)
		// Display retains the region above a placed pass and omits its copy
		// below. Rewinds here affect only the rebuilt window, not that history.
		// Old markers can also synthesize prefix messages; count any survivors
		// exactly as replay does before locating the candidate against its floor.
		prefix := max(0, len(compactionMessages(entry, false))-s.dropped)
		if overlap < 0 || (s.candidateAfter > 0 && prefix+s.messages-s.candidateAfter >= overlap) {
			at, _, err := s.result()
			return at, true, err
		}
		s.dropped, s.messages, s.candidateAfter = 0, 0, 0
		s.candidate = time.Time{}
	case "message":
		if mark.Role == "" {
			return time.Time{}, false, nil
		}
		if s.dropped > 0 {
			s.dropped--
			return time.Time{}, false, nil
		}
		s.messages++
		if mark.Role != "user" || s.candidateAfter > 0 {
			return time.Time{}, false, nil
		}
		var entry sessionEntry
		if json.Unmarshal(line, &entry) != nil {
			return time.Time{}, false, nil
		}
		message := replayedMessage(entry, false)
		if displayMessageRole(message, entry.Note) == "user" && entryRows(message) > 0 {
			s.candidate, s.candidateAfter = lineInstant(entry.Timestamp), s.messages
		}
	}
	return time.Time{}, false, nil
}

// A copy's missing time is irrelevant. Only the surviving original must carry
// a readable time before recovery can compare it with an earlier promise.
func (s *personJournalScan) result() (time.Time, bool, error) {
	if s.candidateAfter == 0 {
		return time.Time{}, false, nil
	}
	if s.candidate.IsZero() {
		return time.Time{}, false, errors.New("session: person message has no readable timestamp")
	}
	return s.candidate, true, nil
}
