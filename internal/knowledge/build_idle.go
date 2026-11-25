package knowledge

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// A CHAT IS COMPILED ONCE IT HAS GONE QUIET, NOT WHILE IT IS BEING TYPED IN.
//
// The build loop used to compile whatever a pass had found, and a pass followed
// any pass that found something new [buildEvery] later. A conversation being
// worked in grows on every turn, so it was compiled in two-second slices while
// it was still being written, and every slice paid for the vault-wide learning
// steps again (compiler.go: extendLearning, refineLearning, curate). Live lane
// E6b measured it: 42 compiler passes and 175 calls across twelve chats, $0.654
// of a $0.96 session, still running minutes after the last request. Pass count,
// not content, was the cost.
//
// So a chat whose journal was written within [buildIdle] is HOT: its new lines
// are still read and captured, but its sources stay out of the compile until
// the journal has been quiet for the whole window, and then they are compiled
// in one pass. The loop wakes when the window ends rather than polling for it.
// A person pressing Build (the job's backfill) compiles everything at once,
// hot or not — that is a person asking, not background work deciding.
//
// internal/manual/chat/models-and-cost.md states the two minutes; a change to
// buildIdle changes that page in the same commit.
var (
	buildIdle = 2 * time.Minute
	buildNow  = time.Now
)

// buildBatchMax is the longest a quiet chat waits for the chats still being
// written in, so that every chat of one sitting is compiled in ONE pass and
// pays for the vault-wide learning steps once. A person who never stops typing
// in some chat still gets the quiet ones compiled this long after they went
// quiet. models-and-cost.md states it; a change here changes that page.
var buildBatchMax = 10 * time.Minute

// hotSessions is every session whose sources are held back from this pass, and
// the moment the loop should look again.
//
// A session is HOT when its journal was written within [buildIdle] of now. A
// journal with no modification time (a fixture, an inventory that cannot say)
// is never hot: it cannot be waited out, so it is compiled as it always was. A
// job's journal is compiled by the job's own session
// (internal/session/knowledge_usage.go) and is not this loop's to hold.
//
// AND WHILE ANY SESSION IS HOT, THE QUIET ONES WITH WORK WAITING ARE HELD
// TOO (E6b B3 follow-up). Compiled the moment each went quiet, chats that
// ended a few minutes apart cost one pass each, and every pass reran the
// learning steps over the whole vault; held together they are one pass, run
// once the last of them goes quiet, and after it nothing more is called. A
// quiet session is held no longer than [buildBatchMax] past its own quiet
// moment, and once one of them is due every quiet session goes in that pass.
// pending names the sessions that have sources waiting to compile; only they
// can be held or make a pass due.
func hotSessions(journals []Journal, backfill bool, pending map[string]bool) ([]string, time.Time) {
	if backfill {
		return nil, time.Time{}
	}
	now := buildNow()
	var hot, waiting []string
	var lookAt time.Time
	soonest := func(at time.Time) {
		if lookAt.IsZero() || at.Before(lookAt) {
			lookAt = at
		}
	}
	quietAt := map[string]time.Time{}
	for _, journal := range journals {
		if journal.Job || journal.Session == "" || journal.Modified.IsZero() {
			continue
		}
		quiet := journal.Modified.Add(buildIdle)
		if seen, ok := quietAt[journal.Session]; !ok || quiet.After(seen) {
			quietAt[journal.Session] = quiet
		}
	}
	due := false
	for session, quiet := range quietAt {
		if now.Before(quiet) {
			hot = append(hot, session)
			soonest(quiet)
			continue
		}
		if !pending[session] {
			continue
		}
		waiting = append(waiting, session)
		deadline := quiet.Add(buildBatchMax)
		if !now.Before(deadline) {
			due = true
		}
		soonest(deadline)
	}
	if len(hot) == 0 || due {
		// Nothing is being written in, or a quiet chat has waited long
		// enough: every quiet chat is compiled now, in one pass.
		lookAt = time.Time{}
		for _, session := range hot {
			soonest(quietAt[session])
		}
		sort.Strings(hot)
		return hot, lookAt
	}
	held := append(hot, waiting...)
	sort.Strings(held)
	return held, lookAt
}

// pendingSessions is every conversation with sources waiting to compile, outside jobs.
func pendingSessions(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT DISTINCT coalesce(json_extract(payload,'$.session'),'') FROM events WHERE partition='profile' AND status IN ('pending','revisit') AND coalesce(json_extract(payload,'$.job'),0)=0")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var session string
		if err = rows.Scan(&session); err != nil {
			return nil, err
		}
		if session != "" {
			out[session] = true
		}
	}
	return out, rows.Err()
}

type heldSessionsKey struct{}

// withHeldSessions keeps the sources of the sessions named out of every
// compile selection made on ctx ([compilerSourceSelection]).
func withHeldSessions(ctx context.Context, sessions []string) context.Context {
	if len(sessions) == 0 {
		return ctx
	}
	return context.WithValue(ctx, heldSessionsKey{}, append([]string(nil), sessions...))
}

// heldClause is the SQL that excludes the held sessions' sources, or "1" when
// nothing is held.
func heldClause(ctx context.Context, payload string) (string, []any) {
	held, _ := ctx.Value(heldSessionsKey{}).([]string)
	if len(held) == 0 {
		return "1", nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(held)), ",")
	args := make([]any, 0, len(held))
	for _, session := range held {
		args = append(args, session)
	}
	return "coalesce(json_extract(" + payload + ",'$.session'),'') NOT IN (" + marks + ")", args
}

type chargedSessionKey struct{}

// ChargedSession is the conversation a background compile is charged to: the
// owning session when a turn compiles its own sources, else the one
// conversation whose sources a background pass compiled. Empty means the pass
// compiled several conversations' sources at once (a person's Build), which no
// one conversation owns; the ledger still files it under the Library.
func ChargedSession(ctx context.Context) string {
	if owner := provider.SessionFrom(ctx); owner != "" {
		return owner
	}
	charged, _ := ctx.Value(chargedSessionKey{}).(string)
	return charged
}

// chargeOnlySession charges ctx to the one session whose sources are about to
// be compiled, when exactly one is. AND EVERY CALL OF THAT PASS IS CHARGED TO
// IT, the learning steps included: they run because that conversation's
// sources arrived.
func chargeOnlySession(ctx context.Context, db *sql.DB) (context.Context, error) {
	held, args := heldClause(ctx, "payload")
	rows, err := db.Query("SELECT DISTINCT coalesce(json_extract(payload,'$.session'),'') FROM events WHERE partition='profile' AND status IN ('pending','revisit') AND coalesce(json_extract(payload,'$.job'),0)=0 AND ("+held+") LIMIT 2", args...)
	if err != nil {
		return ctx, err
	}
	defer rows.Close()
	var sessions []string
	for rows.Next() {
		var session string
		if err = rows.Scan(&session); err != nil {
			return ctx, err
		}
		sessions = append(sessions, session)
	}
	if err = rows.Err(); err != nil {
		return ctx, err
	}
	if len(sessions) == 1 && sessions[0] != "" {
		ctx = context.WithValue(ctx, chargedSessionKey{}, sessions[0])
	}
	return ctx, nil
}
