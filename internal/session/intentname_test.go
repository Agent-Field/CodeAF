package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── A DESKTOP CONVERSATION IS NAMED FROM WHAT THE PERSON OPENED WITH ────────
//
// Every test here drives a real [Agent] with a journal through the door the
// desktop app uses ([Agent.SubmitMessage]) and answers the namer from a local
// fixture, by the shape of its request. Nothing reaches a provider. What is
// asserted is the REQUEST the namer was handed, the NAME the conversation
// carries and the JOURNAL it leaves — the three things a surface can see.

// intentAnswer is one scripted answer to the namer.
type intentAnswer struct {
	title string
	err   error
}

// intentAsked is one request the namer received.
type intentAsked struct {
	model     string
	messages  []ai.Message
	maxTokens int
}

// intentCompleter answers the namer by the shape of its request, records every
// request it receives, and leaves the step queue to the turn.
type intentCompleter struct {
	*scriptedCompleter
	mu      sync.Mutex
	answer  func(n int) intentAnswer
	asked   []intentAsked
	gate    chan struct{}
	arrived chan struct{}
	once    sync.Once
}

func intentFixture(turns int, answer func(n int) intentAnswer) *intentCompleter {
	steps := make([]step, 0, turns)
	for i := 0; i < turns; i++ {
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the assistant's own answer"), nil
		})
	}
	return &intentCompleter{scriptedCompleter: &scriptedCompleter{steps: steps}, answer: answer, arrived: make(chan struct{})}
}

// intentSays is a fixture whose namer always answers title.
func intentSays(turns int, title string) *intentCompleter {
	return intentFixture(turns, func(int) intentAnswer { return intentAnswer{title: title} })
}

func (c *intentCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	if !isTitleCall(messages) {
		return c.scriptedCompleter.CompleteWithMessages(ctx, messages, options...)
	}
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	maxTokens := 0
	if request.MaxTokens != nil {
		maxTokens = *request.MaxTokens
	}
	c.mu.Lock()
	n := len(c.asked)
	c.asked = append(c.asked, intentAsked{model: request.Model, messages: append([]ai.Message(nil), messages...), maxTokens: maxTokens})
	gate := c.gate
	c.mu.Unlock()
	c.once.Do(func() { close(c.arrived) })
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	reply := c.answer(n)
	if reply.err != nil {
		return nil, reply.err
	}
	return textResponse(reply.title), nil
}

func (c *intentCompleter) asks() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.asked)
}

func (c *intentCompleter) ask(at int) intentAsked {
	c.mu.Lock()
	defer c.mu.Unlock()
	if at >= len(c.asked) {
		return intentAsked{}
	}
	return c.asked[at]
}

// request is everything the namer was shown, as one string.
func (a intentAsked) request() string {
	var all strings.Builder
	for _, message := range a.messages {
		all.WriteString(messageContentText(message))
		all.WriteString("\n")
	}
	return all.String()
}

// userWords is the last message the namer read: the exchange and, at its end,
// the instruction.
func (a intentAsked) userWords() string {
	if len(a.messages) == 0 {
		return ""
	}
	return messageContentText(a.messages[len(a.messages)-1])
}

func intentAgent(t *testing.T, completer Completer, mutate func(*Config)) (*Agent, string) {
	t.Helper()
	return titleAgent(t, completer, func(config *Config) {
		config.IntentNames = true
		config.RolesSource = func(key string) (string, bool) { return "test/model", key == roles.PinKey(roles.RoleTitle) }
		if mutate != nil {
			mutate(config)
		}
	})
}

func submitMessage(t *testing.T, agent *Agent, message Submission) []Event {
	t.Helper()
	events, err := agent.SubmitMessage(watchedContext(agent), message)
	if err != nil {
		t.Fatalf("SubmitMessage: %v", err)
	}
	return collect(t, events)
}

func said(text string) *string { return &text }

// fileAttachment is a file as the desktop door describes one: its path and the
// label it gives a person, which is the path's last element.
func fileAttachment(id, path string) MessageAttachment {
	return MessageAttachment{ID: id, Kind: "file", Name: lastPathElement(path), Path: path}
}

// titleLines is the title lines of a journal, in order, with whether each was a
// literal name.
func titleLines(t *testing.T, path string) []string {
	t.Helper()
	var lines []string
	for _, line := range readLines(t, path) {
		if strings.Contains(line, `"type":"title"`) {
			lines = append(lines, line)
		}
	}
	return lines
}

