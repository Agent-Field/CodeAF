# Settings and first launch in the codeaf app

## App model choices — default model, pinned models and effort

The app reads `GET /app/v1/models`: available models, the saved default and
three pinned slots. `PUT /app/v1/models/default` takes `{model, effort?}`;
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
`PATCH /app/v1/settings` takes flat dotted keys and validates the entire
patch before saving. Money is `{usd:"20.00"}`, with two decimal places.
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
Live enforcement of the new app permission and factory settings arrives with
those resource routes.
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
`waiting`, `signed_in`, `failed` or `canceled`; DELETE cancels the wait.
Engine exit also closes the callback listener.

The native app claims the credential once with
`POST /app/v1/providers/openrouter/sign-in/claim {}`, then saves it in
Keychain or Secret Service and hands it back on the next launch.
The authenticated claim response is not cached by HTTP. Ordinary GETs
never return a key. A concurrent sign-in returns 409; other providers
return `this provider has no app browser sign-in`. Paste a key where that
provider's key route is supported. Codex browser credentials are not accepted
by the app key API.
A key replacement or removal while the browser waits wins over its later
completion. No browser credential is saved to the binary's home.
