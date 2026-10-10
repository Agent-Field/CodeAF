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
