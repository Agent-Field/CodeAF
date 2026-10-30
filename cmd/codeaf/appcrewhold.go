package main

// ── a member the crew could not open ────────────────────────────────────────
//
// Every member of an open crew is held (appcrews.go's holdMembers), because a
// member nobody holds is a member whose questions reach nobody: its permission
// prompt is its own ask, never a packet its manager could take, and the app
// hears an ask only on the question lane of a conversation it holds. A member
// whose conversation would not open — another codeaf window holds it and the
// app cannot reach that window — once left no trace at all, while its manager
// told the person it was blocked on their approval.
//
// SO A HOLD THAT FAILS IS NEVER SILENT, and it is said ONCE for a run of
// failures: a line on stderr, where the app collects logs; a warn `log` event;
// a quiet notice in the manager's chat, the crew's home, in words a person can
// act on. And the member's row reads failed with the reason as its line for as
// long as it lasts. It is tried again on a widening wait, at once when the
// crew's log is read (the crew being opened), and whenever the person opens
// the member themselves, so a lock that frees later recovers by itself; the
// recovery is said once too.
//
// WHAT IS SAID IS SAID ON A LATER LOOK THAN THE FIRST. The crews are first
// looked at as the app starts, before its hello is on the wire, and nothing
// may go out ahead of the hello; a hold that fails on that look is said on the
// next one, a poll later.
//
// A conversation that is not on this machine at all is waited on the same way
// and never said: nothing here could raise its questions.
//
// NOR IS A HOLD THAT FAILED ONLY FOR WANT OF A KEY. The crews are first looked
// at as the app starts, and on every launch that look comes before the app
// has handed the keys over (keys.set), so every member
// fails to open with the missing-key refusal for a moment. That is the launch,
// not a member the person must do something about: it is not said, the row is
// not marked, it is not counted towards the widening wait, and it is tried
// again on the next poll, by which time the key is here.

