# Services — the places models come from

## Add a key — connect a service, add an api key for another provider, use a different model service

An api key for another provider, or another model service, is added here. Open `/connect` or
`/connections`. The `models` group lists DeepSeek, Z.ai, Moonshot, MiniMax, Alibaba Qwen, Codex,
Ollama and **Custom OpenAI-compatible API**, followed by any service already connected and, once
a custom connection is connected, an `add custom connection` row. Codex says `browser`; it signs
in a ChatGPT plan instead of asking for an API key. Ollama needs no key. The other named vendors
ask for theirs.
Pick a row and answer its fields. A successful listed service says
`deepseek-direct is connected · 6 models`; one without a list says only
`deepseek-direct is connected`. A service with more than one billing door names the one it
bound: `z-ai-direct is connected · coding plan · 4 models` or
`z-ai-direct is connected · pay-as-you-go · 10 models`. The Providers tab in `/settings`
then shows the service, door, safe spelling of its key, region and order.

The default service remains first. With two or more services, `/model` groups models by
service in that order; with only the default service, the picker remains ungrouped.

## Using codeaf with only a direct service — no OpenRouter key at all

Yes. When the conversation is on a model from a connected service, that service can carry
the turn without an OpenRouter key. Pressing `enter` sends the message; the setup screen
does not open, and codeaf does not show
`openrouter is not connected · enter on your message connects in a browser, or export OPENROUTER_API_KEY`.
Ollama counts as connected without a key because its local service explicitly needs none.

The crew follows the same road. Its small background calls — naming a session, titling a
task, the reflex and the judges — normally use the configured crew models. If one of those
models belongs to the default service and that service has no key, the call instead uses
the conversation's model on the connected service. Tools, tasks and child agents launched
from that turn inherit the same rule, so none of them makes an OpenRouter request. If the
default service does have a key, the crew keeps using its configured models as usual.

## What model do I get after connecting a service — why did my model change

A successful connection from `/connect`, or a reconnect from the Providers tab in
`/settings`, moves this conversation onto that service in the same moment. A plan door's
first documented model wins. Otherwise codeaf uses the vendor's preferred model when the
service listed it or published no list, then the first model the service listed. With no
preferred or listed model there is no move and no extra sentence.

For example, the connection line
`z-ai-direct is connected · coding plan · 4 models` is followed by
`this conversation was on ~deepseek/deepseek-v4-flash-latest · it is now on z-ai-direct/glm-5.3`.
The status line and `/model` show the new model at once. The previous model is named so
opening `/model` and choosing it once takes you back; `/model` is also how to go somewhere
else. If a turn is answering, the connection lands immediately but the model move waits
until that answer ends, so the model does not change under a sentence already streaming.

When the key is a variable, the receipt adds, for example,
`the engine process reads $DEEPSEEK_API_KEY from its own environment`. The daemon keeps
the environment it started with. If it started before that variable existed, run
`codeaf engine --stop --workspace <dir>` and launch codeaf again so the new engine reads
the variable.

`--no-host` has the same immediate result inside its one process. Under `--host` or
`--at`, connecting a service is absent because the profile behind the conversation is
not the local profile the panel could write.

## Use my own DeepSeek key — connecting DeepSeek, GLM, Kimi, Qwen or MiniMax directly

Open `/connect` and choose the vendor in the `models` group. DeepSeek and MiniMax open
`your key` directly. Z.ai, Moonshot and Alibaba Qwen first open `your region` as a
choice with `International` under the cursor and `China` below it; a region is never
typed. Up and down, or `ctrl+p` and `ctrl+n`, move the cursor. A letter jumps to a
region whose name starts with it, enter takes the row under the cursor and opens
`your key`, and esc returns to the service row with nothing saved. The same choice
opens when reconnecting one of these services from its Providers row in `/settings`.
Z.ai is the direct service for GLM and Moonshot is the direct service for Kimi.
MiniMax, Ollama and **Custom OpenAI-compatible API** are single-door services. MiniMax makes no plan
claim because its plan and metered traffic currently have no wire-level difference
codeaf can use to prove which balance answered.

A service name cannot be confused with the author part of a model already on the default
service. When `deepseek` is already an author there, codeaf connects the direct service
under `deepseek-direct` in that same attempt. The region and key are not asked for twice,
and its models read `deepseek-direct/<model id>`.

## Why is my service called z-ai-direct — I connected Z.ai, the name changed

A service may not be written with a name the default service already uses for a model
author. codeaf appends `-direct` and finishes the connection in the same attempt, so the
region and key are not asked for twice. The connect line tells you the name it used, for
example `z-ai-direct is connected · coding plan · 4 models`, and those models read
`z-ai-direct/<model id>`. DeepSeek follows the same rule: it becomes `deepseek-direct`,
and its models read `deepseek-direct/<model id>`.

## Connect a service — what is asked for, and what codeaf checks before it saves anything

