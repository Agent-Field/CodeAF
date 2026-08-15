package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// Every command the funnel has ever taken answered two questions — what was
// asked, and whether the target could take it. It never asked the third one,
// who asked, because for the whole life of the system there was only ever one
// answer: a person, through the one head that speaks for them. The moment a
// second agent can journal a command that stops being true, and a stray verb
// from one task can reach into another's work with nothing in the way.
//
// This file is the third question and exactly one rule about the answer: a
// command issued by a task orchestrator must land inside that task's own
// subtree. Everything else about authority is deliberately absent. The axis is
// journaled now so that the rule can be tightened later against commands that
// already carry an honest answer, rather than against a journal that records
// nobody.

// CommandIssuer names the authority behind one command.
//
// The empty string is the legacy value and reads as IssuerUser: every command
// ever written before this axis existed came from a person, through a path
// nothing checked, and the whole point of a default is that it must describe
// what was actually true. Storing "" rather than rewriting it to "user" keeps
// old events replaying byte-identical — the resolution happens in Authority,
// once, where it can be read.
type CommandIssuer string

const (
	// IssuerUser is the person themselves, and the top of the ladder. It is
	// what the head writes when it is carrying a sentence somebody typed.
	IssuerUser CommandIssuer = "user"
	// IssuerMain is the main head acting on its own initiative rather than
	// relaying a sentence. Nothing distinguishes it from user today; it exists
	// so that the day the two are told apart, the journal already knows which
	// commands were which.
	IssuerMain CommandIssuer = "main"
	// IssuerReconciler is the resident writing a command to itself — a repair,
	// a continuation, a follow-up it decided on while settling something else.
	IssuerReconciler CommandIssuer = "reconciler"
	// IssuerTaskPrefix builds the one bounded issuer: a task orchestrator
	// speaks as task:<root> and may only reach what hangs beneath that root.
	IssuerTaskPrefix = "task:"
)

// maxAncestryDepth bounds the ancestry walk. A graph is stages deep, not
// thousands, so this never fires on a well-formed store — it exists so that a
// parent cycle written by some future bug costs one refused command instead of
// a wedged transaction.
const maxAncestryDepth = 1024

// TaskIssuer is the issuer a task orchestrator rooted at root writes. It is a
// constructor rather than a formatting convention at the call sites, because
// the parsing half below has to agree with it exactly.
func TaskIssuer(root string) CommandIssuer {
	return CommandIssuer(IssuerTaskPrefix + strings.TrimSpace(root))
}

// TaskRoot reports the root a task issuer speaks for. The second return is
// false for every other issuer and for a malformed one — "task:" with nothing
// after it names no root, and a caller that cannot say what it governs is
// refused rather than trusted.
func (issuer CommandIssuer) TaskRoot() (string, bool) {
	text := string(issuer)
	if !strings.HasPrefix(text, IssuerTaskPrefix) {
		return "", false
	}
	root := strings.TrimSpace(strings.TrimPrefix(text, IssuerTaskPrefix))
	return root, root != ""
}

// Authority resolves what the journal holds into who is actually being
// trusted, which is the only form any check should ever read.
func (command Command) Authority() CommandIssuer {
	issuer := command.Issuer.normalized()
	if issuer == "" {
		return IssuerUser
	}
	return issuer
}

func (issuer CommandIssuer) normalized() CommandIssuer {
	return CommandIssuer(strings.TrimSpace(string(issuer)))
}

// valid is a closed list plus the one shape. An unrecognized string is a
// refusal and never anything else: an issuer nobody can interpret must not be
// silently promoted to the trusted default, which is what an open list would
// do the first time a typo reached this line.
func (issuer CommandIssuer) valid() bool {
	switch issuer.normalized() {
	case "", IssuerUser, IssuerMain, IssuerReconciler:
		return true
	}
	_, named := issuer.TaskRoot()
	return named
}

// authorizeCommandIssuer is the one new check on the command funnel, and it
// costs nothing for every issuer that is not a task. Validation sits in front
// of every command in the system, so the first line is a string prefix test and
// the database is not touched at all unless a task is the one asking.
func authorizeCommandIssuer(tx *sql.Tx, command Command) error {
	root, governs := command.Authority().TaskRoot()
	if !governs {
		return nil
	}
	target := strings.TrimSpace(command.Target)
	if target == "" {
		return fmt.Errorf("%w: %s from task %q must name a node inside it", ErrInvalid, command.Kind, root)
	}
	inside, err := withinSubtree(tx, root, target)
	if err != nil {
		return err
	}
	if !inside {
		return fmt.Errorf("%w: %s target %q is outside task %q", ErrInvalid, command.Kind, target, root)
	}
	return nil
}

// withinSubtree answers whether id is root or hangs beneath it, in one
// recursive CTE up the parent index — a primary-key lookup per hop, no scan,
// and no second walk beside the one task_budget.go already built for ceilings.
// The direction is deliberate: walking up from the target is bounded by the
// graph's depth, while walking down from the root is bounded by the task's
// width, and a task may be wide.
//
// The recursion stops at the root rather than continuing to the spine, so an
// authorized command pays for the hops between the two and nothing more.
func withinSubtree(query rowQuerier, root, id string) (bool, error) {
	var inside bool
	if err := query.QueryRow(`
		WITH RECURSIVE ancestry(id, parent_id, depth) AS (
		    SELECT id, parent_id, 0 FROM nodes WHERE id = ?
		    UNION ALL
		    SELECT parent.id, parent.parent_id, ancestry.depth + 1
		    FROM nodes AS parent
		    JOIN ancestry ON parent.id = ancestry.parent_id
		    WHERE ancestry.id <> ? AND ancestry.depth < ?
		)
		SELECT EXISTS(SELECT 1 FROM ancestry WHERE id = ?)`,
		id, root, maxAncestryDepth, root).Scan(&inside); err != nil {
		return false, fmt.Errorf("authorize %q under %q: %w", id, root, err)
	}
	return inside, nil
}
