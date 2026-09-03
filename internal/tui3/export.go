package tui3

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// /export: THE CONVERSATION, AS A DOCUMENT SOMEBODY ELSE CAN READ.
//
// It is the third of the doors onto getting text out (commands.go), and it is
// the one that is not about this screen. /copy and /select hand over what a
// person can SEE — the tail of a conversation, in the shapes a terminal drew it
// in — and the answer to "send me what we worked out this morning" is usually
// not on the screen any more.
//
// SO IT IS WRITTEN FROM THE WHOLE CONVERSATION, [Agent.Transcript], and never
// from [app.entries]. That slice is the forty-entry replay tail (replay.go): a
// document built from it would be a document that quietly starts in the middle,
// which is worse than no document at all, because nothing about the file says
// so.
//
// WHAT IS WRITTEN IS THE CONVERSATION AND NOT THE PLUMBING. The person's turns
// and the model's words are the document — the model's verbatim, because what it
// writes IS markdown and re-rendering it here would be a second opinion about
// somebody else's text. A tool call is one line saying what was done, and its
// answer is kept only while it stays small: a reader opening this file came for
// what was said, and an export carrying a thousand lines of `git status` is a
// file nobody reaches the end of.
//
// NOTHING HERE WAITS ON A DISK. The stat, the directory and the write all happen
// inside the returned command, the way the draft's write does (draft.go), and
// what comes back is one line in the conversation: where it went, or why it did
// not go anywhere.

// The document's three budgets, all of them about the same thing — a tool call
// is a FOOTNOTE in a document about a conversation, and a footnote that runs
// over the page has stopped being one.
const (
	// exportHintCap is how much of a call may stand on its line: the gloss the
	// surface itself drew for it, or the arguments when the journal kept no
	// gloss.
	exportHintCap = 120
	// exportAnswerCap and exportAnswerRows are what a result has to fit inside
	// to be quoted under its call at all. Twelve lines is a paragraph; the rune
	// budget is what stops twelve very long ones.
	exportAnswerCap  = 600
	exportAnswerRows = 12
)

// exportSlugLimit is how much of the session's name reaches the file name. A
// name is eight lowercase words (internal/session's title.go), and a file name
// is a thing somebody types back.
const exportSlugLimit = 48

// exportNothing is the answer to /export on a conversation that has not started.
// It is SAID rather than dropped: the command was typed on purpose, and silence
// after a deliberate command reads as a command that broke.
const exportNothing = "nothing to export yet."

// exportPerson is what the document calls the person. The model is called what
// this surface calls itself (styles.go's [product]), because that is the party
// that answered — and not the model's slug, which is a fact about one turn in a
// session that may have switched models halfway through.
const exportPerson = "you"

// exportedMsg is the write coming back to the loop. The path travels with it so
// the note can name the file even when the error is what happened to it.
type exportedMsg struct {
	path string
	err  error
}

// exportTarget is where one /export is going, and what it may make on the way:
// the path the person's argument resolved to (or the workspace's own default),
// the name to use if that path turns out to be a DIRECTORY, and whether a
// missing parent may be created — which only a path somebody typed out earns.
// The surface does not go making directories under a workspace on its own.
type exportTarget struct {
	path  string
	name  string
	named bool
}

