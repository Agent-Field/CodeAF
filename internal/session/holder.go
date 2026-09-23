package session

// holder.go is the last step of moving a conversation here: naming the window
// that will not let go, and — when the person says so — asking that process to
// stop.
//
// ── THE MOVE PROTOCOL, AND WHY IT IS FROZEN ─────────────────────────────────
//
// Moving a conversation between two windows on one machine is spoken through
// three files in the conversation's own folder, and nothing else:
//
//	transcript.jsonl  held under an exclusive flock by the one process writing
//	                  it (sessionfile.go). The kernel drops a flock when its
//	                  process dies, so A DEAD HOLDER HOLDS NOTHING and the next
//	                  window simply opens it.
//	presence.json     the holder's own record of itself, refreshed every few
//	                  seconds (taskpresence.go): its pid, its build, what it is
//	                  doing and when it last said so.
//	takeover.json     the request, `{"at": <RFC 3339 time>}`, written by the
//	                  window that wants the conversation (takeover.go). Every
//	                  holder since 2026-08-31 looks for it four times a second,
//	                  lets go — stopping a turn where it is and keeping its
//	                  partial reply — and says so in its own window.
//
// THE COMPATIBILITY RULE: those three names, the flock on the transcript, the
// request's one field and presence.json's `schema: 1` fields `pid`, `build`,
// `state` and `updatedAt` are a protocol between BUILDS, not between two copies
// of this source. A later build may add fields to either file and must never
// rename, remove or retype these; a change that has to is a new file name, read
// beside this one for as long as a build that only speaks this one can still be
// running. holder_test.go pins the bytes.
//
// ── AND THE ONE STEP THAT DOES NOT DEPEND ON THE HOLDER'S CODE ──────────────
//
// A holder that is wedged — its surface stuck behind a worker, its build older
// than the request, its window on a screen nobody can reach — never reads the
// request. On 2026-09-23 an older window held about ten conversations until it
// was sent SIGTERM and its worker was stopped by hand. So after a few seconds
// with no answer the asking window names the holder from presence.json (pid,
// terminal, build) and offers to stop it; [StopHolder] is that offer taken. It
// sends SIGTERM, which every build of this program answers by closing its
// conversations and flushing their journals, and a second SIGTERM, which every
// build since the leave road answers by exiting at once.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Holder is the process holding one conversation's journal, as its own
// presence.json last described it.
type Holder struct {
	PID       int
	Build     string
	State     PresenceState
	UpdatedAt time.Time
	// TTY is the holder's terminal, read off the process table when the
	// holder is read, and "" when the machine will not say. It is what tells
	// a person with six terminals open WHICH window this is.
	TTY string
}

// ReadHolder is the holder of the conversation in sessionDir, from its presence
// file, WHETHER OR NOT THAT FILE IS FRESH. Freshness is the right test for "is
// this conversation alive" (the lock answers that); it is the wrong test here,
// because a wedged holder is exactly one whose heartbeat may have stopped while
// its flock is still held. A record older than [TakeoverStale] is refused all
// the same: a pid that old is too likely to have been reused.
func ReadHolder(sessionDir string, now time.Time) (Holder, bool) {
	dir := strings.TrimSpace(sessionDir)
	if dir == "" {
		return Holder{}, false
	}
	presence, ok := readPresenceAnyAge(dir)
	if !ok || presence.PID <= 0 || presence.UpdatedAt.IsZero() || now.Sub(presence.UpdatedAt) > TakeoverStale {
		return Holder{}, false
	}
	return Holder{
		PID:       presence.PID,
		Build:     strings.TrimSpace(presence.Build),
		State:     presence.State,
		UpdatedAt: presence.UpdatedAt,
		TTY:       processTTY(presence.PID),
	}, true
}

// readPresenceAnyAge is [ReadSessionPresence] without the freshness test.
func readPresenceAnyAge(dir string) (SessionPresence, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, presenceName))
	if err != nil {
		return SessionPresence{}, false
	}
	var presence SessionPresence
	if json.Unmarshal(raw, &presence) != nil || presence.Schema != presenceSchema {
		return SessionPresence{}, false
	}
	presence.Dir = dir
	return presence, true
}

// Words is the holder named the way a person reads it: `pid 58673 · ttys004 ·
// a1b2c3d4 built 2026-09-21 09:00`, each part only when it is known.
func (h Holder) Words() string {
	var parts []string
	if h.PID > 0 {
		parts = append(parts, "pid "+strconv.Itoa(h.PID))
	}
	if tty := strings.TrimSpace(h.TTY); tty != "" && tty != "?" && tty != "??" {
		parts = append(parts, tty)
	}
	if build := strings.TrimSpace(h.Build); build != "" {
		parts = append(parts, build)
	}
	return strings.Join(parts, " · ")
}

// ErrHolderUnknown is a conversation whose holder cannot be named: no presence
// record, one too old to trust, or a lock nobody is holding any more.
var ErrHolderUnknown = errors.New("the window holding this conversation cannot be named")

// StopHolder asks the process holding the conversation in sessionDir to stop.
// The first call sends SIGTERM — the ordinary leaving road, which closes every
// conversation that window holds and flushes its journals; a second call, when
// the first did not free the lock, sends the second one, which every build since the leave road
// answers by exiting at once.
//
// IT SIGNALS ONLY A PROCESS IT CAN NAME AND THAT IS STILL HOLDING THE LOCK. The
// pid comes from the holder's own presence record, the journal's flock must
// still be held (a free lock is a holder that already went), and the process
// asking is never its own target. It answers the pid it signalled.
func StopHolder(sessionDir, transcript string, now time.Time) (int, error) {
	if !InUse(transcript) {
		return 0, ErrHolderUnknown
	}
	holder, ok := ReadHolder(sessionDir, now)
	if !ok {
		return 0, ErrHolderUnknown
	}
	if holder.PID <= 1 || holder.PID == os.Getpid() {
		return 0, fmt.Errorf("the window holding this conversation is this one")
	}
	if err := stopProcess(holder.PID); err != nil {
		return 0, err
	}
	return holder.PID, nil
}

// processTTY is a process's terminal as the process table spells it, and ""
// when there is none or the machine will not say.
func processTTY(pid int) string {
	if pid <= 0 || runtime.GOOS == "windows" {
		return ""
	}
	out, err := psField(pid, "tty=")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
