package config

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/modelsource"
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
	// ReadAt is when the account was read. A positive balance lifts a payment
	// refusal only when it was read after that refusal; a reading with no
	// time lifts nothing.
	ReadAt time.Time
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
	// lowBalance are providers whose balance was READ as low before any call
	// ([CreditsLowAt]). They stay unaffordable while a task is probing: the
	// reading is taken again at every launch while it is low
	// ([CreditsNeedRead]), so a top-up is noticed without a refused call.
	lowBalance map[string]bool
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
	now := time.Now()
	// A refusal of a credential no longer in force, or of credit since found,
	// holds nothing back ([crewHeldAgainstNow]).
	h := crewHealthOf(crewHeldAgainstNow(profileDir, history, now), now)
	// With free-route opt-in, a low OpenRouter balance steers automatic seats
	// toward free routes. Without it, a small positive balance remains usable
	// until a real payment refusal or known-zero account says otherwise.
	if useFreeDefaultsAt(profileDir) {
		h.unaffordable[modelsource.DefaultID] = true
		h.lowBalance = map[string]bool{modelsource.DefaultID: true}
	}
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
// routed under, so its first call asks them again ([RouteCrew]). An account
// whose balance was read as low is not put on trial ([crewHealth.lowBalance]).
func (h crewHealth) probing() crewHealth {
	h.unaffordable, h.disconnected = map[string]bool{}, map[string]bool{}
	for id := range h.lowBalance {
		h.unaffordable[id] = true
	}
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
	if send == "" {
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

// CrewRouteAnswers is whether route health expects send's route to answer:
// not quarantined or cooling, not on an account out of credit, not on a
// provider whose key was refused. A profile with nothing wrong on it answers
// at once, without reading its providers.
func CrewRouteAnswers(profileDir, send string) bool {
	send = strings.TrimSpace(send)
	if send == "" {
		return true
	}
	health := crewHealthCached(profileDir)
	if len(health.blocked) == 0 && len(health.unaffordable) == 0 && len(health.disconnected) == 0 {
		return true
	}
	return health.answers(send, CrewProvidersAt(profileDir))
}

// answers is whether this health expects send's route to answer.
func (h crewHealth) answers(send string, providers []CrewProvider) bool {
	if _, blocked := h.blocked[send]; blocked {
		return false
	}
	pin := resolveCrewPin(CrewPin{Model: send}, providers)
	return !h.disconnected[pin.Provider] && !(pin.Kind == crewroute.Metered && h.unaffordable[pin.Provider])
}

// forgetCrewHealth drops the cached route health.
func forgetCrewHealth() {
	crewHealthCache.mu.Lock()
	crewHealthCache.at = time.Time{}
	crewHealthCache.mu.Unlock()
}

// crewHealthCached is [crewHealthAt] read at most once a few seconds per
// profile: auxiliary calls ask it on every request, and the router's log is a
// file.
func crewHealthCached(profileDir string) crewHealth {
	crewHealthCache.mu.Lock()
	defer crewHealthCache.mu.Unlock()
	generation := SettingsGeneration()
	if crewHealthCache.dir == profileDir && crewHealthCache.generation == generation && time.Since(crewHealthCache.at) < crewHealthFresh {
		return crewHealthCache.health
	}
	crewHealthCache.dir, crewHealthCache.at, crewHealthCache.health = profileDir, time.Now(), crewHealthAt(profileDir)
	crewHealthCache.generation = generation
	return crewHealthCache.health
}

// crewHealthFresh is how long a cached health reading stands.
const crewHealthFresh = 3 * time.Second

var crewHealthCache struct {
	mu     sync.Mutex
	dir    string
	at     time.Time
	health crewHealth
	// generation is the settings generation it was read at: a key written, a
	// key the app handed over, a credit reading, is seen by the next call.
	generation uint64
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

// crewCandidatesNoticed keeps the automatic candidate set within the person's
// free-route choice, even when every paid account is out of credit.
func crewCandidatesNoticed(profileDir string, health crewHealth) ([]crewroute.Candidate, string) {
	rule, providers, off := CrewAllowedAt(profileDir), CrewProvidersAt(profileDir), CrewProvidersOffAt(profileDir)
	facts := crewRouteFacts{free: CrewFreeRoutesAt(profileDir), health: health}
	candidates := off.Candidates(crewCandidatesWith(rule, providers, facts))
	if facts.free && len(health.unaffordable) > 0 && onlyFreeRoutes(candidates) {
		return candidates, "free routes in use (may log prompts) · credit unavailable on " + strings.Join(crewSet(health.unaffordable), ", ")
	}
	return candidates, ""
}

// A notice names free routes only when every available route is free; an
// empty candidate set or a subscription/local alternative is not free use.
func onlyFreeRoutes(candidates []crewroute.Candidate) bool {
	found := false
	for _, c := range candidates {
		for _, r := range c.Routes {
			if r.Kind != crewroute.Free {
				return false
			}
			found = true
		}
	}
	return found
}

// AutomaticCrewRungs excludes free pools from automatic substitution while the
// switch is off. An explicit pin is resolved separately and is never rewritten.
// Check again when selecting a saved ladder: a task can outlive a settings edit.
func AutomaticCrewRungs(profileDir string, rungs []crewroute.Pick) []crewroute.Pick {
	if CrewFreeRoutesAt(profileDir) {
		return rungs
	}
	out := make([]crewroute.Pick, 0, len(rungs))
	for _, rung := range rungs {
		if rung.Kind != crewroute.Free && !crewroute.IsFree(rung.Send) {
			out = append(out, rung)
		}
	}
	return out
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
//
// IT KNOWS NOTHING OF THE CREDENTIAL THE CALL WAS SENT WITH, and does not guess
// it from the settings in force now: a key changed while the call was in
// flight would be blamed for the old key's refusal. Its refusals are written
// unprinted, and lifted only by a credential change recorded after them
// ([crewCredentialsSeen]); a caller that took the [CrewDispatch] fact when it
// sent the call writes it with [LogCrewRouteSent].
func LogCrewRoute(profileDir, call string, d crewroute.Decision, repo, title string, seat crewroute.Seat, pick crewroute.Pick, kind string, until time.Time) {
	LogCrewRouteSent(profileDir, call, d, repo, title, seat, pick, kind, until, CrewDispatch{})
}

// LogCrewRouteSent is [LogCrewRoute] for a call whose [CrewDispatch] fact was
// taken when it was sent: a refusal is written against the print of the
// credential that call carried, and holds nothing against any other.
func LogCrewRouteSent(profileDir, call string, d crewroute.Decision, repo, title string, seat crewroute.Seat, pick crewroute.Pick, kind string, until time.Time, sent CrewDispatch) {
	if kind != "" {
		// A FAILURE IS SEEN BY THE VERY NEXT CALL, not a few seconds later:
		// the cache that spares every request a read of the log is dropped.
		forgetCrewHealth()
	}
	outcome := router.CrewRouteOutcome{
		Seat: string(seat), Send: pick.Send, Provider: pick.Provider,
		Paid: pick.Kind == crewroute.Metered, Kind: kind, Until: until,
	}
	if crewCredentialRefusal(kind) {
		// THE REFUSAL IS WRITTEN AGAINST THE CREDENTIAL IT REFUSED — the print
		// taken when the call was sent, never the credential itself. Before
		// it is written, a change of credential since the last reading is
		// recorded, so it is dated before this refusal and never mistaken for
		// a reconnect after it.
		outcome.Cred = sent.Cred
		crewCredentialsSeen(profileDir, []string{pick.Provider}, time.Now())
	}
	router.LogCrewRoute(ProfilePath(profileDir, ""), call, CrewRecordOf(d, repo, title), outcome)
}

// CrewRescue is the last of a seat's ladder, after everything the router
// offered: the seat as the last crew that completed a task on this install
// ran it, then the model the person is talking to — each only when its route
// is healthy and the model can sit the seat. The router's own rungs come first
// ([crewroute.Decision.Ladder]).
func CrewRescue(profileDir string, class crewroute.Class, seat crewroute.Seat, chatModel string) []crewroute.Pick {
	health := crewHealthAt(profileDir)
	rule := CrewAllowedAt(profileDir)
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
		if pin.Kind == crewroute.Free && !CrewFreeRoutesAt(profileDir) {
			return
		}
		if health.disconnected[pin.Provider] || (pin.Kind == crewroute.Metered && health.unaffordable[pin.Provider]) {
			return
		}
		if model, known := crewCatalogModel(send); known && !crewroute.Seatable(seat, crewroute.Candidate{Model: model, Routes: []crewroute.Route{{Send: pin.Send}}}) {
			return
		}
		// NO RESCUE OUTSIDE THE ALLOWED MODELS: the last crew that worked and
		// the person's own model are rescues only where the rule admits them
		// on the route they would run.
		if !crewPinAdmitted(rule, send, pin.Provider) {
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

// crewFreeRescue offers the best few allowed free pools only after explicit
// free-route opt-in. An empty paid balance never grants that permission.
func crewFreeRescue(profileDir string, class crewroute.Class, seat crewroute.Seat, health crewHealth) []crewroute.Pick {
	if !CrewFreeRoutesAt(profileDir) {
		return nil
	}
	candidates, _ := crewCandidatesNoticed(profileDir, health)
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
// formed with that seat on its rescue: the last good crew's model, then a free
// pool of an allowed model — ONLY EVER A MODEL THE ALLOWED MODELS ADMIT.
//
// THE PERSON'S OWN CHAT MODEL IS NOT A RESCUE AT ADMISSION. A role with nothing
// allowed to run on is a question for the person ("allow a model, or pin
// one"), answered with a typed refusal naming the role and why each model was
// turned away ([crewroute.NoEligibleError]) — not a task quietly started on a
// model they never allowed for it. (Inside a running task the seat's ladder may
// still reach the chat model when the rule admits it: [CrewRescue].)
func crewRescued(profileDir string, req crewroute.Request, health crewHealth, cause error) (crewroute.Decision, error) {
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
		rescue := CrewRescue(profileDir, req.Class, missing.Seat, "")
		if len(rescue) == 0 {
			return crewroute.Decision{}, crewNoEligible(profileDir, missing.Seat, ErrCrewUnreachable{Action: health.crewAction()})
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
	return crewroute.Decision{}, crewNoEligible(profileDir, missing.Seat, ErrCrewUnreachable{Action: health.crewAction()})
}