// drainTitles is the name events a standing lane delivers: it waits for the
// first, then takes what follows until the lane has been quiet for a moment.
// Delivery to a lane is asynchronous, so "nothing waiting yet" is not "nothing".
func drainTitles(lane <-chan Event) []string {
	var names []string
	wait := 2 * time.Second
	for {
		select {
		case event := <-lane:
			names = append(names, event.Text)
			wait = 150 * time.Millisecond
		case <-time.After(wait):
			return names
		}
	}
}

// settleNaming joins the naming errand, once it can finish.
func settleNaming(agent *Agent) { agent.waitForTitle() }

// THE ORIGINAL AUTHORED INTENT IS WHAT THE NAMER IS ASKED ABOUT, THE ANSWER IS
// THE NAME, AND THE NAME THE PERSON SEES FIRST IS THEIR OWN WORDS.
func TestTheIntentNamerIsAskedAboutTheWordsThePersonTyped(t *testing.T) {
	completer := intentSays(1, "Fix the flaky lexer test")
	agent, path := intentAgent(t, completer, nil)
	lane, leave := agent.WatchTitle()
	defer leave()

	words := "please fix the flaky lexer test in the parser package before lunch"
	submitMessage(t, agent, Submission{Text: words})
	settleNaming(agent)

	if completer.asks() != 1 {
		t.Fatalf("the namer was asked %d times, want 1", completer.asks())
	}
	asked := completer.ask(0)
	if asked.messages[0].Role != "system" || messageContentText(asked.messages[0]) != titleSystem {
		t.Fatalf("the system message = %q", messageContentText(asked.messages[0]))
	}
	if body := asked.userWords(); !strings.Contains(body, words) || !strings.HasSuffix(body, intentPrompt) {
		t.Fatalf("the request is not the words and then the instruction:\n%s", body)
	}
	if strings.Contains(asked.request(), "the assistant's own answer") {
		t.Fatal("the namer was shown the assistant's answer")
	}
	if asked.maxTokens != intentTokens {
		t.Fatalf("the namer's output ceiling = %d, want %d", asked.maxTokens, intentTokens)
	}
	if got := agent.Title(); got != "Fix the flaky lexer test" {
		t.Fatalf("Title() = %q", got)
	}

	// The surfaces hear the fallback — the person's own words, cut — and then the name.
	fallback := OpeningName(words, nil)
	if names := drainTitles(lane); len(names) != 2 || names[0] != fallback || names[1] != "Fix the flaky lexer test" {
		t.Fatalf("title events = %q, want the fallback %q and then the earned name", names, fallback)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	lines := titleLines(t, path)
	if len(lines) != 2 || !strings.Contains(lines[0], `"literalName":true`) || strings.Contains(lines[1], `"literalName"`) {
		t.Fatalf("title lines = %q", lines)
	}
	if replayed, err := replaySessionFile(path); err != nil || replayed.title != "Fix the flaky lexer test" {
		t.Fatalf("replayed title = %q, %v", replayed.title, err)
	}
}

// THE MODEL IS NEVER SHOWN THE HARNESS'S OWN "ATTACHED FILE" LINE, nor a full
// path the door recorded: it is shown the person's words and the file's name.
func TestTheNamerReadsTheAttachmentRecordsAndNotTheWordsTheModelWasSent(t *testing.T) {
	completer := intentSays(1, "Summarise the quarterly data")
	agent, _ := intentAgent(t, completer, nil)

	path := "/home/ada/secret-project/data/q3-figures.csv"
	submitMessage(t, agent, Submission{
		Text:        "summarise this\n\nAttached file: " + path,
		Said:        said("summarise this"),
		Attachments: []MessageAttachment{fileAttachment("at1", path)},
	})
	settleNaming(agent)

	body := completer.ask(0).userWords()
	for _, want := range []string{"summarise this", "q3-figures.csv"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the namer was not shown %q:\n%s", want, body)
		}
	}
	for _, banned := range []string{"Attached file", "/home/ada", "secret-project"} {
		if strings.Contains(completer.ask(0).request(), banned) {
			t.Fatalf("the namer was shown %q:\n%s", banned, completer.ask(0).request())
		}
	}
}

