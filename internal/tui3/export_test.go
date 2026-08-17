package tui3

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── 1. the command ──────────────────────────────────────────────────────────

// /export IS ON THE LIST AND IN /help, which is one table read twice
// (commands.go) — and it is there TWICE, because the form that takes a path and
// the form that takes none are two rows of one command.
func TestExportIsOnTheCommandList(t *testing.T) {
	bare, withPath := false, false
	for _, c := range commands {
		if c.name != "export" {
			continue
		}
		bare = bare || c.args == ""
		withPath = withPath || c.args != ""
	}
	if !bare {
		t.Fatal("/export with no argument is unreachable from the command list")
	}
	if !withPath {
		t.Fatal("/export <path> is not on the command list")
	}
	if !strings.Contains(helpText(""), "/export") {
		t.Fatal("/export is not in /help")
	}
	// The word a person arrives with reaches the same row, and the table check
	// proves it shadows nothing (commands.go).
	if got := canonicalCommand("save"); got != "export" {
		t.Fatalf("/save resolves to %q, want export", got)
	}
}

// ── 2. the document ─────────────────────────────────────────────────────────

// exportSample is a small conversation with one of everything in it.
func exportSample() []session.DisplayEntry {
	return []session.DisplayEntry{
		{Role: "user", Text: "what is wrong with this", ImageRefs: []string{"/pictures/chart.png"}},
		{Role: "assistant", Text: "## What I found\n\n- one\n- two"},
		{Role: "tool", Tool: "bash", Hint: "git status", Args: `{"command":"git status"}`, Output: "On branch main"},
		{Role: "assistant", Text: "and that is the whole of it"},
		{Role: "note", Text: "the conversation was compacted"},
		{Role: "aside", Text: "the task finished\nand said more than one line"},
		{Role: "assistant", Text: "back again"},
		{Role: "user", Text: "\x1b[2mthank you\x1b[0m"},
	}
}

// THE DOCUMENT IS THE CONVERSATION: who spoke, said once per run of speaking,
// and the model's markdown exactly as it wrote it.
func TestTheExportedDocumentIsTheConversation(t *testing.T) {
	doc := exportDocument("Port The Resume Picker", exportSample())

	if !strings.HasPrefix(doc, "# Port The Resume Picker\n") {
		t.Fatalf("the document does not open on the session's name:\n%s", doc)
	}
	for _, want := range []string{
		"## " + exportPerson + "\n\nwhat is wrong with this [chart.png]",
		"## " + product + "\n\n## What I found\n\n- one\n- two",
		"- `bash` git status",
		"_the task finished_",
		"thank you",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("the document is missing %q:\n%s", want, doc)
		}
	}
	// The speaker is announced when it CHANGES: the model spoke, called a tool
	// and spoke again as one run, and it says so again only after the aside.
	if got := strings.Count(doc, "## "+product); got != 2 {
		t.Fatalf("the model is announced %d times, want 2:\n%s", got, doc)
	}
	if got := strings.Count(doc, "## "+exportPerson); got != 2 {
		t.Fatalf("the person is announced %d times, want 2:\n%s", got, doc)
	}
	// A note is the SURFACE's marker about the session, not a thing anybody
	// said.
	if strings.Contains(doc, "compacted") {
		t.Fatalf("a note reached the document:\n%s", doc)
	}
	if !strings.HasSuffix(doc, "\n") || strings.HasSuffix(doc, "\n\n") {
		t.Fatalf("the document ends %q", doc[len(doc)-4:])
	}
}

// NOTHING PAINTED REACHES THE FILE. A user line carries its picture markers in
// SGR (attach.go) and a shell paints its own output; a file is not a terminal.
func TestTheExportedDocumentCarriesNoColour(t *testing.T) {
	entries := append(exportSample(), session.DisplayEntry{
		Role: "tool", Tool: "bash", Hint: "\x1b[1mls\x1b[0m", Output: "\x1b[32mok\x1b[0m",
	})
	doc := exportDocument("\x1b[2mname\x1b[0m", entries)
	if strings.ContainsRune(doc, 0x1b) {
		t.Fatalf("an escape sequence reached the file:\n%q", doc)
	}
}

// A SHORT ANSWER IS QUOTED UNDER ITS CALL AND A LONG ONE IS NOT: the reader came
// for the conversation, not for a thousand lines of `git status`.
func TestAToolCallKeepsOnlyASmallAnswer(t *testing.T) {
	short := exportCall("bash", "git status", "", "On branch main")
	if want := "- `bash` git status\n\n  ```\n  On branch main\n  ```"; short != want {
		t.Fatalf("the call reads\n%s\nwant\n%s", short, want)
	}
	long := exportCall("bash", "git log", "", strings.Repeat("a line\n", exportAnswerRows+1))
	if long != "- `bash` git log" {
		t.Fatalf("a long answer was quoted anyway:\n%s", long)
	}
	wide := exportCall("bash", "git log", "", strings.Repeat("x", exportAnswerCap+1))
	if wide != "- `bash` git log" {
		t.Fatalf("a wide answer was quoted anyway:\n%s", wide)
	}
	// With no gloss the arguments stand in for it, capped.
	none := exportCall("read", "", `{"path":"internal/tui3/app.go"}`, "")
	if !strings.Contains(none, `{"path":"internal/tui3/app.go"}`) {
		t.Fatalf("a call with no gloss says nothing about itself:\n%s", none)
	}
}

