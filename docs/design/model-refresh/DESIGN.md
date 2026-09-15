# Refreshing the model list from inside `/model`

Owner ruling 2026-09-10: "see how we can refresh model lists, give them that option".
This file is the copy of record for what was built.

## What was true

The picker never fetched. `Options.Models` was a snapshot of a catalog warmed at launch,
`catalog.TTL` is 24 hours, so a model a provider shipped this morning was not in `/model`
until tomorrow — and nothing on the surface offered to ask again. `catalog.Options.Refresh`
existed for exactly this and nothing in the chat called it.

## What is true now

**One key, inside `/model`, asks the router for today's list; the picker stays usable while
it does; the list it was showing is never taken away.**

1. **The key** is `ctrl+r` — `refreshModelsKey` in `internal/tui3/modelrefresh.go`, spelled
   nowhere else. Its two other meanings (spell it out over a draft, reveal in `/files`) cannot
   fire while the picker owns the keyboard.
2. **Where it is named:** the placeholder's ranked field `ctrl+r refresh`, between
   `ctrl+t effort` and `enter · esc` — on sixty cells `enter · esc` give way first — and the
   empty list's line, `no model matches · ctrl+r fetches the newest list`.
3. **The door** is `tui3.Options.RefreshModels`. Nil is the capability absent: the key does
   nothing and no line names it. Both chat doors implement it with a `v3ModelShelf`
   (`cmd/codeaf/chatv3_modelshelf.go`): one atomic pointer the picker's list, the vision
   gate and the task-model list read, which `catalog.Refresh` refills; the shelf writes
   `~/.codeaf/v3/models.json`. `--host` fetches on the laptop, whose list of names it shows.
4. **While it runs** the list's first line reads `fetching the newest list…`; the fetch is a
   command off the loop with the catalog's own fifteen-second ceiling; every key still works;
   a second press does nothing.
5. **When it lands** the list is re-ranked with the filter kept and the cursor on the model
   in use, and the conversation gets `models · 612 · 9 new · a, b, c` or
   `models · 612 · nothing new`. A model that vanished is not mentioned.
6. **When it fails** nothing on the shelf or the picker changes; the note is
   `could not fetch the model list · <the transport's reason, one line>` and the key is
   offered again. `catalog.Refresh` is `Load` plus the error `Load` drops on purpose.
7. **`codeaf models --refresh`** does the same for a script, and says the same sentence on
   stderr when the fetch fails.

## Not done

- The picker does not show the list's age. `FetchedAt` travels with the answer and is used
  only to refuse rows nobody fetched; a dated placeholder would spend the cells the keys need.
- The settings panel's model rows, home's model list and the `alt+o model` list do not
  offer the key. They are the same `picker` type on their own keyboards; wiring them is a
  follow-up, not a flag — and until then `refresh` stays false there, so nothing names it.
- `ContextWindowFor`, `NearestModels` and the price/parameter seams still read the launch
  catalog, so a model that exists only in a refreshed list is answered by the session's
  defaults for those until the next launch (which opens on the refreshed cache).
