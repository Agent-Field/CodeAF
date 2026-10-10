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

## t-d5-be-terminal-persist-decision: terminals and job logs across a bridge restart (2026-10-10)

**Decision: v1 adds no disk persistence or restoration for bridge-owned terminals or job logs.** Existing in-memory retention stays; the engine builds no recovery path and the UI draws no recovered output, status or log action. Covers BE-TERM-08. The BE-TERM-08 row in DESIGN-QUESTIONS.md is ready for the integrator to paste into **Open: for the designer**.

Evidence and scope:

- Shell §3c draws a terminal/job output field, a live header and “Ask codeaf about this output”. Components says “A finished job keeps its log until you Remove it”; Q4 confirms Close keeps the log and list entry. Neither specifies restart recovery. Q6 fixes 16 live terminals per conversation and 512KB scrollback each, not disk retention. Interactions and I2.1–I2.14 add no restart retention rule.
- `internal/desktopbridge/terminal.go` owns a `terminalSet` on each bridge conversation and stores output in `terminal.buf`, capped by `scrollbackBytes`. There is no disk writer or loader for these records; `closeAll` ends the processes when the bridge goes away. `terminal_routes.go` keeps a closed job but removes a closed interactive terminal; Remove drops the record and its buffer. A renderer reload can replay a retained terminal by ID while the same bridge lives. A bridge restart cannot.
- `desktop/src/features/terminal/useTerminalFeed.ts` reattaches by stored session/terminal identity and handles a missing record as “This terminal is gone.” `TerminalPane.tsx` supplies no Open log handler without a real log file. That truthful missing-record behavior remains; “draws nothing” applies to invented recovered data, not to hiding the existing missing-terminal explanation.
- Canonical background jobs are a separate data path: `internal/session/jobs.go` already spools bounded output via `droppingsDir` in `landing.go`, normally under the session's `logs/jobs/`. `sweep.go` defines the seven-day TTL and delegates job-log cleanup to ownership-aware retention. Existing canonical logs are not removed or made memory-only by this decision, and a file on disk alone does not establish a restored desktop job record.

**Recommendation for designer review: finished desktop job logs should survive restart with bounded retention under the session's `logs/`.** Closing a view should not make a recorded failure disappear; the session already owns temporary logs and their cleanup. However, silently expiring a log would contradict “until you Remove it”, so the seven-day limit needs an explicit designer answer before implementation. Memory-only is the conservative shipping assumption, not a claim that seven-day persistence already works.

If approved, the follow-through is:

1. Persist only real finished-job output and enough recorded metadata to identify and display it (stable job/session identity, title, command, working directory, recorded exit status and start/end times). Bound output; do not equate the 512KB scrollback limit with an approved full-log disk budget.
2. Store logs beside their canonical session, never in the borrowed project or renderer storage. Reuse the session's existing retention rules and seven-day sweep based on file modification time; opening a log must not refresh that age. Use owned paths and the existing protections against symlinks and active-writer cleanup.
3. Close detaches and retains; Remove explicitly deletes the retained job and log. Offer Open log only when the engine reports an available file. Expired or missing files provide no recovered output or invented exit status; the designer must specify how an already-saved tab explains expiry.
4. Restore a finished job as read-only recorded output. Do not restore interactive PTYs, automatically rerun commands, or turn a job interrupted by shutdown into a fabricated successful completion. Run again remains an explicit new job.

No code or user-visible feature changes are needed for this research task. No manual page changes are needed: this decision records the current boundary and a proposed future capability, not a capability the app offers.
