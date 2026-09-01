package tui3

// ── A BACKGROUND JOB'S PAGE IS ITS LOG, WHILE THE JOB IS STILL WRITING IT ────
//
// A background job — a server, a build, a watch, a render — is a roster row and
// never a node in the engine's graph (internal/session's jobrow.go: "it
// registers and it does not admit"), so it has no lane to subscribe to, no
// journal to replay, and no transcript it could ever grow one from. What it has
// is a FILE. Its report names the path, the roster's row draws that path
// ([app.railJobLog]), and everything the work has done is in there.
//
// AND THE PAGE USED TO STOP THERE. A person who walked into a job's room got the
// path, one line saying a job keeps a log, and — because the door's refusal was
// read as "this work is over" — the landed foot, over a process that was
// thirty-two seconds into running. Three dead rows under a header whose clock
// was still counting up.
//
// So the page reads the file. It is the same discipline the far room's journal
// tail is read under and deliberately not a second one: a bounded reading, off
// the program loop, on [farRoomTick]'s beat — four times a second, because a
// file on a disk is a reading and not an animation. The last lines are drawn
// dim, in the order the process wrote them, newest at the bottom, under the path
// the row already carries.
//
// THE ROW SAYS WHEN TO STOP, and one reading follows the stop: a process's last
// lines reach the file after the row that watches it has landed, so a reader
// that quit on the landing would cut off exactly the ending somebody opened the
// page for.

