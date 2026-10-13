# Settings and first launch in the codeaf app

## App model choices — default model, pinned models and effort

The app reads `GET /app/v1/models`: available models, the saved default and
three pinned slots. ChatGPT browser sign-in connects the native `codex/`
models; its refreshable credentials stay in app memory. `PUT /app/v1/models/default` takes `{model, effort?}`;
`PUT /app/v1/models/pinned` takes `{models:[id,id,id]}`. An empty string is
an unused pinned slot. Nonempty slots must be distinct available models.
The default is for new agents; changing it does not move existing agents.

App effort is `low`, `medium` or `high`, with `medium` for a fresh app.
App writes refuse `xhigh`, `max` and `auto`. Higher terminal choices appear
as `high` in the app; an automatic terminal choice appears as `medium`.
Only `checks` and `planner` appear at `GET /app/v1/models/roles`.
`PUT /app/v1/models/roles/{role}` takes `{model, effort}`; an empty model
clears the explicit choice. These choices write the engine's existing role
pins; per-agent default inheritance is supplied by the turn-engine lane.

Models keep the order their provider returned. No model is ranked by AI.
A successful key check's listing is reused for fifteen minutes. Stdin keys
fetch their listing on the first model read. Direct provider ids carry the
connection name. A disconnected saved default or pin remains visible as
`available:false`. A checked key whose provider has no engine transport,
including a native Anthropic key, also has unavailable models. Selecting
an unavailable model is refused with
`that model is not available from a connected provider`.

## App binary settings — saved values and stale changes

`GET /app/v1/settings` returns only binary-owned, non-secret settings.
`PATCH /app/v1/settings` takes `{changes:{"factory.done_days":7}, expect?:{version}}`
or the original flat dotted keys, and validates the entire patch before saving. Money is `{usd:"20.00"}`, with two decimal places.
An unknown key, a credential key or an app theme setting is refused.
A mandatory always-ask action cannot be removed: acting in your name,
deleting outside a task, and payments.

Settings, model and setup reads send an opaque version in `ETag`.
A mutation may send `expect:{version}`. A mismatch returns 409 `stale`
with `settings changed; read them again`. Successful writes publish
`settings.changed` with the changed keys. The app and terminal share the
profile; existing terminal keys survive app settings writes.

Stored defaults for a fresh app are `$20.00` per day, `$5.00` per agent,
`$2.00` per task and no per-conversation ceiling. Memory additions and crew
wake are on. The question clock is 15 minutes, proposals 12 seconds, idle
agents 7 days and polling 60 seconds. Context is 160,000 tokens; the work
folder is `~/codeaf/work`. Crew depth is 3 and its share is 50%. Existing
shared-profile choices override these defaults.

Saved choices include budgets, memory additions, app approval mode,
account access, polling, context tokens, work folder, proposal seconds,
idle days and crew defaults. Proposal seconds accept 0–60; 0 disables
that clock. Account choices are `yes`, `ask_first` and `off`, including
`accounts.mcp:<server>`. App approval choices are `ask_risky`,
`ask_everything` and `dont_ask`. The memory-add switch and daily agent budgets are enforced by the engine.
Account action choices reach the native service tools. Factory timeout and local
output settings are available through the factory settings routes.
Theme, windows, notifications and other UI-only settings stay in the app.

## App first launch — resume setup and choose defaults

`GET /app/v1/setup` reports the saved step, model-key presence, default
model, day cap and `complete`. Steps 1–4 are the app welcome; 5 is complete.
`POST /app/v1/setup` with `{step}` advances one step or goes back before
completion. Quit mid-way and the same step returns at the next launch.
A GitHub key alone does not let model setup continue.

Continue from Connect requires a model provider with a usable nonempty
listing. Its first returned model seeds an unset default. While on step 3,
`{step:4, default_model?, day_cap?}` saves Defaults to the same settings
keys. The fresh app day cap is `$20.00`; existing shared-home choices stay.
Invalid defaults never advance setup. Step 4 to 5 completes the welcome;
factory attachment and creation remain separate routes.

Setup POSTs accept `Idempotency-Key`: retries return the original response
within the last 128 keyed requests of this launch. Changed input with the
same key returns 409. Unknown fields, trailing JSON and bodies above 32 KiB
are refused. Completed setup cannot be restarted through this route.

## App provider browser sign-in — waiting, cancel and saving in Keychain

