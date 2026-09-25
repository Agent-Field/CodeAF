package config

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/router"
)

// ROUTE HEALTH — WHAT THIS INSTALL HAS LEARNED ABOUT EACH ROUTE.
//
// A route is a provider, a send id and a price, and a crew seat's first call on
// it either answers or fails with a kind of failure (internal/provider's
// RouteFailure: payment, auth, forbidden, quota, unavailable, transient). Those
// outcomes are written to the router's log as they happen ([LogCrewRoute]) and
// read back here, oldest first, into what every decision is routed around:
//
//   - forbidden   → the route is QUARANTINED for [crewQuarantineFor]: this route
//     will not take this model for this account, and asking again tomorrow
//     will not change that;
//   - quota       → the route COOLS DOWN until the reset it said, or for
//     [crewCooldownFor];
//   - unavailable → the route cools down, and the model is DEMOTED off
//     unpinned seats: it is not served there any more;
//   - payment     → every PAID route on that provider's account is
//     UNAFFORDABLE, until a paid call on it answers again;
//   - auth        → the provider is DISCONNECTED, until a call on it answers
//     again — or the person reconnects it;
//   - transient   → nothing but the route's failure rate.
//
// Every outcome feeds the route's learned chance of refusing a first call,
// which is part of what the route is expected to cost (crewroute's routeCost).
// NOTHING HERE NAMES A PROVIDER: the kinds are the provider layer's, and a
// provider that says its credit is gone before a call is made can say so
// through [CrewAccountState].

// The health windows, spelled once.
const (
	// crewQuarantineFor is how long a forbidden route is kept out.
	crewQuarantineFor = 7 * 24 * time.Hour
	// crewCooldownFor is how long a quota or unavailable route rests when it
	// said no reset of its own.
	crewCooldownFor = time.Hour
	// crewUnavailableFor is how long a route whose model was not served rests.
	crewUnavailableFor = 6 * time.Hour
	// crewDemoteFor is how far back failures count toward demoting a model,
	// and crewDemoteAfter how many start failures (not limits) it takes.
	crewDemoteFor   = 7 * 24 * time.Hour
	crewDemoteAfter = 2
	// A route's learned failure rate is read against a prior worth
	// crewPriorTasks tasks at crewPriorRate, so one lucky call does not make a
	// route look safe.
	crewPriorTasks = 3.0
	crewPriorRate  = 0.3
)

// CrewAccountState is what a provider adapter can say about an account before
// any call is made: its remaining credit, and when its limit resets.
type CrewAccountState struct {
	CreditKnown bool
	CreditUSD   float64
	ResetAt     time.Time
}

// CrewAccounts is the OPTIONAL adapter that primes route health for providers
// that can report their account state. Nil — the default — is providers that
// cannot, whose health is learned from their calls alone.
var CrewAccounts func(profileDir, provider string) (CrewAccountState, bool)

// CrewRouteHistory is how route outcomes reach the health reading: the router
// log's recent first-call outcomes. A variable so a test can hand its own.
var CrewRouteHistory = func(profileDir string) []router.CrewRouteOutcome {
	return router.ReadCrewLog(ProfilePath(profileDir, ""), time.Now()).Routes
}

// crewHealth is the reading.
type crewHealth struct {
	now time.Time
	// blocked is send → until, for a route quarantined or cooling down.
	blocked map[string]time.Time
	// unaffordable and disconnected are providers.
	unaffordable map[string]bool
	disconnected map[string]bool
	// demoted are model lineages kept off unpinned seats.
	demoted map[string]bool
	// fail is send → learned chance of refusing a first call.
	fail map[string]float64
	// resets is provider → the latest reset a cooling route said.
	resets map[string]time.Time
}

// crewRouteFacts are what a candidate list is built with beyond the rule and
// the providers: whether free pools are routes, and route health.
type crewRouteFacts struct {
	free   bool
	health crewHealth
}