import (
	"bytes"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

const (
	// THE FOOT UNDER A JOB THAT IS STILL WRITING is roomrefusal.go's
	// [roomJobRefusal], which is the running half of the sentence
	// [roomFinishedRefusal] says about work that is over. Neither names esc any
	// more — the legend and the focus header both carry it for the whole of a
	// room's life — and both name where the words in the box can go instead.
	//
	// It says THE LOG rather than the job, because the log is what the page is:
	// a job has nobody in it to narrate, and "the process is running" is the one
	// thing the header's own clock has been saying all along.
	// jobReportLogSep is how a job's report joins its handle to its path
	// (session's jobrow.go mints `job 3 · log /…/3.log`). It is a constant rather
	// than three literals because two readers already cut on it — the hosted
	// row's path marker and this file's reader — and a separator spelled twice is
	// a separator that drifts.
	jobReportLogSep = " · log "
	// roomJobTailLines is how many of the log's last lines the page keeps. It is
	// a TAIL and not the file: a build's log is megabytes, the page scrolls, and
	// what a person opens a running job for is what it is doing now.
	roomJobTailLines = 200
	// roomJobTailBytes is the most one reading takes off the disk, read from the
	// END of the file. It is the cap that makes the beat safe on a log that grows
	// without bound — four readings a second of a two-gigabyte file is not a
	// surface, it is a disk — and it is generous enough that the line cap above
	// is what actually decides the page.
	roomJobTailBytes = 256 * 1024
)

// roomJobLogMsg is one reading of a job's log, coming back off the loop. An
// unreadable file is not an error here and carries no error field: the log is
// EVIDENCE and not a prerequisite, exactly as a node's journal is
// ([readRoomJournal] says so), and a page that drew a failure over healthy work
// would be the emptiness law broken in the loudest possible way.
type roomJobLogMsg struct {
	gen   int
	lines []string
}

// roomJobPath is the log this room is a page for, or "" for a room that is not
// a job's.
//
// A HOSTED SESSION HAS NO PATH HERE, and that is a law rather than a limitation.
// The path on the row belongs to the ENGINE'S machine; opening it on this disk
// finds either nothing or a stranger's file, which is precisely the fault the
// far card's own reader was written to avoid (taskcardhost_test.go pins it with
// a real file at the far path). A hosted job's page draws what it always drew.
func (a *app) roomJobPath(node *taskNode) string {
	if node == nil || node.kind != session.TaskKindJob || a.hosted() {
		return ""
	}
	_, path, ok := strings.Cut(node.report, jobReportLogSep)
	if !ok {
		return ""
	}
	return strings.TrimSpace(path)
}

// roomJobOpen arms a job room's reader: the path it will read, and the first
// reading. It hands back nil for every room that is not a local job's, which
// leaves the page exactly as it was.
func (a *app) roomJobOpen() tea.Cmd {
	room := a.room
	if room == nil {
		return nil
	}
	room.jobLogPath = a.roomJobPath(a.tasks[room.id])
	return a.roomJobPoll(room.gen)
}

// roomJobPoll is one bounded reading, as a command. The generation is checked
// here rather than in the reader so that a beat scheduled for a page somebody
// has already left dies where it was scheduled.
func (a *app) roomJobPoll(gen int) tea.Cmd {
	room := a.room
	if room == nil || room.gen != gen || room.jobLogPath == "" {
		return nil
	}
	path := room.jobLogPath
	return func() tea.Msg {
		return roomJobLogMsg{gen: gen, lines: readJobLogTail(path)}
	}
}

// roomJobRead folds one reading onto the page and decides whether there is
// another one owed.
//
// THE ROW IS WHAT SAYS THE WORK IS OVER — never the door, which refuses a job's
// id on a perfectly healthy session (room.go's [app.openRoom] says why), and
// never the file, which stops growing for a process that is merely quiet. So the
// beat re-arms while the roster's row is queued or running, and the page's foot
// follows the same fact.
func (a *app) roomJobRead(msg roomJobLogMsg) tea.Cmd {
	room := a.room
	if room == nil || room.gen != msg.gen {
		return nil
	}
	room.jobLog = msg.lines
	a.roomTouched()
	if !roomRowDone(a.roomNode()) {
		return farRoomTick(room.gen)
	}
	room.done = true
	// ONE READING AFTER THE LANDING, and then the reader stops. A process writes
	// its last lines and exits, and the row lands on the exit — so the reading
	// taken at the instant of the landing is the one reading guaranteed to be
	// short of the ending.
	if room.jobLogLast {
		return nil
	}
	room.jobLogLast = true
	return farRoomTick(room.gen)
}

// roomJobLogRows is the tail as the page draws it: dim, cut rather than wrapped,
// in the order the process wrote it.
//
// IT CUTS FOR THE REASON THE ROW ABOVE IT CUTS ([app.railJobLog]). A log line is
// machine output and its left end is the part that identifies it; one wrapped
// stack trace would spend the whole page on a line nobody chose to read, and the
// page a person came for is the last thirty lines rather than the widest one.
func (a *app) roomJobLogRows(out []row, width int) []row {
	if a.room == nil {
		return out
	}
	for _, line := range a.room.jobLog {
		out = append(out, row{text: a.pal.dim(fit(line, width)), entry: -1})
	}
	return out
}

// readJobLogTail is the last lines of one file, bounded at both ends: at most
// [roomJobTailBytes] off the disk and at most [roomJobTailLines] out of it.
//
// A MISSING, EMPTY OR UNREADABLE FILE ANSWERS NOTHING AT ALL, which is the
// emptiness law over a reading rather than over a number: the page then draws
// the line it has always drawn about what a job keeps, and never an error block
// over work that is going perfectly well. A job that has not written its first
// line yet is the ordinary case for the first second of every one of them.
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
	if info.Size() > roomJobTailBytes {
		at = info.Size() - roomJobTailBytes
	}
	if _, err := file.Seek(at, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(file, roomJobTailBytes))
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
	if len(out) > roomJobTailLines {
		out = out[len(out)-roomJobTailLines:]
	}
	return out
}

// jobLogLine is one line of somebody else's output made safe to draw.
//
// A JOB'S LOG IS NOT THIS SURFACE'S TEXT. It is whatever a compiler, a server or
// a renderer wrote to a pipe, escape sequences and carriage returns and all —
// and an escape sequence drawn into the frame does not merely look wrong, it
// repaints rows this surface owns. So the colour is stripped, the tabs become
// the four spaces every other reader on this surface gives them
// ([expandTabs]), and the control bytes are dropped.
func jobLogLine(raw string) string {
	line := expandTabs(ansi.Strip(raw))
	line = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, line)
	return strings.TrimRight(line, " ")
}
