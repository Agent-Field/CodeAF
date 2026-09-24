package session

// WHAT CHANGED OUTSIDE THIS CONVERSATION, PUT IN FRONT OF THE MODEL.
//
// Every other file in this coordination family answers a person: the home page
// draws the world (world.go), the task page draws another window's rows
// (taskelsewhere.go), the roster draws this session's own. THIS ONE ANSWERS THE
// MODEL, and it is the first thing in the package that does — until it landed,
// the other windows on a project were visible on the screen and completely
// invisible to the thing doing the work.
//
// The cost of that was paid twice a day and never named: a model that edits a
// file another window landed in an hour ago is editing a view of the world that
// stopped being true, and a model that greps its way to a fact a sibling task
// established this morning pays full price for a discovery somebody already
// bought. Neither is a mistake the model could have avoided. It was not told.
//
// ── TWO HALVES, TWO TENSES ──
//
//   - THE PAST: work that LANDED in this project, in other windows, since this
//     session's model was last told. Label, what came of it, and which files it
//     wrote — the citations [TaskIndexEntry.Files] carries.
//   - THE PRESENT: what the other windows have out RIGHT NOW, with the files
//     those runs have already written ([PresenceTask.Files]). This is the half
//     that says whose ground is moving before the model edits it.
//
// ── THIS FOLDER, AND THIS REPOSITORY FROM ANY FOLDER ──
//
// Both halves read this chat's own project folder as they always did, and every
// other project folder for work on a repository this chat is on (taskrepo.go):
// the folder a chat was launched in is not the repository its work was about,
// and most of the overlap measured on 2026-09-24 was one repository reached from
// two folders. A row from another folder says which one, and the rows are ranked
// before the cap bites — shared files, then the same repository, then the same
// folder — so the one row that shares a file with this chat's work is the last
// one to be cut.
//
// ── FACTS, NEVER INSTRUCTIONS ──
//
// The block states what other windows did and are doing, and asks the model for
// nothing. THIS IS A LAW AND NOT A STYLE. A title in it was written by another
// window's model; the moment this block could tell this session what to do,
// every window on the machine would be a way to steer every other one. So the
// block has no verbs aimed at the reader, no "check this", no "avoid that" —
// the model renders it to its person or plans around it, exactly as the design
// doc's noticeboard rule requires.
//
// ── AND IT GOES QUIET ──
//
// A block that said "nothing new" every turn would be a tax on every request
// for the rest of the conversation, which is the emptiness law applied to
// context: nothing to say is NOTHING WRITTEN. And a block whose facts have not
// moved is not rebuilt — the bytes in message[0] stay identical, so the prompt
// prefix stays cached and an unchanged fact costs nothing to keep saying.
//
// That is also why NO CLOCK APPEARS IN IT. "running for 4m 12s" is a fact that
// changes every single turn, and one age in this block would make it churn
// forever and be re-sent forever. The past half's tense is "recently" and the
// present half's is "now"; neither needs a number.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// deltaLandedRows caps how many landings the block names, and
	// deltaLiveRows how many of the other windows' running tasks. Six of each
	// is a screenful of facts; past that the block stops being a note and
	// becomes a report nobody asked for.
	deltaLandedRows = 6
	deltaLiveRows   = 6
	// deltaRowFiles caps the paths ONE row names. The honest total follows the
	// list where the cap bites, on [taskFileCitations]'s rule: the tail goes and
	// the count stays whole, so a row can never look complete when it is not.
	deltaRowFiles = 5
	// deltaOutcomeRunes bounds the outcome sentence in the block. It is already
	// cut to [taskOutcomeLimit] where it is written; this is the second stop,
	// against a row from a build that bounded it differently.
	deltaOutcomeRunes = 200
	// deltaFirstReach is how far back the FIRST delivery of a session looks
	// when there is no stamp yet.
	//
	// A session that has never been told has no origin, and the two honest
	// answers are "nothing" and "everything". Everything is a project's whole
	// history dumped into a first request — hundreds of rows, most of them
	// months old, none of them news. Nothing is a real hole: the commonest
	// reason a person opens a second window is that the first one is running
	// something, and a delta that said nothing about it on its first turn would
	// miss the one case it exists for. So the first delivery is BOUNDED rather
	// than empty — a day of landings, capped at [deltaLandedRows] like every
	// other delivery — and the day is chosen because work older than that is
	// not what a model is about to edit over.
	deltaFirstReach = 24 * time.Hour
)