Open `/connect` and choose a row in `models`. DeepSeek asks for `your key`. Z.ai,
Moonshot and Alibaba Qwen ask `your region` with one row per region: `International`
is first and starts under the cursor, then `China`. Up and down, or `ctrl+p` and
`ctrl+n`, move the cursor; a letter jumps to a region whose name starts with it;
enter takes the row under the cursor and then opens `your key`; esc backs out with
nothing saved. The region is a choice and cannot be typed. Ollama asks for nothing.
**Custom OpenAI-compatible API** asks for `your base url` and then `your key`. A key may also be the
name of an environment variable, such as `$DEEPSEEK_API_KEY`.

A key with the wrong shape is stopped before any call:
`that is not the shape of a deepseek key — they start with sk-`. A refusal carries the
service's own answer, cut at 120 characters on a word boundary:
`deepseek refused that key — Authentication Fails, Your api key is invalid`. No answer is
different: `deepseek did not answer · nothing was saved`.

codeaf checks each billing door in order with a one-token completion and binds the first
one that answers. A no-plan or payment answer on one door moves the connection check to
the next; a bad-key answer stops immediately in the vendor's words. A missing answer also
moves to the next door. If no door answers, codeaf saves nothing. Key-prefix hints may
change which door is tried first, but never skip a door.

A spent plan window proves that plan door works: codeaf binds it, stops before the
pay-as-you-go door, and says `plan paused`. It does not make a paid probe or change the
saved billing door. When the vendor supplies a reset time, the connected line carries
the same sentence used during a turn, for example
`plan paused · resets at 18:30 UTC · /connect can switch to pay-as-you-go`.

The bound door is saved and every later request uses it. codeaf does not silently probe
or change billing doors while a turn runs. Only an explicit reconnect rechecks them:
re-enter the service from its Providers row in `/settings`, or press `ctrl+r` there to
reuse the saved details. Disconnecting and reconnecting the service through `/connect`
does the same check. Where a door has no fixed catalog, its model listing is believed
after the one-token check succeeds.

A payment refusal proves a key authenticated. For an unchanged one-door service, the
service is connected and stored as before. For a multi-door service, codeaf tries the
remaining doors; if every one refuses for plan or payment reasons, it stores nothing and
says, for example,
`z-ai accepted the key but the account cannot pay — Insufficient balance or no resource package. Please recharge.`
A payment refusal on OpenRouter also asks for its balance again, subject to the
30-second quiet period after the last completed read. If OpenRouter
says it can afford a smaller positive output cap, codeaf retries that request
once with that cap. The final refusal keeps the vendor's whole sentence.
A plain `429` with no recognised payment or plan code still means the service is busy and
is waited out. Every saved key lives in the profile `config.json`, owner-readable only.

## Z.ai coding-plan models — why only four GLM models are listed

Z.ai's coding endpoint publishes a wider model listing than its DevPack documentation
says the plan serves. codeaf therefore shows only the documented plan catalog:
`glm-5.3`, `glm-5.3-flash`, `glm-5.3[1m]`, and `glm-5.3-flash[1m]`. The
pay-as-you-go door keeps the model listing returned by that door.

## Plan paused — what happens when my plan runs out, reset times, and pay-as-you-go overflow

When a plan reaches a documented usage window, the turn is paced and the account is not
described as unable to pay. The live line says `plan paused`; when the vendor supplies a
reset time it also says, for example,
`resets at 18:30 UTC · /connect can switch to pay-as-you-go`. An account that cannot pay
remains terminal; a spent plan window is not the same thing.

Each connected plan service has a Providers setting named `when the plan is paused`.
It defaults to `wait`, which never sends the turn to a metered door. Choose
`use pay-as-you-go` only when you want that service to spend through its metered door.
During overflow the status line names it, for example
`writing · 4s · pay-as-you-go 61 t/s`. The setting is per service.

## Is codeaf supported by Zhipu for the coding plan

Zhipu lists the tools its plan covers. codeaf is not currently listed; a request has been
drafted but has not been sent. codeaf identifies itself as codeaf and does not pretend to
be another supported client.

## What a service without a model list can and cannot do

A common reason for “why can't it make pictures any more?” is that the conversation now
uses a service without a model list. The answer depends on that service's empty catalog,
not on the picture tool itself.

A service whose model-list check proves absent says `deepseek is connected`
with no count. Its picker group contains one dim row:
`no list from this service · type a model id`. Type a model id to use one; codeaf does not invent a catalog.

The vendored list fact is only the expectation from the documentation survey. A service that was expected to have no list
but answers the check gets the listed behaviour immediately: its model count, picker group and service-scoped cache all use
the ids it returned, with no reconnect.

An empty catalog also means codeaf cannot know which picture-making, speech or video
models that service offers. Those tools are off the belt for that service—absent rather
than present and broken. Text models can still be named and used. A direct service has
one provider, so there is nothing to choose between; that is not a fault.

## Remove a key — disconnect a service, delete a key, stop using a service

Open `/connect` and press `enter` on a connected row. The row first says
`enter again to disconnect`; press `enter` a second time to confirm. When no turn is using it, codeaf removes its saved key and says
`deepseek is disconnected · its models are gone from the picker`.