// crewHealthAt reads this profile's route health now.
func crewHealthAt(profileDir string) crewHealth {
	var history []router.CrewRouteOutcome
	if CrewRouteHistory != nil {
		history = CrewRouteHistory(profileDir)
	}
	h := crewHealthOf(history, time.Now())
	if CrewAccounts != nil {
		for _, p := range CrewProvidersAt(profileDir) {
			if state, ok := CrewAccounts(profileDir, p.ID); ok && state.CreditKnown && state.CreditUSD <= 0 {
				h.unaffordable[p.ID] = true
			}
		}
	}
	return h
}

// crewHealthOf reads route outcomes, oldest first, as health at now.
func crewHealthOf(history []router.CrewRouteOutcome, now time.Time) crewHealth {
	h := crewHealth{now: now, blocked: map[string]time.Time{}, unaffordable: map[string]bool{},
		disconnected: map[string]bool{}, demoted: map[string]bool{}, fail: map[string]float64{}, resets: map[string]time.Time{}}
	sorted := append([]router.CrewRouteOutcome(nil), history...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })
	starts, fails := map[string]int{}, map[string]int{}
	startFails := map[string]int{}
	block := func(send string, until time.Time) {
		if until.After(now) && until.After(h.blocked[send]) {
			h.blocked[send] = until
		}
	}
	for _, o := range sorted {
		if o.Kind == "" {
			starts[o.Send]++
			delete(h.disconnected, o.Provider)
			if o.Paid {
				delete(h.unaffordable, o.Provider)
			}
			continue
		}
		fails[o.Send]++
		lineage := crewroute.Lineage(o.Send)
		switch o.Kind {
		case "payment":
			h.unaffordable[o.Provider] = true
		case "auth":
			h.disconnected[o.Provider] = true
		case "forbidden":
			block(o.Send, o.At.Add(crewQuarantineFor))
			if now.Sub(o.At) < crewDemoteFor {
				startFails[lineage]++
			}
		case "quota":
			until := o.Until
			if until.IsZero() {
				until = o.At.Add(crewCooldownFor)
			}
			block(o.Send, until)
			if until.After(h.resets[o.Provider]) {
				h.resets[o.Provider] = until
			}
		case "unavailable":
			block(o.Send, o.At.Add(crewUnavailableFor))
			if now.Sub(o.At) < crewDemoteFor {
				h.demoted[lineage] = true
			}
		}
	}
	for lineage, n := range startFails {
		if n >= crewDemoteAfter {
			h.demoted[lineage] = true
		}
	}
	for send := range fails {
		n := float64(starts[send] + fails[send])
		h.fail[send] = (float64(fails[send]) + crewPriorRate*crewPriorTasks) / (n + crewPriorTasks)
	}
	return h
}

// probing is this health with the accounts it holds out of credit and the
// providers whose key it saw refused put back on trial: what a new task is
// routed under, so its first call asks them again ([RouteCrew]).
func (h crewHealth) probing() crewHealth {
	h.unaffordable, h.disconnected = map[string]bool{}, map[string]bool{}
	return h
}

// CrewHealthySend is the model an auxiliary call — a summary, a brief, a
// landing's answer, anything that is not a crew seat's own call nor the
// person's turn — should ask instead of send, when send's route is one route
// health says will not answer: quarantined or cooling, on an account out of
// credit, on a provider whose key was refused. It is the router's standing
// worker pick, then the seat's rescue; send itself when its route is
// healthy or nothing better is reachable.
//
// AN AUXILIARY CALL NEVER PROBES. Only a task's first seat call asks an
// account out of credit again ([RouteCrew]); a helper that did would spend a
// refusal on every summary.
func CrewHealthySend(profileDir, send, chatModel string) string {
	send = strings.TrimSpace(send)
	if profileDir == "" || send == "" {
		return send
	}
	health := crewHealthCached(profileDir)
	if health.answers(send, CrewProvidersAt(profileDir)) {
		return send
	}
	if seat := standingCrewSeat(profileDir, crewroute.Worker); seat != "" && seat != send {
		return seat
	}
	for _, rung := range CrewRescue(profileDir, crewroute.Other, crewroute.Worker, chatModel) {
		if rung.Send != send {
			return rung.Send
		}
	}
	return send
}