// ── the stamp ───────────────────────────────────────────────────────────────

// toldName is the stamp file, in the session's own folder beside meta.json and
// presence.json:
//
//	~/.codeaf/v3/projects/<encoded-workspace>/<session-id>/told.json
//
// IT IS NOT `.last-look`, AND THE DIFFERENCE IS THE WHOLE REASON IT EXISTS.
// look.go's stamp is ONE instant at the places root, for the WHOLE MACHINE,
// written when a PERSON closes the home page — it means "somebody's eyes have
// been over the dashboard". This one is one instant PER CONVERSATION, inside
// that conversation's folder, written when THIS SESSION'S MODEL was handed the
// block — it means "this agent has been told". Different scope, different
// reader, different write moment, and neither can stand in for the other: a
// person glancing at home does not inform the model in a window three desks
// away, and a model being told changes nothing about what its person has seen.
//
// The two names are deliberately unalike for that reason. A second file called
// `.last-look` inside a session folder would be exactly the drift this codebase
// legislates against.
const toldName = "told.json"

// deltaToldSchema is the stamp's version, on presence.json's rule: A READER
// REFUSES A NUMBER IT DOES NOT KNOW rather than guessing at the fields. An
// unknown stamp reads as no stamp, which costs one bounded first delivery.
const deltaToldSchema = 1

// toldStamp is the file. It is JSON and not a bare instant — which is what
// look.go writes — because it lives in a folder whose files all carry a schema
// and are all read by builds that may be older than the file.
type toldStamp struct {
	Schema int       `json:"schema"`
	ToldAt time.Time `json:"toldAt"`
}

// LastTold is when this session's model was last handed the block, and zero
// when it never has been — or when the stamp is missing, unparsable, or from a
// schema this build does not know. Every one of those is the same fact for the
// caller: there is no origin, so the delivery is the bounded first one.
func LastTold(dir string) time.Time {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return time.Time{}
	}
	raw, err := os.ReadFile(filepath.Join(dir, toldName))
	if err != nil {
		return time.Time{}
	}
	var stamp toldStamp
	if json.Unmarshal(raw, &stamp) != nil || stamp.Schema != deltaToldSchema {
		return time.Time{}
	}
	return stamp.ToldAt
}

// NoteTold records that the block has gone in front of this session's model.
//
// Errors are dropped, on [NoteLook]'s reasoning: the stamp is a convenience
// over a conversation that works without it, and a read-only disk must not turn
// a turn into a fault. A TORN WRITE COSTS ONE REPEAT — an unparsable stamp reads
// as no stamp, and the next delivery is the bounded first one again, which is
// the harmless direction to fail in.
//
// IT ONLY EVER MOVES FORWARD. Two readings can be in flight over one
// conversation — the second turn's starts while the first turn's is still
// walking the index — and they are written behind the path ([Agent.toldStamp]),
// so the order they LAND in is not the order they were taken in. An older stamp
// landing last would re-read landings the model has already been told about,
// which [deltaRemember] would then de-duplicate away: news that is never told.
// Reading before writing costs one open of a file this call has to open anyway.
func NoteTold(dir string, at time.Time) {
	dir = strings.TrimSpace(dir)
	if dir == "" || at.IsZero() {
		return
	}
	// The folder may not exist for a memory-only conversation; creating one for
	// a stamp alone would invent a session on disk that nothing else believes in.
	if _, err := os.Stat(dir); err != nil {
		return
	}
	if was := LastTold(dir); !was.Before(at) {
		return
	}
	raw, err := json.Marshal(toldStamp{Schema: deltaToldSchema, ToldAt: at.UTC()})
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, toldName), append(raw, '\n'), 0o600)
}

// ── the past half ───────────────────────────────────────────────────────────

