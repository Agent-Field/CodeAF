package session

// The session's name.
//
// A conversation needs one word-sized handle — for the picker that lists
// sessions, for a window title, for a person deciding which of yesterday's
// three sessions to resume. Nobody wants to type it, and the first exchange
// already says what the session is about, so the session names itself once and
// then never again.
//
// Three properties are the whole design:
//
//   - ONE CALL, ONCE. It fires after the FIRST completed turn of a session that
//     has no name yet, and a session resumed from a journal already has its name
//     — the title line is read back at open. A namer that ran every turn would
//     be a tax on every turn, and a name that changed under the person would
//     make the picker unreadable.
//
//   - THE CHEAP MODEL. The model is resolved through internal/roles as
//     RoleTitle, which puts it on the low tier by default: a wrong title costs a
//     glance and is rewritten by the next session, so it is the archetypal cheap
//     call. A person who has configured nothing gets the session's own model,
//     which is roles.Resolve's floor and not a failure.
//
//   - ONLY A JOURNALED SESSION NAMES ITSELF. A session with no file has
//     nowhere to keep a name and nothing to be listed in — the name exists for
//     the picker, which lists files — so an in-memory conversation (a headless
//     one-shot, a test) pays for nothing. That is also why the gate reads "the
//     session file has no title yet": a resume already has its name.
//
//   - A NAME THAT IS NOT A NAME IS REFUSED, and the session stays unnamed. The
//     models this lands on are the cheapest ones configured, and a small model
//     answering with the instruction it was given is an ordinary failure — one
//     that named a real session on this machine "name this session in ≤8 words,
//     lowercase, no quotes". [cleanTitle] throws that answer away, [healedTitle]
//     throws away the ones already written down, and the person's opening words
//     stand in meanwhile. There is still no retry WITHIN a session — ONE CALL,
//     ONCE is the law above and the attempt is marked before the call — but a
//     session left unnamed asks again the next time it is opened, on its next
//     completed turn: titleTried is a fact about this process, and the title
//     read back off the file is now empty.
//
//   - IT NEVER BREAKS THE TURN. The turn is already done and its usage already
//     sealed when this runs; a failed, empty or cancelled title leaves the
//     session unnamed and says nothing. There is no event kind for "a small
//     thing did not work", and inventing one — or borrowing EventError, which
//     means the turn ended badly — would report a fault about work the person
//     never asked for.

