package session

// A NAMER THAT HANDS BACK ITS OWN INSTRUCTION HAS NOT NAMED ANYTHING.
//
// A real session on this machine was called "name this session in ≤8 words,
// lowercase, no quotes" — the instruction, echoed by one of the cheap models
// the auxiliary calls land on, accepted by a cleaner that only trimmed, and
// then drawn title-cased across home. These tests pin the three answers to it:
// the echo is refused at the moment a name is minted, a name already written
// down under one is read as no name, and an unnamed session is not a blank row
// — the person's own opening words stand in until a later turn names it.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The instruction, in the shapes a model hands it back in: verbatim, title-cased
// by a model that ignored "lowercase", ended with a full stop, cut short, and
// reworded. None of them is a name.
func TestANameThatIsTheInstructionIsRefused(t *testing.T) {
	for _, echoed := range []string{
		"name this session in ≤8 words, lowercase, no quotes",
		"Name This Session in ≤8 Words, Lowercase, No Quotes",
		"Name this session in ≤8 words, lowercase, no quotes.",
		`"name this session in ≤8 words, lowercase, no quotes"`,
		"Name this session in ≤8 words",
		"session name in 8 words, lowercase, no quotes",
		"Name this session in ≤8 words, lowercase, no quotes. Answer with the name only.",
		"Name this piece of work in two or three words",
	} {
		if got := cleanTitle(echoed); got != "" {
			t.Errorf("cleanTitle(%q) = %q, want the instruction refused", echoed, got)
		}
	}
}

// An announcement is stripped and the name behind it kept; an announcement with
// nothing behind it is no answer at all.
func TestTheOpenerIsStrippedAndAnEmptyOneRefused(t *testing.T) {
	for _, row := range []struct{ said, want string }{
		{"Title: tokenizer speed", "tokenizer speed"},
		{"Session name: tokenizer speed", "tokenizer speed"},
		{"Sure, here is the title: tokenizer speed", "tokenizer speed"},
		{"Here is a name: tokenizer speed", "tokenizer speed"},
		{"The session is about: tokenizer speed", "tokenizer speed"},
		{"Sure, tokenizer speed", "tokenizer speed"},
		// Nothing behind the announcement — the name was on a line firstLine
		// never read.
		{"Sure, here is the title:", ""},
		{"Title:", ""},
		{"sure", ""},
		// AND A COLON SOMEBODY MEANT SURVIVES. The words in front of it have to
		// read as an announcement, or the cut would take half of a real name.
		{"fix: nil map crash", "fix: nil map crash"},
		{"port-b failures", "port-b failures"},
		// The ordinary answer is untouched by any of it.
		{"tokenizer speed", "tokenizer speed"},
		{"why the parser drops the last token", "why the parser drops the last token"},
	} {
		if got := cleanTitle(row.said); got != row.want {
			t.Errorf("cleanTitle(%q) = %q, want %q", row.said, got, row.want)
		}
	}
}

// THE INSTRUCTION IS ASKED WHERE A SMALL MODEL READS IT: last in the user
// message, after the exchange, with the system line saying only who is asked.
func TestTheNamerAsksAtTheEndOfTheUserMessage(t *testing.T) {
	completer := &scriptedCompleter{steps: titleTurn("the parser is fine", "tokenizer speed")}
	agent, _ := titleAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))

	asked := completer.request(1)
	if len(asked) != 2 {
		t.Fatalf("the namer sent %d messages, want a system line and a user message", len(asked))
	}
	if got := messageContentText(asked[0]); got != titleSystem {
		t.Fatalf("the namer's system message = %q, want %q", got, titleSystem)
	}
	user := messageContentText(asked[1])
	if !strings.HasSuffix(user, titlePrompt) {
		t.Fatalf("the instruction is not the last thing the namer says:\n%s", user)
	}
	if !strings.Contains(user, "why is the tokenizer slow?") {
		t.Fatalf("the namer was not shown the exchange:\n%s", user)
	}
}