// deltaLanding is one landing as the block names it: the smallest citation that
// tells a model something it did not know.
//
// It is a flattened copy rather than a [TaskIndexEntry] because the rolling list
// below outlives the reading it came from, and a row carrying a live Activity
// string, a cost and two URIs would be a session holding a hundred fields to
// print three.
type deltaLanding struct {
	// Key is (SessionID, ID) — the pair that identifies a row across the whole
	// project (see [TaskIndexEntry.ID]) — and it is what keeps one landing from
	// being told twice when a clock skews or a stamp is lost.
	Key     string
	Label   string
	Status  string
	Outcome string
	Files   []string
	// Wrote is the honest total behind Files, which is capped.
	Wrote int
	// Unread says the row's file list could not be read when it was written
	// ([TaskIndexEntry.FilesUnread]). It is the one thing that tells a row
	// whose files nobody could read from a row that named none, and the row
	// says so rather than reading like one that touched nothing.
	Unread bool
	// Project is the other project folder the landing was filed under, and ""
	// for this chat's own. Score is how much it has to do with this chat
	// ([elsewhereScope.score]); the block keeps the highest when the cap bites.
	Project string
	Score   int
}

// landedElsewhere is the past half: work that FINISHED in this project, in some
// OTHER conversation, strictly after a moment.
//
// OWN ROWS ARE LEFT OUT, always. This session's own landings reached the model
// as the task's own report, in the model's own conversation, in full — telling
// it a second time in three lines would be this block competing with the
// transcript about work the transcript already carries.
//
// LIVE ROWS ARE LEFT OUT TOO, and they are the present half's to report
// ([Elsewhere.Tasks]). A row that appeared in both halves would be one piece of
// work counted twice, which is [LandedTouching]'s rule said again here.
//
// The newest are kept where the cap bites, because news is what is new.
//
// mine is every id THIS conversation answers to: the journal header's, which is
// what its own rows were written with ([Agent.sessionID]), and the session
// folder's name, which is what [Elsewhere] excludes on. They are the same string
// in every session that has a folder, and naming both costs one comparison to be
// certain a build where they diverge does not read this session's own work back
// to it as news.
func landedElsewhere(rows []TaskIndexEntry, mine []string, after time.Time, limit int) []deltaLanding {
	own := make(map[string]bool, len(mine))
	for _, id := range mine {
		if id = strings.TrimSpace(id); id != "" {
			own[id] = true
		}
	}
	var out []deltaLanding
	for _, row := range rows {
		if row.Live() || row.EndedAt.IsZero() || !row.EndedAt.After(after) {
			continue
		}
		if id := strings.TrimSpace(row.SessionID); id == "" || own[id] {
			continue
		}
		out = append(out, deltaLandingOf(row))
		if len(out) >= limit {
			break
		}
	}
	return out
}

// deltaLandingOf is one index row flattened to what the block names.
func deltaLandingOf(row TaskIndexEntry) deltaLanding {
	files, wrote := row.Files, row.FilesChanged
	if len(files) > deltaRowFiles {
		files = files[:deltaRowFiles]
	}
	return deltaLanding{
		Key:     row.SessionID + "\x00" + row.ID,
		Label:   deltaLine(row.Label),
		Status:  strings.TrimSpace(row.Status),
		Outcome: deltaLine(row.Outcome),
		Files:   append([]string(nil), files...),
		Wrote:   wrote,
		Unread:  strings.TrimSpace(row.FilesUnread) != "",
	}
}

