package crewroute

import "strings"

// WHICH PROVIDERS THE CREW MAY ROUTE THROUGH — A SET BESIDE THE RULE, NOT IN IT.
//
// The allowed rule ([Allowed]) says which MODELS a seat may be picked from. The
// providers a person turned off say which ROUTES may carry them, and the two
// are kept apart on purpose: a `-x` in the rule naming a provider would also
// read as a vendor (`-openai` takes OpenAI's models away, not just a route),
// and stepping the rule onto a new base drops every exception it had, so a
// provider switched off there would switch itself back on the next time the
// models row was walked.
//
// THE SET IS OF PROVIDERS TURNED OFF, never of providers turned on, so a
// provider connected tomorrow is on the day it is connected — the same thing
// connecting one has always meant — and nobody has to remember a second place
// to switch it on.
//
// The filter is the last step before a model becomes a candidate: a model's
// routes through providers that are off are taken away, and a model left with
// no route is no candidate, however cheap — exactly the verdict a model no
// connected provider reaches already gets.

// ProvidersOff is the set of provider ids a person turned off, lower case.
// The zero value is every provider on.
type ProvidersOff map[string]bool

// On says whether a provider's routes may be used.
func (off ProvidersOff) On(provider string) bool {
	return !off[strings.ToLower(strings.TrimSpace(provider))]
}

// Routes is the routes a provider that is on carries, in their own order.
func (off ProvidersOff) Routes(routes []Route) []Route {
	if len(off) == 0 {
		return routes
	}
	var out []Route
	for _, r := range routes {
		if off.On(r.Provider) {
			out = append(out, r)
		}
	}
	return out
}

// Candidates is the candidates with the routes of every provider that is off
// taken away, and every candidate left with no route dropped. The route order
// each candidate came with — plans and local first, the default service last —
// is kept, because it is the order a tie between routes of equal cost is
// broken in, and a filter is no reason to break it differently.
func (off ProvidersOff) Candidates(candidates []Candidate) []Candidate {
	if len(off) == 0 {
		return candidates
	}
	var out []Candidate
	for _, c := range candidates {
		routes := off.Routes(c.Routes)
		if len(routes) == 0 {
			continue
		}
		out = append(out, Candidate{Model: c.Model, Routes: routes})
	}
	return out
}
