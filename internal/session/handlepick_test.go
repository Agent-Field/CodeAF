package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// handleModel is a fake title model for the handle ask: it answers with the
// words given for the title it is shown, and counts the asks.
type handleModel struct {
	mu    sync.Mutex
	words map[string]string
	asks  int
}

func (h *handleModel) aside(messages []ai.Message) (*ai.Response, bool) {
	if !isTitleCall(messages) {
		return nil, false
	}
	last := messageContentText(messages[len(messages)-1])
	if !strings.Contains(last, handleAsk) {
		return textResponse(""), true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asks++
	for title, words := range h.words {
		if strings.Contains(last, title) {
			return textResponse(words), true
		}
	}
	return textResponse(""), true
}

func (h *handleModel) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.asks
}

// handleFixture is a team, test, of three conversations with the titles the
// manager made @review, @reviewing and @session of, written by a build that
// did not keep who chose a handle, plus one member whose handle was typed.
type handleFixture struct {
	profile string
	teamID  string
	paths   map[string]string // by legacy handle
}

var handleTitles = map[string]string{
	"review":    "santosh dev2 branch code complexity & security review",
	"reviewing": "CodeAF repo issue tags & milestones",
	"session":   "quantum gravity research updates / session monitor",
	"lexer":     "rewrite the lexer",
}

func newHandleFixture(t *testing.T) handleFixture {
	t.Helper()
	root := t.TempDir()
	f := handleFixture{profile: filepath.Join(root, "profile"), teamID: teams.NewID(), paths: map[string]string{}}
	var members []teams.Member
	for _, handle := range []string{"review", "reviewing", "session", "lexer"} {
		dir := filepath.Join(root, "sessions", handle)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, placeTranscript)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		f.paths[handle] = path
		m := teams.Member{Key: convKeyOf(t, path), File: path, Word: handleTitles[handle], Handle: handle}
		if handle == "lexer" {
			m.HandleBy = teams.HandleByTyped
		}
		members = append(members, m)
	}
	// Written whole, as the older build wrote it: no handle_by on the three.
	err := teams.Save(f.profile, []teams.Team{{ID: f.teamID, Name: "test", Members: members}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f handleFixture) member(t *testing.T, handle string) teams.Member {
	t.Helper()
	file, err := teams.Load(f.profile)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := file.Teams[0].Member(convKeyOf(t, f.paths[handle]))
	if !ok {
		t.Fatalf("no member for %s", handle)
	}
	return m
}

func (f handleFixture) traffic(t *testing.T) []string {
	t.Helper()
	entries, err := teams.ReadTraffic(f.profile, f.teamID, teamLogStart, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Kind+" "+e.From+" "+e.To+" "+e.Text)
	}
	return out
}

// handleAgent is the conversation that was @<handle>, titled already, with the
// fake model behind it.
func handleAgent(t *testing.T, f handleFixture, handle string, model *handleModel) *Agent {
	t.Helper()
	completer := &scriptedCompleter{aside: model.aside}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ProfileDir = f.profile
		config.SessionFile = f.paths[handle]
		config.Place = Place{Dir: filepath.Dir(f.paths[handle])}
	})
	agent.mu.Lock()
	agent.title = handleTitles[handle]
	agent.mu.Unlock()
	return agent
}

