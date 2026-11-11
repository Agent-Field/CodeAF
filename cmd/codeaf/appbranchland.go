package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// branches.land.plan and branches.land: the person's own landing of one of a
// conversation's task branches. session.PlanTaskBranchLand and
// session.LandTaskBranch state every guarantee; this file holds the plans in
// between and nothing else.
//
// A PLAN IS USED ONCE. It is taken out of the store before anything is read
// again, so two confirmations of one plan cannot both reach the merge, and a
// refused or failed land must be planned again from the start.

const (
	// appLandPlanTime bounds one branches.land.plan.
	appLandPlanTime = 10 * time.Second
	// appLandCheckTime bounds a land's wait for the repository's lock and its
	// fresh reading of every fact. The merge after it runs on its own ceiling.
	appLandCheckTime = 15 * time.Second
	appLandPlanTTL   = appwire.BranchLandPlanTTL * time.Second
)

type appLandPlan struct {
	agent     string
	place     session.Place
	workspace string
	plan      session.TaskBranchLandPlan
	made      time.Time
	expires   time.Time
}

// appLandPlanStore holds issued plans by the digest of their id, so the id
// itself is never a map key a lookup could be timed against.
type appLandPlanStore struct {
	mu    sync.Mutex
	now   func() time.Time
	plans map[string]*appLandPlan
}

func newAppLandPlanStore(now func() time.Time) *appLandPlanStore {
	return &appLandPlanStore{now: now, plans: map[string]*appLandPlan{}}
}

// appLandPlans is the process's one store. A variable so a test can give it a
// clock of its own.
var appLandPlans = newAppLandPlanStore(time.Now)

func landPlanKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// put holds a plan and answers its id. Expired plans go first; an agent past
// its share loses its oldest; a store still full is busy.
func (s *appLandPlanStore) put(entry appLandPlan) (string, time.Time, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var oldest string
	mine := 0
	for key, held := range s.plans {
		if !now.Before(held.expires) {
			delete(s.plans, key)
			continue
		}
		if held.agent == entry.agent {
			mine++
			if oldest == "" || held.made.Before(s.plans[oldest].made) {
				oldest = key
			}
		}
	}
	if mine >= appwire.BranchLandPlansPerAgent {
		delete(s.plans, oldest)
	}
	if len(s.plans) >= appwire.BranchLandPlansMax {
		return "", time.Time{}, appwire.Errorf(appwire.CodeBusy, "too many lands are waiting to be confirmed; try again in a little while")
	}
	entry.made, entry.expires = now, now.Add(appLandPlanTTL)
	s.plans[landPlanKey(id)] = &entry
	return id, entry.expires, nil
}

// take removes a plan and answers it when it is the agent's and still fresh.
func (s *appLandPlanStore) take(agent, id string) (appLandPlan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := landPlanKey(id)
	held, ok := s.plans[key]
	if !ok {
		return appLandPlan{}, false
	}
	delete(s.plans, key)
	if held.agent != agent || !s.now().Before(held.expires) {
		return appLandPlan{}, false
	}
	return *held, true
}

