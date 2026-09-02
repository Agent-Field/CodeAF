package tui3

// ── A JOB'S PAGE IS ITS LOG, WHILE THE JOB IS STILL WRITING IT ──────────────
//
// A background job — a server, a build, a watch, a render — has no lane to
// subscribe to, no journal to replay, and no transcript it could ever grow one
// from. What it has is a FILE. The notice carries the path ([session.JobNotice.LogPath]),
// and everything the work has done is in there.
//
// THE LAW THIS FILE EXISTS FOR: the page reads that file under one discipline,
// the same one the far room's journal tail is read under and deliberately not a
// second one. A bounded reading, off the program loop, on [farRoomTick]'s beat —
// four times a second, because a file on a disk is a reading and not an
// animation. The last lines are drawn dim, in the order the process wrote them,
// newest at the bottom.
//
// THE NOTICE SAYS WHEN TO STOP, and one reading follows the stop: a process's
// last lines reach the file after the notice that watches it has settled, so a
// reader that quit on the landing would cut off exactly the ending somebody
// opened the page for.
//
// A HOSTED SESSION HAS NO PATH HERE, and that is a law rather than a limitation.
// The path on the notice belongs to the ENGINE'S machine; opening it on this
// disk finds either nothing or a stranger's file, which is precisely the fault
// the far card's own reader was written to avoid (taskcardhost_test.go pins it
// with a real file at the far path). The page says so rather than drawing a
// false empty log.

import (
	"bytes"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

const (
	// jobLogTailLines is how many of the log's last lines the page keeps. It is
	// a TAIL and not the file: a build's log is megabytes, the page scrolls, and
	// what a person opens a running job for is what it is doing now.
	jobLogTailLines = 200
	// jobLogTailBytes is the most one reading takes off the disk, read from the
	// END of the file. It is the cap that makes the beat safe on a log that grows
	// without bound — four readings a second of a two-gigabyte file is not a
	// surface, it is a disk — and it is generous enough that the line cap above
	// is what actually decides the page.
	jobLogTailBytes = 256 * 1024
)

// jobLogMsg is one reading of a job's log, coming back off the loop. An
// unreadable file is not an error here and carries no error field: the log is
// EVIDENCE and not a prerequisite, exactly as a node's journal is
// ([readRoomJournal] says so), and a page that drew a failure over healthy work
// would be the emptiness law broken in the loudest possible way.
type jobLogMsg struct {
	gen   int
	lines []string
}

// jobPagePath is the log this page is a reading of, or "" when there is none
// to open on this disk.
//
// A HOSTED SESSION HAS NO PATH HERE. The path on the notice belongs to the
// ENGINE'S machine; opening it on this disk finds either nothing or a stranger's
// file. The page says that rather than drawing the miss as an empty log.
func (a *app) jobPagePath(job *session.JobNotice) string {
	if job == nil || a.hosted() {
		return ""
	}
	return strings.TrimSpace(job.LogPath)
}

// jobPageArm starts the log's first reading for the open page. It is the door
// the column takes after [app.showJobPage]: that call only records the id
// (jobstate.go), and this is what actually opens the file.
func (a *app) jobPageArm() tea.Cmd {
	view := a.jobView()
	if view == nil {
		return nil
	}
	job := a.jobPageJob()
	if job == nil {
		return nil
	}
	view.gen++
	view.log = nil
	view.last = false
	view.top = 0
	view.path = a.jobPagePath(job)
	return a.jobPagePoll(view.gen)
}

// jobPagePoll is one bounded reading, as a command. The generation is checked
// here rather than in the reader so that a beat scheduled for a page somebody
// has already left dies where it was scheduled.
func (a *app) jobPagePoll(gen int) tea.Cmd {
	view := a.jobView()
	if view == nil || view.gen != gen || view.path == "" {
		return nil
	}
	path := view.path
	return func() tea.Msg {
		return jobLogMsg{gen: gen, lines: readJobLogTail(path)}
	}
}

// jobPageRead folds one reading onto the page and decides whether there is
// another one owed.
//
// THE NOTICE IS WHAT SAYS THE WORK IS OVER — never the file, which stops
// growing for a process that is merely quiet. So the beat re-arms while the
// job is still running, and one last reading follows the landing.
func (a *app) jobPageRead(msg jobLogMsg) tea.Cmd {
	view := a.jobView()
	if view == nil || view.gen != msg.gen {
		return nil
	}
	view.log = msg.lines
	a.touch()
	job := a.jobPageJob()
	if job != nil && !job.Over() {
		return farRoomTick(view.gen)
	}
	// ONE READING AFTER THE LANDING, and then the reader stops. A process writes
	// its last lines and exits, and the notice lands on the exit — so the reading
	// taken at the instant of the landing is the one reading guaranteed to be
	// short of the ending.
	if view.last {
		return nil
	}
	view.last = true
	return farRoomTick(view.gen)
}

// readJobLogTail is the last lines of one file, bounded at both ends: at most
// [jobLogTailBytes] off the disk and at most [jobLogTailLines] out of it.
//
// A MISSING, EMPTY OR UNREADABLE FILE ANSWERS NOTHING AT ALL, which is the
// emptiness law over a reading rather than over a number: the page then draws
// itself with no tail, and never an error block over work that is going
// perfectly well. A job that has not written its first line yet is the ordinary
// case for the first second of every one of them.
func readJobLogTail(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return nil
	}
	at := int64(0)
	if info.Size() > jobLogTailBytes {
		at = info.Size() - jobLogTailBytes
	}
	if _, err := file.Seek(at, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(file, jobLogTailBytes))
	if err != nil {
		return nil
	}
	// A READING THAT STARTED MID-FILE STARTED MID-LINE, and half a line drawn as
	// a whole one is the page inventing output the process never wrote. The first
	// break is where the truth resumes; a window with no break in it at all is
	// one enormous line and there is nothing honest to take from it.
	if at > 0 {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			return nil
		}
		data = data[nl+1:]
	}
	var out []string
	for _, raw := range strings.Split(string(data), "\n") {
		line := jobLogLine(raw)
		if line == "" {
			// A BLANK LINE IS NOT NEWS. Spacing on this surface is the surface's
			// own (render.go), and a log that separates its stanzas with blanks
			// would otherwise spend half the page on them.
			continue
		}
		out = append(out, line)
	}
	if len(out) > jobLogTailLines {
		out = out[len(out)-jobLogTailLines:]
	}
	return out
}

// jobLogLine is one line of somebody else's output made safe to draw.
//
// A JOB'S LOG IS NOT THIS SURFACE'S TEXT. It is whatever a compiler, a server or
// a renderer wrote to a pipe, escape sequences and carriage returns and all —
// and an escape sequence drawn into the frame does not merely look wrong, it
// repaints rows this surface owns. That rule is [drawableLine]'s now, because a
// tool call's own arguments and results arrive with exactly the same bytes in
// them and were being drawn raw; what is left here is the one thing a log wants
// on top of it, which is that a line padded out by whatever wrote it does not
// carry its padding onto the page.
func jobLogLine(raw string) string {
	return strings.TrimRight(drawableLine(raw), " ")
}
