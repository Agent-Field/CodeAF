package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// WHAT A SESSION IS CALLED, AND WHAT IT IS CALLED ON SCREEN.
//
// A session names itself once, from its first exchange, in five to eight lowercase words
// (internal/session's title.go). That is the name in the file and the name this
// surface draws — except when it is not: a session named by something other than
// that namer, or by a model that answered the instruction with a slug, arrives
// here as ONE TOKEN with its words welded together —
//
//	port_b_parser_fix        →  Port B Parser Fix
//	fix-the-nil-map          →  Fix The Nil Map
//	porting the parser       →  porting the parser   (already words; untouched)
//	20260816-150405_a1b2c3   →  20260816-150405_a1b2c3   (an id; untouched)
//
// — and a machine name in the identity cluster is the surface saying its own
// filing system out loud. So a one-token name is read back as words WHERE IT IS
// DRAWN, and nowhere else: the raw string stays in the file, in the resume path
// and in whatever slug some other tool minted it from, because those are
// identifiers and an identifier that is prettied is an identifier that no longer
// matches.
//
// THE GUARD IS THE WHOLE DESIGN. Only a token whose every segment reads as a
// WORD is expanded — a segment of digits, a hex tail, an extension — any of them
// and the name is left exactly as it is. The failure this prevents is the one
// that matters: a session file's own name, which is a timestamp and a random
// tail, turned into "20260816 150405 A1b2c3" and offered to a person as a title.

// sessionName is the conversation's name as the frame draws it: the session's
// own, read back as words when it arrived as one token.
func (a *app) sessionName() string { return readableName(a.title) }

// unnamedConversationWord describes an empty conversation in breadcrumbs and
// notices. Empty shells have no tab; drafts and sent prompts supply their own names.
const unnamedConversationWord = "new conversation"

func chatTabName(raw string) string {
	if name := readableName(strings.TrimSpace(raw)); name != "" {
		return name
	}
	return unnamedConversationWord
}

// conversationName keeps generated names separate from the opening prompt so
// erasing a draft removes an empty tab without losing a submitted conversation.
func (a *app) conversationName() string {
	if name := strings.TrimSpace(a.title); name != "" && name != unnamedConversationWord {
		return readableName(name)
	}
	if name := strings.TrimSpace(a.openingPrompt); name != "" {
		return promptName(name)
	}
	for _, e := range a.entries {
		if e.kind == entryUser && strings.TrimSpace(e.text) != "" {
			return promptName(e.text)
		}
	}
	// An answer to a question is not an opening prompt, and may be a credential.
	if _, answering := a.questionHead(); answering {
		return ""
	}
	main := a.mainComposer()
	return promptName(main.box.String())
}

// Prompt names preserve the person's words; only whitespace and display width change.
func promptName(text string) string { return strings.Join(strings.Fields(text), " ") }

func (a *app) chatDisplayName() string {
	if name := a.conversationName(); name != "" {
		return name
	}
	return unnamedConversationWord
}
func (a *app) chatTabDisplayName() string { return a.chatDisplayName() }

// ── the naming lane ─────────────────────────────────────────────────────────
//
// A CONVERSATION NAMES ITSELF WHILE THE ANSWER IS STILL BEING WRITTEN, and since
// #653 it starts doing so the moment the person's first message is accepted
// rather than when the turn ends (session's title.go). The turn's own stream
// still carries [session.EventTitleChanged] when a turn is running, which is
// where this surface has always read it; the standing lane below carries it for
// the case that change created — a question answered in four seconds, named in
// six, with no turn left to carry the news.
//
// It is [app.watchDesigns]' shape exactly, down to the generation: the channel
// belongs to the agent that handed it over, so a replaced conversation gets a
// new one and events from the old one are dropped by their generation.

// namedAgent is the standing subscription to the name a conversation gives
// itself. It is asserted rather than added to [Agent] for [designAgent]'s
// reason: a scripted agent in this package's tests has never heard of it, and a
// surface driven by one must stay representable.
type namedAgent interface {
	TitleChanges() <-chan session.Event
}

// leavableNamer is the naming lane WITH A WAY OUT OF IT (session's title.go).
type leavableNamer interface {
	WatchTitle() (<-chan session.Event, func())
}

// watchTitles opens the lane and starts pumping it. It is called wherever
// [app.watchDesigns] is.
func (a *app) watchTitles() tea.Cmd {
	agent, ok := a.agent.(namedAgent)
	if !ok {
		return nil
	}
	a.titleGen++
	if leavable, ok := agent.(leavableNamer); ok {
		a.titleLane, a.stops.titles = leavable.WatchTitle()
	} else {
		a.titleLane, a.stops.titles = agent.TitleChanges(), nil
	}
	return waitTitle(a.titleLane, a.titleGen)
}