// THE ONE-TIME PASS, WITH THE REAL TITLES. Each conversation that already had a
// title and a guessed handle has its handle chosen by the model on its next
// turn, once: @review becomes @security, @reviewing @milestones and @session
// @gravity, each rename is said to everyone in the team's Traffic, and a second
// turn asks nothing more.
func TestTheTitleModelChoosesOneWordHandlesOnce(t *testing.T) {
	f := newHandleFixture(t)
	model := &handleModel{words: map[string]string{
		handleTitles["review"]:    "security complexity branch",
		handleTitles["reviewing"]: "Milestones, tags, issues",
		handleTitles["session"]:   "gravity quantum monitor",
	}}
	want := map[string]string{"review": "security", "reviewing": "milestones", "session": "gravity"}
	for _, old := range []string{"review", "reviewing", "session"} {
		agent := handleAgent(t, f, old, model)
		submitAndWait(t, agent, "carry on")
		agent.titleJobs.Wait()
		submitAndWait(t, agent, "and again")
		agent.titleJobs.Wait()
		m := f.member(t, old)
		if m.Handle != want[old] || m.HandleBy != teams.HandleByModel {
			t.Errorf("@%s became %+v, want @%s chosen by the model", old, m, want[old])
		}
	}
	if got := model.count(); got != 3 {
		t.Errorf("the model was asked %d times for three conversations", got)
	}
	log := strings.Join(f.traffic(t), "\n")
	for _, line := range []string{
		"event system everyone @review is now @security",
		"event system everyone @reviewing is now @milestones",
		"event system everyone @session is now @gravity",
	} {
		if !strings.Contains(log, line) {
			t.Errorf("the Traffic lacks %q:\n%s", line, log)
		}
	}
}

// A HANDLE THAT WAS TYPED IS NEVER REPLACED, and nothing is asked for it.
func TestATypedHandleIsNeverAskedAbout(t *testing.T) {
	f := newHandleFixture(t)
	model := &handleModel{words: map[string]string{handleTitles["lexer"]: "parser tokens grammar"}}
	agent := handleAgent(t, f, "lexer", model)
	submitAndWait(t, agent, "carry on")
	agent.titleJobs.Wait()
	if m := f.member(t, "lexer"); m.Handle != "lexer" || m.HandleBy != teams.HandleByTyped {
		t.Fatalf("a typed handle changed: %+v", m)
	}
	if model.count() != 0 || len(f.traffic(t)) != 0 {
		t.Fatalf("a typed handle was asked about: %d asks, traffic %v", model.count(), f.traffic(t))
	}
}

// A CLASH TAKES THE MODEL'S SECOND WORD. The conversation that was @session
// answers "security" first, which @review already became.
func TestAHandleClashTakesTheSecondWord(t *testing.T) {
	f := newHandleFixture(t)
	model := &handleModel{words: map[string]string{
		handleTitles["review"]:  "security",
		handleTitles["session"]: "security gravity",
	}}
	for _, old := range []string{"review", "session"} {
		agent := handleAgent(t, f, old, model)
		agent.chooseTeamHandles(context.Background(), handleTitles[old], "test/model")
	}
	if got := f.member(t, "session").Handle; got != "gravity" {
		t.Fatalf("the clash took %q", got)
	}
}

// THE RENAME REACHES EVERYONE, the manager included, as codeaf's line.
func TestTheRenameIsToldToTheManagerAndTheMembers(t *testing.T) {
	entry := handleRenameEntry("review", "security")
	for _, role := range []teamRole{{name: "test", handle: "gravity"}, {name: "test", handle: "boss", manager: true, managed: true}} {
		if got := teamLine(role, entry); got != "from codeaf: @review is now @security" {
			t.Errorf("manager %v is told %q", role.manager, got)
		}
	}
	if entry.Member != "" {
		t.Error("the rename names a member, and would be read as its state")
	}
}

// AN ANSWER IS READ FOR ONE-WORD HANDLES AND NOTHING ELSE.
func TestCleanHandleChoices(t *testing.T) {
	for raw, want := range map[string]string{
		"security complexity branch":   "security complexity branch",
		"Milestones, tags, issues":     "milestones tags issues",
		"**gravity**\nbecause physics": "gravity",
		"review session research":      "",
		"x manager everyone":           "",
		"api-docs":                     "apidocs",
		"":                             "",
	} {
		if got := strings.Join(cleanHandleChoices(raw), " "); got != want {
			t.Errorf("cleanHandleChoices(%q) = %q, want %q", raw, got, want)
		}
	}
}