`POST /app/v1/providers/openrouter/sign-in` with `{}` returns
`verification_uri`. Open it in the browser. The binary waits without a
browser deadline, exchanges the return code using PKCE, connects its
memory key and publishes `keys.changed`. GET on the same path reports
`waiting`, `signed_in`, `failed` or `canceled`; before an attempt it reports
`signed_in` for a handed-over credential or `canceled` otherwise. DELETE
cancels the wait and clears the provider credential.
Engine exit also closes the callback listener.

The native app claims the credential once with
`POST /app/v1/providers/openrouter/sign-in/claim {}`, then saves it in
Keychain or Secret Service and hands it back on the next launch.
The authenticated claim response is not cached by HTTP. Ordinary GETs
never return a key. A concurrent sign-in for the same provider returns 409; providers other than
OpenRouter and ChatGPT
return `this provider has no app browser sign-in`. Paste a key where that
provider's key route is supported. ChatGPT browser credentials are not accepted
by the app key API; use `/app/v1/providers/chatgpt/sign-in` instead.
A key replacement or removal while the browser waits wins over its later
completion. No browser credential is saved to the binary's home.

## App accounts — connect Google or Slack, permissions and disconnect

`GET /app/v1/accounts` lists GitHub, Google and Slack without credentials.
`PATCH /app/v1/accounts/{id}` takes `{access:"yes"|"ask_first"|"off"}`.
An unconnected account refuses Yes or Ask first. Off removes its native actions
from use. `PATCH /app/v1/accounts/{id}/actions/{aid}` saves an individual choice.
Sending mail, posting Slack messages and writing calendar events always ask;
their action rows refuse any choice other than `ask_first`.

`POST /app/v1/accounts/{id}/connect` with `{}` returns `connect_url`.
For Google and Slack this starts the ordinary browser grant with a local
callback. It waits at most five minutes. `GET` on the same route polls the
account state. Google currently offers Gmail and Calendar, not Drive.
GitHub's link opens its token settings; the app hands its token to the binary.
`DELETE /app/v1/accounts/{id}` cancels a pending sign-in, forgets the launch's
grant and saves Off. A late callback cannot undo that disconnect.

The app supplies credentials over its private launch pipe. Browser grants and
renewals stay in process memory; the binary never reads or writes the terminal's
credentials.json for app accounts. Account reads and events never return tokens. The first successful connect poll
includes a one-time `credential` payload for the app to save to Keychain. Later
polls omit it; initial launch keys are never exported. The app hands that opaque
payload back under the service id on the next launch.

## App MCP servers — add, access, not answering and remove

`GET /app/v1/mcp` returns saved server addresses, access and tool counts.
`POST /app/v1/mcp` takes `{address}`. Addresses without a scheme use HTTPS,
or HTTP for localhost. Embedded credentials, query strings and fragments are
refused; remote HTTP is refused. A server's real tools list determines
`connected` or `not_answering`, with a two-second limit for each probe.
At most four probes run together and at most 64 servers may be saved.

`PATCH /app/v1/mcp/{id}` takes `{access:"yes"|"ask_first"|"off"}`.
`DELETE /app/v1/mcp/{id}` removes the address and its launch token.
Only addresses and permission choices reach the profile. Tokens are handed
through the app's pipe under `mcp:<id>`. Saved servers join the native tool sources on the next belt rebuild.
Off removes their tools, and removing a server removes that source.
A server must answer before its tools are armed. Adding one does not start
a vendor authorization trip; authenticated servers need an app-supplied token.

## App factory settings — sign out, repository switches and access links

`GET /app/v1/factories/{id}` returns the selected factory's Settings details.
`POST /app/v1/factories/{id}/sign-out` revokes this factory session and forgets
its memory-only token while preserving the attachment. A solo factory cannot
sign out. Repository switches use `PATCH /app/v1/factories/{id}/repos/{rid}`
with `{enabled}`; rid is the URL-encoded owner/name. The factory enforces
operator permission. `POST /app/v1/factories/{id}/links` creates an access link;
its secret URL is returned only at creation. DELETE on that route with `{id}`,
or on `/links/{link}`, revokes it through the factory's operator door.

`PATCH /app/v1/factories/{id}` accepts `runner_timeout_s`, `keep_output_days`
and `keep_output_mb`. Runner silence must be 1–86400 seconds and requires the
factory operator. Its lease and presence reads use the same saved deadline.
Output age is 1–3650 days and size 1–1024 MiB, saved for this Mac. Changing
local retention needs no team operator permission. New app processes use the
saved byte window; completed-log sweeps use the saved age. Factory log reads
and writes trim oldest lines while leaving proof and execution results intact.
Existing running processes keep the window they started with. Output reads
remain bounded to 10 MiB per reply.


