package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── STARTING A TEAM WITH ITS OWN MANAGER, AND THE GLOBAL MANAGER ────────────
//
// crews.create's start_manager and crews.root.start (internal/appwire/crewstart.go
// is the shape). This file is the engine's whole policy for them and it is
// glue: the team is made by [appBackend.newTeam], the root and its seating by
// internal/teams (MakeRoot, SetManager, tidyRoot), the manager conversation and
// its first message by [appBackend.createAgentHeld]. Nothing here ranks, routes,
// writes words for the person or falls back to another model.
//
// THE ORDER IS THE ROLLBACK. Everything that can be refused is checked before
// anything is made; one folder is minted; ONE teams write commits (and
// re-validates under the file's own lock); only then does the conversation take
// the folder and the first turn start. A refusal before the commit takes the
// folder back, so a refused start leaves no team, no agent and no message, and
// the goal can reach a model only after the team it belongs to exists.
//
// ONE START RUNS AT A TIME. A second concurrent start is refused busy rather
// than queued: no waiting line, no goroutine per caller. An outcome the caller
// could not see is theirs to resolve with the same request_id, which is only
// ever a lookup once the crew is recorded — nothing here replays on its own.

// appStartRecord is what the crew layer keeps of the request that made a crew.
type appStartRecord struct {
	RequestID   string `json:"request_id"`
	Fingerprint string `json:"fingerprint"`
	Manager     string `json:"manager"`
	ClientID    string `json:"client_id,omitempty"`
	Goal        bool   `json:"goal,omitempty"`
}

var (
	_ appwire.CrewStarter = (*appBackend)(nil)
	_ appwire.RootStarter = (*appBackend)(nil)
)

