package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// L-FIX9-cross A7
func TestAppScopeWritesAClosedConversationWithoutOpeningIt(t *testing.T) {
	d := newUnreadDisk(t)
	if err := d.backend.state.write(func(f *appStateFile) error {
		f.Closed = map[string]time.Time{d.id: time.Now()}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(d.dir, "transcript.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	w := newActionWire(t, d.backend)
	if !slices.Contains(w.hello, appwire.FeatureAgentScope) {
		t.Fatal("the backend does not announce agents.scope")
	}
	for _, word := range []string{session.WorkScopeConversation, session.WorkScopeProject} {
		raw, failure := w.call("agents.set_scope", map[string]string{"id": d.id, "scope": word})
		if failure != nil {
			t.Fatal(failure)
		}
		var result appwire.AgentScope
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if result != (appwire.AgentScope{ID: d.id, Scope: word}) || session.ConversationScopeAt(d.dir) != word {
			t.Fatalf("set %q = %+v, stored %q", word, result, session.ConversationScopeAt(d.dir))
		}
	}
	d.mu.Lock()
	opens := d.opens
	d.mu.Unlock()
	if opens != 0 || d.backend.held(d.id) || !d.backend.tidied(d.id) {
		t.Fatalf("scope opened or reopened the conversation: opens %d, held %v, closed %v", opens, d.backend.held(d.id), d.backend.tidied(d.id))
	}
	after, err := os.ReadFile(filepath.Join(d.dir, "transcript.jsonl"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("scope changed the transcript: %q, %v", after, err)
	}
	d.rec.mu.Lock()
	defer d.rec.mu.Unlock()
	if slices.Contains(d.rec.events, appwire.EventAgentChanged) {
		t.Fatalf("scope emitted agent.changed: %v", d.rec.events)
	}
}

// L-FIX9-cross A7
func TestAppScopeRefusesAnUnknownAgent(t *testing.T) {
	d := newUnreadDisk(t)
	w := newActionWire(t, d.backend)
	if _, err := w.call("agents.set_scope", map[string]string{"id": "no-such-agent", "scope": session.WorkScopeConversation}); err == nil || err.Code != appwire.CodeNotFound {
		t.Fatalf("unknown agent = %v", err)
	}
}

// L-FIX9-cross A7
func TestAppScopeRefusesALentConversationBeforeWriting(t *testing.T) {
	backend, _, agent, dir := liveUnread(t, &fakeConversation{})
	a, err := backend.seenTarget(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	backend.locked(func() { a.live.conn.Lent = true })
	path := filepath.Join(dir, "meta.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.SetAgentScope(agent.ID, session.WorkScopeConversation)
	failure, ok := err.(*appwire.Error)
	want := errLentStage("scope").(*appwire.Error)
	if !ok || !reflect.DeepEqual(failure, want) {
		t.Fatalf("lent scope change = %v, want %v", err, want)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("refused scope changed metadata: %q, %v", after, err)
	}
}

// L-FIX9-cross A7
func TestAppScopeLeavesAHeldConversationAndItsUseTimeAlone(t *testing.T) {
	conv := &fakeConversation{}
	backend, _, agent, dir := liveUnread(t, conv)
	a, err := backend.seenTarget(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	var before *appLive
	var used time.Time
	var view appwire.Agent
	backend.locked(func() { before, used, view = a.live, a.live.lastUse, a.view })
	result, err := backend.SetAgentScope(agent.ID, session.WorkScopeConversation)
	if err != nil || result.Scope != session.WorkScopeConversation || session.ConversationScopeAt(dir) != session.WorkScopeConversation {
		t.Fatalf("held scope = %+v, %v", result, err)
	}
	backend.locked(func() {
		if a.live != before || !a.live.lastUse.Equal(used) || !reflect.DeepEqual(a.view, view) {
			t.Errorf("scope changed the held conversation or warmed it")
		}
	})
	conv.mu.Lock()
	defer conv.mu.Unlock()
	if conv.submits != 0 || conv.interrupted != 0 {
		t.Fatalf("scope drove the conversation: submits %d, interruptions %d", conv.submits, conv.interrupted)
	}
}

// L-FIX9-cross A7
func TestAppScopeRequiresALocalConversationFolder(t *testing.T) {
	for _, where := range []string{"no transcript", "missing folder", "missing metadata"} {
		t.Run(where, func(t *testing.T) {
			d := newUnreadDisk(t)
			a, err := d.backend.seenTarget(d.id)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			transcript := ""
			if where == "missing folder" {
				dir = filepath.Join(dir, "absent")
				transcript = filepath.Join(dir, "transcript.jsonl")
			} else if where == "missing metadata" {
				transcript = filepath.Join(dir, "transcript.jsonl")
			}
			d.backend.locked(func() { a.transcript = transcript })
			_, err = d.backend.SetAgentScope(d.id, session.WorkScopeConversation)
			failure, ok := err.(*appwire.Error)
			if !ok || failure.Code != appwire.CodeConflict || failure.Message != "this conversation has no local folder on this computer" {
				t.Fatalf("%s = %v", where, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "meta.json")); !os.IsNotExist(err) {
				t.Fatalf("scope created a conversation: %v", err)
			}
		})
	}
}