## App ChatGPT browser sign-in — claim and disconnect

`POST /app/v1/providers/chatgpt/sign-in {}` returns `verification_uri` for
ChatGPT's existing PKCE browser flow. GET on the same path reports
`waiting`, `signed_in`, `failed` or `canceled`, plus a scrubbed `error`.
The callback checks state before exchanging the code. DELETE cancels the
flow and clears the ChatGPT credential, including a completed connection;
engine exit closes the callback without disconnecting the native store.

`POST /app/v1/providers/chatgpt/sign-in/claim {}` returns
`{provider:"chatgpt",key}` once. The key is an opaque JSON string containing
the complete access, refresh and identity token bundle and account identity.
The native app saves that string in Keychain or Secret Service and sends it
back under `keys.chatgpt` at launch. The binary never writes these tokens to
its profile, including during refresh, and never reads terminal token files
as a fallback. Claims cannot be replayed, even with an `Idempotency-Key`.
OpenRouter claims have the same one-time rule.

## App tidying — how many days finished jobs remain in Running

`GET /app/v1/settings` includes numeric `factory.done_days`, default 1.
`PATCH /app/v1/settings {changes:{"factory.done_days":7}}` saves it.
Whole days from 0 through 3650 are accepted; 0 hides merged jobs immediately.
The app-hosted factory applies this interval from each pull request's
`merged_at` to both queue and individual-job `display.running_visible`.
The upper boundary is exclusive. Tidying changes visibility and keeps job
history; a standalone factory keeps its existing 24-hour interval.

Agent detail reads include `snapshot.asleep` from successful app power events
and `snapshot.day_cap_reached` from the binary's effective cap and measured
calendar-day ledger. Raising a cap or crossing midnight updates the next read.


## App canonical Settings actions — account access, polling and MCP names

`POST /app/v1/accounts/{id}/access {access}` and
`POST /app/v1/accounts/{id}/access/{action_id} {access}` return `{ok:true}`.
Access is `yes`, `ask_first` or `off`; acting in your name still always asks.
`POST /app/v1/accounts/{id}/connect {}` returns `connect_url`.
`POST /app/v1/accounts/{id}/poll {}` reports `connected`, `waiting` or `revoked`.
Polling does not consume the native store's one-time credential; the native
core claims that through GET on the account's existing connect route.
`POST /app/v1/accounts/{id}/disconnect {}` cancels the flow and forgets its grant.

`POST /app/v1/mcp {name?,address,access?}` returns McpServer. Name defaults to
the address host; access defaults to the stored choice or `ask_first`.
All fields are validated before the server and access choice are saved together.
`POST /app/v1/mcp/{id}/access {access}` returns `{ok:true}` and uses the same
native capability policy as the existing PATCH route. Original adapters remain.

## App factory Settings actions — details, repositories, links and runner

`GET /app/v1/factories/{id}/settings` returns identity, sign-in and operator
facts, repositories, links, people, runner timeout and this machine's output
retention. `POST /app/v1/factories/{id}/repos/{repo_id} {enabled}` returns
`{ok:true}`; encode owner/name as one path segment.
`POST /app/v1/factories/{id}/links {}` returns `{id,name,detail,url}` once;
`POST /app/v1/factories/{id}/links/{link_id} {}` revokes that link.
`POST /app/v1/factories/{id}/revoke-sessions {}` revokes factory sessions.
The native factory enforces operator permission for these outward changes.

`POST /app/v1/factories/{id}/advanced {runner_timeout_s}` sets the native
runner silence timeout. `POST /app/v1/factories/{id}/runner` takes
`keep_output_days` and/or `keep_output_mb` for this machine's output retention.
Both return `{ok:true}` and keep the existing range checks. Sign-out uses
`POST /app/v1/factories/{id}/sign-out {}`. A solo factory refuses sign-out
because it belongs to this machine. DELETE on the factory id still leaves it.

## App shutdown and in-flight AI spending

Closing the app cancels its active AI calls and waits for their final spending
receipts before the engine stops. New AI requests during shutdown return
"the engine is stopping". A canceled call can still cost money; its final
receipt remains in Spending.