// answers is whether this health expects send's route to answer.
func (h crewHealth) answers(send string, providers []CrewProvider) bool {
	if _, blocked := h.blocked[send]; blocked {
		return false
	}
	pin := resolveCrewPin(CrewPin{Model: send}, providers)
	return !h.disconnected[pin.Provider] && !(pin.Kind == crewroute.Metered && h.unaffordable[pin.Provider])
}

// crewHealthCached is [crewHealthAt] read at most once a few seconds per
// profile: auxiliary calls ask it on every request, and the router's log is a
// file.
func crewHealthCached(profileDir string) crewHealth {
	crewHealthCache.mu.Lock()
	defer crewHealthCache.mu.Unlock()
	if crewHealthCache.dir == profileDir && time.Since(crewHealthCache.at) < crewHealthFresh {
		return crewHealthCache.health
	}
	crewHealthCache.dir, crewHealthCache.at, crewHealthCache.health = profileDir, time.Now(), crewHealthAt(profileDir)
	return crewHealthCache.health
}

// crewHealthFresh is how long a cached health reading stands.
const crewHealthFresh = 3 * time.Second

var crewHealthCache struct {
	mu     sync.Mutex
	dir    string
	at     time.Time
	health crewHealth
}

// usable is the routes this health leaves, with their learned failure rates.
func (h crewHealth) usable(routes []crewroute.Route) []crewroute.Route {
	out := routes[:0]
	for _, r := range routes {
		if _, blocked := h.blocked[r.Send]; blocked {
			continue
		}
		if h.disconnected[r.Provider] || (r.Kind == crewroute.Metered && h.unaffordable[r.Provider]) {
			continue
		}
		r.FailRate = h.fail[r.Send]
		out = append(out, r)
	}
	return out
}

// crewCandidatesNoticed is [CrewCandidatesAt] with the one notice a decision
// carries when it had to reach for free routes.
//
// WHEN NOTHING PAID CAN BE REACHED, THE FREE POOLS ARE USED — whatever the
// free-routes row says — because a task on a free pool is better than a task
// that cannot start, and the line says so: a free pool may log what it is
// sent. A paid route coming back (a paid call answering, a provider reporting
// credit) puts the next task back on normal routing on its own.
func crewCandidatesNoticed(profileDir string, health crewHealth) ([]crewroute.Candidate, string) {
	rule, providers, off := CrewAllowedAt(profileDir), CrewProvidersAt(profileDir), CrewProvidersOffAt(profileDir)
	facts := crewRouteFacts{free: CrewFreeRoutesAt(profileDir), health: health}
	candidates := off.Candidates(crewCandidatesWith(rule, providers, facts))
	if facts.free || len(health.unaffordable) == 0 || anyPaidRoute(candidates) {
		return candidates, ""
	}
	facts.free = true
	withFree := off.Candidates(crewCandidatesWith(rule, providers, facts))
	if len(withFree) == len(candidates) {
		return candidates, ""
	}
	return withFree, "free routes in use (may log prompts) · credit unavailable on " + strings.Join(crewSet(health.unaffordable), ", ")
}

// anyPaidRoute is whether any candidate is reachable on a route that bills.
func anyPaidRoute(candidates []crewroute.Candidate) bool {
	for _, c := range candidates {
		for _, r := range c.Routes {
			if r.Kind == crewroute.Metered {
				return true
			}
		}
	}
	return false
}