// A MESSAGE OF ATTACHMENTS ALONE IS NAMED FROM THE FILES' NAMES, and when the
// model has nothing to add the name stays what the person attached.
func TestAnAttachmentOnlyMessageIsNamedFromTheFileNames(t *testing.T) {
	completer := intentFixture(1, func(int) intentAnswer { return intentAnswer{err: errors.New("provider unavailable")} })
	agent, path := intentAgent(t, completer, nil)

	one, two := "/Users/ada/work/design/Brief v2.pdf", "/Users/ada/work/design/notes.md"
	submitMessage(t, agent, Submission{
		Text:        "Attached file: " + one + "\n\nAttached file: " + two,
		Said:        said(""),
		Attachments: []MessageAttachment{fileAttachment("at1", one), fileAttachment("at2", two)},
	})
	settleNaming(agent)

	body := completer.ask(0).userWords()
	if strings.Contains(body, "What the person typed") {
		t.Fatalf("a message with no words was presented as having some:\n%s", body)
	}
	if !strings.Contains(body, "Files they attached: Brief v2.pdf, notes.md") {
		t.Fatalf("the files' names are not in the request:\n%s", body)
	}
	if got := agent.Title(); got != "Brief v2.pdf, notes.md" {
		t.Fatalf("Title() = %q, want the files' names", got)
	}
	if strings.Contains(agent.Title(), "/") || strings.Contains(agent.Title(), "Attached") {
		t.Fatalf("the name carries the harness's filing: %q", agent.Title())
	}
	if lines := titleLines(t, path); len(lines) != 1 {
		t.Fatalf("title lines = %q, want only the fallback", lines)
	}
}

// THE FOLDER'S ROW — what home and the rail list read without opening a journal
// — carries the same name: never the harness's "Attached file:" line, and the
// earned name once the model has answered.
func TestTheFoldersRowCarriesTheIntentNotTheHarnessFiling(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	completer := intentSays(1, "Review the design brief")
	agent, _ := intentAgent(t, completer, func(c *Config) { c.Place = Place{Dir: dir, Workspace: c.Workspace} })
	completer.gate = make(chan struct{})

	path := "/Users/ada/work/design/Brief v2.pdf"
	submitMessage(t, agent, Submission{
		Text: "Attached file: " + path, Said: said(""),
		Attachments: []MessageAttachment{fileAttachment("at1", path)},
	})
	<-completer.arrived
	agent.SettleWrites()
	if meta, err := LoadMeta(dir); err != nil || meta.Title != "Brief v2.pdf" {
		t.Fatalf("the row before the answer = %q, %v", meta.Title, err)
	}
	close(completer.gate)
	settleNaming(agent)
	agent.SettleWrites()
	if meta, err := LoadMeta(dir); err != nil || meta.Title != "Review the design brief" {
		t.Fatalf("the row after the answer = %q, %v", meta.Title, err)
	}
}

// ONLY AN ATTACHMENT RECORD SAYS "ATTACHED": a person who types a path has typed
// words, and their words are kept whole — nothing is stripped out of them.
func TestAFilePathTheyTypedIsTheirWordsAndIsKept(t *testing.T) {
	completer := intentFixture(1, func(int) intentAnswer { return intentAnswer{err: errors.New("down")} })
	agent, _ := intentAgent(t, completer, nil)

	words := "read /etc/hosts and tell me what Attached file means"
	submitMessage(t, agent, Submission{Text: words})
	settleNaming(agent)

	if body := completer.ask(0).userWords(); !strings.Contains(body, words) {
		t.Fatalf("the typed path was altered:\n%s", body)
	}
	if got, want := agent.Title(), OpeningName(words, nil); got != want || !strings.Contains(got, "/etc/hosts") {
		t.Fatalf("Title() = %q, want %q", got, want)
	}

	// A paste and a link are attachments, but they are not named files.
	if got := OpeningName("", []MessageAttachment{
		{Kind: "link", URL: "https://example.com/a/b.html", Name: "example.com"},
		{Kind: "paste", Name: "Pasted text"},
	}); got != "" {
		t.Fatalf("a link and a paste named a conversation %q", got)
	}
}