// The end of the story a person sees: the session keeps no name, its journal
// keeps no title line, and the folder's row still says what they opened with.
func TestASessionNamedWithTheInstructionKeepsThePlaceholder(t *testing.T) {
	dir := t.TempDir()
	completer := &scriptedCompleter{steps: titleTurn("the parser is fine", titlePrompt)}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
	})

	events := collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))

	if countKind(events, EventTitleChanged) != 0 {
		t.Fatal("the session announced the instruction as its name")
	}
	if got := agent.Title(); got != "" {
		t.Fatalf("Title() = %q, want the session to stay unnamed", got)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, line := range readLines(t, filepath.Join(dir, "session.jsonl")) {
		if strings.Contains(line, `"type":"title"`) {
			t.Fatalf("a refused name was written to the journal: %s", line)
		}
	}
	// UNNAMED IS NOT A BLANK ROW. placemeta.go stamped the person's opening
	// words on the folder at their first message, and nothing has replaced them.
	if got := stampedMeta(t, dir).Title; got != "why is the tokenizer slow?" {
		t.Fatalf("the folder's row says %q, want the person's opening words", got)
	}
}

// A REFUSED NAME COSTS ONE CALL AND NOT TWO. ONE CALL, ONCE is the law in
// title.go's header, and the attempt is marked before the call is made.
func TestARefusedNameIsNotRetriedWithinTheSession(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("first"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(titlePrompt), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("second"), nil },
	}}
	agent, _ := titleAgent(t, completer, nil)

	collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
	collect(t, mustSubmit(t, agent, "and the parser?"))

	if completer.requests() != 3 {
		t.Fatalf("requests = %d, want 3 (two turns and the one namer)", completer.requests())
	}
	if got := agent.Title(); got != "" {
		t.Fatalf("Title() = %q", got)
	}
}

// ── the ones already on disk ────────────────────────────────────────────────

// echoedJournal is a session that was named with the instruction and written
// down under it, exactly as the real one on this machine was.
func echoedJournal(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"c95971eb","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"why is the tokenizer slow?","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"the parser is fine","timestamp":"t"}`,
		`{"type":"title","title":"name this session in ≤8 words, lowercase, no quotes","timestamp":"t"}`,
	)
	return path
}

// Every reader of a stored name comes through the same hand, so a journal
// written under the instruction opens as a session with no name — and the
// picker's own forward scan says the same thing.
func TestAStoredInstructionIsReadAsNoNameAtAll(t *testing.T) {
	path := echoedJournal(t, t.TempDir())

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replayed.title != "" {
		t.Fatalf("the replay read the instruction back as a name: %q", replayed.title)
	}
	summary, ok := Peek(path)
	if !ok {
		t.Fatal("Peek refused a journal with a conversation in it")
	}
	if summary.Title != "" {
		t.Fatalf("the picker row is titled %q, want the instruction dropped", summary.Title)
	}
	// The row is not empty: a surface with no title draws the opening line.
	if summary.Opening != "why is the tokenizer slow?" {
		t.Fatalf("Opening = %q", summary.Opening)
	}
	// AND THE FILE IS NOT REWRITTEN. The journal is append-only; the bad line
	// stays where it is and is simply not read as a name.
	found := false
	for _, line := range readLines(t, path) {
		if strings.Contains(line, `"type":"title"`) {
			found = true
		}
	}
	if !found {
		t.Fatal("the heal rewrote the journal, which is append-only")
	}
}

// meta.json is a citation, so the same name is dropped on the way in there too
// — and this is where the person's opening words were overwritten, so they are
// read back out of the journal to stand in the way they did before it arrived.
func TestAStoredInstructionGivesTheFoldersRowItsPlaceholderBack(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "transcript.jsonl"),
		`{"type":"session","version":1,"id":"c95971eb","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"why is the tokenizer slow?","timestamp":"t"}`,
	)
	if err := SaveMeta(dir, Meta{
		ID:    "c95971eb",
		Title: "name this session in ≤8 words, lowercase, no quotes",
	}); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	if got := stampedMeta(t, dir).Title; got != "why is the tokenizer slow?" {
		t.Fatalf("the folder's row says %q, want the person's opening words back", got)
	}
	// AND NOTHING IS REWRITTEN: the heal is a reading rule, so the file still
	// says what it said until something stamps it.
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	if !strings.Contains(string(raw), "name this session") {
		t.Fatalf("LoadMeta rewrote the identity file: %s", raw)
	}
	// A name somebody's session actually earned is untouched by any of it.
	if err := SaveMeta(dir, Meta{ID: "c95971eb", Title: "tokenizer speed"}); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	if got := stampedMeta(t, dir).Title; got != "tokenizer speed" {
		t.Fatalf("LoadMeta returned %q, want the name kept", got)
	}
}

