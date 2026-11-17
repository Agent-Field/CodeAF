package main

// ── codeaf app: CHANGING HOW MANY JOBS ONE OF YOUR COMPUTERS RUNS ───────────
//
// The engine's half of appwire.FactoryRunnerAdmin. It makes ONE PUT to ONE
// route of the factory's /v1 API, /v1/runners/{runner}, spelled here as a
// constant: the path and the verb are never taken from the app, the computer's
// id is validated by appwire and escaped here, and the body is a local type
// with exactly two fields, so a checkout list or anything else cannot be sent.
//
// WHO MAY: the OWNER of that computer, with the credential this app holds for
// the connection right now. Each call asks GET /v1/me, then GET /v1/runners
// with the same credential, and the computer's owner must be the handle the
// factory just said the caller is. Being the operator gives no power over
// another person's computer, and the factory would refuse it too; the refusal is
// made here, in words, before anything is sent. A connection with no held
// credential is refused before any request leaves, and nothing looks up the
// host's key or the terminal's attach record.
//
// IT IS SENT ONLY TO A FACTORY AND A COMPUTER THAT CAN TAKE IT. The runner
// directory must carry the server's slots_policy (an older factory sends none,
// and would answer 422 to the new field or write to a worker that cannot follow
// it), the policy must fit within this build's supported range, and the computer must say
// live_capacity. The request repeats the last condition as require_live_capacity,
// which the factory checks under the lock that writes, so a worker that went
// back to an older binary between this read and the write is refused with the
// phase marker (api.PhaseHeader) and nothing is changed.
//
// A WRITE IS SENT ONCE, never retried. A connection that never opened is
// "unreachable" (nothing was sent). Only a refusal that carries the phase marker
// proves nothing was stored; any other failure (a request that may have arrived
// and got no answer, a 5xx, a 4xx without the marker, an unexpected 2xx) is
// appwire.CodeUncertain, because the factory saves the capacity and THEN records
// the event, and the second step can fail with a status that looks like a
// refusal. Only 200 or 204 is completed. Every wait is followed by a check that
// the connection still holds the same credential; an answer that arrives for a
// caller who has been replaced reveals nothing, and is neutral.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryRunnerAdmin = (*appBackend)(nil)

const (
	appFactoryRunnersRoute = "/v1/runners"
	appFactoryRunnerRoute  = "/v1/runners/"
)

// appRunnerSettingsBody is the whole PUT body: the number, and the condition the
// factory re-checks while writing. Checkouts are not here, so they are absent
// on the wire and the factory leaves them as they are.
type appRunnerSettingsBody struct {
	Slots               int  `json:"slots"`
	RequireLiveCapacity bool `json:"require_live_capacity"`
}

// A lower server ceiling is compatible (for example an existing eight-slot
// factory). A wider range or missing atomic live guard is not supported.
func expectedRunnerSlotsPolicy(p *api.RunnerSlotsPolicy) bool {
	return p != nil && p.Min == 1 && p.Max >= p.Min && p.Max <= api.MaxRunnerSlots && p.RequiresLive
}

func runnerSignInChanged() error {
	return appwire.Errorf(appwire.CodeConflict, "the sign-in changed while this was loading; try again")
}