// THE NAME IS WHAT IT IS AT ACCEPTANCE AND THE ANSWER REPLACES IT ONLY IF NOBODY
// HAS TOUCHED IT: a person's rename wins whichever side of the answer it lands.
func TestAManualRenameWinsOverAnAnswerInFlight(t *testing.T) {
	completer := intentSays(1, "The model's name")
	completer.gate = make(chan struct{})
	agent, path := intentAgent(t, completer, nil)
	lane, leave := agent.WatchTitle()
	defer leave()

	submitMessage(t, agent, Submission{Text: "tidy the release notes"})
	<-completer.arrived
	if err := agent.RenameByPerson("My release notes"); err != nil {
		t.Fatal(err)
	}
	close(completer.gate)
	settleNaming(agent)

	if got := agent.Title(); got != "My release notes" {
		t.Fatalf("Title() = %q, want the person's name", got)
	}
	names := drainTitles(lane)
	for _, name := range names {
		if name == "The model's name" {
			t.Fatal("a title event carried the model's name over the person's")
		}
	}
	if len(names) == 0 || names[len(names)-1] != "My release notes" {
		t.Fatalf("the last name the surfaces heard = %q, want the person's", names)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	lines := titleLines(t, path)
	if len(lines) != 2 || !strings.Contains(lines[1], `"title":"My release notes"`) {
		t.Fatalf("the journal's last name is not the person's: %q", lines)
	}
	if replayed, _ := replaySessionFile(path); replayed.title != "My release notes" {
		t.Fatalf("replayed title = %q", replayed.title)
	}
}

// A RENAME TO EXACTLY THE FALLBACK IS STILL THE PERSON'S, and an answer that
// arrives afterwards does not take it from them.
func TestARenameToTheFallbackIsStillTheirs(t *testing.T) {
	completer := intentSays(1, "The model's name")
	completer.gate = make(chan struct{})
	agent, path := intentAgent(t, completer, nil)

	submitMessage(t, agent, Submission{Text: "tidy the release notes"})
	<-completer.arrived
	if err := agent.RenameByPerson("tidy the release notes"); err != nil {
		t.Fatal(err)
	}
	close(completer.gate)
	settleNaming(agent)

	if got := agent.Title(); got != "tidy the release notes" {
		t.Fatalf("Title() = %q", got)
	}
	for _, line := range titleLines(t, path) {
		if strings.Contains(line, "The model's name") {
			t.Fatalf("the model's answer reached the journal: %s", line)
		}
	}
}

// A SECOND TURN, A LATE SUBSCRIBER, A REOPEN AND THE END OF A TURN BUY NOTHING.
func TestTheNamerIsAskedOnceNotOnAReplayAReopenOrALaterTurn(t *testing.T) {
	completer := intentSays(3, "Lexer test repair")
	agent, path := intentAgent(t, completer, nil)

	submitMessage(t, agent, Submission{Text: "fix the lexer test"})
	settleNaming(agent)
	submitMessage(t, agent, Submission{Text: "and now rewrite the parser entirely"})
	agent.maybeTitle(context.Background(), nil)
	settleNaming(agent)

	// A late subscriber is handed the name, with no call behind it.
	late, leave := agent.WatchTitle()
	defer leave()
	if names := drainTitles(late); len(names) != 1 || names[0] != "Lexer test repair" {
		t.Fatalf("a late subscriber was told %q", names)
	}
	if completer.asks() != 1 {
		t.Fatalf("the namer was asked %d times, want 1", completer.asks())
	}
	for _, ask := range completer.asked {
		if strings.Contains(ask.request(), "rewrite the parser") {
			t.Fatal("a later message was shown to the namer")
		}
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopened, the conversation has its name and a new message buys none.
	reopened, err := newAgent(Config{
		Workspace: agent.config.Workspace, Model: "test/model", System: "SYSTEM", SessionFile: path, IntentNames: true,
	}, completer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if got := reopened.Title(); got != "Lexer test repair" {
		t.Fatalf("reopened Title() = %q", got)
	}
	completer.steps = append(completer.steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("carried on"), nil
	})
	submitMessage(t, reopened, Submission{Text: "carry on"})
	settleNaming(reopened)
	if completer.asks() != 1 {
		t.Fatalf("a reopened conversation bought a name: %d asks", completer.asks())
	}
}

// A CONVERSATION CLOSED BEFORE THE ANSWER ARRIVED IS ALREADY NAMED ON DISK, so
// reopening it asks for nothing: the fallback was written at acceptance.
func TestAConversationClosedBeforeTheAnswerIsNotAskedAgainOnReopen(t *testing.T) {
	completer := intentSays(1, "never arrives")
	completer.gate = make(chan struct{})
	agent, path := intentAgent(t, completer, nil)

	submitMessage(t, agent, Submission{Text: "plan the harbour redesign"})
	<-completer.arrived
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if replayed, err := replaySessionFile(path); err != nil || replayed.title != "plan the harbour redesign" {
		t.Fatalf("the conversation was left unnamed on disk: %q, %v", replayed.title, err)
	}

	completer.gate = nil
	reopened, err := newAgent(Config{
		Workspace: agent.config.Workspace, Model: "test/model", System: "SYSTEM", SessionFile: path, IntentNames: true,
	}, completer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	completer.steps = append(completer.steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("ok"), nil
	})
	submitMessage(t, reopened, Submission{Text: "continue"})
	settleNaming(reopened)
	if completer.asks() != 1 || reopened.Title() != "plan the harbour redesign" {
		t.Fatalf("asks = %d, title = %q", completer.asks(), reopened.Title())
	}
}

// A MODEL THAT IS DOWN, REFUSES, OR ANSWERS WITH THE INSTRUCTION LEAVES THE
// FALLBACK AND IS NOT ASKED AGAIN — not on the next turn, not after a wait.
func TestAFailedNameIsNotRetriedAndLeavesTheFallback(t *testing.T) {
	for _, row := range []struct {
		name   string
		answer intentAnswer
	}{
		{"an error", intentAnswer{err: errors.New("503 service unavailable")}},
		{"a timeout", intentAnswer{err: context.DeadlineExceeded}},
		{"an empty answer", intentAnswer{}},
		{"the instruction handed back", intentAnswer{title: "Name this conversation in at most six words"}},
		{"a plan", intentAnswer{title: "I'll start by reading the lexer"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			completer := intentFixture(2, func(int) intentAnswer { return row.answer })
			agent, path := intentAgent(t, completer, nil)
			words := "port the tokenizer to the new span api"
			submitMessage(t, agent, Submission{Text: words})
			settleNaming(agent)
			submitMessage(t, agent, Submission{Text: "and keep going"})
			settleNaming(agent)

			if completer.asks() != 1 {
				t.Fatalf("the namer was asked %d times after %s, want exactly 1", completer.asks(), row.name)
			}
			if got := agent.Title(); got != words {
				t.Fatalf("Title() = %q, want the person's words %q", got, words)
			}
			if lines := titleLines(t, path); len(lines) != 1 {
				t.Fatalf("title lines = %q, want only the fallback", lines)
			}
		})
	}
}

// THE MODEL NAME IS AT MOST TWENTY-FOUR CHARACTERS, THE ELLIPSIS INCLUDED, WHATEVER THE
// MODEL ANSWERS — and the request is bounded however much was typed.
func TestTheNameAndTheRequestAreBounded(t *testing.T) {
	long := "Refactoring the entire authentication middleware so that sessions survive restarts and tokens rotate"
	for _, answer := range []string{long, strings.Repeat("字", 80), strings.Repeat("a", 90)} {
		completer := intentSays(1, answer)
		agent, _ := intentAgent(t, completer, nil)
		submitMessage(t, agent, Submission{Text: strings.Repeat("a long, rambling opening sentence. ", 400)})
		settleNaming(agent)
		got := agent.Title()
		if n := utf8.RuneCountInString(got); n == 0 || n > AppNameLimit {
			t.Fatalf("a name of %d characters: %q", n, got)
		}
		if body := completer.ask(0).request(); utf8.RuneCountInString(body) > 1200 {
			t.Fatalf("the request was %d characters for a message of %d", utf8.RuneCountInString(body), 400*35)
		}
	}

	// Ten attached files with absurd names are six short ones in the request.
	completer := intentSays(1, "Many files")
	agent, _ := intentAgent(t, completer, nil)
	var attachments []MessageAttachment
	for i := 0; i < 10; i++ {
		attachments = append(attachments, fileAttachment(fmt.Sprintf("at%d", i), fmt.Sprintf("/x/%d-%s.txt", i, strings.Repeat("n", 200))))
	}
	submitMessage(t, agent, Submission{Text: "look", Said: said("look"), Attachments: attachments})
	settleNaming(agent)
	line := ""
	for _, l := range strings.Split(completer.ask(0).userWords(), "\n") {
		if strings.HasPrefix(l, "Files they attached:") {
			line = l
		}
	}
	if names := strings.Split(strings.TrimPrefix(line, "Files they attached: "), ", "); len(names) != intentFileMax {
		t.Fatalf("%d names in the request, want %d: %q", len(names), intentFileMax, line)
	}
	if utf8.RuneCountInString(line) > len("Files they attached: ")+intentFileMax*(intentFileClip+2) {
		t.Fatalf("the file line is %d characters", utf8.RuneCountInString(line))
	}
}

// THE ROUTE IS THE PERSON'S: the pin, then the tier — never a model this
// package names — and ONLY that rung. When it fails, the conversation's own
// model is not asked in its place.
func TestTheNamerRidesTheConfiguredTitleRouteAndNoOtherRung(t *testing.T) {
	for _, row := range []struct {
		name     string
		settings map[string]string
		want     string
	}{
		{"the tier", map[string]string{roles.TierKey(roles.TierLow): "cheap/model"}, "cheap/model"},
		{"the pin beats the tier", map[string]string{
			roles.TierKey(roles.TierLow):  "cheap/model",
			roles.PinKey(roles.RoleTitle): "pinned/model",
		}, "pinned/model"},
	} {
		t.Run(row.name, func(t *testing.T) {
			completer := intentFixture(1, func(int) intentAnswer { return intentAnswer{err: errors.New("503 service unavailable")} })
			agent, _ := intentAgent(t, completer, func(config *Config) {
				config.RolesSource = func(key string) (string, bool) {
					value, ok := row.settings[key]
					return value, ok
				}
			})
			submitMessage(t, agent, Submission{Text: "name this from the cheap route"})
			settleNaming(agent)
			if completer.asks() != 1 {
				t.Fatalf("%d requests reached the provider, want the one to the configured route", completer.asks())
			}
			if got := completer.ask(0).model; got != row.want {
				t.Fatalf("the namer rode %q, want %q", got, row.want)
			}
		})
	}
}

// THE SPEND GUARD IS CONSULTED BEFORE THE REQUEST LEAVES: a day at its cap sends
// nothing, and the conversation keeps the person's words as its name.
func TestTheSpendGuardStopsTheNamerBeforeDispatch(t *testing.T) {
	dir := t.TempDir()
	if err := config.SetCrewCap(dir, "0.5"); err != nil {
		t.Fatal(err)
	}
	completer := intentSays(1, "never asked")
	agent, _ := intentAgent(t, completer, func(c *Config) {
		c.ProfileDir = dir
		c.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) { return crewroute.Decision{}, nil }
	})
	agent.crewDayOnce.Do(func() { agent.crewDayHeld = NewSpendDay(0.5) })

	words := "name me while the day is spent"
	submitMessage(t, agent, Submission{Text: words})
	settleNaming(agent)

	if completer.asks() != 0 {
		t.Fatalf("a request was dispatched past the cap (%d)", completer.asks())
	}
	if got := agent.Title(); got != words {
		t.Fatalf("Title() = %q, want the person's words", got)
	}
}

// ONLY A JOURNALED CONVERSATION'S FIRST AUTHORED MESSAGE NAMES IT: an in-memory
// session has nowhere to keep a name, and a shell opening is not words to name.
func TestAnInMemoryDesktopConversationBuysNoName(t *testing.T) {
	completer := intentSays(1, "no")
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.IntentNames = true })
	submitMessage(t, agent, Submission{Text: "hello there"})
	settleNaming(agent)
	if completer.asks() != 0 || agent.Title() != "" {
		t.Fatalf("asks = %d, title = %q", completer.asks(), agent.Title())
	}
}

