# Custom connections: named, multiple, switchable (#1089)

Design note pinning the interfaces the #1089 work shares, so the parts that run in
parallel cannot drift. The code must match this; if it cannot, correct this page in
the same change.

## The problem

A custom connection is persisted today as one row whose `PersistedSource.ID` is the
literal `"custom"`. `WriteSources` clears `Address` for every row whose id is not
`custom`, `resolveSources` only recognises ids that `modelsource.Vendored()` lists,
and `persistConnectedSource` upserts by id. The consequence: a second custom
connection replaces the first, and the id carries no information about which
connection it is.

## (a) Identity and resolution

One predicate, `modelsource.IsCustomID(id)` (`id == "custom"` or prefix `custom-`),
replaces every `== "custom"` comparison (at least `internal/config/sources.go:83`,
`sources.go:167`, `internal/tui3/modelservices.go:168/191/225`).

The first custom connection keeps the id `"custom"`, so existing profiles and tests
stay byte-identical. Later instances mint `custom-<slug>` with a numeric tiebreak.

The load-bearing line is in `resolveSources`: an instance id resolves to the vendored
`custom` Source as a TEMPLATE, and the resolver then stamps
`Connected.Source.ID = row.ID`. Without that stamp every instance collapses into one
`Connected`, and `Set.ByID`, `modelServiceRows`, `prepareModelServices`,
`reconnectModelService`, `cyclePlanPause` and `DisconnectService` all see one
connection where the person has two.

## (b) One validate-and-persist path

`ConnectService` already owns validation, the probe, `Collides` and persistence. It
stays the one path; the /connect flow and the settings Providers tab both call it,
neither reimplements it.

New: `config.PrepareCustomSource(profileDir, address, written) PersistedSource` mints
the instance id, defaults `Written` from the host slug, and sets `Order`. Both doors
call `PrepareCustomSource`, then `ConnectService`.

The slug derivation moves from `modelServiceSlug` (`internal/tui3/modelservices.go:331`)
to `modelsource.SourceSlug`, so config and both surfaces share one derivation. The
move fixes the IP-literal defect: `127.0.0.1` currently yields `"0"`, and will yield
`127-0-0-1`.

## (c) The active connection is derived, never stored

Routing already keys on the `Written` prefix of a model id (`Set.For` splits
`written/bare`), so a stored `active` key would be a second source of truth that can
disagree with the model actually in use.

- READ: `config.ActiveCustomSource(profileDir, sources)` splits the conversation
  slot's model against the instances' `Written` names.
- WRITE: the Providers tab switcher rewrites the slot's model to
  `<written>/<preferred>` through the same config write the /model picker uses.

No new profile key.

## (d) Model grouping is already half built

`Model.Group` / `GroupOrder`, plus `a.sourceModels` keyed by `Connected.Source.ID`,
mean each instance gets its own cache, group and heading once (a)'s stamp lands. The
group heading is the person's own name for the connection; `GroupOrder` is the
persisted row's `Order`.

## Hazards

1. **id versus Written is the whole design hazard.** Persistence keys on id; model
   routing keys on `Written`. Any path that conflates them aliases two instances into
   one. Test both.
2. **The slug move crosses the partition** (it lands in `internal/config` /
   `internal/modelsource`, but the surface calls it). It is safe only because the
   surface work depends on the backend work.
3. **Renaming a connection re-prefixes every already-picked model id.** After a
   rename, `mybox/...` rows strand onto the default service; the rename flow must
   carry that.

## The wire path (t-wire)

Refusal detection sits four deep in `internal/provider/prefcarry.go:328`:
`prefsMayBeRefused = prefSent && carriesPreferences && !prefsProven && prefRefused(status, payload)`.
A custom base has no lanes and no endpoints sheet. Establish first whether a provider
object was ever sent for this base at all: if it was not, the defect is upstream in
why it was sent, not in `widenPastTheUncarriedPreference`'s reach. Do not widen a
retry that never runs.
