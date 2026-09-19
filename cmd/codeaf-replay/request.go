package main

import (
	"math"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/callrows"
	"github.com/Agent-Field/codeaf/internal/lane"
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
	// guessed marks a request whose own answer never arrived, so its work is the
	// typical work of its role on its model rather than a measurement. The report
	// counts them.
	guessed bool
}

// shapeKey is what a typical answer length is a property of: the model, and
// whether anybody was reading it.
type shapeKey struct {
	model string
	class string
}

// typicalShapes is the median answer this build gets from each model in each
// role class, in tokens, so that a request whose answer never arrived can be
// priced for the work it asked for rather than for none.
//
// IT IS A MEDIAN OVER THE LOG'S OWN ANSWERS and not a figure chosen here: the
// completion and reasoning tokens of every clean row of that model and class.
func typicalShapes(rows []callrows.Row) map[shapeKey]shape {
	answers := map[shapeKey][]float64{}
	thinking := map[shapeKey][]float64{}
	for _, row := range rows {
		if !row.Finished() || row.Model == "" || row.CompletionTokens <= 0 || row.Failed() {
			continue
		}
		key := shapeKey{model: lane.BareModel(row.Model), class: classOf(roleOf(row.Tag))}
		answers[key] = append(answers[key], float64(row.CompletionTokens))
		thinking[key] = append(thinking[key], float64(row.ReasoningTokens))
	}
	typical := make(map[shapeKey]shape, len(answers))
	for key, lengths := range answers {
		answer := int(quantile(lengths, 0.5))
		reasoning := int(quantile(thinking[key], 0.5))
		if key.class == "watched" {
			visible := answer - reasoning
			if visible < 0 {
				visible = 0
			}
			typical[key] = shape{visible: visible, hidden: reasoning}
			continue
		}
		typical[key] = shape{hidden: answer}
	}
	return typical
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
	typical := typicalShapes(rows)
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
		want, guessed := shapeOf(end, role), false
		if want.visible+want.hidden == 0 {
			// AN ANSWER THAT NEVER ARRIVED STILL ASKED FOR SOMETHING. A refused
			// request carries no completion tokens, so reading its work as zero
			// prices every machine at its first token alone and makes the one case
			// this instrument matters most for — a paced pool refusing the same
			// errand eight times in forty seconds — rank below a slow but healthy
			// answer. What it asked for is unknown and not nothing, so the honest
			// stand-in is what this role typically asks of this model, measured
			// from the log's own answers.
			want, guessed = typical[shapeKey{model: lane.BareModel(row.Model), class: classOf(role)}], true
		}
		requests = append(requests, asked{
			index:    len(requests),
			id:       row.ID,
			at:       row.At,
			model:    lane.BareModel(row.Model),
			role:     role,
			want:     want,
			guessed:  guessed,
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
// delete when the role reaches the row (#928, which carries the acceptance).
//
// THE RULE IS THAT AN ERRAND'S TAG IS ALREADY ITS ROLE. Every side call of a
// turn is tagged with its own role word — internal/session's auxiliary.go hands
// `callPurpose(role)` to the one door, which spells it onto the request
// (clientdoor.go), so three records of one call agree about what to call it —
// which resolves `judge`, `memory`, `design`, `auxiliary`, `recall`, `tool`,
// `probe` and `standing` with no table at all. What is left is the handful of
// tags that name a CALL SITE
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
	// A TAG MAY CARRY WHAT IT WAS ABOUT AFTER THE WORD FOR WHAT IT IS.
	// internal/session's toolCallTag writes `tool:<the tool's name>`, so the
	// role is the part before the colon. This is ONE RULE about the shape of a
	// tag and not a row per tool: a tool added to the belt tomorrow writes a tag
	// nobody can enumerate, and reading only up to the colon is what keeps it
	// from being an anonymous background errand.
	if word, _, qualified := strings.Cut(tag, ":"); qualified {
		if role := lane.Role(word); role.Known() {
			return role
		}
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
//
// IT IS COMPLETE, AND A LAW READS THE TREE TO KEEP IT SO
// (`TestEveryTagTheBuildWritesResolvesToARoleSomebodyDeclared`). Every tag this
// build can write is reachable from source: the [session.callPurpose] values
// handed to internal/session's one door (clientdoor.go, which is the only file
// in that package that spells `provider.WithCallTag`), the errand names
// `cmd/codeaf`'s errandContext passes, and — because auxiliary.go hands the door
// `callPurpose(role)` — every [roles.Role] constant there is. A word missing
// from here reads as a background
// errand, which is quiet, plausible and moves every number in the table, so the
// law fails the build rather than the report going quietly wrong.
var callSiteRoles = map[string]lane.Role{
	// internal/session/loop.go's laneRole: a conversation's own turn is talk.
	"turn": lane.RoleTalk,
	// internal/session/checkpoint.go's draft rung: the model that has just spent
	// the turn, asked on the turn's own transcript for the document a worker will
	// finish from. It is made with lane.RoleAuxiliary and billed to the turn, and
	// it reached the log with NO TAG AT ALL until #996 — which made the dearest
	// side-call on the ceiling road invisible to every reading of this file.
	"handoff-draft": lane.RoleAuxiliary,
	// internal/session/subharness_env.go: one AI step of a saved program, which
	// runs on the program's own model rather than on any role's. Nobody is
	// reading its stream and nobody is waiting on its first word.
	"subharness": lane.RoleAuxiliary,
	// ── two of the three roads that carry their own provider client ─────────
	//
	// Each builds a client the one door does not make and completes on it
	// directly (internal/session/clientdoor.go's withPurpose), and all three
	// reached this file with NO TAG AT ALL until #996 — so the errand this build
	// runs most often with nobody there was priced as anonymous. The third,
	// `consolidate`, already had a row below because internal/roles has that
	// word: the tidy-up and the role are the same errand and the same reading.
	//
	// internal/session/standing_run.go: one standing item's yes-or-no, on every
	// check of every item forever. It is spelled `standing-check` and NOT
	// `sentinel`, which is already the resident's own quorum errand above and is
	// judged: this one sets lane.RoleStanding on its own context, and pricing it
	// as a judge would put a wait nobody is having into the table.
	"standing-check": lane.RoleStanding,
	// internal/session/tools_doc.go: the model's own eyes on a document the
	// `read` tool cannot open as text. A TOOL CALL INSIDE A TURN, so somebody IS
	// waiting — the one of the three whose seconds are a person's.
	"document": lane.RoleTalk,
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
	"gate":      lane.RoleJudge,
	"satisfied": lane.RoleJudge,
	"delivery":  lane.RoleJudge,

	// cmd/codeaf's errandContext names each of the resident's side errands with
	// its own word and declares the role beside it in the same call. The role
	// here is the one that call site passes, read off the argument.
	"quorum":       lane.RoleJudge,
	"sentinel":     lane.RoleJudge,
	"craft-repair": lane.RoleDesign,
	"craft-params": lane.RoleDesign,
	"reflect":      lane.RoleAuxiliary,

	// internal/session/auxiliary.go tags every errand with `string(role)`, so
	// the whole of internal/roles' vocabulary reaches the log as a tag. The
	// reading below is that package's own — internal/session's errandRole, which
	// is the function this join stands in for on the other side of the seam: the
	// two judging errands and the two remembering ones are named, the three that
	// design are named, and an errand is AUXILIARY otherwise, because a side call
	// of a turn made without the turn's stream is what an errand is.
	"router":        lane.RoleJudge,
	"routerconfirm": lane.RoleJudge,
	"markreader":    lane.RoleJudge,
	"guardian":      lane.RoleJudge,
	"auditor":       lane.RoleJudge,
	"consolidate":   lane.RoleMemory,
	"planner":       lane.RoleDesign,
	"designer":      lane.RoleDesign,
	"division":      lane.RoleDesign,
	// internal/session/taxonomy_boundary.go hands this word to the classifier
	// that reads a repair round's evidence, and internal/session/repair_role.go
	// resolves the hands the round itself runs on. Both are work inside a task
	// with nobody's stream open, which is what an errand IS — the table's own
	// default, said out loud because this word only became visible here when the
	// role stopped being conjured at its call site as roles.Role("repair").
	"repair": lane.RoleAuxiliary,
	"title":  lane.RoleAuxiliary,
	// internal/session/image.go is the only writer of this tag and it sets
	// lane.RoleTalk, deliberately and with the reasoning beside it: a look at an
	// image STREAMS INTO THE ROOM the person is reading, delta by delta, during
	// their own turn. This row said RoleAuxiliary — a call nobody's seconds are
	// being spent on — which is the opposite of what that file says, and it moved
	// every number in this table that separates a person's wait from a machine's.
	"vision": lane.RoleTalk,
	// internal/session/spellout.go declares lane.RoleAuxiliary on its own
	// context, so this row is that file's own reading and not a second one.
	//
	// IT IS WORTH A LOOK AND IT IS NOT THIS FILE'S CALL. A person is SITTING AND
	// WATCHING a spell-out — it writes three lines they read before deciding
	// whether to keep them — and an auxiliary is by definition a call nobody's
	// seconds are being spent on. Either the lane role or the product is wrong
	// there; the table's job is to agree with the build.
	"spellout": lane.RoleAuxiliary,
	"imagegen": lane.RoleAuxiliary,
	"embed":    lane.RoleAuxiliary,
	// RoleOrganize is the standing pass's folder filer. Nobody is sitting in
	// front of `codeaf tick`; pricing it as talk would put a wait that is not
	// happening into the watched half of the report.
	"organize": lane.RoleAuxiliary,
	// RoleCollabConsult is one invited participant's bounded call during
	// coordinate invite. callRoleChecked prices it as an errand (no stream);
	// errandRole's default is auxiliary, same as organize.
	"collab-consult": lane.RoleAuxiliary,
	"worker":         lane.RoleAuxiliary,
	"speech":         lane.RoleAuxiliary,
	"video":          lane.RoleAuxiliary,
	"handoff":        lane.RoleAuxiliary,
	"taskname":       lane.RoleAuxiliary,
	"jobname":        lane.RoleAuxiliary,
	"caption":        lane.RoleAuxiliary,
	"shaper":         lane.RoleAuxiliary,
	"intake":         lane.RoleAuxiliary,
	"careful":        lane.RoleAuxiliary,
	"distill":        lane.RoleAuxiliary,
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
func (a asked) class() string { return classOf(a.role) }

// classOf is the split itself, read from the role table and from nothing else.
func classOf(role lane.Role) string {
	if role.Visible() {
		return "watched"
	}
	return "unattended"
}