// THE FENCE IS LONGER THAN WHAT IT FENCES, or a quoted answer closes the block
// it is inside and spills the rest of the document out into it.
func TestAQuotedAnswerCannotBreakOutOfItsFence(t *testing.T) {
	call := exportCall("bash", "cat readme", "", "```\nsome code\n```")
	if !strings.Contains(call, "  ````\n") {
		t.Fatalf("the fence did not grow:\n%s", call)
	}
	if exportFence("no backticks here") != "```" {
		t.Fatal("a plain answer got a fence longer than three")
	}
}

// ── 3. the name on the file ─────────────────────────────────────────────────

// THE NAME IS THE SESSION'S IDENTITY: the transcript's stem, which makes two
// conversations two files, and the words, which make the directory readable.
func TestTheExportFileIsNamedAfterTheSession(t *testing.T) {
	for _, c := range []struct {
		file, title, want string
	}{
		{"/j/20260817-150405_a3f2.jsonl", "port the resume picker", "20260817-150405_a3f2-port-the-resume-picker.md"},
		{"/j/20260817-150405_a3f2.jsonl", "", "20260817-150405_a3f2.md"},
		// A welded one-token name is read back as words first (names.go).
		{"/j/s.jsonl", "port_b_parser_fix", "s-port-b-parser-fix.md"},
		// Nothing a file name can be made of leaves the stem standing alone.
		{"/j/s.jsonl", "…", "s.md"},
		// A conversation with no file at all is still a conversation.
		{"", "port the resume picker", "port-the-resume-picker.md"},
		{"", "", "conversation.md"},
	} {
		if got := exportFileName(c.file, c.title); got != c.want {
			t.Fatalf("exportFileName(%q, %q) = %q, want %q", c.file, c.title, got, c.want)
		}
	}
	// The words are capped: a file name is a thing somebody types back.
	long := exportFileName("", strings.Repeat("word ", 40))
	if len(long) > exportSlugLimit+len(".md")+1 {
		t.Fatalf("the name ran to %d characters: %q", len(long), long)
	}
}

// ── 4. where it goes ────────────────────────────────────────────────────────

// exportLab is a workspace with a named session in it, and nothing on screen.
func exportLab(t *testing.T) (*app, *fakeAgent, string) {
	t.Helper()
	dir := t.TempDir()
	agent := &fakeAgent{model: "vendor/model", past: exportSample()}
	a := newApp(context.Background(), Options{Agent: agent, Workspace: dir})
	// WIDER THAN THE OTHER LABS, because what these tests read back is a note
	// with a FILE PATH in it: at sixty cells the name wraps mid-word, and an
	// assertion that had to know where it wrapped would be a test of the
	// renderer's fold rather than of what the surface said.
	a.width, a.height = 160, 20
	a.pal = newPalette(tokens.ANSI256, false)
	a.entries = nil
	a.welcome = welcome{spent: true}
	a.file = filepath.Join(dir, ".sessions", "20260817-150405_a3f2.jsonl")
	a.title = "port the resume picker"
	a.touch()
	return a, agent, dir
}

const exportSampleName = "20260817-150405_a3f2-port-the-resume-picker.md"

func exportBody(a *app) string { return strings.Join(plainRows(a), "\n") }

// NO ARGUMENT PUTS IT IN THE WORKSPACE, under the name the session derives, and
// the line it leaves says where it went.
func TestExportWritesTheWholeConversationToTheWorkspace(t *testing.T) {
	a, _, dir := exportLab(t)
	typeLine(t, a, "/export")

	raw, err := os.ReadFile(filepath.Join(dir, exportSampleName))
	if err != nil {
		t.Fatalf("nothing was written: %v", err)
	}
	doc := string(raw)
	if !strings.Contains(doc, "what is wrong with this") || !strings.Contains(doc, "## What I found") {
		t.Fatalf("the file is not the conversation:\n%s", doc)
	}
	if body := exportBody(a); !strings.Contains(body, "exported ·") || !strings.Contains(body, exportSampleName) {
		t.Fatalf("the surface did not say where it went:\n%s", body)
	}
}

