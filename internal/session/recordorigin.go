package session

import (
	"sort"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// recordUserLine is one physical address, indexed across Earlier then Entries.
// The sorted, private vector is immutable and shared by record leases.
type recordUserLine struct{ entry, line int }

// JournalLine is a user entry's physical address in the journal, or zero when
// unknown. Earlier selects Record.Earlier; otherwise index selects Entries.
// Origins stay on the record so a display entry costs the same live and replayed.
func (r Record) JournalLine(earlier bool, index int) int {
	entries := r.Entries
	offset := len(r.Earlier)
	if earlier {
		entries, offset = r.Earlier, 0
	}
	if index < 0 || index >= len(entries) {
		return 0
	}
	entry := offset + index
	at := sort.Search(len(r.userLines), func(i int) bool { return r.userLines[i].entry >= entry })
	if at == len(r.userLines) || r.userLines[at].entry != entry {
		return 0
	}
	return r.userLines[at].line
}

// recordUserLines translates the replay's message identities to display entry
// indexes through the shaper's own row count. Only user messages have a physical
// address, so tool batches and long turns add no provenance rows to the record.
func recordUserLines(replayed replayedSession) []recordUserLine {
	if len(replayed.userLines) == 0 {
		return nil
	}
	var lines []recordUserLine
	offset := 0
	add := func(messages []ai.Message) {
		for _, message := range messages {
			rows := entryRows(message)
			if line := replayed.userLines[stampKey(message)]; line > 0 && rows > 0 {
				lines = append(lines, recordUserLine{entry: offset, line: line})
			}
			offset += rows
		}
	}
	add(replayed.earlier)
	add(replayed.messages)
	return lines
}
