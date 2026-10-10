package desktopbridge

// snapshotview.go shapes a Snapshot for the wire: a header without the
// transcript, a tail of entries after a count the window already holds, and
// tool outputs elided past a cap. The window fetches an elided output from
// /tools/{callId}. Nothing here touches the conversation; it only reads a
// Snapshot value, so the route and the ring (bridge.go) stay thin.

import "github.com/Agent-Field/codeaf/internal/session"

// outputCap is the largest tool Output a snapshot carries inline. Past it the
// payload travels on demand, so a long transcript does not re-send megabytes
// of build logs on every refresh.
const outputCap = 16 << 10

// SnapshotHeader is every Snapshot field except the transcript, plus how many
// entries the transcript holds, so a window can tell whether it is behind.
type SnapshotHeader struct {
	ID             string                `json:"id"`
	SessionFile    string                `json:"sessionFile"`
	Workspace      string                `json:"workspace"`
	Model          string                `json:"model"`
	Persistent     bool                  `json:"persistent"`
	Running        bool                  `json:"running"`
	NeedsPerson    bool                  `json:"needsPerson"`
	Questions      []session.Question    `json:"questions"`
	RecentOutcomes []OutcomeWire         `json:"recentOutcomes,omitempty"`
	PlanError      string                `json:"planError,omitempty"`
	Title          string                `json:"title"`
	Tasks          []session.PlanTaskRow `json:"tasks"`
	Usage          session.Usage         `json:"usage"`
	Queue          []QueuedWire          `json:"queue"`
	UpdatedAt      string                `json:"updatedAt,omitempty"`
	Seq            uint64                `json:"seq"`
	WorkingFolder  *WorkingFolder        `json:"workingFolder,omitempty"`
	EntryCount     int                   `json:"entryCount"`
}

// EntryWire is one transcript entry as it travels. An elided Output is empty
// and flagged, with its true size, so "omitted" is never confused with "the
// call printed nothing".
type EntryWire struct {
	session.DisplayEntry
	OutputOmitted bool `json:"OutputOmitted,omitempty"`
	OutputBytes   int  `json:"OutputBytes,omitempty"`
}

// SnapshotTail answers GET /sessions/{id}?since=N: the header, the index the
// entries start at, and the entries. Reset says the window must discard what
// it holds and take these entries as the whole transcript.
type SnapshotTail struct {
	Header  SnapshotHeader `json:"header"`
	From    int            `json:"from"`
	Entries []EntryWire    `json:"entries"`
	Reset   bool           `json:"reset,omitempty"`
}

// headerOf copies the non-transcript fields of a snapshot.
func headerOf(s Snapshot) SnapshotHeader {
	return SnapshotHeader{
		ID: s.ID, SessionFile: s.SessionFile, Workspace: s.Workspace, Model: s.Model,
		Persistent: s.Persistent, Running: s.Running, NeedsPerson: s.NeedsPerson,
		Questions: s.Questions, RecentOutcomes: s.RecentOutcomes, PlanError: s.PlanError,
		Title: s.Title, Tasks: s.Tasks, Usage: s.Usage, Queue: s.Queue,
		UpdatedAt: s.UpdatedAt, Seq: s.Seq, WorkingFolder: s.WorkingFolder,
		EntryCount: len(s.Entries),
	}
}

// tailOf returns the entries after the first `since`. A since beyond the
// transcript (or negative) means the transcript was rewritten under the window
// — compaction shrinks it — so the answer is everything, flagged reset.
func tailOf(s Snapshot, since int) SnapshotTail {
	n := len(s.Entries)
	if since < 0 || since > n {
		return SnapshotTail{Header: headerOf(s), From: 0, Entries: elideOutputs(s.Entries, outputCap), Reset: true}
	}
	return SnapshotTail{Header: headerOf(s), From: since, Entries: elideOutputs(s.Entries[since:], outputCap)}
}

// elideOutputs wraps entries for the wire, emptying any Output over cap bytes.
// The input is never modified: the conversation's own entries stay whole.
func elideOutputs(entries []session.DisplayEntry, cap int) []EntryWire {
	out := make([]EntryWire, len(entries))
	for i, e := range entries {
		w := EntryWire{DisplayEntry: e}
		if len(e.Output) > cap {
			w.OutputBytes = len(e.Output)
			w.OutputOmitted = true
			w.Output = ""
		}
		out[i] = w
	}
	return out
}

// elidedSnapshot is today's full-snapshot body with outputs elided. Entries
// is a distinct field name so the JSON keeps the "Entries" key the window
// already reads.
type elidedSnapshot struct {
	SnapshotHeader
	Entries []EntryWire `json:"entries"`
}

// fullView is the answer without ?since.
func fullView(s Snapshot) elidedSnapshot {
	return elidedSnapshot{SnapshotHeader: headerOf(s), Entries: elideOutputs(s.Entries, outputCap)}
}
