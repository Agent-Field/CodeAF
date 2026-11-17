package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE WATCHDOG'S TURN IS THE MANAGER'S ────────────────────────────────────
//
// The factory's service finds a job whose wait has gone wrong (internal/
// factory/serve's watchdog.go) and reports it to the job's manager, and to
// nobody else: the owner's decision of 2026-11-17. On this machine's factory
// that report is ONE turn of the manager's own conversation
// ([appFactoryHost.stalled], wired as serve's Config.Stalled), by the road
// the inbox's turn takes ([shapeTurns.through]): through the conversation
// when this process holds it open, else opened for the turn.
//
// THE TURN CARRIES THE MANAGER'S OWN DOOR, the one its conversation has when
// the person talks to it, so every tool it has there works here under the
// same rules, and the turn gains no power of its own. What moves the job is
// whatever the manager presses; the watchdog presses nothing. A turn in which
// the manager moves nothing leaves the stall where it was, and the service
// hands it to the person at its next look.

// stallTurnWait is how long the manager's turn on a stall may take: the wait
// the inbox's turn is given (internal/factory/run's shapeWaitDefault).
const stallTurnWait = time.Minute

// stallLogLines is how many of the job's last log lines the report carries.
const stallLogLines = 6

// The report's words, said once so the tests quote the same ones.
const (
	stallAsk      = "The factory's watchdog found this job stalled: %s"
	stallAskSeen  = "First seen %s ago; this is turn %d of %d on it."
	stallAskLog   = "Its log's last lines: %s"
	stallAskNoLog = "Its log has no lines."
	stallAskTail  = "Read factory_status for the job and its pull request as they stand now. If a tool you already have moves the job within its own rules, use it now and say in one sentence what you did. Never merge. Approve publication or mark the pull request ready only when factory_status says the job is in Auto; in Manual those are the person's. If nothing you may do moves it, say in one or two plain sentences what the person must decide: the job then waits on them, with your reason on its log."
	stallAskYours = "The job already waits on the person to decide; say only what has changed since your last look."
)

// errStallReadsOff is a stall on a factory that reads and runs nothing yet:
// no person turned its runner on, so it spends nothing on a manager's turn.
var errStallReadsOff = errors.New("this factory's runner is off, so its manager was not asked")

// stallAskFor is the stall report one manager turn is given: what tripped,
// the wait and offers the service stored, and the job's last log lines.
func stallAskFor(it factory.Item, st factory.Stall, now time.Time) string {
	parts := []string{fmt.Sprintf(stallAsk, oneLineWords(st.Words))}
	if !st.At.IsZero() {
		parts = append(parts, fmt.Sprintf(stallAskSeen, stallAge(now.Sub(st.At)), st.Turns+1, len(factory.StallBackoff)+1))
	}
	if line := factory.WaitLine(it); line != "" {
		parts = append(parts, line)
	}
	parts = append(parts, stallLog(it))
	parts = append(parts, stallAskTail)
	if st.GaveUp {
		parts = append(parts, stallAskYours)
	}
	return strings.Join(parts, " · ")
}

// stallLog is the job's last log lines on one line, oldest first.
func stallLog(it factory.Item) string {
	if it.Stream == nil || len(it.Stream.Log) == 0 {
		return stallAskNoLog
	}
	log := it.Stream.Log
	if len(log) > stallLogLines {
		log = log[len(log)-stallLogLines:]
	}
	lines := make([]string, 0, len(log))
	for _, line := range log {
		text := oneLineWords(line.Text)
		if r := []rune(text); len(r) > 200 {
			text = string(r[:200]) + "…"
		}
		lines = append(lines, line.At.UTC().Format("15:04")+" "+text)
	}
	return fmt.Sprintf(stallAskLog, strings.Join(lines, " | "))
}

// stallAge says how long ago in whole minutes, as a person would.
func stallAge(d time.Duration) string {
	minutes := int(d / time.Minute)
	switch {
	case minutes < 1:
		return "under a minute"
	case minutes == 1:
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", minutes)
}

// Stalled is the manager's one turn on a stall, with door as its tools for
// the turn. It answers what the manager said.
func (s *shapeTurns) Stalled(ctx context.Context, it factory.Item, st factory.Stall, door session.RunDoor) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, stallTurnWait)
	defer cancel()
	return s.through(ctx, it, stallAskFor(it, st, time.Now()), door)
}

// stalled is serve's Config.Stalled on the factory this host serves.
//
// THE TURN IS CHARGED TO THE JOB. A conversation this process holds open
// already spends through the job's chat door (factory_chatspend.go); one
// opened for the turn is opened from the home's own config, which carries no
// such door, so it is hung here. The dollars then go where the job's budget
// is checked, as every turn of the job's conversation does.
func (h *appFactoryHost) stalled(ctx context.Context, it factory.Item, st factory.Stall) (string, error) {
	if !h.readsOn() {
		return "", errStallReadsOff
	}
	source, raw, ok := h.parentSource()
	if !ok {
		return "", errors.New("this machine has no model settings to open the manager with")
	}
	current := func() session.Config {
		cfg := source.Config()
		if cfg.JobChat == nil {
			cfg.JobChat = jobChatDoorOf(h.floor)
		}
		return cfg
	}
	turns := factoryShapeTurnsFrom(h.floor, h.workspace, h.profile, current)
	turns.refusal = source.Refusal
	turns.liveOK = factoryLiveOK(raw, h.profile)
	return turns.Stalled(ctx, it, st, runDoor(h.floor, h.workspace, h.profile))
}

// readsOn says this host's reads are running: its GitHub poll, and with it
// the runner a person turned on. A quiet host spends nothing by itself.
func (h *appFactoryHost) readsOn() bool {
	h.loopMu.Lock()
	defer h.loopMu.Unlock()
	return h.loopsOn && !h.loopsClosed
}