// exportTranscript is the command: the conversation is read here, and everything
// that touches a disk happens in what is returned.
func (a *app) exportTranscript(arg string) tea.Cmd {
	if a.agent == nil {
		a.note(exportNothing)
		return nil
	}
	entries := a.agent.Transcript()
	if len(entries) == 0 {
		a.note(exportNothing)
		return nil
	}
	target := exportTarget{name: exportFileName(a.file, a.title)}
	if arg = strings.TrimSpace(arg); arg == "" {
		// [app.pathRoot] and not the workspace, which are the same directory on
		// every local session and different ones over --host: this write happens
		// HERE, with this process's own hands, so it lands in a directory this
		// process can actually reach. host.go states the whole bargain, and
		// [exportDone] says out loud where the file went.
		target.path = filepath.Join(a.pathRoot(), target.name)
	} else {
		// The same resolution the /image argument gets, and for the same reason:
		// a path a person types is meant the way they type it — "~" is home, a
		// bare name is under the directory this conversation is about, and an
		// absolute path is left alone (attach.go).
		target.path, target.named = a.resolvePath(arg), true
	}
	name := a.sessionName()
	// Read off the app here, on the loop, and closed over: the command runs on
	// its own goroutine and must not be reading fields the next keystroke is
	// writing.
	index, id := a.artifactsIndex(), exportSession(a.file)
	return func() tea.Msg {
		path, err := writeExport(target, exportDocument(name, entries))
		if err == nil {
			// AN EXPORT IS A DELIVERABLE, so it earns its row in the index a
			// person finds their work again by (internal/session's
			// artifacts.go). It is recorded HERE — beside the write, inside the
			// command — because this file's law is that nothing on the loop
			// waits on a disk, and because only a write that came back clean is
			// a file worth citing: the refusal below is somebody else's file.
			// The failure is silent by the same contract the row is written
			// under.
			session.RecordArtifact(index, session.Artifact{
				Path:    path,
				Session: id,
				Title:   filepath.Base(path),
				Kind:    "export",
				Created: time.Now(),
			})
		}
		return exportedMsg{path: path, err: err}
	}
}

// exportDone is the write's one line in the conversation.
//
// THE REFUSAL NAMES THE FILE AND THE WAY PAST IT. A conversation is written
// once and worked on for an hour, so the name this command derives is the same
// name every time it is typed — and a second /export that silently replaced the
// first would take a file somebody had already sent somewhere.
// AND OVER A CONNECTION IT NAMES THE MACHINE. The conversation is on the far
// machine and the file is on this one, which is the one moment in this surface
// where those two come apart — so the note says so in words rather than leaving
// a person to search a remote workspace for a file that is on their laptop
// (host.go's [exportHereWord] and the STUB beside it).
func (a *app) exportDone(msg exportedMsg) {
	short := shortPath(msg.path, a.tilde, 0)
	here := ""
	if a.hosted() {
		here = exportHereWord
	}
	switch {
	case msg.err == nil:
		a.note("exported · " + short + here)
		a.noticeEvent(eventDeliverableMade)
	case errors.Is(msg.err, fs.ErrExist):
		a.note(short + " is already there · /export <path> writes it somewhere else")
	default:
		a.note("export failed: " + msg.err.Error())
	}
}

// writeExport puts the document on disk and answers with the path it used.
//
// O_EXCL IS THE REFUSAL ITSELF. Asking whether the file exists and then writing
// it would be two answers to one question with a gap in the middle; this way the
// filesystem decides, once, and [fs.ErrExist] is what comes back.
//
// The mode is the draft's: 0o600 for the file and 0o700 for a directory, because
// a conversation is the most private thing on this machine — it is what somebody
// said while they were working.
func writeExport(target exportTarget, doc string) (string, error) {
	path := target.path
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		// A DIRECTORY IS A PLACE AND NOT A NAME. "/export ~/notes" means "put it
		// in there", and the name it gets in there is the one this session
		// derives for itself.
		path = filepath.Join(path, target.name)
	}
	if dir := filepath.Dir(path); target.named && dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return path, err
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return path, err
	}
	if _, err := file.WriteString(doc); err != nil {
		file.Close()
		return path, err
	}
	return path, file.Close()
}

// artifactsIndex is where the row goes: what the door said, or the product's
// own file under the state root. The fallback is [Options.Models]'s — a surface
// nobody wired still records where everything else in the product looks, and
// AFORGE_HOME moves it with the rest.
func (a *app) artifactsIndex() string {
	if index := strings.TrimSpace(a.artifacts); index != "" {
		return index
	}
	return home.Join("v3", session.ArtifactsIndexName)
}