import (
	"errors"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/redact"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// appHoldRetryMax is the longest a member the crew could not open waits
// before it is tried again; the wait starts at one crew poll and doubles.
const appHoldRetryMax = time.Minute

// appUnheldLead opens the line a member's row reads while it cannot be opened.
const appUnheldLead = "can't open here: "

// appUnheld is one member the crew could not hold: why, how many times it was
// tried, when it is tried next, whether it is worth saying at all, whether the
// person was told, and, once it is held after all, that the person is owed the
// news.
type appUnheld struct {
	reason string
	tries  int
	next   time.Time
	quiet  bool
	said   bool
	healed bool
}

// appCrewWarn writes one line to stderr, where the app collects logs. A var so
// a test can read what was written.
var appCrewWarn = func(format string, args ...any) { log.Printf(format, args...) }

// holdDueLocked says whether a member may be tried now. Callers hold run.mu.
func (run *appCrewRun) holdDueLocked(agent string, now time.Time) bool {
	u := run.unheld[agent]
	return u == nil || u.healed || !now.Before(u.next)
}

// appHoldReason is why a hold failed, in the words the person reads, on one
// line: a wire error's own sentence, anything else with the machinery taken
// off ([plainWords]), and no closing full stop, since the notice goes on.
func appHoldReason(err error) string {
	var reason string
	var wire *appwire.Error
	if errors.As(err, &wire) {
		reason = wire.Message
	} else {
		reason = plainWords(err.Error())
	}
	reason = strings.TrimRight(strings.Join(strings.Fields(redact.Secrets(reason)), " "), ".")
	if reason == "" {
		reason = "its conversation would not open"
	}
	return reason
}

// appWaitingForKey says whether a hold failed only because no key has been
// handed over yet ([config.ErrNoAPIKey]). The refusal reaches here either as
// itself or as the wire error the app's opener made of it, which says it in the
// app's words, so it is read the one way the app's door reads it
// ([appMissingKey]).
func appWaitingForKey(err error) bool {
	return appMissingKey(err)
}

// appMemberWho is how the person knows a member: its handle, else its title.
func appMemberWho(m teamstore.Member) string {
	if h := strings.TrimPrefix(strings.TrimSpace(m.Handle), "@"); h != "" {
		return "@" + h
	}
	if w := strings.TrimSpace(m.Word); w != "" {
		return w
	}
	return "a member"
}

// holdFailed records one failed hold and sets when the member is tried again.
// quiet is a conversation that is not on this machine, which is never said.
func (b *appBackend) holdFailed(run *appCrewRun, agent string, err error, quiet bool) {
	if b.isClosing() {
		return
	}
	reason := appHoldReason(err)
	now := b.now()
	run.locked(func() {
		if run.unheld == nil {
			run.unheld = map[string]*appUnheld{}
		}
		u := run.unheld[agent]
		if u == nil || u.healed {
			u = &appUnheld{}
			run.unheld[agent] = u
		}
		if appWaitingForKey(err) {
			// The launch, not the member: quiet, uncounted, tried on the next poll.
			u.reason, u.quiet = reason, true
			u.next = now.Add(appCrewPoll)
			return
		}
		u.reason, u.quiet = reason, quiet
		u.tries++
		u.next = now.Add(min(appCrewPoll<<min(u.tries-1, 6), appHoldRetryMax))
	})
}

// holdRecovered marks a member the crew could not hold as held after all; the
// news is said on the next look when the failure had been, and otherwise the
// record is simply let go.
func (b *appBackend) holdRecovered(run *appCrewRun, agent string) {
	run.locked(func() {
		u := run.unheld[agent]
		switch {
		case u == nil:
		case u.said:
			u.healed = true
		default:
			delete(run.unheld, agent)
		}
	})
}

// appHoldNews is one thing a look owes the person about a member's hold.
type appHoldNews struct {
	agent, reason string
	healed        bool
}

// sayHolds says every failed hold not yet said and every recovery owed, and
// forgets what no open crew holds any more.
func (b *appBackend) sayHolds(run *appCrewRun) {
	var news []appHoldNews
	run.locked(func() {
		for agent, u := range run.unheld {
			switch {
			case len(run.held[agent]) == 0:
				delete(run.unheld, agent)
			case u.healed:
				news = append(news, appHoldNews{agent: agent, healed: true})
				delete(run.unheld, agent)
			case !u.said && !u.quiet:
				u.said = true
				news = append(news, appHoldNews{agent: agent, reason: u.reason})
			}
		}
	})
	if len(news) == 0 {
		return
	}
	sort.Slice(news, func(i, j int) bool { return news[i].agent < news[j].agent })
	for _, n := range news {
		who, crew := "a member", "its crew"
		var manager teamstore.Member
		seated := false
		if t, m, ok := b.crewSeatOf(n.agent); ok {
			who, crew = appMemberWho(m), t.Name
			manager, seated = t.Member(t.Manager)
		}
		if n.healed {
			appCrewWarn("codeaf app: crew member %s (agent %s) of %q is open again", who, n.agent, crew)
			b.emit.Emit(appwire.EventLog, appwire.Log{Level: "info", Message: who + " in " + crew + " is open again"})
			if seated {
				b.crewNote(manager, who+" is open here again; its questions reach you now.")
			}
			continue
		}
		appCrewWarn("codeaf app: could not open crew member %s (agent %s) of %q: %s", who, n.agent, crew, n.reason)
		b.emit.Emit(appwire.EventLog, appwire.Log{Level: "warn", Message: who + " in " + crew + " could not be opened: " + n.reason})
		if seated {
			b.crewNote(manager, who+" could not be opened here, so its questions cannot reach you: "+n.reason+". codeaf keeps trying, and tries again when you open "+crew+".")
		}
	}
}

// unheldLine is the line a member's row reads while the crew cannot open it.
func (b *appBackend) unheldLine(agent string) (string, bool) {
	var run *appCrewRun
	b.locked(func() { run = b.crewRun })
	if run == nil || b.isHeld(agent) {
		return "", false
	}
	line, unheld := "", false
	run.locked(func() {
		if u := run.unheld[agent]; u != nil && !u.quiet && !u.healed {
			line, unheld = appUnheldLead+u.reason, true
		}
	})
	return line, unheld
}

// retryHolds tries every member of a crew that could not be opened again now.
func (b *appBackend) retryHolds(crew string) {
	var run *appCrewRun
	b.locked(func() { run = b.crewRun })
	if run == nil {
		return
	}
	due := map[string][]string{}
	run.locked(func() {
		for agent, u := range run.unheld {
			for _, id := range run.held[agent] {
				if id == crew {
					u.next = time.Time{}
					due[agent] = run.held[agent]
				}
			}
		}
	})
	if len(due) > 0 {
		b.holdMembers(run, due)
	}
}