// CrewStart is crews.create with start_manager.
func (b *appBackend) CrewStart(ask appwire.CrewCreate) (appwire.CrewStartResult, error) {
	if err := ask.CheckStart(); err != nil {
		return appwire.CrewStartResult{}, err
	}
	ask.Name, ask.Goal = strings.TrimSpace(ask.Name), strings.TrimSpace(ask.Goal)
	print := appStartFingerprint(ask)
	if got, done, err := b.startReplay(ask.RequestID, print); done || err != nil {
		return got, err
	}
	if !b.startMu.TryLock() {
		return appwire.CrewStartResult{}, appwire.Errorf(appwire.CodeBusy, "a team is already being started; try again in a moment")
	}
	defer b.startMu.Unlock()
	// Another call with this request may have finished while this one came in.
	if got, done, err := b.startReplay(ask.RequestID, print); done || err != nil {
		return got, err
	}
	if b.isClosing() {
		return appwire.CrewStartResult{}, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	workspace, err := b.startWorkspace(ask.Workspace)
	if err != nil {
		return appwire.CrewStartResult{}, err
	}
	// Attachments are read whole before anything is made, as agents.create does.
	for _, source := range ask.Attachments {
		if _, err := b.prepareAttach(workspace, nil, appwire.Attachment{
			Kind: source.Kind, Path: strings.TrimSpace(source.Path), URL: strings.TrimSpace(source.URL),
		}, source.Text, true); err != nil {
			return appwire.CrewStartResult{}, err
		}
	}
	hue, err := b.askedHue(ask.Hue, ask.Tint)
	if err != nil {
		return appwire.CrewStartResult{}, err
	}
	members := make([]teamstore.Member, 0, len(ask.Agents)+1)
	for _, agent := range ask.Agents {
		m, err := b.agentMember(agent)
		if err != nil {
			return appwire.CrewStartResult{}, err
		}
		members = append(members, m)
	}
	transcript, err := b.mint(workspace)
	if err != nil {
		return appwire.CrewStartResult{}, appEngineError(err)
	}
	manager := b.startMember(transcript, workspace)
	id, err := b.newTeam(ask.Name, ask.Parent, append(members, manager), manager.Key, hue, ask.Settings, ask.CapUSDDay)
	if err != nil {
		b.takeBack(transcript)
		return appwire.CrewStartResult{}, err
	}
	b.crewExtraOnCreate(id, "", ask.Goal, ask.EndsAt)
	// FROM HERE THE TEAM EXISTS: a failure is reported as that, never as nothing.
	words := ask.Goal != "" || len(ask.Attachments) > 0
	agent, launch, err := b.createAgentHeld(appwire.CreateAsk{
		Workspace: workspace, Model: ask.Model, Text: ask.Goal, Said: ask.Said,
		ClientID: ask.ClientID, Attachments: ask.Attachments,
	}, transcript)
	if err != nil {
		return appwire.CrewStartResult{}, appStartedButNot(ask.Name, err)
	}
	record := &appStartRecord{RequestID: ask.RequestID, Fingerprint: print, Manager: agent.ID, ClientID: ask.ClientID, Goal: words}
	// The record is written before the first turn starts: if it is lost the
	// worst outcome is a goal not sent, never one sent twice.
	_ = b.crewLayer().write(func(f *appCrewLayerFile) error {
		f.crew(id).Start = record
		return nil
	})
	b.crewReceipt(agent.ID, id, ask.Name)
	launch()
	crew, err := b.CrewGet(id)
	if err != nil {
		return appwire.CrewStartResult{}, err
	}
	state := appwire.GoalNone
	if words {
		state = appwire.GoalStarting
	}
	return appwire.CrewStartResult{Crew: crew, Start: appwire.CrewStarted{
		RequestID: ask.RequestID, Manager: agent.ID, Goal: appwire.CrewGoal{State: state, ClientID: ask.ClientID},
	}}, nil
}

// startReplay answers a request this engine already accepted: the recorded crew
// and nothing executed. done is false when the request is new. The same id with
// different words is refused rather than answered with another team.
func (b *appBackend) startReplay(request, fingerprint string) (appwire.CrewStartResult, bool, error) {
	var found *appStartRecord
	var crewID string
	_ = b.crewLayer().read(func(f *appCrewLayerFile) {
		for id, extra := range f.Crews {
			if extra != nil && extra.Start != nil && extra.Start.RequestID == request {
				found, crewID = extra.Start, id
				return
			}
		}
	})
	if found == nil {
		return appwire.CrewStartResult{}, false, nil
	}
	crew, err := b.CrewGet(crewID)
	if err != nil {
		// The crew is gone and takes its key with it.
		return appwire.CrewStartResult{}, false, nil
	}
	if found.Fingerprint != fingerprint {
		return appwire.CrewStartResult{}, true, appwire.Errorf(appwire.CodeBadParams, "request_id %s was used for a different team", request)
	}
	state := appwire.GoalNone
	if found.Goal {
		state = appwire.GoalEarlier
	}
	return appwire.CrewStartResult{Crew: crew, Start: appwire.CrewStarted{
		RequestID: request, Replayed: true, Manager: found.Manager, Goal: appwire.CrewGoal{State: state, ClientID: found.ClientID},
	}}, true, nil
}

// appStartFingerprint is what makes two requests the same one: the whole ask,
// normalised, without its key.
func appStartFingerprint(ask appwire.CrewCreate) string {
	ask.RequestID = ""
	raw, _ := json.Marshal(ask)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// appStartedButNot says a team exists whose manager could not be started, in
// the code the cause had.
func appStartedButNot(name string, cause error) error {
	words := cause.Error()
	code := appwire.CodeEngine
	var wire *appwire.Error
	if errors.As(cause, &wire) {
		words, code = wire.Message, wire.Code
	}
	return appwire.Errorf(code, "the crew %s was made, but its manager's conversation could not be started: %s", name, words)
}

// RootStart is crews.root.start: the person's explicit act that makes the root
// ("All teams") and its manager. Everything already true is returned as it is.
func (b *appBackend) RootStart(ask appwire.RootStartAsk) (appwire.RootStartResult, error) {
	if !b.startMu.TryLock() {
		return appwire.RootStartResult{}, appwire.Errorf(appwire.CodeBusy, "a team is already being started; try again in a moment")
	}
	defer b.startMu.Unlock()
	if b.isClosing() {
		return appwire.RootStartResult{}, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	if existing, ok, err := b.rootHeld(); err != nil || ok {
		return existing, err
	}
	workspace, err := b.startWorkspace(ask.Workspace)
	if err != nil {
		return appwire.RootStartResult{}, err
	}
	transcript, err := b.mint(workspace)
	if err != nil {
		return appwire.RootStartResult{}, appEngineError(err)
	}
	manager := b.startMember(transcript, workspace)
	var rootID string
	var rootMade, taken bool
	err = b.crewEdit(func(f *teamstore.File) error {
		rootID, rootMade, taken = "", false, false
		if r, ok := f.Root(); ok {
			if r.Manager != "" && appConversationHere(r, r.Manager) {
				// Another start won while this one minted: it is the answer.
				rootID, taken = r.ID, true
				return nil
			}
			rootID = r.ID
		} else {
			rootID, rootMade = f.MakeRoot(b.now()), true
		}
		if err := f.AddMember(rootID, manager); err != nil {
			return err
		}
		return f.SetManager(rootID, manager.Key)
	})
	if err != nil {
		b.takeBack(transcript)
		return appwire.RootStartResult{}, err
	}
	if taken {
		b.takeBack(transcript)
		existing, _, err := b.rootHeld()
		return existing, err
	}
	agent, launch, err := b.createAgentHeld(appwire.CreateAsk{Workspace: workspace, Model: ask.Model}, transcript)
	if err != nil {
		return appwire.RootStartResult{}, appStartedButNot(teamstore.RootName, err)
	}
	launch()
	crew, err := b.CrewGet(rootID)
	if err != nil {
		return appwire.RootStartResult{}, err
	}
	return appwire.RootStartResult{Crew: crew, Manager: agent.ID, RootCreated: rootMade, ManagerCreated: true}, nil
}

// rootHeld is the root when it already has a manager whose conversation is
// still there: the answer to a start that has nothing left to make.
func (b *appBackend) rootHeld() (appwire.RootStartResult, bool, error) {
	file, err := teamstore.Load(b.crewProfile())
	if err != nil {
		return appwire.RootStartResult{}, false, appEngineError(err)
	}
	root, ok := file.Root()
	if !ok || root.Manager == "" || !appConversationHere(root, root.Manager) {
		return appwire.RootStartResult{}, false, nil
	}
	m, _ := root.Member(root.Manager)
	crew, err := b.CrewGet(root.ID)
	if err != nil {
		return appwire.RootStartResult{}, false, err
	}
	return appwire.RootStartResult{Crew: crew, Manager: appMemberAgent(m)}, true, nil
}

// appConversationHere says the conversation a team member names still has its
// folder. A conversation that has not had its first turn has no transcript yet,
// only its folder, so the folder is what is looked for.
func appConversationHere(t teamstore.Team, key string) bool {
	m, ok := t.Member(key)
	if !ok {
		return false
	}
	for _, path := range []string{m.File, m.Key} {
		if path == "" {
			continue
		}
		if _, err := os.Stat(filepath.Dir(path)); err == nil {
			return true
		}
	}
	return false
}

// startWorkspace is the folder a manager conversation is made in: the one asked
// for, or the person's work folder, as agents.create resolves it.
func (b *appBackend) startWorkspace(asked string) (string, error) {
	if strings.TrimSpace(asked) == "" {
		_ = b.state.read(func(f *appStateFile) { asked = f.workFolder() })
	}
	resolved, err := engineWorkspace(asked)
	if err != nil {
		return "", appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	}
	return resolved, nil
}

// startMember is a minted conversation as a team member, under the key the
// teams file will know it by once it has a transcript: its folder resolved,
// since the file itself does not exist yet.
func (b *appBackend) startMember(transcript, where string) teamstore.Member {
	key := filepath.Clean(transcript)
	if real, err := filepath.EvalSymlinks(filepath.Dir(key)); err == nil {
		key = filepath.Join(filepath.Clean(real), filepath.Base(key))
	}
	return teamstore.Member{Key: key, File: transcript, Where: where, JoinedAt: b.now()}
}

// takeBack removes a folder this call minted and nothing has used.
func (b *appBackend) takeBack(transcript string) {
	if b.unmint != nil {
		b.unmint(transcript)
		return
	}
	appUnmint(session.PlacesRoot(), transcript)
}

// appUnmint removes a minted conversation folder, and only that: it must sit
// under root and hold nothing but the lookup file mint wrote. A folder with
// anything else in it is somebody's, and stays.
func appUnmint(root, transcript string) {
	dir := filepath.Dir(filepath.Clean(transcript))
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() != "meta.json" {
			return
		}
	}
	_ = os.RemoveAll(dir)
}
