package main

import (
	"math"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/callrows"
	"github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── THE QUESTION PUT TO EACH POLICY ─────────────────────────────────────────

// asked is one replayed request: everything a chooser is allowed to know about
// a call that really happened, rebuilt from the pair of rows it left behind.
type asked struct {
	// index is this request's place in the replayable set, which is how a pass
	// records its answer without a map keyed on a moment two calls can share.
	index int
	// id is this attempt's own pair of rows, carried so that the machine that
	// served it is never priced using the answer being scored.
	id    string
	at    time.Time
	model string
	role  lane.Role
	want  shape
	// demanded is the machine the router was asked for and machine the one that
	// answered. THEY DIFFER ON A THIRD OF THE ROWS and that is not a defect of
	// the log: `provider.order` is a ranking the router may ignore whenever
	// `allow_fallbacks` is on, so the first is our intention and only the second
	// is anybody's behaviour (#850).
	demanded string
	machine  string
	// prompt, cached and tools are the request's own facts, carried because the
	// chooser prices a prefix hold and gates on whether a machine can take a
	// tool call.
	prompt int
	cached int
	tools  bool
	// run and node are what this call belonged to. They are how a SWITCH is
	// counted: two consecutive requests of one errand served by two machines
	// forfeit the prompt cache between them, which is a real cost of a policy
	// that changes its mind and is invisible in a regret alone.
	run  string
	node string
	// ownFelt is what this request measurably cost on the machine that answered
	// it, read off its OWN row rather than out of the window.
	//
	// IT IS THE INSTRUMENT'S ONLY CHECK ON ITSELF. Every other number here is a
	// counterfactual estimated from a machine's neighbouring answers, and nothing
	// inside a counterfactual can say whether the estimate is any good. This one
	// quantity was both estimated and observed, so the difference between the two
	// is the estimator's own error, measured on the same log, and the report
	// prints it before it prints anything anybody might act on.
	ownFelt float64
}

// requestsOf pairs the log's start and end rows into replayable requests.
//
// A REQUEST IS ONLY REPLAYABLE WHEN ITS OUTCOME IS KNOWN. The start row says
// what was asked and the end row says what the answer was worth — how many
// tokens it turned out to be, and therefore how much of the wait was the
// machine's. A start with nothing under it is a call that was still in flight
// when the file was read, or one whose row nobody wrote; either way there is no
// work to price, so it is skipped and counted rather than guessed at.
func requestsOf(rows []callrows.Row) (requests []asked, unpaired int) {
	ends := map[string]callrows.Row{}
	for _, row := range rows {
		if row.Finished() && row.ID != "" {
			ends[row.ID] = row
		}
	}
	for _, row := range rows {
		if row.Finished() || row.Lost() || row.Model == "" {
			continue
		}
		end, paired := ends[row.ID]
		if !paired {
			unpaired++
			continue
		}
		role := roleOf(row.Tag)
		requests = append(requests, asked{
			index:    len(requests),
			id:       row.ID,
			at:       row.At,
			model:    lane.BareModel(row.Model),
			role:     role,
			want:     shapeOf(end, role),
			demanded: row.Lane,
			machine:  answered(end),
			prompt:   end.PromptTokens,
			cached:   end.CachedTokens,
			tools:    row.Tools > 0,
			run:      row.Run,
			node:     row.Node,
			ownFelt:  measuredFelt(end, role),
		})
	}
	return requests, unpaired
}

// shapeOf is how much of this answer a person was going to read and how much of
// it was pure waiting.
//
// EVERYTHING IS HIDDEN UNLESS SOMEBODY READS THE STREAM. Reasoning tokens, the
// JSON of a tool call and every token of an errand nobody is watching are worth
// their full rate, because none of them is being read while it arrives; only the
// visible text of a role whose stream is drawn ([lane.RoleFacts.Visible]) has a
// reader to catch up with. It is the split [lane.PerceivedSeconds] is built on,
// and reading it from the role rather than from a guess is what stops a title
// errand being priced as though somebody were watching it land.
func shapeOf(end callrows.Row, role lane.Role) shape {
	answer := end.CompletionTokens
	if answer <= 0 {
		return shape{}
	}
	if !role.Visible() || end.Finish == "tool_calls" {
		return shape{hidden: answer}
	}
	visible := answer - end.ReasoningTokens
	if visible < 0 {
		visible = 0
	}
	return shape{visible: visible, hidden: end.ReasoningTokens}
}

// measuredFelt is what this answer cost on the machine that wrote it, from its
// own row: its first token, its own writing rate, and the work it did. It is
// NaN for a row that measured neither — a refusal, an answer too short to rate —
// which is the honest reading of "there is nothing here to check against".
func measuredFelt(end callrows.Row, role lane.Role) float64 {
	one := drawOf(end)
	if one.refused || one.ttft <= 0 || one.rate <= 0 {
		return math.NaN()
	}
	return lane.PerceivedSeconds(one.ttft/1000, one.rate, shapeOf(end, role).visible, shapeOf(end, role).hidden)
}

// ── WHAT THE CALL WAS FOR ───────────────────────────────────────────────────

// roleOf is the role a logged call was made in.
//
// THE ROW DOES NOT CARRY ONE, AND THAT IS THE SEAM THIS FUNCTION STANDS IN FOR.
// internal/calllog records a `tag` — the call site's own word for the errand —
// and internal/lane's chooser is steered by a [lane.Role], and the two
// vocabularies are joined nowhere a reader of the file can reach. So the join is
// made here, by one rule and one small table, and it is the first thing to
// delete when the role reaches the row (see the report's seam).
//
// THE RULE IS THAT AN ERRAND'S TAG IS ALREADY ITS ROLE. Every side call of a
// turn is tagged with its own role word — internal/session's auxiliary.go writes
// `WithCallTag(errandCtx, string(role))` for exactly this reason, "so three
// records of one call agree about what to call it" — which resolves `judge`,
// `memory`, `design`, `auxiliary`, `recall`, `tool`, `probe` and `standing` with
// no table at all. What is left is the handful of tags that name a CALL SITE
// rather than a role, and each of those is one row of data below with the file
// that sets it.
//
// A TAG NOBODY HAS WRITTEN DOWN IS [lane.RoleUnknown], which is the conservative
// reading and not a gap: a call that claims to have a person waiting when it
// does not buys speed with somebody's money, so an unrecognised errand is priced
// as the background errand it almost certainly is.
func roleOf(tag string) lane.Role {
	if role := lane.Role(tag); role.Known() {
		return role
	}
	if role, named := callSiteRoles[tag]; named {
		return role
	}
	return lane.RoleUnknown
}

// callSiteRoles are the tags that name a place in the code rather than a role,
// with the role that place declares beside it. IT IS DATA AND NOT A LADDER: each
// row is one call site, named, and a tag added to the build joins the table
// rather than growing a condition.
var callSiteRoles = map[string]lane.Role{
	// internal/session/loop.go's laneRole: a conversation's own turn is talk.
	"turn": lane.RoleTalk,
	// The same function, in a task. WHICH LEAF ROLE IT IS CANNOT BE READ FROM
	// THE ROW — laneRole asks `someoneIsWatching()` at the moment of the call
	// and nothing records the answer — so the unattended reading is taken,
	// which is the one that assumes nobody's seconds are being spent. It makes
	// this instrument's `task` numbers a floor on how bad a watched task step
	// was, never a ceiling, and the report says so.
	"task": lane.RoleLeafUnattended,
	// internal/exec/linear.go: a plan leaf, which nobody is sitting in front of.
	"leaf": lane.RoleLeafUnattended,
	// internal/reflex/reflex.go tags every reflex call with one word while its
	// three call sites declare two roles — the route question is recall and the
	// extract and decide passes are memory. Memory is taken because it is two of
	// the three and the patient one.
	"reflex": lane.RoleMemory,
	// internal/head/compiler.go: the resident's compiler, a craft pass.
	"compile": lane.RoleDesign,
	// internal/revision and internal/plan: gates reading finished work.
	"gate":       lane.RoleJudge,
	"satisfied":  lane.RoleJudge,
	"delivery":   lane.RoleJudge,
	"markreader": lane.RoleJudge,
	"router":     lane.RoleJudge,
}

// requestOf turns a replayed request into the one a chooser is asked.
//
// EVERY COLUMN COMES FROM THE ROLE TABLE AND NONE OF THEM FROM A CONSTANT HERE.
// λ, the quality bar and the exploration horizon are all properties the role
// already declares (internal/lane's roles.go), and a replay that set them itself
// would be scoring the candidates against a policy about waiting that this build
// does not hold.
func (a asked) request() lane.Request {
	facts := a.role.Facts()
	return lane.Request{
		Model:        a.model,
		PromptTokens: a.prompt,
		Prefix:       a.run,
		Visible:      a.want.visible,
		Hidden:       a.want.hidden,
		Tools:        a.tools,
		ValueOfTime:  a.role.Lambda(),
		QualityNeed:  facts.QualityNeed,
		Horizon:      facts.Horizon,
		Role:         a.role,
		Now:          a.at,
	}
}

// class is the two-way reading the report is cut by: whether a person is
// watching this answer arrive. It is [lane.RoleFacts.Visible] and nothing else,
// because that column is already what decides whether the tail or the median is
// the number that matters ([rolePatience.riskZ]), and a second spelling of the
// same split would be a second thing to keep in step.
func (a asked) class() string {
	if a.role.Visible() {
		return "watched"
	}
	return "unattended"
}