// A CONVERSATION THAT IS NOT THE DESKTOP'S KEEPS ITS NAMER: the terminal's
// five-to-eight-word name, asked for at the same moment, is untouched.
func TestATerminalConversationKeepsItsOwnNamer(t *testing.T) {
	completer := namedTurn(t, "answer", "tokenizer compatibility investigation")
	agent, _ := titleAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
	if got := awaitTitle(t, agent); got != "tokenizer compatibility investigation" {
		t.Fatalf("Title() = %q", got)
	}
	if completer.asks() != 1 {
		t.Fatalf("the terminal's namer was asked %d times", completer.asks())
	}
}

// A TEAM MEMBER STARTED BY A BRIEF IS STILL NAMED BY ITS BRIEF: nobody typed it,
// so no model is asked to represent it.
func TestAMemberStartedByABriefKeepsTheNameItsBriefGives(t *testing.T) {
	completer := intentSays(1, "never asked")
	agent, path := intentAgent(t, completer, nil)
	agent.mu.Lock()
	agent.recordUserLocked(userMessage{
		message: textMessage("user", "The manager started you.\n"+teamNewsLead+`"backend"`+":\n"+
			teamBriefWord+" #1: Rewrite the lexer so tokens carry spans.\n"),
		authored: true,
	})
	agent.mu.Unlock()
	agent.maybeTitle(context.Background(), nil)
	settleNaming(agent)
	if completer.asks() != 0 {
		t.Fatalf("the namer was asked for a member nobody typed to (%d)", completer.asks())
	}
	if got := agent.Title(); got != "Rewrite the lexer so tokens carry spans." {
		t.Fatalf("Title() = %q (journal %q)", got, titleLines(t, path))
	}
}