// IT IS THE WHOLE CONVERSATION AND NOT THE SCREEN: the replay tail is forty
// entries (replay.go), and a document built from it would start in the middle.
func TestExportReadsPastTheReplayTail(t *testing.T) {
	a, agent, dir := exportLab(t)
	agent.past = append([]session.DisplayEntry{{Role: "user", Text: "the first thing anybody said"}}, agent.past...)
	for i := 0; i < replayTail*2; i++ {
		agent.past = append(agent.past, session.DisplayEntry{Role: "assistant", Text: "filler"})
	}
	a.entries = nil // the surface is showing nothing at all
	typeLine(t, a, "/export")

	raw, err := os.ReadFile(filepath.Join(dir, exportSampleName))
	if err != nil {
		t.Fatalf("nothing was written: %v", err)
	}
	if !strings.Contains(string(raw), "the first thing anybody said") {
		t.Fatal("the export began after the conversation did")
	}
}

// A CONVERSATION IS WRITTEN ONCE AND SENT SOMEWHERE. A second /export must not
// quietly replace the file the first one made — it says the name, and says the
// way past it.
func TestExportRefusesToOverwrite(t *testing.T) {
	a, _, dir := exportLab(t)
	path := filepath.Join(dir, exportSampleName)
	if err := os.WriteFile(path, []byte("something somebody kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	typeLine(t, a, "/export")

	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "something somebody kept" {
		t.Fatalf("the file was replaced: %q, %v", raw, err)
	}
	body := exportBody(a)
	if !strings.Contains(body, "already there") || !strings.Contains(body, exportSampleName) {
		t.Fatalf("the refusal does not name the file:\n%s", body)
	}
}

// A PATH IS WHERE IT GOES, resolved the way a person means it: a bare name is
// under the directory this conversation is about (attach.go), and a parent that
// is not there yet is made.
func TestExportTakesAPathOfItsOwn(t *testing.T) {
	a, _, dir := exportLab(t)
	typeLine(t, a, "/export notes/today.md")

	raw, err := os.ReadFile(filepath.Join(dir, "notes", "today.md"))
	if err != nil {
		t.Fatalf("nothing was written where it was asked for: %v", err)
	}
	if !strings.Contains(string(raw), "what is wrong with this") {
		t.Fatal("the file is not the conversation")
	}
}

// A DIRECTORY IS A PLACE AND NOT A NAME: what lands in it is the name the
// session derives for itself.
func TestExportIntoADirectoryKeepsTheSessionsName(t *testing.T) {
	a, _, dir := exportLab(t)
	if err := os.MkdirAll(filepath.Join(dir, "out"), 0o700); err != nil {
		t.Fatal(err)
	}
	typeLine(t, a, "/export out")

	if _, err := os.Stat(filepath.Join(dir, "out", exportSampleName)); err != nil {
		t.Fatalf("the document is not in the directory it was pointed at: %v", err)
	}
}

// A CONVERSATION THAT HAS NOT STARTED IS ANSWERED, not ignored: the command was
// typed on purpose.
func TestExportSaysWhenThereIsNothingYet(t *testing.T) {
	a, agent, dir := exportLab(t)
	agent.past = nil
	typeLine(t, a, "/export")

	if body := exportBody(a); !strings.Contains(body, exportNothing) {
		t.Fatalf("an empty session was answered with:\n%s", body)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("an empty session wrote %v", entries)
	}
}

// ── 5. the path completion, now that two commands take one ──────────────────

// BOTH PREFIXES ARE FOUND AND NOTHING ELSE IS (files.go's [argPrefixes]).
func TestTheArgumentTokenAnswersEveryPathCommand(t *testing.T) {
	for _, c := range []struct {
		line  string
		at    int
		query string
		ok    bool
	}{
		{"/image shot.png", 7, "shot.png", true},
		{"/export notes/today.md", 8, "notes/today.md", true},
		{"/EXPORT notes.md", 8, "notes.md", true},
		{"/export ", 8, "", true},
		{"/compact now", 0, "", false},
		{"just a sentence", 0, "", false},
		{"/export a\nb", 0, "", false},
	} {
		value := []rune(c.line)
		at, query, ok := argToken(value, len(value))
		if ok != c.ok || (ok && (at != c.at || query != c.query)) {
			t.Fatalf("argToken(%q) = %d, %q, %v — want %d, %q, %v", c.line, at, query, ok, c.at, c.query, c.ok)
		}
	}
}

// TAB COMPLETES /export's PATH the way it completes /image's, and enter still
// belongs to the line under the list.
func TestTabCompletesTheExportCommandsPath(t *testing.T) {
	a, _, _ := attachLab(t, map[string]int{"notes/today.md": 8})
	typeText(t, a, "/export ")
	if a.comp.open {
		t.Fatal("the path list opened over an empty argument")
	}
	drive(t, a, tab())
	if !a.comp.open || !a.comp.arg {
		t.Fatal("tab did not open the path list")
	}
	typeText(t, a, "notes/to")
	drive(t, a, tab())
	if got, want := a.input.String(), "/export notes/today.md"; got != want {
		t.Fatalf("the draft is %q, want %q", got, want)
	}
	if a.comp.open {
		t.Fatal("the list stayed open on top of its own answer")
	}
}
