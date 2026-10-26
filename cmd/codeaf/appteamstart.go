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
// glue: the team is made by [appBackend.newTeamAs], the root and its seating by
// internal/teams (MakeRoot, RemoveMember, SetManager, tidyRoot), the manager
// conversation and its first message by [appBackend.createAgentHeld]. Nothing
// here ranks, routes, writes words for the person or falls back to another
// model.
//
// THE ORDER IS THE ROLLBACK, AND THE CLAIM IS THE MEMORY. Everything that can
// be refused is checked, and every attachment read, before anything is made;
// one folder is minted; the request is CLAIMED on disk (appteamstartclaim.go)
// under the crew id the commit will use; ONE teams write commits (and
// re-validates under the file's own lock); the claim is rewritten to say the
// goal may be handed over; only then does the conversation take the folder and
// the first turn start. A refusal before the commit takes the folder and the
// claim back, so a refused start leaves no team, no agent and no message, and
// the goal can reach a model only after its team AND the durable intent of its
// request exist.
//
// A FAILED READ IS NEVER "NEW". A repeat whose claim, teams file or crew cannot
// be read is answered uncertain and starts nothing; so is a claim whose start
// has not finished. A write that failed is looked for under the id it would
// have written before it is called refused.
//
// ONE START RUNS AT A TIME in this process. A second concurrent start is
// refused busy rather than queued: no waiting line, no goroutine per caller.
// Across processes the claim's exclusive link and the teams file's lock are the
// authority. An outcome the caller could not see is theirs to resolve with the
// same request_id, which is only ever a lookup once claimed — nothing here
// replays on its own.

// appStartRecord is what the first, unreleased cut of this door kept on the
// crew layer for the request that made a crew. It is READ, never written: a
// request it names is never taken for a new one, and what it cannot say — whether
// its goal was accepted — stays unknown.
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
	// The manager's first message is prepared here, ONCE: its attachments are
	// read whole before anything is made, and the reading is what is held.
	prep, err := b.prepareCreate(appwire.CreateAsk{
		Workspace: workspace, Model: ask.Model, Text: ask.Goal, Said: ask.Said,
		ClientID: ask.ClientID, Attachments: ask.Attachments,
	})
	if err != nil {
		return appwire.CrewStartResult{}, err
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
	words := prep.text != "" || len(prep.kept) > 0
	transcript, err := b.mint(workspace)
	if err != nil {
		return appwire.CrewStartResult{}, appEngineError(err)
	}
	manager := b.startMember(transcript, workspace)
	claim := appStartClaim{
		V: appStartClaimV, RequestID: ask.RequestID, Fingerprint: print, State: appClaimIntent,
		Crew: teamstore.NewID(), Manager: appAgentID(transcript), ManagerKey: manager.Key,
		ClientID: ask.ClientID, Goal: words,
	}
	claims := b.startClaims()
	made, err := claims.create(claim)
	if made {
		// From the claim on, an intent claim of this request is this start.
		b.locked(func() { b.starting = ask.RequestID })
		defer b.locked(func() { b.starting = "" })
	}
	if err != nil || !made {
		b.takeBack(transcript)
		if err != nil {
			return appwire.CrewStartResult{}, appwire.Errorf(appwire.CodeEngine, "the start could not be recorded, so nothing was made: %s", plainWords(err.Error()))
		}
		// Another start, here or in another process, holds this request.
		if got, done, err := b.startReplay(ask.RequestID, print); done || err != nil {
			return got, err
		}
		return appwire.CrewStartResult{}, appwire.Errorf(appwire.CodeBusy, "this request is being started; try again in a moment")
	}
	if err := b.newTeamAs(claim.Crew, ask.Name, ask.Parent, append(members, manager), manager.Key, hue, ask.Settings, ask.CapUSDDay); err != nil {
		// A WRITE THAT FAILED MAY HAVE LANDED: it is looked for by its own id.
		switch committed, lookErr := b.teamCommitted(claim.Crew); {
		case lookErr != nil:
			// The claim and the folder stay: a repeat of this request is
			// answered from them, and is never a second team.
			return appwire.CrewStartResult{}, appwire.Errorf(appwire.CodeUncertain,
				"the crew %s may have been made; ask again with the same request_id", ask.Name)
		case !committed:
			claims.drop(ask.RequestID)
			b.takeBack(transcript)
			return appwire.CrewStartResult{}, err
		}
	}
	b.crewExtraOnCreate(claim.Crew, "", ask.Goal, ask.EndsAt, claim)
	// FROM HERE THE TEAM EXISTS: a failure is reported as that, never as
	// nothing, and the goal is handed over only once the claim says it may be.
	if words {
		if b.isClosing() {
			return b.startNotSent(claims, claim, prep, transcript, "codeaf is shutting down")
		}
		commit := appStartClaims.commit
		if b.commitStart != nil {
			commit = b.commitStart
		}
		if err := commit(claims, claim); err != nil {
			return b.startNotSent(claims, claim, prep, transcript, "the start could not be recorded: "+plainWords(err.Error()))
		}
	}
	// The goal is the person's message to the crew being started: it states
	// that crew's exact id, and the engine proves it against the manager's
	// membership when the message is accepted.
	prep.creation = appInTeam(claim.Crew)
	agent, launch, err := b.createAgentHeld(prep, transcript)
	if err != nil {
		// The claim may say committed here, and a repeat then says unknown;
		// this answer knows better, because launch was never called.
		return b.startNotSent(claims, claim, prep, transcript, appStartedWords(err))
	}
	b.crewReceipt(agent.ID, claim.Crew, ask.Name)
	launch()
	state := appwire.GoalNone
	if words {
		state = appwire.GoalStarting
	}
	return b.startAnswer(claim, false, appwire.CrewGoal{State: state, ClientID: ask.ClientID})
}