A service answering the current turn cannot be cut:
`deepseek is answering right now · try again in a moment`. If this conversation used the removed service, codeaf either says
the disconnected sentence first and then says
`this conversation was on deepseek-direct/deepseek-v4-pro · it is now on ~deepseek/deepseek-v4-flash-latest`, or, when nothing can replace it,
`this conversation was on deepseek-direct/deepseek-v4-pro and nothing else here can take it · connect a service or pick a model`.

## Model names carry the service they came from

The default service's model ids remain unchanged and unqualified. A model from another
service is written `<service>/<model id>`, such as
`deepseek-direct/deepseek-v4-pro`. That first segment is how the conversation remembers
where the model can be reached. With two or more connected services, `/model` shows a dim
heading for each service, default first, in the order shown in the Providers tab. A
custom connection's heading is the name you gave it.

The status line uses the same spelling: an unqualified default-service id, and
`<service>/<model id>` for every other service. It does not shorten
`ollama/llama3.2:latest` to `llama3.2:latest`, because two services may publish the
same model name. `via <provider>` belongs only to a default-service model with router
providers. A direct-service row and status line draw no `via` at all and open no provider
sheet; that service has one road, not a choice of providers.

## Why does my plan show no cost instead of unbilled or could not be priced?

Phase 1 records no cost for a direct service. Its calls therefore add nothing to the
spend page and show no invented `$0.00`. This does not mean the vendor charged nothing;
consult that account for its bill and limits.

When a direct stream ends before its usage block arrives, codeaf asks for no OpenRouter
receipt and writes no ledger row for that unmeasured call. `/cost` stays silent about it
rather than saying a subscription call was charged but could not be priced. Direct calls
whose usage block does arrive still record their model call and token counts without an
invented price.

A direct service has one provider, so there is no provider picker and nothing to
choose between. That is not a fault. Price caps, privacy negotiation and provider routing
belong to the default routed service and are not applied to a direct call.

## A local runner — Ollama, LM Studio, vLLM, llama.cpp

Choose **Ollama** in `/connect` to use its usual local OpenAI-compatible address;
Ollama asks for no key. For LM Studio, vLLM, llama.cpp, or an Ollama address that is not
the usual one, choose **Custom OpenAI-compatible API**, then enter its base URL and any key that server
requires.

The connection check asks the local runner for its model list first. When it answers,
its models appear under the service's heading in `/model`; when that address is absent,
the runner can still connect and its group asks for a model id. A local
service has one provider, so there is nothing to choose between and that is not a fault.

## Custom OpenAI-compatible API — a proxy, a gateway, or your own endpoint

The **Custom OpenAI-compatible API** row in `/connect` accepts an OpenAI-compatible base URL and key.
Use it for a proxy, gateway, self-hosted endpoint, or vendor not already named. codeaf
checks the address before saving anything, then asks `name` before `your key`. The name
box opens on the host's own spelling: `localhost` for a local runner, `127-0-0-1` for
the loopback address, the host for anything else. Clearing the box takes that default
again. A name cannot carry `/` or a space, and a refused name reopens the box with the
reason: the slash is what separates connection from model in a model id, and a space
would travel into every id the connection qualifies. A name another service or a
default-service model author already uses is not asked twice about: codeaf takes an
available spelling (`localhost-direct`, then numbered ones) and the connect line names
what it used.

That name is the connection everywhere. It is the row's name in `/connect` and on the
Providers tab, the heading its models sit under in `/model`, and the first segment of
every model id it serves, so a model on a connection named `homelab` reads
`homelab/glm-5.3` and `/model homelab/glm-5.3` moves onto it. A refusal or a success
names the connection by the name it was given; neither switches back to `custom`.

Several custom connections coexist, each under the name you gave it, each with its own
key, its own rows and its own picker group. On /connect the **Custom OpenAI-compatible API** row
becomes that first connection's edit door once one is connected and an `add custom
connection` row connects a new one; with none connected yet, **Custom OpenAI-compatible API** is the
door onto the first.

On the Providers tab in `/settings` each custom connection is a row of its own. `enter`
opens it for editing with the address and name pre-filled, and an empty key box keeps
the saved key. A changed name is a rename: every model id already picked under the old
name is re-spelled with the new one, the conversation's own pick first (a turn still
answering is waited out), and with it the stored ones: reasoning levels, the
crew's role models, role pins, the fallback chain and the capability slots. A rename changes a label and nothing
else; it does not move the conversation onto a different model. `ctrl+r` on the row
reconnects with the saved details. The `add custom connection` row runs the same three
questions for a new connection, so the tab never sends you to `/connect` to add one.
The `active connection` row reads
`answering on localhost · enter moves it to homelab` and enter does that, wrapping past
the last connection back to the first; which one is active is read from the model the
conversation is on, so there is nothing else to store. The row is absent while no
custom connection is connected, and when the next one has no model list yet the move
says so instead: `no model list for homelab yet · reconnect it (ctrl+r on its row) or
type a model id in /model`.

In Phase 1 a **Custom OpenAI-compatible API** service must provide the compatible chat path. codeaf
tries `GET <base>/models` first; the models from an answered list fill its picker group.
When that address is absent, codeaf connects the service without inventing rows and the
picker asks you to type a model id. Direct calls record no cost in Phase 1 and have one provider.