// flakyHandle is a title model that fails its first handle asks, then answers.
type flakyHandle struct {
	mu    sync.Mutex
	left  int
	err   error
	words string
	asks  int
}

func (f *flakyHandle) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	last := ""
	if len(messages) > 0 {
		last = messageContentText(messages[len(messages)-1])
	}
	if !strings.Contains(last, handleAsk) {
		return textResponse("ok"), nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asks++
	if f.left > 0 {
		f.left--
		return nil, f.err
	}
	return textResponse(f.words), nil
}

func (f *flakyHandle) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asks
}

// A TRANSIENT FAILURE IS ASKED ONCE MORE. The first call times out, the second
// answers, and a later turn does not ask again: still one question per title.
func noHandleWait(t *testing.T) {
	t.Helper()
	prev := handleBackoff
	handleBackoff = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	t.Cleanup(func() { handleBackoff = prev })
}

func TestAHandleAskRetriesOnceAfterATimeout(t *testing.T) {
	noHandleWait(t)
	f := newHandleFixture(t)
	model := &flakyHandle{left: 1, err: context.DeadlineExceeded, words: "security"}
	agent, _ := newTestAgent(t, model, func(config *Config) {
		config.ProfileDir = f.profile
		config.SessionFile = f.paths["review"]
		config.Place = Place{Dir: filepath.Dir(f.paths["review"])}
	})
	agent.mu.Lock()
	agent.title = handleTitles["review"]
	agent.mu.Unlock()
	submitAndWait(t, agent, "carry on")
	agent.titleJobs.Wait()
	if m := f.member(t, "review"); m.Handle != "security" || m.HandleBy != teams.HandleByModel {
		t.Fatalf("a timeout then an answer left %+v", m)
	}
	if got := model.count(); got != 2 {
		t.Fatalf("the model was asked %d times, want one retry", got)
	}
	submitAndWait(t, agent, "and again")
	agent.titleJobs.Wait()
	if got := model.count(); got != 2 {
		t.Fatalf("a second turn asked again: %d asks", got)
	}
}

// A REFUSAL IS NOT ASKED AGAIN. The guess stands.
func TestAHandleAskRetriesOnceAfterANetworkFailure(t *testing.T) {
	noHandleWait(t)
	f := newHandleFixture(t)
	model := &flakyHandle{left: 1, err: errors.New("read: connection reset by peer"), words: "gravity"}
	agent, _ := newTestAgent(t, model, func(config *Config) {
		config.ProfileDir = f.profile
		config.SessionFile = f.paths["session"]
		config.Place = Place{Dir: filepath.Dir(f.paths["session"])}
	})
	agent.mu.Lock()
	agent.title = handleTitles["session"]
	agent.mu.Unlock()
	submitAndWait(t, agent, "carry on")
	agent.titleJobs.Wait()
	if m := f.member(t, "session"); m.Handle != "gravity" {
		t.Fatalf("a dropped connection then an answer left %+v", m)
	}
	if got := model.count(); got != 2 {
		t.Fatalf("the model was asked %d times, want one retry", got)
	}
}

func TestAHandleAskDoesNotRetryARefusal(t *testing.T) {
	noHandleWait(t)
	f := newHandleFixture(t)
	model := &flakyHandle{left: 2, err: &provider.APIError{Status: 400, Message: "invalid request body"}, words: "security"}
	agent, _ := newTestAgent(t, model, func(config *Config) {
		config.ProfileDir = f.profile
		config.SessionFile = f.paths["review"]
		config.Place = Place{Dir: filepath.Dir(f.paths["review"])}
	})
	agent.mu.Lock()
	agent.title = handleTitles["review"]
	agent.mu.Unlock()
	submitAndWait(t, agent, "carry on")
	agent.titleJobs.Wait()
	if m := f.member(t, "review"); m.Handle != "review" {
		t.Fatalf("a refusal replaced the guess: %+v", m)
	}
	if got := model.count(); got != 1 {
		t.Fatalf("a refusal was asked %d times, want 1", got)
	}
}
