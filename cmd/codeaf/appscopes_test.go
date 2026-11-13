package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Each offered scope travels through asks.answer, writes exactly one rule and
// leaves the question open on invalid input. No command is executed here.
func TestAppPermissionScopesOnTheWire(t *testing.T) {
	for _, tc := range []struct {
		tool, command, reason string
		scopes                []string
	}{
		{"bash", "git status --short", "", []string{"git status*", "git *", "git status --short"}},
		{"write", "", "", []string{"write"}},
		{"bash", "git status && git push", "compound", nil},
		{"slack_send", "", "acts_as_you", nil},
		{"bash", "rm -rf /", "critical", nil},
	} {
		t.Run(tc.tool+tc.command, func(t *testing.T) {
			attempts := tc.scopes
			if len(attempts) == 0 {
				attempts = []string{""}
			}
			for _, scope := range attempts {
				t.Run(scope, func(t *testing.T) {
					profile := t.TempDir()
					t.Setenv(config.ProfileDirEnv, profile)
					conv := &fakeConversation{}
					var mu sync.Mutex
					var opens, releases int
					rec := newAppRecorder()
					backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
					clock := time.Date(2026, time.October, 20, 10, 45, 0, 0, time.Local)
					backend.now = func() time.Time { return clock }
					defer backend.Close()
					agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
					if err != nil {
						t.Fatal(err)
					}
					a, _ := backend.find(agent.ID)
					live := backend.liveOf(a)
					args, _ := json.Marshal(map[string]string{"command": tc.command})
					backend.locked(func() { live.consents[4] = appConsentCall{Tool: tc.tool, Args: string(args)} })
					q := session.Question{ID: 4, Kind: session.QuestionConsent, Subject: session.SubjectRef{Kind: session.SubjectCall, Name: tc.tool}, Options: session.AnswerOptions(session.QuestionConsent)}
					backend.questionEvent(a, agent.ID, live, session.Event{Kind: session.EventQuestion, Question: &q})
					w := newActionWire(t, backend)
					raw, wireErr := w.call("asks.list", map[string]any{"agent": agent.ID})
					if wireErr != nil {
						t.Fatal(wireErr)
					}
					var result struct {
						Asks []appwire.Ask `json:"asks"`
					}
					if err := json.Unmarshal(raw, &result); err != nil || len(result.Asks) != 1 {
						t.Fatalf("asks: %s %v", raw, err)
					}
					ask := result.Asks[0]
					if !reflect.DeepEqual(ask.Scopes, tc.scopes) || ask.AlwaysAsk != tc.reason {
						t.Fatalf("ask = %+v", ask)
					}
					always := false
					for _, o := range ask.Options {
						always = always || o.ID == appwire.OptionAlways
					}
					if always != (len(tc.scopes) > 0) {
						t.Fatalf("always = %v: %+v", always, ask)
					}
					if !always {
						return
					}
					for _, invalid := range []string{"", "git push*"} {
						_, wireErr = w.call("asks.answer", map[string]any{"agent": agent.ID, "id": ask.ID, "option": "always", "scope": invalid})
						wantCode(t, wireErr, appwire.CodeBadScope)
						if len(conv.remembered) != 0 || config.BashApprovalsAt(profile) != "" || config.ToolApprovalsAt(profile) != "" {
							t.Fatal("bad scope saved or answered")
						}
					}
					blocked := filepath.Join(profile, "not-a-directory")
					if err := os.WriteFile(blocked, []byte("fixture"), 0600); err != nil {
						t.Fatal(err)
					}
					t.Setenv(config.ProfileDirEnv, blocked)
					_, wireErr = w.call("asks.answer", map[string]any{"agent": agent.ID, "id": ask.ID, "option": "always", "scope": scope})
					wantCode(t, wireErr, appwire.CodeEngine)
					backend.locked(func() {
						if _, open := live.questions[ask.ID]; !open || live.answered[ask.ID] != "" {
							t.Error("failed save closed or answered the ask")
						}
					})
					t.Setenv(config.ProfileDirEnv, profile)
					_, wireErr = w.call("asks.answer", map[string]any{"agent": agent.ID, "id": ask.ID, "option": "always", "scope": scope})
					if wireErr != nil {
						t.Fatal(wireErr)
					}
					if tc.tool == "bash" {
						rules, err := config.ParseBashApprovals(config.BashApprovalsAt(profile))
						if err != nil || len(rules) != 1 || rules[0].Match != scope {
							t.Fatalf("rules = %+v %v", rules, err)
						}
					} else if config.ToolApprovalsAt(profile) != "write:allow" {
						t.Fatalf("tool rules = %q", config.ToolApprovalsAt(profile))
					}
					backend.questionEvent(a, agent.ID, live, session.Event{Kind: session.EventQuestionWithdrawn, Question: &q})
					rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
						e, ok := data.(appwire.EntryAdded)
						return ok && e.Entry.Text == "Always allowed · "+scope+" · you · "+clock.Format("15:04") && e.Entry.Link != nil && e.Entry.Link.Label == "Settings › Permissions" && e.Entry.Link.Target == "settings:permissions"
					})
				})
			}
		})
	}
}