// OPENING NAMES ARE PURE: the same function the desktop draws before the engine
// answers, from structured records, with no path in it.
func TestOpeningNameFromRecords(t *testing.T) {
	for _, row := range []struct {
		name        string
		said        string
		attachments []MessageAttachment
		want        string
	}{
		{"words win", "fix the parser", []MessageAttachment{fileAttachment("1", "/a/b/c.go")}, "fix the parser"},
		{"files when no words", "  ", []MessageAttachment{fileAttachment("1", "/a/b/c.go"), fileAttachment("2", "/a/b/")}, "c.go, b"},
		{"windows separators", "", []MessageAttachment{{Kind: "file", Path: `C:\work\notes\todo.txt`}}, "todo.txt"},
		{"the label when there is no path", "", []MessageAttachment{{Kind: "file", Name: "todo.txt"}}, "todo.txt"},
		{"a link is not a file", "", []MessageAttachment{{Kind: "link", URL: "https://x.test/a.pdf"}}, ""},
		{"many files are cut", "", []MessageAttachment{
			fileAttachment("1", "/x/quarterly-report-final-v3.pdf"), fileAttachment("2", "/x/appendix-b.xlsx"),
		}, "quarterly-report-final-v3.pdf…"},
		{"nothing", "", nil, ""},
	} {
		if got := OpeningName(row.said, row.attachments); got != row.want {
			t.Errorf("%s: OpeningName = %q, want %q", row.name, got, row.want)
		}
		if n := utf8.RuneCountInString(OpeningName(row.said, row.attachments)); n > OpeningNameLimit {
			t.Errorf("%s: %d characters", row.name, n)
		}
	}
}