// rankedLandings is the past half of one wide reading ([Agent.readElsewhereWide]),
// best first and cut to the cap: the highest score first, and the newest first
// among rows that score the same, which is the order the block always kept.
func rankedLandings(reading elsewhereReading, limit int) []deltaLanding {
	rows := append([]TaskIndexEntry(nil), reading.landed...)
	key := func(row TaskIndexEntry) string { return row.SessionID + "\x00" + row.ID }
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := reading.scores[key(rows[i])], reading.scores[key(rows[j])]
		if left != right {
			return left > right
		}
		return rows[i].EndedAt.After(rows[j].EndedAt)
	})
	var out []deltaLanding
	for _, row := range rows {
		landing := deltaLandingOf(row)
		landing.Project = deltaLine(reading.names[key(row)])
		landing.Score = reading.scores[key(row)]
		out = append(out, landing)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// rankLandings orders what the block will say, best first, keeping the order it
// was given among rows that score the same, and cuts it to the cap. It is what
// lets a landing the model was already told stay ahead of fresher news that has
// less to do with this chat.
func rankLandings(landed []deltaLanding, limit int) []deltaLanding {
	out := append([]deltaLanding(nil), landed...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// deltaRemember folds new landings into the ones this session has already been
// told, newest first, dropping the OLDEST past the cap.
//
// THE LIST IS KEPT AFTER IT IS DELIVERED, and that is the one thing here that
// is not obvious. The stamp advances the moment the block goes out, so a naive
// block would name a landing on one turn and forget it on the next — and a
// conversation whose model was told at turn five and is editing at turn nine
// would be editing stale again, which is the entire failure this file exists to
// stop. So the block carries a SHORT MEMORY of what it has said, bounded exactly
// like the state card's lists (card.go's [cardAppend]) and dropped from the
// front for the same reason. The stamp still moves, which is what stops the same
// landing being read out of the index twice.
func deltaRemember(told, fresh []deltaLanding, limit int) []deltaLanding {
	if len(fresh) == 0 {
		return told
	}
	seen := make(map[string]bool, len(told))
	for _, landing := range told {
		seen[landing.Key] = true
	}
	next := append([]deltaLanding(nil), fresh...)
	for _, landing := range told {
		next = append(next, landing)
	}
	// Fresh rows go in front and the already-told fall behind them; duplicates
	// keep the first spelling seen, which is the newest reading of that row.
	out := make([]deltaLanding, 0, len(next))
	seen = make(map[string]bool, len(next))
	for _, landing := range next {
		if seen[landing.Key] {
			continue
		}
		seen[landing.Key] = true
		out = append(out, landing)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// ── the block ───────────────────────────────────────────────────────────────

// renderElsewhereBlock is the whole block, or "" when there is nothing to say.
//
//	<elsewhere>
//	Work on this project from outside this conversation. Facts, not requests.
//	recently landed in other windows:
//	- Fix the nil-map crash · done · internal/reconciler/state.go, internal/reconciler/state_test.go
//	  Added the guard and the regression test; the parser suite passes.
//	running in another window now:
//	- Sweep the call sites · window "docs pass" · internal/session/agent.go
//	- Survey the config loaders · window "docs pass" · 3 quick parts running · internal/config/load.go
//	- Compare the two lockfiles · quick · window "release"
//	</elsewhere>
//
// A NODE'S PARTS RIDE ON ITS ROW ([foldElsewhere]), and a kind that is not the
// ordinary one says so in its own word ([TaskKindWord]). Both are there for the
// one reason this half exists — so this model does not start work another
// window already has out — and both were missing: eight quick parts under one
// task read as eight unrelated jobs, which is the exact input that makes a
// model propose the same work a ninth time, and a quick task read like a task
// though it writes in that window's folder while it runs.
//
// It is the <memory> and <state> blocks' shape (memory.go, card.go), tagged the
// same way and joined onto message[0] the same way, because a model that has
// learned to read two fenced blocks in its system prompt should not have to
// learn a third grammar for the third.
//
// EVERY CLAUSE WITH NOTHING IN IT IS DROPPED rather than written empty: a row
// reading "· ·" is two facts this build does not have, stated as though it did.
//
// A ROW FROM ANOTHER PROJECT FOLDER SAYS WHICH ONE (`in home`), and only that
// row: the folder is the one fact that tells work on this repository from
// somewhere else apart from a window beside this one.
//
// AND WHAT COULD NOT BE LOOKED AT IS SAID, under the lead line (notes). A
// reading that could not resolve this chat's repository, or could not read
// another folder's record, did not find "nothing elsewhere" — it did not look —
// and when the block would otherwise be empty that line is the whole block,
// because silence would read as the first and the truth is the second.
func renderElsewhereBlock(landed []deltaLanding, live []ElsewhereTask, notes ...string) string {
	var body strings.Builder
	for _, note := range notes {
		if note = deltaLine(note); note != "" {
			body.WriteString(note + "\n")
		}
	}
	if len(landed) > 0 {
		// "recently" AND NOT "since you were last told", although the stamp is
		// what selects these. The list keeps what it has already said
		// ([deltaRemember]), so a heading claiming everything under it is news
		// would be untrue the moment a second landing arrived beside a first
		// the model was shown three turns ago.
		body.WriteString("recently landed in other windows:\n")
		for _, row := range landed {
			parts := []string{row.Label}
			if row.Status != "" {
				parts = append(parts, row.Status)
			}
			if row.Project != "" {
				parts = append(parts, "in "+row.Project)
			}
			if word := deltaFilesWord(row.Files, row.Wrote); word != "" {
				parts = append(parts, word)
			} else if row.Unread {
				parts = append(parts, deltaFilesUnknown)
			}
			body.WriteString("- " + strings.Join(parts, " · ") + "\n")
			if row.Outcome != "" {
				body.WriteString("  " + row.Outcome + "\n")
			}
		}
	}
	families := foldElsewhere(live)
	// THE SAME RANKING AS THE PAST HALF, over whole families: a family counts
	// for its best member, and families that score the same keep the order they
	// were read in, which is newest window first.
	sort.SliceStable(families, func(i, j int) bool { return families[i].score() > families[j].score() })
	if len(families) > deltaLiveRows {
		families = families[:deltaLiveRows]
	}
	if len(families) > 0 {
		body.WriteString("running in another window now:\n")
		for _, family := range families {
			body.WriteString(family.row())
		}
	}
	if body.Len() == 0 {
		return ""
	}
	return "\n<elsewhere>\nWork on this project from outside this conversation. Facts, not requests.\n" +
		body.String() + "</elsewhere>\n"
}

// elsewhereFamily is one row of the block's present half: a piece of work
// another window has out, with the parts it handed out folded under it.
type elsewhereFamily struct {
	head  ElsewhereTask
	parts []ElsewhereTask
}

// score is the family's best member's ([ElsewhereTask.score]).
func (f elsewhereFamily) score() int {
	best := f.head.score
	for _, part := range f.parts {
		best = max(best, part.score)
	}
	return best
}

// foldElsewhere folds every part onto the work at the top of its family, in
// the order the families first appear.
//
// THE FAMILY IS READ OFF THE ROWS THEMSELVES, one window at a time: a row whose
// [PresenceTask.Parent] names another row of the SAME window hangs under it, and
// a part of a part climbs to the top, because a reader deciding whether to start
// the same work needs to know it is one piece of work and not how deep its tree
// is. A row whose parent is not in the reading — the parent landed while its
// parts run on, or a build too old to say — stands on its own, which is exactly
// how it read before the field existed. Ids are only unique inside one window,
// so the window is half of every key.
func foldElsewhere(live []ElsewhereTask) []elsewhereFamily {
	type key struct{ session, id string }
	at := make(map[key]int, len(live))
	for index, row := range live {
		at[key{row.SessionID, row.Task.ID}] = index
	}
	top := func(index int) int {
		// Every step climbs to a row the reading holds, and a file that named a
		// cycle is stopped at the first row seen twice rather than trusted.
		seen := map[int]bool{index: true}
		for {
			row := live[index]
			parent, held := at[key{row.SessionID, row.Task.Parent}]
			if row.Task.Parent == "" || !held || seen[parent] {
				return index
			}
			seen[parent] = true
			index = parent
		}
	}
	var out []elsewhereFamily
	placed := make(map[int]int, len(live))
	for index := range live {
		head := top(index)
		slot, open := placed[head]
		if !open {
			slot = len(out)
			placed[head] = slot
			out = append(out, elsewhereFamily{head: live[head]})
		}
		if head != index {
			out[slot].parts = append(out[slot].parts, live[index])
		}
	}
	return out
}

// row is one family as the block's present half draws it: the title, the kind
// where it is not the ordinary one, the window, the parts, and the files.
//
// EVERY CLAUSE WITH NOTHING IN IT IS DROPPED ([renderElsewhereBlock]'s law).
func (f elsewhereFamily) row() string {
	at := f.head
	title := deltaLine(at.Task.Title)
	if title == "" {
		// A window running work nothing has titled still says something worth
		// saying — which files it is in — so the row is kept and only the
		// missing words are.
		title = "untitled work"
	}
	parts := []string{title}
	if word := TaskKindWord(at.Task.Kind); word != "" {
		parts = append(parts, word)
	}
	// THE WINDOW'S NAME AND NOTHING ELSE. An unnamed window contributes no clause
	// (the emptiness law); its id is hex and would put a machine's handle where a
	// person's word belongs.
	if name := deltaLine(at.Session); name != "" {
		parts = append(parts, `window "`+name+`"`)
	}
	if project := deltaLine(at.Project); project != "" {
		parts = append(parts, "in "+project)
	}
	if word := elsewherePartsWord(f.parts); word != "" {
		parts = append(parts, word)
	}
	written := f.files()
	files := written
	if len(files) > deltaRowFiles {
		files = files[:deltaRowFiles]
	}
	if word := deltaFilesWord(files, len(written)); word != "" {
		parts = append(parts, word)
	}
	return "- " + strings.Join(parts, " · ") + "\n"
}

// files is every path the family has written so far, the head's first and each
// part's after it, each named once.
func (f elsewhereFamily) files() []string {
	written := append([]string(nil), f.head.Task.Files...)
	for _, part := range f.parts {
		written = mergePaths(written, part.Task.Files)
	}
	return written
}

// elsewherePartsWord is how many parts a family has out, and how many of them
// are quick — `3 parts running`, `3 quick parts running`, `3 parts running, 2
// quick` — and "" for work that handed nothing out.
//
// QUICK IS COUNTED BECAUSE IT CHANGES WHERE THE WRITING IS. A task's part
// writes in a copy of its own and comes home through a merge; a quick part
// writes in that window's folder while it runs. How far down its list each one
// is stays off this line on purpose: a count that moved with every tick would
// churn the block the header promises does not churn.
func elsewherePartsWord(parts []ElsewhereTask) string {
	if len(parts) == 0 {
		return ""
	}
	quick := 0
	for _, part := range parts {
		if part.Task.Kind == TaskKindQuick {
			quick++
		}
	}
	noun := "parts"
	if len(parts) == 1 {
		noun = "part"
	}
	count := strconv.Itoa(len(parts))
	kind := TaskKindWord(TaskKindQuick)
	switch quick {
	case 0:
		return count + " " + noun + " running"
	case len(parts):
		return count + " " + kind + " " + noun + " running"
	}
	return count + " " + noun + " running, " + strconv.Itoa(quick) + " " + kind
}

// deltaFilesUnknown is the clause a landing carries in place of its files when
// its file list could not be read ([TaskIndexEntry.FilesUnread]). It is a word
// and not silence because silence is what a row that named no files says.
const deltaFilesUnknown = "files unknown"

// deltaFilesWord is a row's paths, and the honest total where the list was cut.
// No files is NOT "touched nothing" — it is a run that has not written yet, or a
// row from a build too old to say — so it answers "" and the clause disappears
// rather than claiming an absence nobody can verify (taskclaims.go's second law).
func deltaFilesWord(files []string, wrote int) string {
	var kept []string
	for _, path := range files {
		if path = strings.TrimSpace(path); path != "" {
			kept = append(kept, path)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	word := strings.Join(kept, ", ")
	if wrote > len(kept) {
		word += ", and " + strconv.Itoa(wrote-len(kept)) + " more"
	}
	return word
}

// deltaLine flattens one field to a single bounded line. A title or an outcome
// carrying a newline would turn one row of this block into three, and a
// paragraph where a sentence was expected is how a small block stops being one.
func deltaLine(text string) string {
	line := strings.Join(strings.Fields(text), " ")
	if runes := []rune(line); len(runes) > deltaOutcomeRunes {
		line = strings.TrimSpace(string(runes[:deltaOutcomeRunes])) + "…"
	}
	return line
}

// ── the delivery ────────────────────────────────────────────────────────────

// refreshElsewhere reads what the other windows on this project have landed and
// have out, and puts it in front of the model.
//
// IT IS A READING BESIDE THE WORK AND GOES THROUGH THE ONE DOOR FOR THAT SHAPE
// ([readBeside], sidecar.go), for [Agent.refreshMemory]'s reason said about disk
// instead of a provider: this opens the project index and one small JSON per
// live window, and a read of a shared directory must never be the thing a
// person's keystroke waits behind. It was a bare `go` of its own until it was
// measured writing its stamp into a conversation's folder after that
// conversation had closed — which is what an unowned goroutine is, and what
// sidecar.go exists to stop there being a fifth of.
//
// SO IT TAKES THE READING'S OWN CONTEXT AND OBEYS IT, at the two points where
// obeying it costs anything: before the walk, and again before the assignment.
// A reading that ignored its context would have exactly the lifetime the bare
// `go` had, and the door would be decoration.
//
// AND NOTHING IT WRITES HAPPENS ON THE PATH. The stamp is OWED under the lock
// and performed behind it ([Agent.toldStamp], placemeta.go's [stampWriter]) —
// `a.mu` is the lock every request goes through, and a stat-marshal-write inside
// it is the exact shape #876 took out of the meta stamp. The owe is registered
// while the session is still open, which is what puts it ahead of `closed` and
// therefore inside the settle [Agent.Close] already performs.
//
// THE FIRST TURN IS SESSION OPEN. A model does not exist between the
// constructor and its first request, so there is no earlier moment at which it
// could be "told" — and stamping it told in the constructor would mark a day of
// landings seen on a session the person opened and closed without typing.
//
// WHAT IS SIMPLY ABSENT FAILS QUIET. A conversation with no folder, a project
// with no index, an unreadable stamp, a working directory that is no repository:
// each answers an empty block, and a turn with an empty block is a turn exactly
// as it would have been. What is THERE AND COULD NOT BE READ — a repository whose
// git link points at nothing, another folder's record that would not open — is
// said in one line instead ([renderElsewhereBlock]'s notes), because reading it
// as nothing would tell the model nobody else is on its repository.
func (a *Agent) refreshElsewhere(ctx context.Context) {
	// THE SAME PREDICATE THE CALL SITE ASKED, asked again by the door that acts
	// on it — [Question.Revisable]'s shape exactly: one reading of who may be
	// told, asked by the loop that decides whether to start a reading at all and
	// by the door that would otherwise tell a task node something its contract
	// took away (loop.go, [Agent.tellsElsewhere]).
	if !a.tellsElsewhere() {
		return
	}
	if ctx.Err() != nil {
		return
	}
	a.mu.Lock()
	if a.closed {
		// A CLOSED SESSION DOES NOT EVEN WALK THE INDEX. The check is here, at
		// the first thing this reading does with the lock, rather than only at
		// the assignment: a project index and one JSON per live window is disk
		// nobody will ever read the answer to.
		a.mu.Unlock()
		return
	}
	mine := a.sessionID()
	a.mu.Unlock()

	dir := strings.TrimSpace(a.config.Place.Dir)
	now := time.Now()
	since := LastTold(dir)
	if since.IsZero() {
		since = now.Add(-deltaFirstReach)
	}

	// THE FILES AND NOT [Agent.TaskIndex]. That door merges THIS session's live
	// graph over the file, and this half of the block is by definition about
	// other conversations' landed work — a merge would cost a scheduler nobody
	// asked for and could not add a single row this reads. The files are this
	// project folder's and every other folder's rows on this chat's
	// repositories, ranked ([Agent.readElsewhereWide], taskrepo.go).
	reading := a.readElsewhereWide(dir, []string{mine, a.config.Place.ID()}, since, now)
	fresh := rankedLandings(reading, deltaLandedRows)
	live := reading.live

	// ── EVERYTHING THIS READING CHANGES, UNDER ONE HOLD OF THE LOCK ──────────
	//
	// NOTHING IT COULD SAY WOULD REACH ANYBODY once the session has closed, so a
	// closed session takes neither the block nor the stamp. It is the ask lane's
	// own law about a clock that ran out after the door shut
	// ([Agent.askClockRanOut]), said about a reading instead of a decision, and
	// it is what makes "nothing armed outlives the session that armed it"
	// (steer_grace.go) true of this goroutine: a reading still in flight when
	// [Agent.Close] runs either owes its stamp BEFORE `closed` is set — and the
	// close's own settle then lands it — or finds the door shut and leaves the
	// folder alone. Measured on the Spark, 2026-09-12: thirteen runs in five
	// thousand of one asklane fixture failed as `TempDir RemoveAll cleanup:
	// directory not empty`, and the file left behind was this stamp.
	//
	// THE CHECK AND THE WRITES ARE ONE HOLD BECAUSE THE CHECK IS TRUE ONLY FOR
	// AS LONG AS IT IS HELD — `Close` sets `closed` under this same lock. The
	// shape is deliberate and temporary: #999 lands [Agent.writeIfOpen], the one
	// door for exactly this — write nothing into a conversation that has closed,
	// already carried by the memory pass and the phase news — and this region
	// becomes a call to it with the three lines below as its closure. Nothing but
	// those three lines belongs between the check and the unlock until it does.
	//
	// AND THE TURN ENDING IS NOT THAT DOOR. The context is what stops this
	// reading STARTING work — the walk above — and it deliberately does not stop
	// the answer landing: `elsewhereText` is not this turn's, it is the
	// conversation's, and the note the next request opens with reads it
	// ([Agent.landVolatileLocked]). That is the bargain loop.go states in so many
	// words, "a read that lands after the first request rides the next step", and
	// it is the difference between a slow disk costing one turn and a slow disk
	// costing every turn: dropped here, the next reading starts from the same
	// unmoved `since` and re-walks the same index, forever, delivering nothing on
	// a machine where the walk outlasts a short turn.
	//
	// AND THE STAMP IS OWED WITH THE ASSIGNMENT RATHER THAN AHEAD OF THE READS.
	// It says THIS SESSION'S MODEL WAS TOLD, and the assignment is the last
	// moment at which that can still be arranged: after it the block is in
	// `elsewhereText` and the next request carries it. That is not the same as
	// the model having READ it, and this stamp cannot honestly claim more than it
	// — `elsewhereTold` lives in memory alone, so a process that stops between
	// here and the next request loses those rows from the block while the stamp
	// says they were told. What the old order — stamping before the reads —
	// bought over this was nothing at all, and what it cost was that same loss
	// over the whole of the walk, plus every session that closed during one.
	//
	// The assignment itself puts the block at the tail of the transcript, in the
	// note the drain lands immediately before the next request, and not in
	// message[0] where it used to sit: writing it there re-priced every message
	// of the conversation behind it each time another window landed something.
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.oweToldStampLocked(dir, now)
	a.elsewhereTold = rankLandings(deltaRemember(a.elsewhereTold, fresh, len(a.elsewhereTold)+len(fresh)), deltaLandedRows)
	a.elsewhereText = renderElsewhereBlock(a.elsewhereTold, live, reading.notes...)
	a.mu.Unlock()
}

// toldStamp is the one deferred write this reading owes told.json.
//
// It is [Agent.metaStamp]'s shape for [Agent.metaStamp]'s reason, and the two
// are separate writers because a [stampWriter] coalesces by replacing its patch:
// sharing one would let a meta stamp drop a told stamp owed a moment earlier.
// Built on first use, so a conversation that is never told starts no goroutine.
func (a *Agent) toldStamp() *stampWriter {
	a.toldStampOnce.Do(func() { a.toldStampWriter = &stampWriter{} })
	return a.toldStampWriter
}

// oweToldStampLocked owes the stamp for the reading that has just landed, and
// the INSTANT IT WRITES IS THE LATEST ONE OWED rather than the last one owed.
//
// A [stampWriter] coalesces by REPLACING its patch, so two owes collapse into
// the later CLOSURE — which is the same thing as the later instant only while
// the owes are ordered, and there is one window in this session where they are
// not. [Agent.Abandon] releases `a.mu` before it cancels, so an older reading
// can be inside this function while a newer one, started by the turn that
// replaced it, is already through: the older closure would then be the survivor
// and would write a stamp BEHIND the block the model has actually been handed,
// which re-reads landings it was told about and [deltaRemember] then
// de-duplicates into news nobody is ever told.
//
// So the instant is kept beside the writer under `a.mu` and only ever moves
// forward, and the patch writes what is kept rather than what it closed over.
// [NoteTold]'s own forward-only guard is the same law one layer down, against
// the other process on the same folder; this one is about this session's own two
// readings, which that guard cannot see because they are minted from one clock.
func (a *Agent) oweToldStampLocked(dir string, at time.Time) {
	if at.After(a.toldAtOwed) {
		a.toldAtOwed = at
	}
	a.toldStamp().Owe(func() {
		a.mu.Lock()
		owed := a.toldAtOwed
		a.mu.Unlock()
		NoteTold(dir, owed)
	})
}

// tellsElsewhere reports whether this agent is one that may be told about other
// windows at all.
//
// A TASK NODE IS NOT. A node's brief is its whole world by contract
// (task_contract.go) and its `tasks` tool is scoped to the pieces it handed out
// itself (tools_tasks.go); handing it a running account of the project's other
// windows would be giving it, through the back door, exactly the context the
// contract took away. A conversation is where a person sits, and a person's
// model is the one that has to know whose ground is moving.
//
// A conversation with no folder is not, either: no folder is no stamp to
// advance, and no bucket to read the other windows out of.
func (a *Agent) tellsElsewhere() bool {
	if a.config.InTask || a.config.taskID != 0 {
		return false
	}
	return strings.TrimSpace(a.config.Place.Dir) != ""
}