import (
	"context"
	"strings"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// titleSystem is all the system message says, and it is deliberately not the
// instruction. A cheap model reads the system message as CHARACTER and the end
// of the user message as THE THING TO DO — so the instruction goes last, where
// it is read, and the system line only says who is being asked.
const titleSystem = "You name conversations."

// titlePrompt is the whole instruction, and it is the LAST thing in the user
// message, after the exchange it is about. Short, because the shape of the
// answer IS the requirement: eight words is a picker column, lowercase and
// unquoted is what every other label in this surface looks like. The final
// sentence is there because a small model that is not told the answer is the
// whole reply will introduce it ("Sure, here is the title: ...").
const titlePrompt = "Name this session in ≤8 words, lowercase, no quotes. Answer with the name only."

// titleClip bounds each half of the opening exchange handed to the namer. A
// title is derived from what the session is ABOUT, and the first paragraph of
// the question and of the answer says that; sending a 300KB tool-assisted reply
// would pay for a whole context to produce eight words.
const titleClip = 2000

// titleLimit bounds the name itself. Eight words asked for, 80 bytes accepted:
// the cap is a guard against a model that answers with a paragraph, not a
// second attempt at the instruction.
const titleLimit = 80

// maybeTitle names the session if it has no name yet. It runs at the end of a
// completed turn, on that turn's context and hub.
func (a *Agent) maybeTitle(ctx context.Context, hub *eventHub) {
	a.mu.Lock()
	if a.file == nil || a.titleTried || strings.TrimSpace(a.title) != "" || a.closed {
		a.mu.Unlock()
		return
	}
	// The attempt is marked before the call, not after it: this is ONE call per
	// session, and a namer that retried on every later turn would turn a
	// provider having a bad minute into a charge on every turn after it.
	a.titleTried = true
	question, answer := a.firstExchangeLocked()
	model := a.model
	a.mu.Unlock()

	if question == "" {
		return
	}
	// One errand, through the one door errands go through (auxiliary.go): the
	// role's tier bounds how long eight words may take, and a model that cannot
	// answer at all costs one fall-through down the ladder rather than the
	// session's name. No tools — the namer's only job is to produce eight words.
	response, named, err := a.callRole(ctx, roles.RoleTitle, model,
		[]ai.Message{
			textMessage("system", titleSystem),
			// THE INSTRUCTION IS LAST, after the exchange rather than above it.
			// The models this call lands on are the cheapest ones a person has
			// configured, and a small model asked in the system message answers
			// the system message: a real session on this machine was named
			// "name this session in ≤8 words, lowercase, no quotes" by one of
			// them. [cleanTitle] refuses that answer whatever it costs to make,
			// but the cheaper fix is to ask in the place it reads.
			textMessage("user", "First message:\n"+clip(question, titleClip)+
				"\n\nFirst reply:\n"+clip(answer, titleClip)+
				"\n\n"+titlePrompt),
		})
	if err != nil || response == nil {
		return
	}
	// BILLED AGAINST THE MODEL THAT ANSWERED, which is not always the one the
	// ladder resolved first.
	a.addAuxiliaryUsageAs(response, named, 1, auxRoleTitle)

	title := cleanTitle(response.Text())
	if title == "" {
		return
	}
	a.setTitle(title)
	if hub != nil {
		hub.send(Event{Kind: EventTitleChanged, Text: title})
	}
}

// setTitle records the name in the agent and in the journal.
func (a *Agent) setTitle(title string) {
	a.mu.Lock()
	a.title = title
	file := a.file
	a.mu.Unlock()
	if file != nil {
		file.appendTitle(title)
	}
	// The folder's row says what the journal says. Until now it has carried the
	// person's opening words as a placeholder (placemeta.go); this is the name
	// the conversation actually earned.
	a.stampTitle(title)
}

// firstExchangeLocked returns the session's opening question and the first
// thing the assistant said back, both as plain text.
//
// It reads from the front of the transcript rather than from the turn that just
// ended, which matters for the session whose first turn is not its first
// message — a resume that was never titled, a session whose opening turn was
// interrupted. The name should describe what the conversation is about, and
// that is where it was stated.
func (a *Agent) firstExchangeLocked() (string, string) {
	question, answer := "", ""
	for _, message := range a.messages {
		switch message.Role {
		case "user":
			if question == "" {
				question = messageContentText(message)
			}
		case "assistant":
			if question != "" && answer == "" {
				answer = messageContentText(message)
			}
		}
		if question != "" && answer != "" {
			break
		}
	}
	return strings.TrimSpace(question), strings.TrimSpace(answer)
}

func messageContentText(message ai.Message) string {
	var text strings.Builder
	for _, part := range message.Content {
		if part.Type == "text" && part.Text != "" {
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(part.Text)
		}
	}
	return text.String()
}

// cleanTitle takes the first line and strips the things a model adds against
// the instruction: the throat-clearing it opens with ("Title:", "Sure, here is
// the name:"), surrounding quotes, a trailing full stop, and the separators of
// a name answered as a SLUG. Then it REFUSES an answer that is not a name at
// all — the instruction handed back, or an opener with nothing behind it.
//
// The slug is the one worth explaining. The instruction asks for words, and a
// model that has spent its life reading identifiers sometimes answers
// "porting_the_parser" — which is the right eight words welded into a filename.
// A name is read by a person, in a status line and in a list of yesterday's
// sessions, so the welding is undone at the moment the name is minted rather
// than at each of the places it is drawn.
//
// Only a ONE-TOKEN answer is touched. A title that already has a space in it is
// words, and a hyphen inside words is a hyphen somebody meant ("port-b failures"
// keeps it).
//
// A REFUSAL IS THE EMPTY STRING, and every caller already knows what to do with
// it: the session stays unnamed ([Agent.maybeTitle] returns), the task namer
// leaves the row's title where it was ([cleanTaskName]), the shaper falls back
// to the person's own first words (task_shape.go). None of them is a blank row
// — a session with no name of its own is drawn under the person's opening
// words, which meta.json has carried since their first message (placemeta.go's
// [Agent.stampUserLocked]).
func cleanTitle(raw string) string {
	title := strings.TrimSpace(firstLine(raw))
	title = strings.Trim(title, `"'“”`)
	title = stripOpener(title)
	title = strings.Trim(title, `"'“”`)
	title = strings.TrimRight(title, ".")
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if !strings.ContainsAny(title, " \t") {
		title = strings.NewReplacer("_", " ", "-", " ").Replace(title)
		title = strings.Join(strings.Fields(title), " ")
	}
	title = clip(strings.TrimSpace(title), titleLimit)
	if namesTheInstruction(title) {
		return ""
	}
	return title
}

// ── an answer that is not a name ────────────────────────────────────────────
//
// A CHEAP MODEL ECHOING ITS OWN INSTRUCTION IS AN ORDINARY FAILURE and has to
// be survivable. A real session on this machine was called "name this session
// in ≤8 words, lowercase, no quotes" — the namer's instruction, answered
// verbatim by whichever of nex-n2-mini, ling-2.6-flash and glm-latest that
// turn's auxiliary calls landed on, cleaned by a hand that only trimmed, and
// then drawn title-cased across home. Asking better (the instruction is last in
// the user message now) makes it rarer; refusing the answer is what makes it
// harmless.
//
// THERE IS NO SECOND ATTEMPT. ONE CALL, ONCE stands exactly as the file header
// states it: [Agent.maybeTitle] marks the attempt BEFORE the call, so a refused
// name costs this session its name and nothing more. A retry would turn one bad
// minute at a provider into two calls for every session that has one, to earn a
// label that the person's own opening words already stand in for.

// instructionPhrases are the openings of the two namers' instructions, and one
// of them appearing anywhere in an answer means the model handed the
// instruction back rather than doing what it said. They are matched against the
// normalized answer, so case, quotes and punctuation do not hide an echo.
var instructionPhrases = []string{"name this session", "name this piece of work"}

// instructionEchoDivisor sets how much of a prompt an answer may borrow: an
// answer holding one in every two of a prompt's distinct words — HALF of them —
// is read as that prompt rather than as a name. Half is far past coincidence
// for eight words or fewer, and it catches the echo that was reworded on the
// way back ("session name in 8 words, lowercase, no quotes"), which no phrase
// test can.
const instructionEchoDivisor = 2

// namesTheInstruction reports whether a cleaned name is one of the namers' own
// instructions rather than an answer to it.
//
// BOTH INSTRUCTIONS ARE TESTED WHICHEVER NAMER ASKED, because one hand cleans
// both ([cleanTaskName], task_shape.go's parseShapedBrief) and a name that is
// the OTHER prompt is no more a name than one that is this prompt.
func namesTheInstruction(name string) bool {
	said := normalizedWords(name)
	if len(said) == 0 {
		return false
	}
	joined := " " + strings.Join(said, " ") + " "
	for _, phrase := range instructionPhrases {
		if strings.Contains(joined, " "+strings.Join(normalizedWords(phrase), " ")+" ") {
			return true
		}
	}
	spoken := make(map[string]bool, len(said))
	for _, word := range said {
		spoken[word] = true
	}
	for _, prompt := range []string{titlePrompt, taskNamePrompt} {
		asked := 0
		shared := 0
		for _, word := range uniqueWords(normalizedWords(prompt)) {
			asked++
			if spoken[word] {
				shared++
			}
		}
		if asked > 0 && shared*instructionEchoDivisor >= asked {
			return true
		}
	}
	return false
}

// openerLimit and openerWords bound what may be read as throat-clearing. A
// label is short and stands at the very front; anything longer is a sentence
// the model meant, and cutting at a colon inside one would take half a name
// away.
const (
	openerLimit = 48
	openerWords = 6
)

// stripOpener removes the announcement a model puts in front of a name it was
// asked to give on its own — "Title:", "Session name:", "Sure, here is the
// title:", "Sure, ...", "The session is about:".
//
// IT IS A LABEL TEST AND NOT A COLON TEST. Cutting at every leading colon would
// rewrite "fix: nil map crash" into "nil map crash", so the words in front of
// the colon have to read as an announcement: an interjection at the front, or
// one of the words a label ends on at the back.
//
// An opener with NOTHING after it leaves the empty string, and [cleanTitle]
// refuses that. A model that answered "Sure, here is the title:" and put the
// name on the next line has not answered this call — firstLine has already
// taken the only line the answer is read from.
func stripOpener(title string) string {
	// A "Sure," or "Okay!" with no colon after it is the same throat-clearing
	// without the punctuation the label rule keys on.
	if cut := strings.IndexAny(title, ",!"); cut > 0 && cut <= openerLimit {
		if words := normalizedWords(title[:cut]); len(words) == 1 && isInterjection(words[0]) {
			title = strings.TrimSpace(title[cut+1:])
		}
	}
	// Twice, because "Sure: Title: porting the parser" is two announcements and
	// stopping after the first would keep half of one.
	for range 2 {
		colon := strings.Index(title, ":")
		if colon <= 0 || colon > openerLimit {
			break
		}
		words := normalizedWords(title[:colon])
		if len(words) == 0 || len(words) > openerWords || !isOpener(words) {
			break
		}
		title = strings.TrimSpace(title[colon+1:])
	}
	// AND AN ANSWER MADE OF NOTHING BUT ANNOUNCEMENT is an opener with nothing
	// behind it however it was punctuated — "Sure", "Here is the title", "the
	// name". A name has a word in it that is about the conversation.
	if words := normalizedWords(title); len(words) > 0 && len(words) <= openerWords && allOpenerWords(words) {
		return ""
	}
	return title
}

// openerVocabulary is the words an announcement is built out of. It is used
// only to test whether an answer is ENTIRELY announcement, so a name that
// happens to contain one of them ("the parser name") is untouched — it also
// contains a word about the conversation, which is what makes it a name.
var openerVocabulary = map[string]bool{
	"sure": true, "ok": true, "okay": true, "certainly": true, "absolutely": true,
	"here": true, "heres": true, "is": true, "are": true, "the": true, "a": true,
	"an": true, "this": true, "that": true, "it": true, "its": true, "i": true,
	"ll": true, "would": true, "call": true, "title": true, "titled": true,
	"name": true, "named": true, "session": true, "for": true, "of": true,
	"and": true, "your": true, "my": true, "answer": true, "about": true,
	"called": true, "as": true, "follows": true,
}

// allOpenerWords reports whether every word of an answer is announcement.
func allOpenerWords(words []string) bool {
	for _, word := range words {
		if !openerVocabulary[word] {
			return false
		}
	}
	return true
}

// isOpener reads the words in front of a colon and says whether they are an
// announcement. Either end decides it: an interjection at the front ("sure,
// here is the name"), or one of the words a label ends on at the back ("title",
// "session name", "the session is about", "the title is").
func isOpener(words []string) bool {
	if isInterjection(words[0]) {
		return true
	}
	switch words[len(words)-1] {
	case "title", "name", "is", "about", "called", "answer":
		return true
	}
	return false
}

// isInterjection is the handful of words a model agrees with the request in
// before it answers it.
func isInterjection(word string) bool {
	switch word {
	case "sure", "ok", "okay", "certainly", "absolutely", "here", "heres":
		return true
	}
	return false
}

// normalizedWords reduces text to the lowercase words in it, with every mark
// that is not a letter or a digit read as a space. It is what makes "Name This
// Session in ≤8 Words, Lowercase, No Quotes." and the instruction it came from
// the same sequence of words.
func normalizedWords(text string) []string {
	folded := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, text)
	return strings.Fields(folded)
}

// uniqueWords drops the repeats, so a prompt that says "words" twice does not
// count for two against the share an echo has to reach.
func uniqueWords(words []string) []string {
	seen := make(map[string]bool, len(words))
	kept := make([]string, 0, len(words))
	for _, word := range words {
		if seen[word] {
			continue
		}
		seen[word] = true
		kept = append(kept, word)
	}
	return kept
}

// healedTitle is what a STORED name is worth on the way back in.
//
// The refusal above is minted at the moment a name is made, and the names that
// were already accepted and written down are still on disk — a journal's title
// line, a folder's meta.json. So every reader of a stored name comes through
// here: the replay a resume is built from (sessionfile.go), the picker's
// forward scan ([Peek]) and the identity file ([LoadMeta]).
//
// NOTHING IS REWRITTEN. The journal is append-only and meta.json is a citation
// rebuilt from it; a stored echo is simply not read as a name, which leaves the
// session unnamed — drawn under the person's opening words — and leaves
// [Agent.titleTried] false, so the namer gets its one call again on the next
// completed turn and the next good name is appended as every name always is.
func healedTitle(stored string) string {
	stored = strings.TrimSpace(stored)
	if stored == "" || namesTheInstruction(stored) {
		return ""
	}
	return stored
}
