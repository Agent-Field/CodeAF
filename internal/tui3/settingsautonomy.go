package tui3

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/fuzzy"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── WHAT THIS PROJECT DOES WHILE NOBODY IS THERE, ON THE SETTINGS PAGE ──────
//
// A SETTING THAT CHANGES WHAT THE PROGRAM DOES ON ITS OWN IS NEVER REACHABLE
// ONLY BY A SLASH COMMAND (owner ruling 2026-09-11). `/autonomy` has been the
// only door to these rules since they landed, and slash commands are invisible:
// a person who has never been told the word cannot find it, and this is the one
// setting that decides whether their work is answered without them.
//
// So the rules have a row where settings are browsed. It is the SAME reader and
// the SAME writer as the command — [app.autonomySheetText] prints what these
// rows draw, [app.changeAutonomy] writes what these rows write, and the engine's
// own door ([session.Agent.SetAutonomy]) refuses what it always refused — so the
// two can never disagree. What this file adds is a place to stand.
//
//	questions while you are away
//	permission        ask me
//	choice            recommend then go · 30s
//	judgement         ask me
//	clarification     ask me · never runs on a clock
//	confirmation      ask me · destructive always asks
//	landing           decide yourself
//	assumptions       recommend then go · 10m
//	already done      ask me
//
// THE WORDS ARE THE PERSON'S AND THE KIND IS THE ENGINE'S. The value cycles
// through the three answers in a person's words; the row's own name is the word
// `/autonomy` takes, so somebody who wants the command can read it off the page
// they are already looking at.

// The words these rows say, spelled once here and quoted in the manual exactly.
const (
	// autonomyRowsHead is the section's heading, and it is the sheet's own
	// ([autonomyHeadWord]) so the page and the command say one thing.
	autonomyRowsHead = autonomyHeadWord
	// autonomyRecommendWord is the middle answer in a person's words. The sheet
	// spells the same rule as `recommend, auto in 30s`, which says what the
	// machine does; this says what a person asked for.
	autonomyRecommendWord = "recommend then go"
	// autonomyRowAbout is the one line under a row. It names the command as the
	// second door rather than the first, which is this file's whole point.
	autonomyRowAbout = "what happens to a question of this shape when nobody answers it · " +
		"kept for this project · /autonomy <kind> ask · recommend <duration> · decide"
)

// autonomyRow is one question kind as the settings page holds it, hung off
// [sheetItem] exactly as a role or an account is: the cursor walk, the scroll,
// the pointer and the hover need to know nothing about it.
type autonomyRow struct {
	kind session.AskKind
	// word is the answer as this page says it, already carrying the law where
	// the kind has one.
	word string
	// fixed says the engine will refuse a change to this kind, so the row is
	// read-only and says why on itself. A setting that looks changeable and is
	// not is worse than one that says it is not.
	fixed bool
}

// autonomyItems is the section: one row per question kind, in the kind table's
// own order, reading the rules this project currently keeps.
//
// A CONVERSATION WITH NO PROJECT HAS NO ROWS AT ALL, which is the emptiness law
// over a setting that is stored per project: rules that cannot be kept are not
// drawn as rules that are.
func (s *sheet) autonomyItems(terms []fuzzy.Term) []sheetItem {
	if !s.autonomyDoor {
		return nil
	}
	items := make([]sheetItem, 0, len(autonomyKinds))
	for _, kind := range autonomyKinds {
		// A ROW WHOSE VALUE HAS NOT ARRIVED SAYS NOTHING IN ITS VALUE COLUMN.
		// The rules are a call to another process and the rows are not, so the
		// section is here from the first frame and fills in a moment later —
		// which is the emptiness law rather than a gap: `ask me` printed over a
		// project that turns out to be on `decide` is the one sentence this page
		// must never say, and it is what a nil read coerced to an empty map says
		// on every row.
		word := ""
		if s.autonomy != nil {
			word = autonomyPersonWord(s.autonomy[kind])
		}
		row := &autonomyRow{kind: kind, word: word}
		switch kind {
		case session.AskConfirmation:
			row.word, row.fixed = autonomyAskWord+" · "+autonomyAlwaysWord, true
		case session.AskClarification:
			row.word, row.fixed = autonomyAskWord+" · "+autonomyNoClockWord, true
		}
		if len(terms) > 0 {
			if _, ok := s.autonomyRowMatches(row, terms); !ok {
				continue
			}
		}
		items = append(items, sheetItem{
			autonomy: row,
			meta:     settingMeta{tab: tabSafety, label: string(kind), about: autonomyRowAbout},
		})
	}
	if len(items) == 0 {
		return nil
	}
	return append([]sheetItem{{head: autonomyRowsHead}}, items...)
}

// autonomyRowMatches is the search over one of these rows: the kind's own name,
// the answer it carries, the heading and the section's about line — the fuzzy
// matcher every picker on this surface shares (internal/fuzzy), scored per
// term by whichever field carries the word best.
//
// THE HEADING AND THE ABOUT LINE ARE IN IT DELIBERATELY, because somebody
// looking for this section searches for "away" or "decide" or "autonomy" —
// words that live on the heading and in the value rather than in a registry
// key — and the about line names `/autonomy` as the second door to the same
// rules, which is the one word this page exists so a person can find.
func (s *sheet) autonomyRowMatches(row *autonomyRow, terms []fuzzy.Term) (int, bool) {
	if len(terms) == 0 {
		return 0, true
	}
	// The sheet's own reusable field buffer, for the same per-keystroke reason
	// [sheet.settingScore] gives: a rebuild scores every row.
	if len(s.matchFields) < 4 {
		s.matchFields = make([]string, 5)
	}
	fields := s.matchFields[:4]
	fields[0] = string(row.kind)
	fields[1] = row.word
	fields[2] = autonomyRowsHead
	fields[3] = autonomyRowAbout
	return fuzzy.ScoreFields(fields, terms)
}