// A NAME THE MODEL ANSWERS IS CLEANED THE WAY EVERY NAMER'S IS, then held to the limit.
func TestCleanIntentTitle(t *testing.T) {
	for _, row := range []struct{ raw, want string }{
		{`"Fix the lexer test."`, "Fix the lexer test"},
		{"Title: Port the tokenizer", "Port the tokenizer"},
		{"**Harbour redesign plan**", "Harbour redesign plan"},
		{"port_the_tokenizer", "port the tokenizer"},
		{"", ""},
		{"Untitled", ""},
		{"<|im_start|>x", ""},
		{"Name this conversation in at most six words", ""},
	} {
		if got := cleanIntentTitle(row.raw); got != row.want {
			t.Errorf("cleanIntentTitle(%q) = %q, want %q", row.raw, got, row.want)
		}
	}
}

// A MESSAGE OF WORDS AND A PICTURE IS NAMED FROM THE WORDS ALONE — the namer is
// never handed an image — and a PICTURE ALONE has no words for a name to
// represent, so the conversation is left to be named by the person.
func TestAComposedMessageIsNamedFromItsWordsAndAPictureAloneIsNotNamed(t *testing.T) {
	completer := intentSays(2, "Review the harbour photo")
	agent, _ := intentAgent(t, completer, withVision)
	picture := writePNG(t, agent.config.Workspace, "harbour.png", 8, 8, 1)

	_, events, err := agent.SubmitComposed(watchedContext(agent), Submission{Text: "is the tide in?", Media: []SubmissionMedia{{ID: "a", Path: picture}}})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	settleNaming(agent)
	if completer.asks() != 1 || agent.Title() != "Review the harbour photo" {
		t.Fatalf("asks = %d, title = %q", completer.asks(), agent.Title())
	}
	if strings.Contains(completer.ask(0).request(), "data:image") || len(imagePartURLs(completer.ask(0).messages[len(completer.ask(0).messages)-1])) != 0 {
		t.Fatal("the namer was handed a picture")
	}

	silent := intentSays(2, "never asked")
	quiet, _ := intentAgent(t, silent, withVision)
	only := writePNG(t, quiet.config.Workspace, "only.png", 8, 8, 2)
	_, events, err = quiet.SubmitComposed(watchedContext(quiet), Submission{Media: []SubmissionMedia{{ID: "a", Path: only}}})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	settleNaming(quiet)
	submitMessage(t, quiet, Submission{Text: "now some words"})
	settleNaming(quiet)
	if silent.asks() != 0 || quiet.Title() != "" {
		t.Fatalf("a picture alone bought a name: asks = %d, title = %q", silent.asks(), quiet.Title())
	}
}

func TestIntentNamesIncludeAtMostTwentySiblingNames(t *testing.T) {
	c := intentSays(1, "Release review")
	a, _ := intentAgent(t, c, func(cfg *Config) {
		for n := 0; n < 21; n++ {
			cfg.IntentSiblingNames = append(cfg.IntentSiblingNames, fmt.Sprintf("Sibling %02d", n))
		}
	})
	submitMessage(t, a, Submission{Text: "Review the release with Maya"})
	settleNaming(a)
	body := c.ask(0).request()
	if !strings.Contains(body, "Sibling 00") || !strings.Contains(body, "Sibling 19") || strings.Contains(body, "Sibling 20") {
		t.Fatalf("sibling names not bounded in prompt: %s", body)
	}
}