// startNotSent answers a team that was made and a goal that was provably never
// handed to its manager. The claim is rewritten withheld when it still can be
// (best-effort: one left at intent is answered unknown, never not_sent, by a
// repeat). The manager is held as an ordinary idle conversation, opened with
// no words, so the person can send it the goal themselves.
func (b *appBackend) startNotSent(claims appStartClaims, claim appStartClaim, prep appCreatePrep, transcript, why string) (appwire.CrewStartResult, error) {
	if current, found, err := claims.read(claim.RequestID); err == nil && found && current.State == appClaimIntent {
		_ = claims.withhold(claim)
	}
	if !b.isClosing() {
		bare := appCreatePrep{ask: appwire.CreateAsk{Workspace: prep.ask.Workspace, Model: prep.ask.Model}, resolved: prep.resolved}
		if _, launch, err := b.createAgentHeld(bare, transcript); err == nil {
			launch()
		}
	}
	return b.startAnswer(claim, false, appwire.CrewGoal{State: appwire.GoalNotSent, ClientID: claim.ClientID, Error: why})
}

// startAnswer is a started (or repeated) request's reply: its crew read back.
// A crew that cannot be read back is said to be uncertain, never absent.
func (b *appBackend) startAnswer(claim appStartClaim, replayed bool, goal appwire.CrewGoal) (appwire.CrewStartResult, error) {
	crew, err := b.CrewGet(claim.Crew)
	if err != nil {
		return appwire.CrewStartResult{}, appwire.Errorf(appwire.CodeUncertain,
			"the crew request_id %s made cannot be read back; ask again with the same request_id", claim.RequestID)
	}
	return appwire.CrewStartResult{Crew: crew, Start: appwire.CrewStarted{
		RequestID: claim.RequestID, Replayed: replayed, Manager: claim.Manager, Goal: goal,
	}}, nil
}

// teamCommitted says whether a team with id is in the teams file, and errors
// when the file cannot be read: a failed read is not an absent team.
func (b *appBackend) teamCommitted(id string) (bool, error) {
	file, err := teamstore.Load(b.crewProfile())
	if err != nil {
		return false, err
	}
	_, ok := file.Team(id)
	return ok, nil
}

