package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// helpersProposal is the crew the scripted model proposes.
const helpersProposal = `{"name": "flaky", "goal": "keep CI green", "helpers": [{"handle": "ci", "role": "ci watcher", "brief": "watch every run and report a new flake"}]}`

// helpersModel is an OpenAI-shaped endpoint that answers a request carrying the
// app's proposal question with helpersProposal, and every other request with
// "done", keeping every request it was sent.
type helpersModel struct {
	server          *httptest.Server
	mu              sync.Mutex
	requests        []recordedRequest
	proposal        string
	proposalRelease <-chan struct{}
}

func newHelpersModel(t *testing.T) *helpersModel {
	t.Helper()
	model := &helpersModel{}
	model.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			http.Error(writer, "no catalog for a test", http.StatusServiceUnavailable)
			return
		}
		var envelope recordedRequest
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		model.mu.Lock()
		model.requests = append(model.requests, envelope)
		model.mu.Unlock()
		answer := "done"
		if envelope.messageContaining("Propose a crew") != "" {
			model.mu.Lock()
			answer = model.proposal
			if answer == "" {
				answer = helpersProposal
			}
			model.mu.Unlock()
		}
		content, _ := json.Marshal(answer)
		if envelope.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			model.mu.Lock()
			release := model.proposalRelease
			model.mu.Unlock()
			waited := false
			for start := 0; start < len(answer); start += 7 {
				end := min(start+7, len(answer))
				chunk, _ := json.Marshal(answer[start:end])
				_, _ = writer.Write([]byte(`data: {"id":"helpers","choices":[{"index":0,"delta":{"content":` + string(chunk) + `}}]}` + "\n\n"))
				writer.(http.Flusher).Flush()
				if !waited && release != nil && envelope.messageContaining("Propose a crew") != "" && strings.Contains(answer[:end], "\n") {
					select {
					case <-release:
					case <-request.Context().Done():
						return
					}
					waited = true
				}
			}
			_, _ = writer.Write([]byte(`data: {"id":"helpers","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = writer.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":` + string(content) + `},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(model.server.Close)
	return model
}

// sent is every request the model was sent that carries want in any message.
func (m *helpersModel) sent(want string) []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []recordedRequest
	for _, request := range m.requests {
		if request.messageContaining(want) != "" {
			out = append(out, request)
		}
	}
	return out
}

// helpersEnvironment is a scratch home whose model is the helpers model, and
// the workspace the conversation works in.
func helpersEnvironment(t *testing.T) (*helpersModel, string) {
	t.Helper()
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-dummy-not-a-real-key")
	model := newHelpersModel(t)
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)
	freshEngineProcess(t)
	return model, workspace
}

// plumbing reports whether an entry carries the app's question or the
// model's JSON answer to it.
func plumbing(text string) bool {
	return strings.Contains(text, "Propose a crew") || strings.Contains(text, `"helpers"`)
}

// A CREW PROPOSAL SHOWS ITS ASK AND NOTHING ELSE, end to end on a real
// conversation: the person asks for one, and what reaches the app is the
// proposal's ask — no turn, no message of theirs, no streamed reply and no
// answer carrying the app's question or the model's JSON. The listing reads
// the conversation back without either, so does a backend started afresh on
// the same home, and the person's next turn is sent to the model without them.
func TestAppCrewProposalShowsOnlyTheCard(t *testing.T) {
	model, workspace := helpersEnvironment(t)
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	closed := false
	defer func() {
		if !closed {
			backend.Close()
		}
	}()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatalf("create = %v", err)
	}
	if _, err := backend.Send(agent.ID, "say done", ""); err != nil {
		t.Fatalf("send = %v", err)
	}
	rec.waitFor(t, 30*time.Second, appTurnDone)
	rec.mu.Lock()
	mark := len(rec.events)
	rec.mu.Unlock()

	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatalf("crews.propose = %v", err)
	}
	i := rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		opened, ok := data.(appwire.AskOpened)
		return event == appwire.EventAskOpened && ok && opened.Ask.Subkind == appwire.SubkindCrewProposal
	})
	rec.mu.Lock()
	ask := rec.data[i].(appwire.AskOpened).Ask
	for j := mark; j < len(rec.events); j++ {
		switch data := rec.data[j].(type) {
		case appwire.TurnStarted, appwire.TurnDelta, appwire.TurnTool, appwire.TurnDone:
			rec.mu.Unlock()
			t.Fatalf("asking for a crew proposal ran a turn: %s %+v", rec.events[j], data)
		case appwire.EntryAdded:
			rec.mu.Unlock()
			t.Fatalf("asking for a crew proposal added an entry to the chat: %+v", data.Entry)
		}
	}
	rec.mu.Unlock()
	if ask.Agent != agent.ID || ask.Proposal == nil || ask.Proposal.Name != "flaky" || len(ask.Proposal.Helpers) != 1 || ask.Proposal.Helpers[0].Handle != "ci" {
		t.Fatalf("the proposal card = %+v", ask)
	}

	// The model was asked about the conversation itself, with no belt to call.
	asked := model.sent("Propose a crew")
	if len(asked) != 1 || asked[0].messageContaining("say done") == "" || len(asked[0].Tools) != 0 {
		t.Fatalf("the proposal was asked %d times; first: %+v", len(asked), asked)
	}

	entries, _, err := backend.Entries(agent.ID, "", appwire.MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Text != "say done" {
		t.Fatalf("the conversation reads back as %+v", entries)
	}
	for _, e := range entries {
		if plumbing(e.Text) {
			t.Fatalf("the conversation reads back the app's plumbing: %+v", e)
		}
	}

	// The person's next turn is the conversation's second, and is sent
	// without the question or its answer.
	if _, err := backend.Send(agent.ID, "next step please", ""); err != nil {
		t.Fatalf("the next send = %v", err)
	}
	// waitFor reads each event once, in order, so counting the agent's turn
	// endings finds the second turn's and not the first's again.
	endings := 0
	rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		if done, ok := data.(appwire.TurnDone); ok && event == appwire.EventTurnDone && done.Agent == agent.ID {
			endings++
		}
		return endings == 2
	})
	for _, request := range model.sent("next step please") {
		for _, text := range request.texts() {
			if plumbing(text) {
				t.Fatalf("the next turn was sent the app's plumbing: %q", text)
			}
		}
	}
	entries, _, err = backend.Entries(agent.ID, "", appwire.MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 || entries[2].ID != appUserEntryID(1) || entries[2].Text != "next step please" {
		t.Fatalf("after the next turn the conversation reads %+v", entries)
	}

	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true

	// A RELAUNCH reads the same conversation, and the proposal's card comes
	// back from the app layer rather than from the chat.
	again := newAppBackend(newAppRecorder(), appOpenInProcess)
	defer again.Close()
	if _, err := again.ListAgents(workspace); err != nil {
		t.Fatal(err)
	}
	entries, _, err = again.Entries(agent.ID, "", appwire.MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("after a relaunch the conversation reads %+v", entries)
	}
	for _, e := range entries {
		if plumbing(e.Text) {
			t.Fatalf("after a relaunch the conversation reads back the app's plumbing: %+v", e)
		}
	}
}

// proposingConversation is a fake conversation whose side ask answers what a
// test scripts, and can be held until the test lets it answer.
type proposingConversation struct {
	*fakeConversation
	mu      sync.Mutex
	answer  string
	err     error
	asked   []string
	release chan struct{}
}

func (p *proposingConversation) SideAsk(ctx context.Context, question string) (string, error) {
	p.mu.Lock()
	p.asked = append(p.asked, question)
	release, answer, err := p.release, p.answer, p.err
	p.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return answer, err
}

func (p *proposingConversation) questions() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.asked...)
}

// proposingBackend is a backend whose one agent's conversation is conv, with
// its app file, crew layer and spending of its own.
func proposingBackend(t *testing.T, conv *proposingConversation) (*appBackend, *appRecorder, appwire.Agent) {
	t.Helper()
	// The folders are made before the backend's close is registered, so the
	// close runs first and the crew watch has stopped writing before they go.
	state, profile, workspace := t.TempDir(), t.TempDir(), t.TempDir()
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(state, appStateName)}
	backend.profileDir = profile
	backend.today = func(time.Time) float64 { return 0 }
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	return backend, rec, agent
}

// notice waits for the agent's next quiet notice and answers its words.
func notice(t *testing.T, rec *appRecorder, agent string) string {
	t.Helper()
	i := rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return ok && added.Entry.Agent == agent && added.Entry.Kind == appwire.EntryNotice
	})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	entry := rec.data[i].(appwire.EntryAdded).Entry
	if !entry.Quiet {
		t.Fatalf("the notice is not quiet: %+v", entry)
	}
	return entry.Text
}

// AN ANSWER THAT IS NOT A PROPOSAL IS SAID IN ONE QUIET LINE, and so is an ask
// that failed, with its reason; neither opens a card or sends the conversation
// anything.
func TestAppCrewProposalSaysWhenNoProposalCameBack(t *testing.T) {
	conv := &proposingConversation{fakeConversation: &fakeConversation{}, answer: "I would rather do this alone."}
	backend, rec, agent := proposingBackend(t, conv)
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	if got := notice(t, rec, agent.ID); got != appNoProposal {
		t.Fatalf("an unreadable answer says %q", got)
	}

	conv.mu.Lock()
	conv.answer, conv.err = "", errors.New("the provider refused: 402 out of credits")
	conv.mu.Unlock()
	rec.mu.Lock()
	mark := len(rec.events)
	rec.mu.Unlock()
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return ok && strings.HasPrefix(added.Entry.Text, appNoAnswer+" · the provider refused")
	})

	rec.mu.Lock()
	defer rec.mu.Unlock()
	for j := range rec.events {
		if rec.events[j] == appwire.EventAskOpened || rec.events[j] == appwire.EventTurnStarted {
			t.Fatalf("a press with no proposal said %s (%d after the second press)", rec.events[j], j-mark)
		}
	}
	conv.fakeConversation.mu.Lock()
	submits := conv.fakeConversation.submits
	conv.fakeConversation.mu.Unlock()
	if submits != 0 {
		t.Fatalf("the conversation was sent %d turns", submits)
	}
	if asked := conv.questions(); len(asked) != 2 || asked[0] != appProposalAsk {
		t.Fatalf("the model was asked %q", asked)
	}
}

// ONE ASK AT A TIME: a second press while the first is out is answered by the
// first's card, and the model is asked once.
func TestAppCrewProposalAsksOnceWhilePressedTwice(t *testing.T) {
	conv := &proposingConversation{fakeConversation: &fakeConversation{}, answer: helpersProposal, release: make(chan struct{})}
	backend, rec, agent := proposingBackend(t, conv)
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(conv.questions()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	close(conv.release)
	rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	if asked := conv.questions(); len(asked) != 1 {
		t.Fatalf("two presses asked the model %d times", len(asked))
	}
	// And once it has answered, the next press asks again.
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for len(conv.questions()) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if asked := conv.questions(); len(asked) != 2 {
		t.Fatalf("a press after the answer asked the model %d times in all", len(asked))
	}
}

// A LIMIT NOBODY CAN READ REFUSES THE PRESS, as it refuses a turn: with the
// spending ledger unreadable the press ends in its one quiet line, saying why,
// the model is not asked and nobody is asked about a cap.
func TestAppCrewProposalWithAnUnreadableLedgerAsksNothing(t *testing.T) {
	profile := appDayProfile(t)
	if err := config.WriteDailyBudgetUSD(profile, 20); err != nil {
		t.Fatal(err)
	}
	conv := &proposingConversation{fakeConversation: &fakeConversation{}, answer: helpersProposal}
	backend, rec, agent := proposingBackend(t, conv)
	backend.today = nil
	if err := os.MkdirAll(session.UsageLedgerPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	got := notice(t, rec, agent.ID)
	if !strings.HasPrefix(got, appNoAnswer+" · ") || strings.Contains(got, "Day cap reached") {
		t.Fatalf("with the ledger unreadable the press says %q", got)
	}
	if asked := conv.questions(); len(asked) != 0 {
		t.Fatalf("with the ledger unreadable the model was asked %q", asked)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, event := range rec.events {
		if event == appwire.EventAskOpened {
			t.Fatal("with the ledger unreadable the press opened an ask")
		}
	}
}

// AT THE DAY CAP THE MODEL IS NOT ASKED: the press is answered with one quiet
// line saying so, and the person is asked about the cap.
func TestAppCrewProposalAtTheDayCapAsksAboutTheCap(t *testing.T) {
	conv := &proposingConversation{fakeConversation: &fakeConversation{}, answer: helpersProposal}
	backend, rec, agent := proposingBackend(t, conv)
	backend.today = func(time.Time) float64 { return 1e6 }
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	i := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	ask := rec.data[i].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	if ask.Subkind != appwire.SubkindBudget {
		t.Fatalf("at the cap the press opened %+v", ask)
	}
	if got := notice(t, rec, agent.ID); !strings.HasPrefix(got, appNoAnswer+" · Day cap reached · ") {
		t.Fatalf("at the cap the press says %q", got)
	}
	if asked := conv.questions(); len(asked) != 0 {
		t.Fatalf("at the cap the model was asked %q", asked)
	}
}

// THE CARD ENDS IN ONE RECEIPT: keeping the
// agent alone leaves "Kept as one agent · you · <time>", and making the crew
// leaves the crew's formation fact alone — not a second "Made the crew" beside
// it.
// Contract 1: proposal answers settle the Inbox and chat exactly once.
func TestAppCrewProposalEndsInOneReceipt(t *testing.T) {
	conv := &proposingConversation{fakeConversation: &fakeConversation{}, answer: helpersProposal}
	backend, rec, agent := proposingBackend(t, conv)
	opened := func() appwire.Ask {
		t.Helper()
		rec.mu.Lock()
		seen := len(rec.events)
		rec.mu.Unlock()
		if err := backend.CrewPropose(agent.ID); err != nil {
			t.Fatal(err)
		}
		i := rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
			seen--
			opened, ok := data.(appwire.AskOpened)
			return seen < 0 && ok && opened.Ask.Subkind == appwire.SubkindCrewProposal
		})
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return rec.data[i].(appwire.AskOpened).Ask
	}
	receipts := func(from int) []appwire.Entry {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		var out []appwire.Entry
		for j := from; j < len(rec.events); j++ {
			if added, ok := rec.data[j].(appwire.EntryAdded); ok && added.Entry.Kind == appwire.EntryReceipt && added.Entry.Agent == agent.ID {
				out = append(out, added.Entry)
			}
		}
		return out
	}

	kept := opened()
	if err := backend.AnswerAsk(agent.ID, kept.ID, appwire.ProposalEdit, ""); err != nil {
		t.Fatal(err)
	}
	if waiting, counts, err := backend.ListInbox(appwire.InboxWaiting, ""); err != nil || len(waiting) != 1 || counts.Waiting != 1 {
		t.Fatalf("Edit changed Waiting: %+v %+v %v", waiting, counts, err)
	}
	rec.mu.Lock()
	mark := len(rec.events)
	rec.mu.Unlock()
	if err := backend.AnswerAsk(agent.ID, kept.ID, appwire.ProposalKeep, ""); err != nil {
		t.Fatal(err)
	}
	if got := receipts(mark); len(got) != 1 || !strings.HasPrefix(got[0].Text, "Kept as one agent · you · ") {
		t.Fatalf("keeping the agent alone left %+v", got)
	}

	made := opened()
	rec.mu.Lock()
	mark = len(rec.events)
	rec.mu.Unlock()
	// Made with its helpers edited away, so no start runs on behind the test:
	// the receipt is what is asserted, and a start is the crew watch's own.
	edits := &appwire.Proposal{Helpers: []appwire.ProposalHelper{}}
	create := appwire.CrewCreate{From: "proposal", Proposal: strings.TrimPrefix(made.ID, appProposalAskPrefix), Edits: edits}
	crew, err := backend.CrewCreate(create)
	if err != nil {
		t.Fatal(err)
	}
	if asks, err := backend.ListAsks(agent.ID); err != nil || len(asks) != 0 {
		t.Fatalf("asks after create: %+v %v", asks, err)
	}
	if waiting, counts, err := backend.ListInbox(appwire.InboxWaiting, ""); err != nil || len(waiting) != 0 || counts.Waiting != 0 {
		t.Fatalf("Waiting after create: %+v %+v %v", waiting, counts, err)
	}
	if again, err := backend.CrewCreate(create); err != nil || again.ID != crew.ID {
		t.Fatalf("retried create: %+v %v", again, err)
	}
	done, counts, err := backend.ListInbox(appwire.InboxDone, "")
	if err != nil || len(done) != 2 || counts.DoneToday != 2 || done[0].ID != appInboxID(agent.ID, made.ID) || done[0].Answered == nil || done[0].Answered.Option != appwire.ProposalCreate {
		t.Fatalf("Done after Keep and retried create: %+v %+v %v", done, counts, err)
	}
	got := receipts(mark)
	if len(got) != 1 || got[0].Op == nil || got[0].Op.Kind != appwire.OpCrewFormed || got[0].Op.Name != "flaky" {
		t.Fatalf("making the crew left %+v", got)
	}
}