func (b *appBackend) FactoryRunnerSlots(ctx context.Context, p appwire.FactoryRunnerSlotsParams) (appwire.FactoryRunnerSlotsDone, error) {
	none := appwire.FactoryRunnerSlotsDone{}
	h := b.factoryHub()
	release, err := h.claimAdmin(p.Factory)
	if err != nil {
		return none, err
	}
	defer release()
	who, err := b.heldWho(ctx, p.Factory)
	if err != nil {
		return none, err
	}
	if !h.callerIs(p.Factory, who.token) {
		return none, runnerSignInChanged()
	}
	if who.me.Handle == "" {
		return none, appwire.Errorf(appwire.CodeForbidden, "this sign-in has no owner name, so it owns no computer")
	}
	// The directory is read with the same credential. Nothing has been written.
	r, err := h.linkDo(ctx, who, http.MethodGet, appFactoryRunnersRoute, nil, appFactoryBody)
	if err != nil {
		return none, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering; nothing was changed")
	}
	if !h.callerIs(p.Factory, who.token) {
		return none, runnerSignInChanged()
	}
	if r.status != http.StatusOK {
		if r.status >= 400 && r.status < 500 {
			return none, linkRefusal(r, "the factory could not list its computers")
		}
		return none, appwire.Errorf(appwire.CodeEngine, "the factory could not list its computers; nothing was changed")
	}
	var dir api.Runners
	if r.bodyBad || json.Unmarshal(r.body, &dir) != nil {
		return none, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read; nothing was changed")
	}
	if !expectedRunnerSlotsPolicy(dir.SlotsPolicy) {
		return none, appwire.Errorf(appwire.CodeConflict, "this factory cannot change a computer's slots from here yet; nothing was changed")
	}
	if p.Slots == nil || *p.Slots < dir.SlotsPolicy.Min || *p.Slots > dir.SlotsPolicy.Max {
		return none, appwire.Errorf(appwire.CodeConflict, "that number is outside this factory's supported range; read its computers before trying again. Nothing was changed.")
	}
	var own *api.Runner
	elsewhere := false
	for i := range dir.Runners {
		if dir.Runners[i].ID != p.Runner {
			continue
		}
		if dir.Runners[i].Owner != who.me.Handle {
			elsewhere = true
			continue
		}
		if own != nil {
			return none, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read; nothing was changed")
		}
		own = &dir.Runners[i]
	}
	switch {
	case own == nil && elsewhere:
		return none, appwire.Errorf(appwire.CodeForbidden, "only the person who owns a computer can change its slots")
	case own == nil:
		return none, appwire.Errorf(appwire.CodeNotFound, "that computer is not listed")
	case !own.LiveCapacity:
		return none, appwire.Errorf(appwire.CodeConflict, "that computer's codeaf needs an update before its slots can change from here; restarting it is not enough. Nothing was changed.")
	}
	// The waits are over; the write is made only for the caller who asked.
	if !h.callerIs(p.Factory, who.token) {
		return none, runnerSignInChanged()
	}
	body := appRunnerSettingsBody{Slots: *p.Slots, RequireLiveCapacity: true}
	r, err = h.linkDo(ctx, who, http.MethodPut, appFactoryRunnerRoute+url.PathEscape(p.Runner), body, appFactorySmall)
	switch {
	case err == errLinkNotSent:
		return none, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering; nothing was changed")
	case err != nil:
		return none, runnerSlotsUncertain()
	}
	// Whatever the factory said describes the caller it was asked as. If that
	// is no longer the connection's, none of it is used, not even a refusal.
	if !h.callerIs(p.Factory, who.token) {
		return none, appwire.Errorf(appwire.CodeUncertain, "the sign-in changed while this was saving, and the result is not known; read the computers before trying again")
	}
	if r.provedNoWrite() {
		return none, runnerSlotsRefusal(r)
	}
	if !completedAdminStatus(r.status) {
		return none, runnerSlotsUncertain()
	}
	h.touch(p.Factory)
	return appwire.FactoryRunnerSlotsDone{Factory: p.Factory, Runner: p.Runner, Slots: *p.Slots}, nil
}

func runnerSlotsUncertain() error {
	return appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm the change; it may have happened, so read the computers before trying again")
}

// runnerSlotsRefusal is the wire error for a refusal that PROVED nothing was
// stored. The words are fixed here; the factory's own are never passed on.
func runnerSlotsRefusal(r linkReply) error {
	if r.status == http.StatusConflict {
		return appwire.Errorf(appwire.CodeConflict, "that computer's codeaf cannot follow a slots change right now, so nothing was changed; it needs an update, and restarting it is not enough")
	}
	return linkRefusal(r, "that computer is not listed")
}
