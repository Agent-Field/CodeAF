# Decisions

Dated decisions taken without waiting for the designer. Each section is titled with its plandb task id.

## t-d5-be-key-set-decision: entering or replacing the provider key in Settings (2026-10-10)

**Decision: v1 does not set or replace the provider key from the app.** The engine builds no write route and the UI draws no key field. Covers BE-SET-07 (Shell IX S-IX-12).

Why:

- The design's Settings "engine connection" row only states where the key comes from. It draws no input, and the emptiness law forbids inventing one.
- `config.writeProfileValues` (`internal/config/budget.go`) can write `api_key`, but the key ladder in `apiKeyResolution` (`internal/config/apikey.go`) puts the `OPENROUTER_API_KEY` variable first. A key typed into the app would be silently ignored on any machine where the variable is set, so the field could say "set" while the engine uses another key.
- A write path puts a secret through the renderer, the bridge and the mock engine. The brief forbids secrets in renderer storage, logs and URLs, and the first-run setup in the terminal already owns key entry.

What ships instead (BE-SET-04, `t-d5-be-key-status`): a read-only row from `APIKeySourceAt`, giving `{source, present}` and never the value. It reads "OpenRouter variable", "Profile", "OpenAI variable", or draws nothing when no key is found.

Recommendation if the designer later wants entry:

1. Write only to the profile `api_key` row through `writeProfileValues`, never to the environment.
2. Add a write-only field. After saving it clears and shows a "Key set" mask with a "Replace" action. The value is never read back, shown, logged, or kept in renderer state beyond the submit.
3. If `source` is not "Profile", the field says that a variable overrides the profile key, so a saved key cannot look active when it is not.
4. Offer "Remove key" through `removeProfileKey`.

## t-d5-be-pg-suggest-decision: cluster, filing-offer and "Move them" suggestions (2026-10-10)

**Decision: v1 builds no engine path and draws no UI for any of the three suggestions until the designer answers.** Covers BE-PL-46 (Places §6e P-6e-16, P-6e-17, §8c P-8c-10, P-8c-11, §6d P-6d-2c). The design fixes the numbers (5 or more chats cluster; at most one line after the first reply; only existing places are offered) but names no model role, no similarity signal, and no surface for the filing offer.

What exists already: `config.desktoproles.go` registers the roles `placefiling` (`roles.RolePlaceFile`: picks an existing place, never creates one) and `placesuggest` (`roles.RolePlaceSuggest`: names a group and offers it as a new place, for approval). Those are Settings rows only; nothing calls them.

Recommendation, when the designer answers:

1. **Role.** Cluster suggestion and "Move them" use `RolePlaceSuggest`; the filing offer uses `RolePlaceFile`. Neither role runs on the chat's conversation model, and neither runs without a configured cheap role model.
2. **Cluster threshold.** The signal is shared workspace first: 5 or more unplaced chats whose recorded working folder is the same repository root. Shared words are a fallback only for chats with no workspace, and a model-named group of fewer than 5 is dropped. The model names the group; it never decides membership by itself. A dismissed set is not offered again until it grows by 3 chats.
3. **Filing offer.** A transcript line after the first reply, not a header affordance. A header affordance would be a persistent control for a one-time offer and would add colour or chrome the design forbids. The line is dim, one sentence, with "Move" and "Not now", names an existing place only, and stays in the transcript as a record. Never offered in a chat already in a place.
4. **"Move them" on the unplaced root.** One quiet line at the root ("Not in any place"), showing the cluster's count and place name, with "Move them" as a single action. It moves members through the existing membership write and is undoable; it never creates a place without the person approving the name.
5. **Silence.** With no role model configured, fewer than 5 chats, or an unavailable engine, nothing is drawn (emptiness law). No placeholder, no count of zero.

Until answered the app ships none of this; the Settings rows stay informational.