func TestVagueIntentGetsOneSecondNameFromFinishedReply(t *testing.T) {
	c := intentFixture(2, func(n int) intentAnswer {
		if n == 0 {
			return intentAnswer{title: "Quick conversation"}
		}
		return intentAnswer{title: "Maya release review"}
	})
	a, path := intentAgent(t, c, nil)
	submitMessage(t, a, Submission{Text: "help me"})
	settleNaming(a)
	if c.asks() != 2 {
		t.Fatalf("naming calls = %d, want exactly 2", c.asks())
	}
	if !strings.Contains(c.ask(1).request(), "the assistant's own answer") || !strings.Contains(c.ask(1).request(), "help me") {
		t.Fatal("second name lacks the opening exchange")
	}
	if a.Title() != "Maya release review" {
		t.Fatalf("Title() = %q", a.Title())
	}
	submitMessage(t, a, Submission{Text: "keep going"})
	settleNaming(a)
	if c.asks() != 2 {
		t.Fatalf("later turn renamed again: %d calls", c.asks())
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newAgent(Config{Workspace: a.config.Workspace, Model: "test/model", System: "SYSTEM", SessionFile: path, IntentNames: true}, c)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c.steps = append(c.steps, func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("continued"), nil })
	submitMessage(t, reopened, Submission{Text: "continue"})
	settleNaming(reopened)
	if c.asks() != 2 {
		t.Fatalf("relaunch renamed again: %d calls", c.asks())
	}
}

func TestPersonsRenamePreventsSecondIntentName(t *testing.T) {
	c := intentSays(1, "Quick conversation")
	gate := make(chan struct{})
	c.steps[0] = func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		select {
		case <-gate:
			return textResponse("Maya needs a release review"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	a, _ := intentAgent(t, c, nil)
	finished := make(chan struct{})
	go func() { defer close(finished); submitMessage(t, a, Submission{Text: "help me"}) }()
	<-c.arrived
	if err := a.RenameByPerson("My chosen name"); err != nil {
		t.Fatal(err)
	}
	close(gate)
	<-finished
	settleNaming(a)
	if c.asks() != 1 || a.Title() != "My chosen name" {
		t.Fatalf("calls = %d, Title() = %q", c.asks(), a.Title())
	}
}

func TestGenericIntentWaitsForFirstReplyBeforeSecondName(t *testing.T) {
	for _, first := range []string{"Quick conversation", "Untitled", `"Untitled"`, "Untitled.", "Title: Untitled"} {
		t.Run(first, func(t *testing.T) {
			c := intentFixture(1, func(n int) intentAnswer {
				if n == 0 {
					return intentAnswer{title: first}
				}
				return intentAnswer{title: "Maya release review"}
			})
			reply := make(chan struct{})
			c.steps[0] = func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				select {
				case <-reply:
					return textResponse("Maya needs a release review. " + strings.Repeat("x", 500)), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			a, path := intentAgent(t, c, nil)
			finished := make(chan struct{})
			go func() { defer close(finished); submitMessage(t, a, Submission{Text: "Can you help me please"}) }()
			<-c.arrived
			if c.asks() != 1 {
				t.Fatalf("second name before reply: %d calls", c.asks())
			}
			close(reply)
			<-finished
			settleNaming(a)
			if c.asks() != 2 || a.Title() != "Maya release review" {
				t.Fatalf("calls = %d, Title() = %q", c.asks(), a.Title())
			}
			if strings.Contains(c.ask(1).request(), strings.Repeat("x", 400)) {
				t.Fatal("reply context was not clipped to 400 characters")
			}
			if !strings.Contains(strings.Join(readLines(t, path), "\n"), `"intentNameAttempts":2`) {
				t.Fatal("second naming attempt was not persisted")
			}
		})
	}
}

func TestAbandonedOpeningDoesNotNameFromALaterReply(t *testing.T) {
	c := intentSays(2, "Opening conversation")
	entered, release := make(chan struct{}), make(chan struct{})
	c.steps[0] = func(context.Context, []ai.Message) (*ai.Response, error) {
		close(entered)
		<-release
		return textResponse("abandoned reply"), nil
	}
	a, _ := intentAgent(t, c, nil)
	events, err := a.SubmitMessage(watchedContext(a), Submission{Text: "help me"})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	<-c.arrived
	a.Interrupt()
	if _, ok := a.Abandon(AbandonStopTimeout); !ok {
		t.Fatal("opening turn was not abandoned")
	}
	submitMessage(t, a, Submission{Text: "Review Maya's release instead"})
	close(release)
	collect(t, events)
	settleNaming(a)
	if c.asks() != 1 {
		t.Fatalf("later reply bought an opening rename: %d calls", c.asks())
	}
}