// autonomyPersonWord is one rule in a person's words. It is the sheet's own
// reading ([autonomyRuleWord]) with the middle answer said the way somebody
// would ask for it, and the duration kept, because "recommend then go" without
// a number is half a rule.
func autonomyPersonWord(rule session.Policy) string {
	switch rule.Kind {
	case session.PolicyDecide:
		return autonomyDecideWord
	case session.PolicyRecommendThenAuto:
		if word := shortAutonomyDuration(rule.After); word != "" {
			return autonomyRecommendWord + " · " + word
		}
		return autonomyRecommendWord
	default:
		return autonomyAskWord
	}
}

// autonomyRowLines draws one row through the page's own row grammar, so it reads
// as a setting and not as a second kind of thing on the same list.
func (s *sheet) autonomyRowLines(row *autonomyRow, selected, hovered bool, width int, pal palette) []string {
	return overlayLines(string(row.kind), row.word, selected, false, hovered, width, pal)
}

// autonomyRowNext is `enter` on one of these rows: the next answer round the
// three, written through the command's own door.
//
// THE TWO FIXED ROWS DO NOT CYCLE. The engine refuses a rule over a confirmation
// and over a clarification, in its own words, and a key that walked the value and
// then watched it snap back would be this page arguing with the engine in front
// of somebody.
func (a *app) autonomyRowNext(row *autonomyRow) tea.Cmd {
	if row.fixed {
		a.sheet.msg = autonomyFixedWord
		return nil
	}
	agent, ok := a.agent.(autonomyAgent)
	if !ok {
		a.sheet.msg = autonomyNoProjectWord
		return nil
	}
	// WHAT IT IS NOW COMES FROM THE PAGE'S OWN READING, not from a second ask.
	//
	// THIS IS A CALL OVER A CONNECTION AND NOT A MAP LOOKUP. On the ordinary
	// launch the surface talks to its own engine process, so every one of these
	// is a round trip — and this key used to make THREE of them for one press:
	// read what it is, write what it becomes, read it back. The page already
	// holds the reading it opened with ([sheet.autonomy]) and is the only thing
	// that writes to it, so the press costs the one call that changes something.
	// The keystroke is on the update loop, which is exactly where a surface may
	// not wait on a network.
	next := session.Policy{Kind: session.PolicyAsk}
	// THE UNWRITTEN RULE IS `ask me`, and it is unwritten rather than stored:
	// a project with no rules has no entries at all, so the empty reading and
	// [session.PolicyAsk] are the same answer and both walk on to the middle one.
	switch a.sheet.autonomy[row.kind].Kind {
	case session.PolicyRecommendThenAuto:
		next.Kind = session.PolicyDecide
	case session.PolicyDecide:
	default:
		next.Kind, next.After = session.PolicyRecommendThenAuto, autonomyRowStep
	}
	// THE ROW MOVES ON THE KEYSTROKE AND THE WRITE GOES OFF THE LOOP. This is a
	// call over a connection — on the ordinary launch the surface talks to its
	// own engine process — and a row that sat unchanged for a round trip is a row
	// somebody presses twice (offloop.go's law, and the whole account of why).
	// So what a person sees is decided here, immediately, and the rare road — the
	// engine refusing — puts it back with the engine's own sentence.
	was, had := a.sheet.autonomy[row.kind]
	wasWord := row.word
	if a.sheet.autonomy == nil {
		a.sheet.autonomy = map[session.AskKind]session.Policy{}
	}
	a.sheet.autonomy[row.kind] = next
	row.word = autonomyPersonWord(next)
	a.sheet.msg = string(row.kind) + " · " + row.word + " · for this project"
	return a.offLoop(func() func(bool) tea.Cmd {
		err := agent.SetAutonomy(row.kind, next)
		return func(here bool) tea.Cmd {
			if !here || err == nil {
				// AND THE PAGE'S READING MOVES WITH THE WRITE RATHER THAN BEING
				// FETCHED AGAIN. The engine took this exact rule — it said so by
				// not refusing — so re-asking would be a round trip to be told
				// what this press already knows. The conversation-wide cache is
				// a different reading and is refreshed, because a question raised
				// after this must wear the rule that now exists.
				if err == nil {
					return a.autonomyChanged()
				}
				return nil
			}
			// The engine's refusal is the person's to read, whole — and the row
			// goes back to what it was, because it never became what it said.
			if had {
				a.sheet.autonomy[row.kind] = was
			} else {
				delete(a.sheet.autonomy, row.kind)
			}
			row.word = wasWord
			a.sheet.msg = err.Error()
			a.touch()
			return nil
		}
	})
}

// autonomyFixedWord is what a fixed row says when somebody presses it. It names
// the law rather than refusing in the abstract.
const autonomyFixedWord = "this shape always asks you · " + autonomyAlwaysWord

// autonomyRowStep is the wait this page writes when a row is cycled onto
// `recommend then go`. It is the sheet's own default rather than a number chosen
// here, and `/autonomy <kind> recommend <duration>` is how a person names another.
const autonomyRowStep = 30 * time.Second

// hasAutonomyDoor reports whether this conversation has somewhere to keep
// question rules. It is a type assertion on the agent and nothing else — local,
// free, and safe to ask on the update loop — which is exactly why it is a
// separate question from what the rules SAY ([sheet.autonomyDoor]).
func (a *app) hasAutonomyDoor() bool {
	_, ok := a.agent.(autonomyAgent)
	return ok
}