// crewSet is a set's members in order.
func crewSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k, on := range set {
		if on {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// crewAction is the ONE thing a person can do when no crew can be formed or a
// seat has nowhere left to go, read off what the routes said — never the
// refusal's own text.
func (h crewHealth) crewAction() string {
	switch {
	case len(h.unaffordable) > 0:
		return "add credit on " + strings.Join(crewSet(h.unaffordable), ", ") + " to continue"
	case len(h.disconnected) > 0:
		return "reconnect " + strings.Join(crewSet(h.disconnected), ", ") + " with /connect"
	}
	var latest time.Time
	for _, at := range h.resets {
		if at.After(latest) {
			latest = at
		}
	}
	if !latest.IsZero() && latest.After(h.now) {
		return "the limit resets at " + latest.Local().Format("15:04") + " — try again then"
	}
	return "no allowed model on a connected provider can start · widen /crew models or pin one with /crew"
}

// pinnedBefore is whether a seat was the person's pin rather than a rescue.
func pinnedBefore(seat crewroute.Seat, pins map[crewroute.Seat]crewroute.Pin, rescued []string) bool {
	for _, said := range rescued {
		if strings.HasPrefix(said, string(seat)+" on ") {
			return false
		}
	}
	_, ok := pins[seat]
	return ok
}

// ErrCrewUnreachable is a crew nothing reachable can form; its text is the one
// action ([crewHealth.crewAction]).
type ErrCrewUnreachable struct{ Action string }

func (e ErrCrewUnreachable) Error() string { return e.Action }

// LogCrewRoute writes one seat's first-call outcome on its route: answered
// (kind empty), or failed with a route-failure kind and when the route said it
// may be asked again.
func LogCrewRoute(profileDir, call string, d crewroute.Decision, repo, title string, seat crewroute.Seat, pick crewroute.Pick, kind string, until time.Time) {
	router.LogCrewRoute(ProfilePath(profileDir, ""), call, CrewRecordOf(d, repo, title), router.CrewRouteOutcome{
		Seat: string(seat), Send: pick.Send, Provider: pick.Provider,
		Paid: pick.Kind == crewroute.Metered, Kind: kind, Until: until,
	})
}

// CrewRescue is the last of a seat's ladder, after everything the router
// offered: the seat as the last crew that completed a task on this install
// ran it, then the model the person is talking to — each only when its route
// is healthy and the model can sit the seat. The router's own rungs come first
// ([crewroute.Decision.Ladder]).
func CrewRescue(profileDir string, class crewroute.Class, seat crewroute.Seat, chatModel string) []crewroute.Pick {
	health := crewHealthAt(profileDir)
	var out []crewroute.Pick
	seen := map[string]bool{}
	add := func(send string) {
		send = strings.TrimSpace(send)
		if send == "" || seen[send] {
			return
		}
		seen[send] = true
		if _, blocked := health.blocked[send]; blocked {
			return
		}
		pin := resolveCrewPin(CrewPin{Model: send}, CrewProvidersAt(profileDir))
		// NOT EVEN THE PERSON'S OWN MODEL rides an account that just said it
		// is out of credit, or a key that was just refused: the conversation
		// running on it proves the model, not the account.
		if health.disconnected[pin.Provider] || (pin.Kind == crewroute.Metered && health.unaffordable[pin.Provider]) {
			return
		}
		if model, known := crewCatalogModel(send); known && !crewroute.Seatable(seat, crewroute.Candidate{Model: model, Routes: []crewroute.Route{{Send: pin.Send}}}) {
			return
		}
		out = append(out, crewroute.Pick{Seat: seat, Model: pin.Model, Provider: pin.Provider, Send: pin.Send, Kind: pin.Kind})
	}
	if last := CrewLastGood(profileDir); last != nil {
		add(last.Seats[string(seat)])
	}
	out = append(out, crewFreeRescue(profileDir, class, seat, health)...)
	add(chatModel)
	return out
}

// crewFreeRescue is the seat on a free pool when EVERY PAID ROUTE IS OUT OF
// REACH — the free-routes switch off or on. With the switch on the pools are
// already routes on the seat's ladder; with it off they are used only here,
// and the crew line says so ([crewCandidatesNoticed]'s notice). The best few
// free picks for the seat, each a different model, so a pool at its limit
// leaves the next.
func crewFreeRescue(profileDir string, class crewroute.Class, seat crewroute.Seat, health crewHealth) []crewroute.Pick {
	candidates, notice := crewCandidatesNoticed(profileDir, health)
	if notice == "" {
		return nil
	}
	var free []crewroute.Candidate
	for _, c := range candidates {
		var routes []crewroute.Route
		for _, r := range c.Routes {
			if r.Kind == crewroute.Free {
				routes = append(routes, r)
			}
		}
		// THE RESCUE KEEPS A FLOOR: a model tuned for one domain (finance,
		// medicine, law) or too small to do a seat's work is no rescue at all,
		// however little else is left — a task stopped on its one action is
		// better than one sent to a model that cannot do it.
		if len(routes) > 0 && !crewroute.DomainTuned(c.Model.ID) && !crewroute.Tiny(c.Model.ID) {
			free = append(free, crewroute.Candidate{Model: c.Model, Routes: routes})
		}
	}
	var out []crewroute.Pick
	avoid := map[string]bool{}
	for len(out) < crewFreeRescues && len(free) > 0 {
		d, err := crewroute.Decide(crewroute.Request{Class: class, Candidates: free, Avoid: avoid, Rescue: true})
		if err != nil {
			break
		}
		pick := d.Seat(seat)
		if pick.Send == "" || avoid[crewroute.Lineage(pick.Model)] {
			break
		}
		pick.Pinned = false
		out = append(out, pick)
		avoid[crewroute.Lineage(pick.Model)] = true
	}
	return out
}

// crewFreeRescues is how many free models a seat's rescue tries.
const crewFreeRescues = 3

// CrewLastGood is the crew of the newest task whose result was kept.
var CrewLastGood = func(profileDir string) *router.CrewRecord {
	return router.ReadCrewLog(ProfilePath(profileDir, ""), time.Now()).LastGood
}

// crewRescued is a decision the router could not form for want of a seat,
// formed with that seat on its rescue: the last good crew's model, then the
// person's own. Nothing to rescue it with is the one action.
func crewRescued(profileDir string, req crewroute.Request, health crewHealth, chatModel string, cause error) (crewroute.Decision, error) {
	var missing crewroute.NoCandidateError
	if !errors.As(cause, &missing) {
		return crewroute.Decision{}, cause
	}
	pins := map[crewroute.Seat]crewroute.Pin{}
	for seat, pin := range req.Pins {
		pins[seat] = pin
	}
	var rescued []string
	for tries := 0; tries < len(crewroute.Seats); tries++ {
		rescue := CrewRescue(profileDir, req.Class, missing.Seat, chatModel)
		if len(rescue) == 0 {
			return crewroute.Decision{}, ErrCrewUnreachable{Action: health.crewAction()}
		}
		r := rescue[0]
		pins[missing.Seat] = crewroute.Pin{Model: r.Model, Provider: r.Provider, Send: r.Send, Kind: r.Kind}
		rescued = append(rescued, string(missing.Seat)+" on "+crewroute.ShortModel(r.Model))
		req.Pins = pins
		d, err := crewroute.Decide(req)
		if err == nil {
			// The rescues rode in as pins to be seated; they are not the person's.
			for i := range d.Crew {
				if _, was := req.Pins[d.Crew[i].Seat]; was && !pinnedBefore(d.Crew[i].Seat, pins, rescued) {
					d.Crew[i].Pinned = false
				}
			}
			d.Note = strings.TrimSpace(d.Note + " running on fallback crew · " + strings.Join(rescued, ", "))
			return d, nil
		}
		if !errors.As(err, &missing) {
			return crewroute.Decision{}, err
		}
	}
	return crewroute.Decision{}, ErrCrewUnreachable{Action: health.crewAction()}
}