// AND THE HEALED SESSION ASKS AGAIN. The title read back is empty and the one
// attempt is a fact about this process, so the next completed turn names it —
// once — and the name is appended, as every name always is.
func TestAHealedSessionNamesItselfOnItsNextTurn(t *testing.T) {
	dir := t.TempDir()
	path := echoedJournal(t, dir)
	completer := &scriptedCompleter{steps: titleTurn("still slow", "tokenizer speed")}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })

	if got := agent.Title(); got != "" {
		t.Fatalf("the resumed session opened titled %q", got)
	}
	events := collect(t, mustSubmit(t, agent, "and now?"))

	changed, ok := firstOfKind(events, EventTitleChanged)
	if !ok {
		t.Fatalf("the healed session did not name itself; got %v", kinds(events))
	}
	if changed.Text != "tokenizer speed" {
		t.Fatalf("the healed session named itself %q", changed.Text)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	titles := []string{}
	for _, line := range readLines(t, path) {
		if strings.Contains(line, `"type":"title"`) {
			titles = append(titles, line)
		}
	}
	if len(titles) != 2 || !strings.Contains(titles[1], `"title":"tokenizer speed"`) {
		t.Fatalf("title lines = %v, want the good name appended under the old one", titles)
	}
}

// ── who named it ────────────────────────────────────────────────────────────

// The auxiliary line says WHAT the call was for, so the next bad name can be
// traced to the model that gave it.
func TestTheNamersOwnLineSaysItWasTheTitle(t *testing.T) {
	completer := &scriptedCompleter{steps: titleTurn("the parser is fine", "tokenizer speed")}
	agent, path := titleAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	roles := []string{}
	for _, line := range readLines(t, path) {
		var entry sessionEntry
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Usage == nil {
			continue
		}
		// A TURN'S OWN SEAL NAMES NO ROLE. The emptiness law reaches this field
		// like every other one on the line.
		if !entry.Usage.Aux && entry.Usage.Role != "" {
			t.Fatalf("a turn's seal carries a role: %s", line)
		}
		if entry.Usage.Role != "" {
			roles = append(roles, entry.Usage.Role)
		}
	}
	if len(roles) != 1 || roles[0] != auxRoleTitle {
		t.Fatalf("roles on the journal's usage lines = %v, want one %q", roles, auxRoleTitle)
	}
}

// ── the other namer ─────────────────────────────────────────────────────────

// One hand cleans both names, so the piece-of-work namer refuses the same
// answers — its own instruction, and the session namer's.
func TestTheTaskNamerRefusesTheInstructionToo(t *testing.T) {
	for _, echoed := range []string{
		taskNamePrompt,
		"Name this piece of work in two or three words — a label for a narrow column",
		"name this session in ≤8 words, lowercase, no quotes",
		"Sure, here is the name:",
	} {
		if got := cleanTaskName(echoed); got != "" {
			t.Errorf("cleanTaskName(%q) = %q, want it refused", echoed, got)
		}
	}
	// And the answers it exists to take are still taken.
	for _, row := range []struct{ said, want string }{
		{"nil-map crash fix", "nil-map crash fix"},
		{"Title: launch post", "launch post"},
		{"frieren pdf summary", "frieren pdf summary"},
	} {
		if got := cleanTaskName(row.said); got != row.want {
			t.Errorf("cleanTaskName(%q) = %q, want %q", row.said, got, row.want)
		}
	}
}
