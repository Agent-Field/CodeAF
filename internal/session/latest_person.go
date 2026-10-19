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
// Compaction's original region remains history only when its window is recorded.
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
	// visited and retained measure work and BYTES, including the scratch capacity.
	visited, retained int
}

func (s *personJournalScan) latest() (time.Time, bool, error) {
	dropped := 0
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
			if at, done, err := s.visit(&dropped); done || err != nil {
				return at, !at.IsZero(), err
			}
		}
	}
	at, _, err := s.visit(&dropped)
	return at, !at.IsZero(), err
}

func (s *personJournalScan) visit(dropped *int) (time.Time, bool, error) {
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
	// line needs reconstruction through the session's authorship rules.
	var mark struct {
		Type, Role      string
		Dropped, Window int
	}
	if json.Unmarshal(line, &mark) != nil {
		return time.Time{}, false, nil
	}
	switch mark.Type {
	case "rewind":
		*dropped += max(0, mark.Dropped)
	case "compaction":
		*dropped = 0
		if mark.Window <= 0 {
			return time.Time{}, true, nil
		}
	case "message":
		if mark.Role == "" {
			return time.Time{}, false, nil
		}
		if *dropped > 0 {
			*dropped--
			return time.Time{}, false, nil
		}
		if mark.Role != "user" {
			return time.Time{}, false, nil
		}
		var entry sessionEntry
		if json.Unmarshal(line, &entry) != nil {
			return time.Time{}, false, nil
		}
		message := replayedMessage(entry, false)
		if displayMessageRole(message, entry.Note) == "user" && entryRows(message) > 0 {
			at := lineInstant(entry.Timestamp)
			if at.IsZero() {
				return time.Time{}, false, errors.New("session: person message has no readable timestamp")
			}
			return at, true, nil
		}
	}
	return time.Time{}, false, nil
}