// PlanBranchLand answers branches.land.plan.
func (b *appBackend) PlanBranchLand(q appwire.BranchLandPlanQuery) (appwire.BranchLandPlan, error) {
	known := b.deliverEnv().agent(q.Agent)
	if !known.known {
		if _, err := b.find(q.Agent); err != nil {
			return appwire.BranchLandPlan{}, err
		}
		return appwire.BranchLandPlan{}, appwire.Errorf(appwire.CodeNotFound, "no agent %s", q.Agent)
	}
	ctx, cancel := context.WithTimeout(context.Background(), appLandPlanTime)
	defer cancel()
	plan, err := session.PlanTaskBranchLand(ctx, known.place, known.workspace, q.Branch)
	if err != nil {
		return appwire.BranchLandPlan{}, appLandError(err)
	}
	id, expires, err := appLandPlans.put(appLandPlan{agent: q.Agent, place: known.place, workspace: known.workspace, plan: plan})
	if err != nil {
		return appwire.BranchLandPlan{}, err
	}
	out := appwire.BranchLandPlan{
		Plan: id, Revision: plan.Revision, ExpiresAt: expires.UTC().Format(time.RFC3339),
		Task: plan.Task, Title: plan.Title, Branch: plan.Branch, BranchHead: plan.BranchHead, Base: plan.Base,
		Target: plan.Target, Head: plan.Head, Repository: plan.Root, FastForward: plan.FastForward,
		CountsCapped: plan.CountsCapped, Hooks: make([]appwire.BranchLandHook, 0, len(plan.Hooks)),
	}
	if plan.CountsKnown {
		files, added, removed := plan.Files, plan.Added, plan.Removed
		out.Files, out.Added, out.Removed = &files, &added, &removed
	}
	for _, hook := range plan.Hooks {
		out.Hooks = append(out.Hooks, appwire.BranchLandHook{Name: hook.Name, SHA256: hook.SHA256, Size: hook.Size, Executable: hook.Executable})
	}
	return out, nil
}

// LandBranch answers branches.land: the native host's, after its own
// confirmation of exactly the plan it names.
func (b *appBackend) LandBranch(q appwire.BranchLandQuery) (appwire.BranchLanded, error) {
	held, ok := appLandPlans.take(q.Agent, q.Plan)
	if !ok {
		return appwire.BranchLanded{}, landRefused("expired", "that land is no longer waiting to be confirmed; plan it again", nil)
	}
	if subtle.ConstantTimeCompare([]byte(q.Revision), []byte(held.plan.Revision)) != 1 {
		return appwire.BranchLanded{}, landRefused(session.LandChanged, "that is not the land that was planned; plan it again", nil)
	}
	known := b.deliverEnv().agent(q.Agent)
	if !known.known || known.workspace != held.workspace || known.place.Dir != held.place.Dir {
		return appwire.BranchLanded{}, landRefused(session.LandChanged, "this conversation's folder changed since the land was planned; plan it again", nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), appLandCheckTime)
	defer cancel()
	landed, err := session.LandTaskBranch(ctx, held.place, held.workspace, held.plan)
	if err != nil {
		return appwire.BranchLanded{}, appLandError(err)
	}
	return appwire.BranchLanded{Branch: landed.Branch, Target: landed.Target, HeadBefore: landed.HeadBefore, HeadAfter: landed.HeadAfter, FastForward: landed.FastForward}, nil
}

func landRefused(reason, said string, paths []string) *appwire.Error {
	return &appwire.Error{Code: appwire.CodeConflict, Message: said, Changed: paths, Detail: appwire.BranchLandRefusal{Reason: reason, Paths: paths}}
}

func appLandError(err error) error {
	var refusal *session.LandRefusal
	var uncertain *session.LandUncertain
	switch {
	case errors.As(err, &refusal):
		return landRefused(refusal.Reason, refusal.Said, refusal.Paths)
	case errors.As(err, &uncertain):
		f := uncertain.Facts
		return &appwire.Error{Code: appwire.CodeUncertain, Message: uncertain.Said, Detail: appwire.BranchLandFacts{
			Head: f.Head, Target: f.Target, MergeInProgress: f.MergeInProgress, IndexLocked: f.IndexLocked, Clean: f.Clean, Conflicted: f.Conflicted}}
	case errors.Is(err, session.ErrLandNotOwned), errors.Is(err, session.ErrLandNoRepo):
		return appwire.Errorf(appwire.CodeNotFound, "%s", err.Error())
	case errors.Is(err, session.ErrLandBusy):
		return appwire.Errorf(appwire.CodeBusy, "%s", err.Error())
	}
	return appBranchError(err)
}

var _ appwire.BranchLander = (*appBackend)(nil)
