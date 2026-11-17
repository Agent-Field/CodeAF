package main

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/skillpick"
	"github.com/Agent-Field/codeaf/internal/skills"
	"github.com/Agent-Field/codeaf/internal/store"
)

// appSkillDoor is the same shelf and attachment the terminal picker asks of
// its conversation. Remote handles forward these doors to the session host.
type appSkillDoor interface {
	SkillFacts(string, int) ([]store.Fact, error)
	AttachSkills(...string) []string
	DetachSkill(string) bool
	AttachedSkills() []string
}

func (b *appBackend) skillConversation(agent string) (*appLive, appSkillDoor, func(), error) {
	_, live, endLive, err := b.borrowLive(agent)
	if err != nil {
		return nil, nil, nil, err
	}
	door, ok := live.conn.Conv.(appSkillDoor)
	if far, asks := live.conn.Conv.(interface{ SkillsSupported() bool }); asks && !far.SkillsSupported() {
		ok = false
	}
	if !ok {
		endLive()
		return nil, nil, nil, appwire.Errorf(appwire.CodeUnknownMethod, "this conversation cannot carry attached skills")
	}
	return live, door, endLive, nil
}

// ListSkills uses the terminal's merge and order. The shelf is always read
// through the conversation; discovery only adds folders on this machine.
func (b *appBackend) ListSkills(agent string) ([]skillpick.Row, error) {
	release, ok := b.actions.reading()
	if !ok {
		return nil, appActionReadsBusy()
	}
	defer release()
	live, door, endLive, err := b.skillConversation(agent)
	if err != nil {
		return nil, err
	}
	defer endLive()
	facts, readErr := door.SkillFacts(store.FactActive, skillpick.ListLimit)
	if readErr != nil {
		return nil, appEngineError(readErr)
	}
	entries := make([]skillpick.Entry, 0, len(facts))
	for _, fact := range facts {
		entries = append(entries, skillpick.Entry{Name: fact.SkillName(), Description: strings.TrimSpace(fact.Body), Artifact: fact.Artifact})
	}
	var disk []skills.Skill
	// Discovery follows the same optional door as the terminal, so the whole
	// far shelf is visible even when more than ListLimit folders are installed.
	if far, ok := live.conn.Conv.(interface {
		SkillFolders() ([]skills.Skill, error)
	}); ok {
		if capability, ok := live.conn.Conv.(interface{ SkillFoldersSupported() bool }); !ok || capability.SkillFoldersSupported() {
			disk, err = far.SkillFolders()
		}
	} else if _, far := live.conn.Conv.(interface{ SkillsSupported() bool }); !far {
		homeDir, _ := home.Login()
		disk, err = skills.Discover(skills.Options{ProjectDir: live.conn.Workspace, HomeDir: homeDir, OmitSize: true})
	}
	if err != nil {
		return nil, appEngineError(err)
	}
	return skillpick.Rows(door.AttachedSkills(), entries, disk, true), nil
}

// SetSkill calls the terminal's attachment doors and checks the engine's
// answer, since an older remote handle may return its previous replica.
func (b *appBackend) SetSkill(p appwire.SkillSetParams) (appwire.SkillState, error) {
	_, door, endLive, err := b.skillConversation(p.Agent)
	if err != nil {
		return appwire.SkillState{}, err
	}
	defer endLive()
	if p.On {
		door.AttachSkills(p.Name)
	} else {
		door.DetachSkill(p.Name)
	}
	names := door.AttachedSkills()
	state := appwire.SkillState{Name: p.Name}
	for _, name := range names {
		if strings.EqualFold(name, p.Name) {
			state.Name, state.On = name, true
			break
		}
	}
	if state.On != p.On {
		return state, appwire.Errorf(appwire.CodeEngine, "the conversation did not take the skill change; read its shelf again")
	}
	if names == nil {
		names = []string{}
	}
	b.emit.Emit(appwire.EventSkillsChanged, appwire.SkillsChanged{Agent: p.Agent, Names: names})
	return state, nil
}