// startReplay answers a request already claimed: the crew it made and nothing
// executed. done is false ONLY when the request is provably new — no claim on
// disk and no earlier record names it. Everything that cannot be read, and
// every record that cannot be resolved to one answer, is refused uncertain.
func (b *appBackend) startReplay(request, fingerprint string) (appwire.CrewStartResult, bool, error) {
	claim, found, err := b.startClaims().read(request)
	if err != nil {
		return appwire.CrewStartResult{}, true, appStartUnknown(request, err)
	}
	if !found {
		return b.startLegacy(request, fingerprint)
	}
	if claim.Fingerprint != fingerprint {
		return appwire.CrewStartResult{}, true, appwire.Errorf(appwire.CodeBadParams, "request_id %s was used for a different team", request)
	}
	var mine bool
	b.locked(func() { mine = b.starting == request })
	if mine && claim.State == appClaimIntent {
		// This process is starting it right now.
		return appwire.CrewStartResult{}, true, appwire.Errorf(appwire.CodeBusy, "this request is being started; try again in a moment")
	}
	here, err := b.teamCommitted(claim.Crew)
	switch {
	case err != nil:
		return appwire.CrewStartResult{}, true, appStartUnknown(request, err)
	case !here && claim.State == appClaimCommitted:
		return appwire.CrewStartResult{}, true, appStartGone(request)
	case !here && claim.State == appClaimWithheld:
		return appwire.CrewStartResult{}, true, appwire.Errorf(appwire.CodeBadParams,
			"request_id %s started a crew that has since been deleted; its goal was never sent. Start another with a new request_id", request)
	case !here:
		// Claimed and never committed — or committing right now, in another
		// process. Nothing was handed over: the hand-over waits for the commit.
		return appwire.CrewStartResult{}, true, appwire.Errorf(appwire.CodeUncertain,
			"an earlier start with request_id %s has not finished, or stopped before its crew was made; nothing was sent for it. Look at the crews before starting it again with a new request_id", request)
	}
	goal := appwire.CrewGoal{State: appwire.GoalNone, ClientID: claim.ClientID}
	if claim.Goal {
		// ONLY WITHHELD PROVES THE GOAL UNSENT. An intent claim whose crew
		// exists may belong to a start, in another process, that is about to
		// hand it over; it is unknown, like a committed one.
		goal.State = appwire.GoalUnknown
		if claim.State == appClaimWithheld {
			goal.State = appwire.GoalNotSent
		}
	}
	got, err := b.startAnswer(claim, true, goal)
	return got, true, err
}

// startLegacy answers a request the unreleased first cut recorded on the crew
// layer. One record resolved to a live crew is a repeat whose goal is unknown;
// two records, a layer that cannot be read, or a crew that cannot be read are
// refused; no record at all is the only "new".
func (b *appBackend) startLegacy(request, fingerprint string) (appwire.CrewStartResult, bool, error) {
	var found []appStartRecord
	var crews []string
	err := b.crewLayer().read(func(f *appCrewLayerFile) {
		for id, extra := range f.Crews {
			if extra != nil && extra.Start != nil && extra.Start.RequestID == request {
				found, crews = append(found, *extra.Start), append(crews, id)
				if len(found) > 1 {
					return
				}
			}
		}
	})
	switch {
	case err != nil:
		return appwire.CrewStartResult{}, true, appStartUnknown(request, err)
	case len(found) == 0:
		return appwire.CrewStartResult{}, false, nil
	case len(found) > 1:
		return appwire.CrewStartResult{}, true, appwire.Errorf(appwire.CodeUncertain,
			"more than one earlier record names request_id %s, so which crew it made cannot be said; nothing is started for it", request)
	}
	record := found[0]
	if record.Fingerprint != fingerprint {
		return appwire.CrewStartResult{}, true, appwire.Errorf(appwire.CodeBadParams, "request_id %s was used for a different team", request)
	}
	here, err := b.teamCommitted(crews[0])
	switch {
	case err != nil:
		return appwire.CrewStartResult{}, true, appStartUnknown(request, err)
	case !here:
		return appwire.CrewStartResult{}, true, appStartGone(request)
	}
	goal := appwire.CrewGoal{State: appwire.GoalNone, ClientID: record.ClientID}
	if record.Goal {
		goal.State = appwire.GoalUnknown
	}
	got, err := b.startAnswer(appStartClaim{RequestID: request, Crew: crews[0], Manager: record.Manager}, true, goal)
	return got, true, err
}

// appStartUnknown refuses a request whose earlier outcome cannot be read.
func appStartUnknown(request string, err error) error {
	return appwire.Errorf(appwire.CodeUncertain,
		"what request_id %s did earlier cannot be read (%s); nothing is started for it", request, plainWords(err.Error()))
}

// appStartGone refuses a request whose crew has been deleted since.
func appStartGone(request string) error {
	return appwire.Errorf(appwire.CodeBadParams,
		"request_id %s started a crew that has since been deleted, and its goal may have been sent; start another with a new request_id", request)
}

