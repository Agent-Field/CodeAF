---
kind: fixed
title: a machine that refuses a model is struck at once, dropped from the pin, and drawn as refused
pr: 368
surface: [chat, engine]
invalidates:
  - "A 4xx the router answered for ITSELF struck no lane. `Client.refuseUpstream` required
    `error.metadata.provider_name`, which OpenRouter sends only when it is relaying somebody
    else's refusal, so the one refusal class that names a machine with certainty —
    `your request's provider.only preference permits only: coreweave` — was structurally
    exempt. It now strikes: the lane a refusal is about comes from the request's own
    `provider.only`, and a router refusal against a demanded machine also writes that
    machine out of `internal/lane`'s serving set for the model."
  - "The endpoint-refusal ladder's first rung dropped `provider.require_parameters`,
    `provider.ignore` and `provider.max_price`. It now drops the WHOLE provider object,
    `provider.only` and `provider.allow_fallbacks` included, and it is offered whenever a
    demand is on the wire — including one a rescue added after the ledger's own preferences
    were assembled, which a pinned request never used to get a first rung for at all."
  - "`internal/lane`'s candidate gate read only the endpoints sheet. `lane.Serves(model, lane)`
    is now a second, negative half of the serving set, written by the transport when the wire
    refuses and read by `capable` — so a lane the router will not serve this model from leaves
    the frontier rather than being ranked and picked again."
  - "`provider.HedgeReport.OnHedgeStart` took `func(alt string)`. It now takes
    `func(provider.RescueNews)`, carrying the reason and a retraction, and it fires a second
    time when the machine a rescue went to fails."
  - "The status row said `· slow · trying X…` for every rescue, including refusals, and never
    withdrew the claim. It now says `· refused · trying X…` when the wire refused, and
    `· X refused` when X itself refuses — the promise is taken back rather than left standing
    until a ten-minute window ages it out."
  - "`internal/lane/lanestub` served its sheet and its completions from one slice, so the
    sheet-versus-router disagreement could not be staged. `lanestub.Lane.SheetOnly` publishes a
    lane on the endpoints page that `pick` refuses with the router's real `permits only:` body,
    and `lanestub.Server.RouterURL` returns a base URL this build recognises as a router, which
    is what makes the whole frontier reachable from an `AFORGE_BASE_URL` run."
---

A measured chat run collected six HTTP 404s meaning "the machine your request demanded
cannot serve this model", and not one of them could act. Three pieces of code classified
the same refusal three ways and each was told a different half: the strike wanted an
upstream name the router omits when it answers for itself, the ladder's first rung was
offered for three fields that did not include the demand, and nothing wrote the refusal
back — so the same machine was chosen three separate times in one run and the whole thing
was drawn as a sentence about a wait.

There is now ONE classifier, in `internal/provider/refusalobject.go`, returning what a
refusal is, which machine it is about, and whether that machine is finished for this
model. The machine comes from the `provider.only` this process wrote itself and never
from the router's prose, so a reworded refusal changes nothing. Three readers consume it:
the strike, the ladder and the race's walk, and the status row.

The deferred half is issue #266's fourth part — the session emits no request-sent event,
so the surface infers the wait clock — which is filed separately and lands in
`internal/tui3/app.go`.
