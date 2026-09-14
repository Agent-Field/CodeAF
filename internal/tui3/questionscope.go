package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── HOW LONG AN ANSWER LASTS, ON THE FRAME THAT ASKS FOR IT ─────────────────
//
// "STOP ASKING ME FOR THIS" IS A THING PEOPLE SAY AT A PERMISSION GATE, and
// until this row there was nowhere to say it. The question already carried the
// answer — [session.Question.Scope] is the lifetimes the lane said an answer
// could have, and [session.Answer.Scope] is the one it went out with — but the
// only way to reach a wider one from the block was the `always` ANSWER, which
// grants a different thing (every later call of that tool) and is the lane's
// choice rather than the person's. The owner ruled one row (2026-09-11): the
// offered lifetimes drawn under the answers, cycled by a key, defaulting to the
// narrowest, and OFFERED NOWHERE ELSE.
//
//	│ which store should the ledger sit on?
//	│
//	│ ▸ 1  postgres                                       ◆ recommended
//	│   2  sqlite
//	│
//	│   ✓ just this once · for this project
//	╰─ ↑↓ choose · enter take it · esc later ──────────────────────────
//	  t how long · c change · ? ask back · 1–2 jump
//
// THREE LAWS, AND EACH ONE IS A WAY THIS ROW COULD LIE:
//
//   - NEVER A SCOPE THE QUESTION DID NOT OFFER. The lifetimes are the lane's to
//     say — only it knows whether a rule for this shape can be written at all —
//     so this row draws [session.Question.Scope] and never a list of its own.
//   - THE NARROWEST IS THE DEFAULT, which is [questionDefaultScope]'s law said
//     about the block: a frame that opened on `from now on` would turn somebody
//     answering one question into somebody writing a rule they never read.
//   - AN IRREVERSIBLE QUESTION OFFERS NONE. A call that cannot be taken back is
//     asked about every time, and a row offering to stop asking would be this
//     surface selling the one guarantee it has.
//   - AND A PERMISSION OFFERS NONE EITHER, YET. See [questionScopes].

// questionScopes is what this question may be answered for, narrowest first, or
// none where there is no choice to make. A question that offered ONE lifetime
// has nothing to toggle, and drawing `just this once` beside every answer would
// teach people to stop reading the word.
//
// A PERMISSION OFFERS NO LIFETIMES UNTIL THE ENGINE HONOURS ONE. A consent's
// answer is read as a key plus the banked comment and nothing else
// (session/answers.go, session/question.go) — [session.Answer.Scope] never
// reaches the gate — so `t → for this project` followed by `enter` grants
// exactly ConsentOnce and the very next call asks again. A row that a person can
// move, that says a thing, and that changes nothing is worse than no row:
// "A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN" (CLAUDE.md), and this
// one would be a promise about safety. The engine-side seam — read Answer.Scope
// in the consent path and map it onto the gate's own lifetimes — is written down
// in ~/af-qv-P.report.md; delete this clause the day it lands. The row stays
// live for every question whose lane DOES read Scope.
func questionScopes(q session.Question) []session.AnswerScope {
	if q.Ask == session.AskPermission || q.Stakes == session.StakesIrreversible || len(q.Scope) < 2 {
		return nil
	}
	// NARROWEST FIRST, WHATEVER ORDER THE LANE LISTED THEM IN. The row is read
	// left to right and the key walks it in that order, so a lane that happened
	// to say `always, once` must not draw a row that widens leftwards.
	out := make([]session.AnswerScope, 0, len(q.Scope))
	for _, want := range []session.AnswerScope{
		session.ScopeOnce, session.ScopeTask, session.ScopeProject, session.ScopeAlways,
	} {
		for _, got := range q.Scope {
			if got == want {
				out = append(out, want)
				break
			}
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// questionScopeNow is the lifetime this question's answer would go out with:
// the one the person cycled to, or the narrowest offered.
func questionScopeNow(q questionShown) session.AnswerScope {
	scopes := questionScopes(q.question)
	for _, one := range scopes {
		if one == q.scope {
			return one
		}
	}
	return questionDefaultScope(q.question)
}

// questionScopeRow is the row itself: every offered lifetime in the person's
// own words, with the one that is on wearing the vocabulary's tick.
//
// THE TICK IS THE MARK AND THE HUE IS NOT. Colour is stroke and never fill on
// this block (the owner's colour ruling), so the lifetime that is on is ordinary
// ink beside a tick and the rest are dim — which is the same drawing a checklist
// gives an item that is ticked, and it survives a terminal with no colour at all.
func (a *app) questionScopeRow(q questionShown, room int) string {
	scopes := questionScopes(q.question)
	if len(scopes) == 0 {
		return ""
	}
	now := questionScopeNow(q)
	parts := make([]string, 0, len(scopes))
	for _, one := range scopes {
		word := questionScopeWord(one)
		if one == now {
			parts = append(parts, a.pal.ink(a.icon(tokens.GSettled)+" "+word))
			continue
		}
		parts = append(parts, a.pal.dim(word))
	}
	return questionPanelGap + fit(strings.Join(parts, a.pal.dim(questionSep)), room)
}

// questionScopeNext is the key: the next offered lifetime, round and round.
//
// IT CHANGES NOTHING BUT THE ROW. Nothing is written and nothing is answered —
// the lifetime travels with the answer when one is given ([questionScopeOf]),
// so a person who cycles this and then presses `esc` has written no rule, which
// is what makes it safe to press.
// THE WALK ITSELF IS [questionScopeAfter] AND IS NOT SPELLED TWICE. This is the
// key — it finds the question the key was pressed on and marks the surface dirty;
// which lifetime comes next is one rule, asked here and by the page's own `t`.
func (a *app) questionScopeNext(head questionShown) {
	open := a.questionHeld(head.token())
	if open == nil || len(questionScopes(open.question)) == 0 {
		return
	}
	open.scope = questionScopeAfter(open.question, questionScopeNow(*open))
	a.touch()
}
