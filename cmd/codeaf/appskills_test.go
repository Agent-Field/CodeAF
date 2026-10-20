package main

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/skills"
	"github.com/Agent-Field/codeaf/internal/store"
)

// appSkillConv embeds a real agent's skill doors beside the scripted turn
// stream, so this test exercises the same attachment state the terminal uses.
type appSkillConv struct {
	*fakeConversation
	skills *session.Agent
}

func (c *appSkillConv) AttachSkills(names ...string) []string { return c.skills.AttachSkills(names...) }
func (c *appSkillConv) DetachSkill(name string) bool          { return c.skills.DetachSkill(name) }
func (c *appSkillConv) AttachedSkills() []string              { return c.skills.AttachedSkills() }
func (c *appSkillConv) SkillFacts(string, int) ([]store.Fact, error) {
	return []store.Fact{{Artifact: "/far/.codex/skills/reviewer", Body: "Review changes", Kind: store.FactSkill, Status: store.FactActive}}, nil
}

// SkillsSupported also proves local discovery cannot leak into a hosted list.
func (c *appSkillConv) SkillsSupported() bool { return true }
func (c *appSkillConv) SkillFolders() ([]skills.Skill, error) {
	return []skills.Skill{{Name: "host-folder", Description: "Not in the active window", Root: ".claude/skills", Scope: skills.ScopeProject}}, nil
}

func TestAppSkillsShareAttachmentDoorsAndEmitState(t *testing.T) {
	conv := &appSkillConv{fakeConversation: &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}, skills: &session.Agent{}}
	recorder := newAppRecorder()
	backend := newAppBackend(recorder, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := backend.ListSkills(agent.ID)
	if err != nil || len(rows) != 2 || rows[0].Source != ".claude/skills" || rows[0].On || rows[1].Source != ".codex/skills" {
		t.Fatalf("shelf: %+v, %v", rows, err)
	}
	for _, name := range []string{"reviewer", "unknown", "REVIEWER"} {
		got, err := backend.SetSkill(appwire.SkillSetParams{Agent: agent.ID, Name: name, On: true})
		if err != nil || !got.On {
			t.Fatalf("attach %s: %+v %v", name, got, err)
		}
	}
	if got := conv.AttachedSkills(); !reflect.DeepEqual(got, []string{"reviewer", "unknown"}) {
		t.Fatalf("order: %v", got)
	}
	rows, err = backend.ListSkills(agent.ID)
	if err != nil || len(rows) != 3 || !rows[0].On || rows[0].Description != "Review changes" {
		t.Fatalf("attached shelf: %+v %v", rows, err)
	}
	got, err := backend.SetSkill(appwire.SkillSetParams{Agent: agent.ID, Name: "REVIEWER", On: false})
	if err != nil || got.On {
		t.Fatalf("detach: %+v %v", got, err)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	var last appwire.SkillsChanged
	count := 0
	for i, event := range recorder.events {
		if event == appwire.EventSkillsChanged {
			count++
			last = recorder.data[i].(appwire.SkillsChanged)
		}
	}
	if count != 4 || last.Agent != agent.ID || !reflect.DeepEqual(last.Names, []string{"unknown"}) {
		t.Fatalf("events: %d %+v", count, last)
	}
}