// waitTitle takes one event off the lane and asks for the next.
func waitTitle(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return titleLaneClosedMsg{gen: gen}
		}
		return titleEventMsg{gen: gen, ev: ev}
	}
}

// listName is what a conversation is called ON A LIST, and it is [humanName]'s
// ladder with its last rung taken out.
//
// THE DEFECT IT FIXES. That ladder ends at the transcript's own file name, and
// under Decision 26 a session folder is named with an id, so the row a person is
// most likely to be standing in — the one they just opened, which nothing has
// named yet — drew `○ 927D303242f9d00e     codeaf here`. At sixty columns
// that hex is a third of the row, and it is a machine's word in the one column
// whose whole job is matching names. It is worse than nothing, too, because
// title-casing it makes it read as a name SOMEBODY CHOSE: a person scanning for
// their own conversation has to learn that the capitalised thing is not one.
//
// WHAT THE ROW ACTUALLY HAS. A [session.SessionRow] carries the title and the
// paths and no transcript text at all — no opening line, nothing the person
// typed — and home may not go and read one, because this list is rebuilt on
// every frame and reading a transcript here is reading a hundred of them sixty
// times a second (tui3.go's seam law). So the person's own words are not
// available to borrow at this layer, and the honest answer is a WORD: the
// emptiness law says an unknown draws nothing, and where a column cannot be
// blank it says so in a sentence rather than in hex.
//
// THE FILE NAME'S RUNG SURVIVES FOR THE ONE CASE IT WAS RIGHT ABOUT: a stem that
// genuinely reads as words — a folder somebody or some other tool named
// `port-the-parser` — is still a better name than a generic one, and
// [idShaped] is the guard that tells the two apart.
func listName(title, transcript string) string {
	if name := strings.TrimSpace(title); name != "" {
		return titleCase(unpackName(name))
	}
	if stem := strings.TrimSuffix(sessionStem(transcript), ".jsonl"); stem != "" && !idShaped(stem) {
		return titleCase(unpackName(stem))
	}
	return unnamedConversationWord
}

// idShaped reports whether a name is a MACHINE'S name rather than a person's:
// a timestamp, a hex tail, a ULID, a folder minted by a counter.
//
// It is [readableName]'s own guard asked as a question instead of applied as a
// rewrite, and it is deliberately the same guard: two answers to "is this a
// name or an id" would drift, and the day they disagreed one surface would
// title-case what the other refused to draw.
func idShaped(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	for _, segment := range strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '.'
	}) {
		if !wordish(segment) {
			return true
		}
	}
	return false
}

// readableName turns a one-token machine name into words, and leaves everything
// else exactly as it found it.
//
// IT IS NOT THE RESUME PICKER'S [humanName], and the two are kept apart on
// purpose. That one is a LADDER — the title, then the first thing the person
// said, then the transcript's file name — climbed for a row on a page somebody
// went to in order to choose between sessions, and it title-cases what it finds
// because that page has the width and the shape for it. This one is a single
// display rule for a name the surface ALREADY HAS, and its whole content is the
// guard below: a name that is not plainly words is left exactly as it is, so a
// session file called "20260816-150405_a1b2c3" is never offered to anybody as a
// title. The picker reaches a file name only after two better answers failed;
// this never reaches one at all.
func readableName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" || strings.ContainsAny(name, " \t.") {
		// A name with a space in it is already words, and one with a dot in it is
		// a file — neither is this function's business.
		return name
	}
	if !strings.ContainsAny(name, "_-") {
		// A single plain word is a name somebody chose. Capitalizing it here
		// would be this surface deciding it knew better.
		return name
	}
	segments := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	if len(segments) < 2 {
		return name
	}
	words := make([]string, 0, len(segments))
	for _, segment := range segments {
		if !wordish(segment) {
			return name
		}
		words = append(words, upFirst(segment))
	}
	return strings.Join(words, " ")
}

// wordish reports whether one segment reads as a word: it starts with a letter
// and carries nothing but letters and digits after it. "parser" and "b2" pass;
// "150405" and "a1b2c3" — a timestamp and a random tail — do not, because a
// segment that starts with a letter and then runs on into digits is what an id's
// hex tail looks like.
func wordish(segment string) bool {
	if segment == "" || !isLetter(rune(segment[0])) {
		return false
	}
	digits := 0
	for _, r := range segment {
		switch {
		case isLetter(r):
		case r >= '0' && r <= '9':
			digits++
		default:
			return false
		}
	}
	// A word may end in a number ("v2", "b2"); a word is not mostly numbers.
	return digits*2 <= len([]rune(segment))
}

func isLetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

// upFirst raises a segment's first letter and touches nothing else: "parser"
// becomes "Parser" and "nilMap" stays "NilMap" rather than being re-spelled by a
// function that was asked for one capital.
func upFirst(segment string) string {
	if segment == "" {
		return segment
	}
	head := segment[:1]
	return strings.ToUpper(head) + segment[1:]
}