// appStartFingerprint is what makes two requests the same one: the whole ask,
// normalised, without its key.
func appStartFingerprint(ask appwire.CrewCreate) string {
	ask.RequestID = ""
	raw, _ := json.Marshal(ask)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// appStartedWords is why a made team's manager could not be held, in words.
func appStartedWords(cause error) string {
	var wire *appwire.Error
	if errors.As(cause, &wire) {
		return wire.Message
	}
	return plainWords(cause.Error())
}

// RootStart is crews.root.start: the person's explicit act that makes the root
// ("All teams") and its manager. Everything already true is returned as it is.
// A root manager whose conversation is gone — its folder removed, or deleted
// for good — is replaced, and LEAVES the root rather than staying a member of
// it; one whose conversation cannot be checked is not replaced on a guess.
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
			rootID = r.ID
			if r.Manager != "" {
				here, err := appConversationHere(r, r.Manager)
				if err != nil {
					return appwire.Errorf(appwire.CodeEngine, "whether the global manager's conversation is still there cannot be read: %s", plainWords(err.Error()))
				}
				if here {
					// Another start won while this one minted: it is the answer.
					taken = true
					return nil
				}
				// THE MANAGER WHOSE CONVERSATION IS GONE LEAVES THE ROOT, so the
				// root never holds a member nobody can open.
				if err := f.RemoveMember(r.ID, r.Manager); err != nil {
					return err
				}
			}
		} else {
			rootID, rootMade = f.MakeRoot(b.now()), true
		}
		if err := f.AddMember(rootID, manager); err != nil {
			return err
		}
		return f.SetManager(rootID, manager.Key)
	})
	if err != nil {
		// A WRITE THAT FAILED MAY HAVE LANDED: it is looked for by the key of
		// the conversation it would have seated.
		file, lookErr := teamstore.Load(b.crewProfile())
		if lookErr != nil {
			return appwire.RootStartResult{}, appwire.Errorf(appwire.CodeUncertain,
				"the global manager may have been made; look at All teams before starting it again")
		}
		root, ok := file.Root()
		if !ok || root.Manager != manager.Key {
			b.takeBack(transcript)
			return appwire.RootStartResult{}, err
		}
		rootID, taken = root.ID, false
	}
	if taken {
		b.takeBack(transcript)
		existing, _, err := b.rootHeld()
		return existing, err
	}
	agent, launch, err := b.createAgentHeld(appCreatePrep{ask: appwire.CreateAsk{Workspace: workspace, Model: ask.Model}, resolved: workspace}, transcript)
	if err != nil {
		return appwire.RootStartResult{}, appwire.Errorf(appwire.CodeEngine, "the global manager was made, but its conversation could not be held: %s", appStartedWords(err))
	}
	launch()
	crew, err := b.CrewGet(rootID)
	if err != nil {
		return appwire.RootStartResult{}, err
	}
	return appwire.RootStartResult{Crew: crew, Manager: agent.ID, RootCreated: rootMade, ManagerCreated: true}, nil
}

// rootHeld is the root when it already has a manager whose conversation is
// still there: the answer to a start that has nothing left to make. A
// conversation that cannot be checked is an error, not a missing manager.
func (b *appBackend) rootHeld() (appwire.RootStartResult, bool, error) {
	file, err := teamstore.Load(b.crewProfile())
	if err != nil {
		return appwire.RootStartResult{}, false, appEngineError(err)
	}
	root, ok := file.Root()
	if !ok || root.Manager == "" {
		return appwire.RootStartResult{}, false, nil
	}
	here, err := appConversationHere(root, root.Manager)
	if err != nil {
		return appwire.RootStartResult{}, false, appwire.Errorf(appwire.CodeEngine, "whether the global manager's conversation is still there cannot be read: %s", plainWords(err.Error()))
	}
	if !here {
		return appwire.RootStartResult{}, false, nil
	}
	m, _ := root.Member(root.Manager)
	crew, err := b.CrewGet(root.ID)
	if err != nil {
		return appwire.RootStartResult{}, false, err
	}
	return appwire.RootStartResult{Crew: crew, Manager: appMemberAgent(m)}, true, nil
}

// appConversationHere says the conversation a team member names is still
// there. A conversation that has not had its first turn has no transcript yet,
// only its folder, so the folder is what is looked for; a folder that carries
// the permanent-deletion mark ([teamstore.ConversationDeletedFile]) is gone
// even though it is still on disk. Only a folder PROVABLY absent or marked is
// gone: anything that cannot be read is an error.
func appConversationHere(t teamstore.Team, key string) (bool, error) {
	m, ok := t.Member(key)
	if !ok {
		return false, nil
	}
	var dirs []string
	for _, path := range []string{m.File, m.Key} {
		if filepath.IsAbs(path) {
			dirs = append(dirs, session.DirOf(path))
		}
	}
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, teamstore.ConversationDeletedFile)); err == nil {
			return false, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
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
	dir := session.DirOf(filepath.Clean(transcript))
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
