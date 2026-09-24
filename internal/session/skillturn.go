// The skills one message carries, chosen from the words of the message itself.
//
// The catalog (skillcatalog.go) is the MENU: a windowed, stable section of the
// prompt prefix that names the shelf. It cannot choose, because its only signal
// is the workspace path, which does not change between turns. This file is the
// choosing half: on the text of the message being sent it pins what the words
// name outright, retrieves what shares words with them, and puts the person's
// own attachments ahead of both — then renders the result WITH THE TURN rather
// than in the prefix, because a prefix that changed with every message would
// be a cache miss on every message (orientation/digest.go, lane/choose.go,
// prefixbudget_test.go).
//
// THE ORDER IS THE PERSON'S. An attachment is somebody saying "use this one"
// (skillattach.go), which is not a guess and may never be scored away or
// windowed: retrieval fills whatever room the attachments leave, and never
// takes a place from one.
//
// ZERO SKILLS IS ZERO BYTES. A conversation with an empty shelf and nothing
// attached renders byte-for-byte what it rendered before this file existed,
// because nothing is appended to the message at all — the same law the catalog
// section obeys.
package session

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/plan"
	store "github.com/Agent-Field/codeaf/internal/store"
)

// skillTurnMax bounds how many skills one turn's block may carry in total.
//
// IT BOUNDS THE RETRIEVED HALF, NOT THE PERSON'S. A retrieved skill is a guess
// and four guesses beside one message is already a menu rather than an
// instruction; an attachment or a name the person typed is their own choice and
// is never cut by this number — the block grows past it when the person asked
// for that many, because the person asked for that many.
const skillTurnMax = 4

// skillTurnResolveLimit is how far into the active shelf the resolution reads,
// from the one source of truth in internal/store (the executor's own
// skillEntries reads the same bound, so a name resolves the same way here as
// it does in a task's brief).
const skillTurnResolveLimit = store.SkillShelfLimit

// attachTurnSkillsLocked composes the block for one message the person is
// sending and splices it onto what the model reads, under a.mu, at the one
// door every person-typed message passes through (agent.go submitUser). It is
// THE [userMessage.said] SHAPE, not a new one: the message the model reads
// grows by the block, and the journal and the store keep the words the person
// actually typed — a replay is a reading of the conversation, and the block
// was chosen for one message, not said by anybody.
//
// A MESSAGE WITH PICTURE PARTS IS LEFT ALONE. Its journal line carries the
// parts beside the text, and a text-only replacement would strand the
// references; the words of a picture message still reach the catalog and
// `use_skill`, so nothing is lost by waiting for the next text one.
func (a *Agent) attachTurnSkillsLocked(user *userMessage) {
	if user.empty() || user.refs != nil {
		return
	}
	words := user.text()
	block, carried := a.turnSkills(words)
	if block == "" {
		return
	}
	if user.said == "" {
		user.said = words
	}
	user.message = textMessage("user", messageContentText(user.message)+block)
	user.skills = carried
}

// turnSkills composes the skills one message carries and renders them as the
// block appended to it. It returns the rendered block and the ordered names the
// turn carried — the names are the half a surface is told about
// ([turnSkillsNote]), and they are only the ones that resolved, because a name
// the shelf does not hold has nothing to say. Empty string means the message
// carries nothing and must not be touched at all.
//
// THE MESSAGE'S OWN WORDS ARE THE SIGNAL, which is the whole difference from
// the catalog's window: two messages in the same workspace can carry two
// different blocks, and a skill whose whole subject is what was just asked is
// no longer crowded out by one that shares a folder name.
func (a *Agent) turnSkills(text string) (string, []string) {
	text = strings.TrimSpace(text)
	shelf := a.config.skillShelf()
	if shelf == nil || text == "" {
		return "", nil
	}
	facts, err := shelf.SkillFacts(store.FactActive, skillTurnResolveLimit)
	if err != nil {
		// A shelf that cannot be read is no shelf: nothing is attached, the
		// message goes out as the person typed it, and the next turn reads a
		// shelf that may have come back.
		return "", nil
	}

	// Attachments first, then what the words name outright, then retrieval in
	// whatever room is left. ComposeSkills is the task road's own order rule —
	// earlier wins — so an attached skill keeps its place ahead of a pinned or
	// retrieved one of the same name.
	attached := append([]string(nil), a.attachedSkills...)
	pinned := plan.ComposeSkills(attached, plan.PinnedSkills(text, facts))
	room := skillTurnMax - len(pinned)
	if room < 0 {
		room = 0
	}
	names := plan.ComposeSkills(pinned, fillSkillRoom(plan.RetrieveSkills(text, a.config.Workspace, facts), room))

	// Resolution is the executor's shape (internal/exec's skillEntries): the
	// first fact a name hits is the one every other reader of that name serves,
	// and a name with no fact is dropped rather than rendered as an empty line.
	byName := make(map[string]store.Fact, len(facts))
	for _, fact := range facts {
		if name := fact.SkillName(); name != "" {
			if _, held := byName[name]; !held {
				byName[name] = fact
			}
		}
	}
	entries := make([]plan.SkillEntry, 0, len(names))
	carried := make([]string, 0, len(names))
	for _, name := range names {
		if fact, held := byName[name]; held {
			entries = append(entries, plan.SkillEntryFromFact(fact))
			carried = append(carried, name)
		}
	}
	block := plan.RenderSkillsBlock(entries)
	if block == "" {
		return "", nil
	}
	return "\n\nSkills suited to this message:\n" + block, carried
}

// fillSkillRoom takes at most room names off the retrieved half. It is the one
// place [skillTurnMax] bites: attachments and pins have already taken their
// seats, and the guesses fill only what is left.
func fillSkillRoom(retrieved []string, room int) []string {
	if room <= 0 || len(retrieved) <= room {
		return retrieved
	}
	return retrieved[:room]
}

// turnSkillsNote is the one line that says which skills a turn carried — the
// dim notice shape the rest of this package reports its own machinery through
// (session.go's [EventNotice]), so a surface that already draws those lines
// draws this one too and a surface that ignores them is unchanged.
func turnSkillsNote(names []string) string {
	return "skills carried: " + strings.Join(names, ", ")
}

// turnSkillsNotice is the whole event, and it is one function so the sentence
// and the field can never drift apart. A surface reads [Event.Skills]; the
// sentence is for a reader that draws notices as prose and nothing else.
func turnSkillsNotice(names []string) Event {
	return Event{
		Kind:   EventNotice,
		Text:   turnSkillsNote(names),
		Skills: append([]string(nil), names...),
	}
}