// exportSession is the conversation the row cites, read off the transcript's
// own path: a session folder is named for its session (Decision 26), so a
// transcript.jsonl's parent directory IS the id and nothing has to be opened to
// learn it.
//
// A FLAT-LAYOUT TRANSCRIPT ANSWERS NOTHING. Its name carries a stem rather than
// an id, and a citation assembled out of a file name is a citation that points
// at a session nobody can look up — better absent than invented (design-law
// §EMPTINESS).
func exportSession(file string) string {
	if file = strings.TrimSpace(file); file == "" {
		return ""
	}
	if filepath.Base(file) != session.TranscriptName {
		return ""
	}
	return filepath.Base(filepath.Dir(file))
}

// ── the name on the file ────────────────────────────────────────────────────

// exportFileName is what the document is called when nobody said: the session's
// own identity, spelled as a file name.
//
//	20260817-150405_a3f2-port-the-resume-picker.md
//
// BOTH HALVES ARE THERE ON PURPOSE. The stem is the transcript's name, which is
// what makes two exports of two conversations two files even when a model gave
// them near-identical titles; the words are what makes the directory listing
// readable, which is the half a person actually looks at.
//
// A title with no ASCII letters in it leaves the stem standing alone rather than
// producing a file called "-.md": a file name is a thing somebody types back.
func exportFileName(file, title string) string {
	stem := ""
	if file = strings.TrimSpace(file); file != "" {
		stem = strings.TrimSuffix(filepath.Base(file), ".jsonl")
	}
	// The name is read as WORDS first (names.go): a session named in one welded
	// token is "port_b_parser_fix", and the kebab below would keep that
	// underscore as a dash-free run.
	words := exportKebab(readableName(title))
	switch {
	case stem != "" && words != "":
		return stem + "-" + words + ".md"
	case stem != "":
		return stem + ".md"
	case words != "":
		return words + ".md"
	}
	// A session with no file and no name yet: memory-only, and the person is
	// asking for it anyway. It is called what it is.
	return "conversation.md"
}

// exportKebab is a name as a file name: lowercase, one dash between words,
// nothing a shell or a filesystem has to be escaped from.
func exportKebab(name string) string {
	var out strings.Builder
	dash := false
	for _, char := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			out.WriteRune(char)
			dash = false
		default:
			if !dash && out.Len() > 0 {
				out.WriteByte('-')
				dash = true
			}
		}
		if out.Len() >= exportSlugLimit {
			break
		}
	}
	return strings.Trim(out.String(), "-")
}

// ── the document ────────────────────────────────────────────────────────────

// exportDocument is the whole conversation as markdown: a heading with the
// session's name on it, and then who said what.
//
// THE SPEAKER IS ANNOUNCED WHEN IT CHANGES, and not once per entry. A turn where
// the model wrote a paragraph, called four tools and wrote another paragraph is
// ONE thing the model did, and six headings over it would be the document
// mistaking the wire's messages for the conversation's shape.
//
// A "note" is skipped. It is the surface's own marker for something the SESSION
// did to itself — the compaction rule is the one that exists — and a document
// about what two parties said is not the place to file the fact that a context
// window was tidied. An "aside" is kept and set apart in italics, because that
// one IS an event in the conversation: work landed, a job exited, and the model
// answered it (session's agent.go documents both roles).
func exportDocument(name string, entries []session.DisplayEntry) string {
	blocks := make([]string, 0, len(entries)+1)
	if name = strings.TrimSpace(ansi.Strip(name)); name != "" {
		blocks = append(blocks, "# "+name)
	}
	speaker := ""
	say := func(who string) {
		if who == speaker {
			return
		}
		speaker = who
		blocks = append(blocks, "## "+who)
	}
	for _, e := range entries {
		// EVERY FIELD IS STRIPPED ON THE WAY IN. A file is not a terminal, and
		// several of these carry colour — a user line's picture markers are
		// painted where they are built (attach.go), and a shell's output arrives
		// with whatever the command decided to paint it with.
		text := strings.TrimSpace(ansi.Strip(e.Text))
		switch e.Role {
		case "user":
			// The pictures are part of what was said, so a message that was only
			// a picture is still a message (replay.go says the same).
			line := exportUserLine(text, e.ImageRefs)
			if line == "" {
				continue
			}
			say(exportPerson)
			blocks = append(blocks, line)

		case "assistant":
			if text == "" {
				continue // a step that only called tools; its calls follow
			}
			say(product)
			blocks = append(blocks, text)

		case "tool":
			// A tool entry with no name is a RESULT message from the wire, not a
			// call, and the document shows calls.
			tool := strings.TrimSpace(ansi.Strip(e.Tool))
			if tool == "" {
				continue
			}
			say(product)
			blocks = append(blocks, exportCall(tool, e.Hint, e.Args, e.Output))

		case "aside":
			if text == "" {
				continue
			}
			// Nobody is speaking on this line, so the next party to speak says who
			// it is again.
			speaker = ""
			blocks = append(blocks, "_"+firstLine(text)+"_")
		}
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

// exportUserLine is a message as the person sent it: their words, and the names
// of the pictures that went with them.
//
// It is [replayUserLine]'s rule with the colour taken out — the same markers in
// the same order, because a message drawn one way on screen and another way in
// the file would be two records of one thing.
func exportUserLine(text string, refs []string) string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		base := filepath.Base(ref)
		if ref == "" || base == "." || base == string(filepath.Separator) {
			continue
		}
		names = append(names, "["+base+"]")
	}
	markers := strings.Join(names, " ")
	switch {
	case markers == "":
		return text
	case text == "":
		return markers
	default:
		return text + " " + markers
	}
}