// The real app server and consent gate share the terminal's settings. The model
// is scripted and git's status/push calls are harmless fixture executables.
func TestAppScopeAcceptanceOnTheWire(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	model := consentSequenceSource(t, []string{"git status --short", "git status --porcelain", "git push origin main"})
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "config.json"), []byte(`{"tools.approvalMode":"prompt","tools.approval":"bash:prompt"}`), 0600); err != nil {
		t.Fatal(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(root, "fixture-bin")
	if err := os.MkdirAll(fakeBin, 0700); err != nil {
		t.Fatal(err)
	}
	// Only the two model-requested verbs are faked; repository discovery keeps
	// using the real git so the app is opened through its ordinary door.
	script := "#!/bin/sh\ncase \"$1\" in status) printf 'fixture status\\n'; exit 0;; push) exit 91;; esac\nexec " + strconv.Quote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	backend, w, rec := newScopeWire(t)
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	send := func() uint64 {
		raw, e := w.call("agents.send", map[string]any{"id": agent.ID, "text": "run the next probe"})
		if e != nil {
			t.Fatal(e)
		}
		var result struct {
			Turn uint64 `json:"turn"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		return result.Turn
	}
	done := func(turn uint64) {
		rec.waitFor(t, 30*time.Second, func(_ string, data any) bool { d, ok := data.(appwire.TurnDone); return ok && d.Turn == turn && d.OK })
		rec.waitFor(t, 30*time.Second, func(_ string, data any) bool {
			d, ok := data.(appwire.AgentChanged)
			return ok && d.Agent.State == appwire.StateIdle
		})
	}
	first := send()
	i := rec.waitFor(t, 30*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	ask := rec.data[i].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	if !reflect.DeepEqual(ask.Scopes, []string{"git status*", "git *", "git status --short"}) {
		t.Fatalf("scopes = %v", ask.Scopes)
	}
	_, e := w.call("asks.answer", map[string]any{"agent": agent.ID, "id": ask.ID, "option": "always", "scope": "git status*"})
	if e != nil {
		t.Fatal(e)
	}
	rec.waitFor(t, 30*time.Second, func(_ string, data any) bool {
		r, ok := data.(appwire.EntryAdded)
		return ok && strings.HasPrefix(r.Entry.Text, "Always allowed · git status* · you · ") && r.Entry.Link != nil && r.Entry.Link.Target == "settings:permissions"
	})
	done(first)
	second := send()
	rec.waitFor(t, 30*time.Second, func(_ string, data any) bool {
		r, ok := data.(appwire.EntryAdded)
		return ok && r.Entry.Turn == second && r.Entry.Text == "Allowed automatically · bash git status --porcelain · you always allow git status*" && r.Entry.Why == "Matches your rule git status* in Settings › Permissions"
	})
	done(second)
	third := send()
	j := rec.waitFor(t, 30*time.Second, func(_ string, data any) bool { a, ok := data.(appwire.AskOpened); return ok && a.Ask.ID != ask.ID })
	rec.mu.Lock()
	outside := rec.data[j].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	if outside.Outside != "git status*" {
		t.Fatalf("outside = %q, want git status*", outside.Outside)
	}
	if !reflect.DeepEqual(outside.Scopes, []string{"git push origin main*", "git *", "git push origin main"}) {
		t.Fatalf("outside scopes = %v", outside.Scopes)
	}
	_, e = w.call("asks.answer", map[string]any{"agent": agent.ID, "id": outside.ID, "option": "deny"})
	if e != nil {
		t.Fatal(e)
	}
	done(third)
	// A new backend has only the saved conversation, not the emitted entries.
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	_, reopened, _ := newScopeWire(t)
	raw, wireErr := reopened.call("agents.entries", map[string]any{"id": agent.ID})
	if wireErr != nil {
		t.Fatal(wireErr)
	}
	var history struct {
		Entries []appwire.Entry `json:"entries"`
	}
	if err := json.Unmarshal(raw, &history); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"Always allowed · ", "Allowed automatically · "} {
		t.Run("reload "+prefix, func(t *testing.T) {
			var emitted appwire.Entry
			rec.mu.Lock()
			for _, data := range rec.data {
				if added, ok := data.(appwire.EntryAdded); ok && strings.HasPrefix(added.Entry.Text, prefix) {
					emitted = added.Entry
				}
			}
			rec.mu.Unlock()
			for i, entry := range history.Entries {
				if entry.ID != emitted.ID {
					continue
				}
				if !reflect.DeepEqual(entry, emitted) {
					t.Fatalf("reloaded receipt = %+v, emitted = %+v", entry, emitted)
				}
				if i == 0 || i+1 == len(history.Entries) || history.Entries[i-1].Kind != appwire.EntryTool {
					t.Fatalf("receipt is not after its tool call: %+v", history.Entries)
				}
				return
			}
			t.Fatalf("receipt %q missing after reopen: %+v", emitted.Text, history.Entries)
		})
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, data := range rec.data[i+1 : j] {
		if a, ok := data.(appwire.AskOpened); ok {
			t.Fatalf("inside rule asked again: %+v", a)
		}
	}
}

func TestAppAlwaysAnswersSelectedBatchItemOnTheWire(t *testing.T) {
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	conv := &fakeConversation{}
	backend, _, agent := workBackend(t, conv)
	a, _ := backend.find(agent.ID)
	live := backend.liveOf(a)
	var asks []appwire.Ask
	for i, command := range []string{"git status", "git push"} {
		q := session.Question{ID: uint64(i + 1), Kind: session.QuestionConsent, Batch: "batch", Subject: session.SubjectRef{Kind: session.SubjectCall, Name: "bash"}, Options: session.AnswerOptions(session.QuestionConsent)}
		args, _ := json.Marshal(map[string]string{"command": command})
		backend.locked(func() { live.consents[q.ID] = appConsentCall{Tool: "bash", Args: string(args)} })
		backend.questionEvent(a, agent.ID, live, session.Event{Kind: session.EventQuestion, Question: &q})
	}
	backend.locked(func() { asks = append(asks, live.asks...) })
	w := newActionWire(t, backend)
	_, err := w.call("asks.answer", map[string]any{"agent": agent.ID, "id": asks[0].ID, "batch_item": asks[1].ID, "option": "always", "scope": "git push*"})
	if err != nil {
		t.Fatal(err)
	}
	rules, parseErr := config.ParseBashApprovals(config.BashApprovalsAt(config.ProfileDir()))
	if parseErr != nil || len(rules) != 1 || rules[0].Match != "git push*" {
		t.Fatalf("saved rules = %+v, %v", rules, parseErr)
	}
	conv.mu.Lock()
	defer conv.mu.Unlock()
	if !reflect.DeepEqual(conv.rememberedIDs, []uint64{2}) || len(conv.answers) != 0 {
		t.Fatalf("resolved consents = %v, questions = %+v", conv.rememberedIDs, conv.answers)
	}
	backend.locked(func() {
		if live.answered[asks[0].ID] != "" || live.savedScopes[asks[1].ID] != "git push*" {
			t.Errorf("answered = %v, saved scopes = %v", live.answered, live.savedScopes)
		}
	})
}

// newScopeWire decodes events from the same JSON stream that answers requests,
// so acceptance observes the desktop's actual receipt and ask payloads.
func newScopeWire(t *testing.T) (*appBackend, *actionWire, *appRecorder) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	backend := newAppBackend(server, appOpenInProcess)
	w := &actionWire{t: t, in: inW, waiting: map[int]chan actionFrame{}}
	rec := newAppRecorder()
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, backend)
		outW.Close()
	}()
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)
	if !scanner.Scan() {
		t.Fatal("no hello")
	}
	go func() {
		for scanner.Scan() {
			var frame struct {
				actionFrame
				Event string          `json:"event"`
				Data  json.RawMessage `json:"data"`
			}
			if json.Unmarshal(scanner.Bytes(), &frame) != nil {
				continue
			}
			if frame.Event != "" {
				shape := appwire.EventShape(frame.Event)
				if shape == nil {
					continue
				}
				value := reflect.New(shape)
				if json.Unmarshal(frame.Data, value.Interface()) == nil {
					rec.Emit(frame.Event, value.Elem().Interface())
				}
				continue
			}
			w.mu.Lock()
			ch := w.waiting[frame.ID]
			delete(w.waiting, frame.ID)
			w.mu.Unlock()
			if ch != nil {
				ch <- frame.actionFrame
			}
		}
	}()
	t.Cleanup(func() { backend.Close(); w.close() })
	return backend, w, rec
}

func TestAppOutsideIsPermissionOnlyAndOmittedWhenAbsent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    session.QuestionKind
		outside string
		want    bool
	}{
		{"permission", session.QuestionConsent, "git status*", true},
		{"no same-program rule", session.QuestionConsent, "", false},
		{"non-bash", session.QuestionConsent, "", false},
		{"other ask", session.QuestionKeep, "git status*", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ask := appAskOf("agent", session.Question{ID: 1, Kind: tc.kind, Outside: tc.outside})
			raw, err := json.Marshal(ask)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			outside, present := fields["outside"]
			if present != tc.want || present && outside != tc.outside {
				t.Fatalf("outside payload = %s", raw)
			}
		})
	}
}

// Inbox scope errors are synchronous; valid answers save only after the undo
// hold. Advancing the held timer avoids paying five seconds for every fixture.
func TestAppInboxPermissionScopesOnTheWire(t *testing.T) {
	for _, tc := range []struct {
		name, tool, command, scope string
		batch                      bool
	}{
		{"bash", "bash", "git status", "git status*", false},
		{"tool", "write", "", "write", false},
		{"batch item", "bash", "git push", "git push*", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := t.TempDir()
			t.Setenv(config.ProfileDirEnv, profile)
			conv := &fakeConversation{}
			backend, rec, agent := workBackend(t, conv)
			a, _ := backend.find(agent.ID)
			live := backend.liveOf(a)
			done := make(chan struct{})
			live.conn.Conv = &inboxScopeConversation{fakeConversation: conv, done: done}
			open := func(id uint64, tool, command, batch string) string {
				q := session.Question{ID: id, Kind: session.QuestionConsent, Batch: batch, Subject: session.SubjectRef{Kind: session.SubjectCall, Name: tool}, Options: session.AnswerOptions(session.QuestionConsent)}
				args, _ := json.Marshal(map[string]string{"command": command})
				backend.locked(func() { live.consents[id] = appConsentCall{Tool: tool, Args: string(args)} })
				backend.questionEvent(a, agent.ID, live, session.Event{Kind: session.EventQuestion, Question: &q})
				askID := "consent:" + strconv.FormatUint(id, 10)
				// A client can answer only after the asynchronous outbox has
				// delivered the Inbox item that contains the answer's id.
				rec.waitFor(t, 10*time.Second, func(event string, data any) bool {
					added, ok := data.(appwire.InboxAddedEvent)
					return event == appwire.EventInboxAdded && ok && added.Item.ID == appInboxID(agent.ID, askID)
				})
				return askID
			}
			batch := ""
			if tc.batch {
				batch = "batch"
				open(1, "bash", "git status", batch)
			}
			ask := open(2, tc.tool, tc.command, batch)
			id := appInboxID(agent.ID, ask)
			w := newActionWire(t, backend)
			for _, scope := range []string{"", "git status* foreign"} {
				params := map[string]any{"id": id, "option": "always"}
				if scope != "" {
					params["scope"] = scope
				}
				_, err := w.call("inbox.answer", params)
				wantCode(t, err, appwire.CodeBadScope)
				backend.inbox.locked(func() {
					if backend.inbox.held[id] != nil || backend.inbox.items[id] == nil {
						t.Error("bad scope held or removed the Inbox item")
					}
				})
			}
			// A sibling's valid scope is foreign to the selected batch item.
			if tc.batch {
				_, err := w.call("inbox.answer", map[string]any{"id": id, "option": "always", "scope": "git status*"})
				wantCode(t, err, appwire.CodeBadScope)
			}
			_, err := w.call("inbox.answer", map[string]any{"id": id, "option": "always", "scope": tc.scope})
			if err != nil {
				t.Fatal(err)
			}
			conv.mu.Lock()
			answered := len(conv.remembered)
			conv.mu.Unlock()
			if answered != 0 || config.BashApprovalsAt(profile) != "" || config.ToolApprovalsAt(profile) != "" {
				t.Fatal("an answer or rule escaped the undo hold")
			}
			backend.inbox.locked(func() { backend.inbox.held[id].timer.Reset(0) })
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("held answer did not reach the conversation")
			}
			if tc.tool == "bash" {
				rules, err := config.ParseBashApprovals(config.BashApprovalsAt(profile))
				if err != nil || len(rules) != 1 || rules[0].Match != tc.scope {
					t.Fatalf("saved rules = %+v, %v", rules, err)
				}
			} else if config.ToolApprovalsAt(profile) != "write:allow" {
				t.Fatalf("tool rules = %q", config.ToolApprovalsAt(profile))
			}
			conv.mu.Lock()
			defer conv.mu.Unlock()
			if !reflect.DeepEqual(conv.rememberedIDs, []uint64{2}) || len(conv.answers) != 0 {
				t.Fatalf("resolved consents = %v, questions = %+v", conv.rememberedIDs, conv.answers)
			}
			backend.locked(func() {
				if live.savedScopes[ask] != tc.scope || tc.batch && live.answered["consent:1"] != "" {
					t.Errorf("answered = %v, saved scopes = %v", live.answered, live.savedScopes)
				}
			})
		})
	}
}

// inboxScopeConversation signals completion after the real fake has recorded
// the saved-rule answer, so the test need not poll the conversation or disk.
type inboxScopeConversation struct {
	*fakeConversation
	done chan struct{}
}

func (c *inboxScopeConversation) ResolveConsentRemember(id uint64, allow bool, scope session.ConsentScope) {
	c.fakeConversation.ResolveConsentRemember(id, allow, scope)
	close(c.done)
}