// exportCall is one tool call as a footnote: a list item saying what was done,
// and — only while it stays small — the answer under it.
//
// THE ARGUMENTS GET NO BLOCK OF THEIR OWN, because the hint already is them: it
// is the gloss the surface drew for this call, which is the arguments written
// for a person (session's DisplayEntry). They stand in for it only when the
// journal kept no gloss.
func exportCall(tool, hint, args, output string) string {
	line := "- `" + tool + "`"
	if gloss := exportGloss(hint, args); gloss != "" {
		line += " " + gloss
	}
	answer := strings.TrimRight(ansi.Strip(output), " \t\n")
	if answer == "" || !exportShort(answer) {
		return line
	}
	rail := exportFence(answer)
	return line + "\n\n" + exportIndent(rail+"\n"+answer+"\n"+rail)
}

// exportGloss is the words after the tool's name: what the surface called this
// call, or the arguments when it called it nothing.
func exportGloss(hint, args string) string {
	if gloss := strings.TrimSpace(firstLine(ansi.Strip(hint))); gloss != "" {
		return clipBytes(gloss, exportHintCap)
	}
	return clipBytes(strings.TrimSpace(firstLine(ansi.Strip(args))), exportHintCap)
}

// clipBytes cuts at a byte budget without splitting a rune.
//
// It lives here because this is its one caller now. It was room.go's, part of a
// second reader of the session file that this surface no longer has (#252); the
// cutting itself is still owed to a gloss that has to fit a line.
func clipBytes(text string, n int) string {
	if len(text) <= n {
		return text
	}
	cut := n - len("…")
	for cut > 0 && !runeStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

func runeStart(b byte) bool { return b&0xC0 != 0x80 }

// exportShort is the budget a result has to fit inside to be quoted at all.
func exportShort(answer string) bool {
	return len([]rune(answer)) <= exportAnswerCap &&
		strings.Count(answer, "\n") < exportAnswerRows
}

// exportFence is a fence LONGER than the longest run of backticks inside what it
// is fencing. Tool output is arbitrary text and some of it is markdown; a fixed
// three would let a quoted answer close the block it is inside and spill the
// rest of the conversation out into it.
func exportFence(text string) string {
	longest, run := 0, 0
	for _, char := range text {
		if char != '`' {
			run = 0
			continue
		}
		run++
		if run > longest {
			longest = run
		}
	}
	if longest < 3 {
		return "```"
	}
	return strings.Repeat("`", longest+1)
}

// exportIndent puts a block under a list item, which in markdown means two
// spaces — the width of the "- " the item starts with. A blank line stays blank:
// trailing spaces in a fenced block are two invisible characters that some
// renderers turn into a line break.
func exportIndent(block string) string {
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = "  " + line
	}
	return strings.Join(lines, "\n")
}
