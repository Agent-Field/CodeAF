# Services — the places models come from

## Add a key — connect a service, add a provider, use a different model service

Open `/connect` or `/connections`. The `models` group lists DeepSeek, Z.ai, Moonshot,
MiniMax, Alibaba Qwen, Ollama and **Something else**, followed by any service already
connected. Pick a row and answer its fields. A successful listed service says
`deepseek-direct is connected · 6 models`; one without a list says only
`deepseek-direct is connected`. A service with more than one billing door names the one it
bound: `z-ai-direct is connected · coding plan · 4 models` or
`z-ai-direct is connected · pay-as-you-go · 10 models`. The Providers tab in `/settings`
then shows the service, door, safe spelling of its key, region and order.

The default service remains first. With two or more services, `/model` groups models by
service in that order; with only the default service, the picker remains ungrouped.

## When a newly connected model service starts working in this conversation

On a plain launch on this machine, a successful connection with a pasted key is live in
the conversation that is already open. The surface writes the service to the engine's
profile; the engine re-reads that profile through the model-setting door before it applies
the chosen model. Pick one of the new service's models in `/model`, and the very next
request uses that service's address and pasted key. Opening another conversation is not
required.

When the key is a variable, the receipt adds, for example,
`the engine process reads $DEEPSEEK_API_KEY from its own environment`. The daemon keeps
the environment it started with. If it started before that variable existed, run
`aforge engine --stop --workspace <dir>` and launch aforge again so the new engine reads
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
MiniMax, Ollama and **Something else** are single-door services. MiniMax makes no plan
claim because its plan and metered traffic currently have no wire-level difference
aforge can use to prove which balance answered.

A service name cannot be confused with the author part of a model already on the default
service. When `deepseek` is already an author there, aforge connects the direct service
under `deepseek-direct` in that same attempt. The region and key are not asked for twice,
and its models read `deepseek-direct/<model id>`.

## Why is my service called z-ai-direct — I connected Z.ai, the name changed

A service may not be written with a name the default service already uses for a model
author. Aforge appends `-direct` and finishes the connection in the same attempt, so the
region and key are not asked for twice. The connect line tells you the name it used, for
example `z-ai-direct is connected · coding plan · 4 models`, and those models read
`z-ai-direct/<model id>`. DeepSeek follows the same rule: it becomes `deepseek-direct`,
and its models read `deepseek-direct/<model id>`.

## Connect a service — what is asked for, and what aforge checks before it saves anything

Open `/connect` and choose a row in `models`. DeepSeek asks for `your key`. Z.ai,
Moonshot and Alibaba Qwen ask `your region` with one row per region: `International`
is first and starts under the cursor, then `China`. Up and down, or `ctrl+p` and
`ctrl+n`, move the cursor; a letter jumps to a region whose name starts with it;
enter takes the row under the cursor and then opens `your key`; esc backs out with
nothing saved. The region is a choice and cannot be typed. Ollama asks for nothing.
**Something else** asks for `your base url` and then `your key`. A key may also be the
name of an environment variable, such as `$DEEPSEEK_API_KEY`.

A key with the wrong shape is stopped before any call:
`that is not the shape of a deepseek key — they start with sk-`. A refusal carries the
service's own answer, cut at 120 characters on a word boundary:
`deepseek refused that key — Authentication Fails, Your api key is invalid`. No answer is
different: `deepseek did not answer · nothing was saved`.

Aforge checks each billing door in order with a one-token completion and binds the first
one that answers. A no-plan or payment answer on one door moves the connection check to
the next; a bad-key answer stops immediately in the vendor's words. A missing answer also
moves to the next door. If no door answers, aforge saves nothing. Key-prefix hints may
change which door is tried first, but never skip a door.

A spent plan window proves that plan door works: aforge binds it, stops before the
pay-as-you-go door, and says `plan paused`. It does not make a paid probe or change the
saved billing door. When the vendor supplies a reset time, the connected line carries
the same sentence used during a turn, for example
`plan paused · resets at 18:30 UTC · /connect can switch to pay-as-you-go`.

The bound door is saved and every later request uses it. Aforge does not silently probe
or change billing doors while a turn runs. Only an explicit reconnect rechecks them:
re-enter the service from its Providers row in `/settings`, or press `ctrl+r` there to
reuse the saved details. Disconnecting and reconnecting the service through `/connect`
does the same check. Where a door has no fixed catalog, its model listing is believed
after the one-token check succeeds.

A payment refusal proves a key authenticated. For an unchanged one-door service, the
service is connected and stored as before. For a multi-door service, aforge tries the
remaining doors; if every one refuses for plan or payment reasons, it stores nothing and
says, for example,
`z-ai accepted the key but the account cannot pay — Insufficient balance or no resource package. Please recharge.`
A plain `429` with no recognised payment or plan code still means the service is busy and
is waited out. Every saved key lives in the profile `config.json`, owner-readable only.

## Z.ai coding-plan models — why only four GLM models are listed

Z.ai's coding endpoint publishes a wider model listing than its DevPack documentation
says the plan serves. Aforge therefore shows only the documented plan catalog:
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

## Is aforge supported by Zhipu for the coding plan

Zhipu lists the tools its plan covers. Aforge is not currently listed; a request has been
drafted but has not been sent. Aforge identifies itself as aforge and does not pretend to
be another supported client.

## What a service without a model list can and cannot do

A common reason for “why can't it make pictures any more?” is that the conversation now
uses a service without a model list. The answer depends on that service's empty catalog,
not on the picture tool itself.

A service whose model-list check proves absent says `deepseek is connected`
with no count. Its picker group contains one dim row:
`no list from this service · type a model id`. Type a model id to use one; aforge does not invent a catalog.

The vendored list fact is only the expectation from the documentation survey. A service that was expected to have no list
but answers the check gets the listed behaviour immediately: its model count, picker group and service-scoped cache all use
the ids it returned, with no reconnect.

An empty catalog also means aforge cannot know which picture-making, speech or video
models that service offers. Those tools are off the belt for that service—absent rather
than present and broken. Text models can still be named and used. A direct service has
one lane, so there is nothing to choose between; that is not a fault.

## Remove a key — disconnect a service, delete a key, stop using a service

Open `/connect` and press `enter` on a connected row. The row first says
`enter again to disconnect`; press `enter` a second time to confirm. When no turn is using it, aforge removes its saved key and says
`deepseek is disconnected · its models are gone from the picker`.

A service answering the current turn cannot be cut:
`deepseek is answering right now · try again in a moment`. If this conversation used the removed service, aforge either says
the disconnected sentence first and then says
`this conversation was on deepseek-direct/deepseek-v4-pro · it is now on ~deepseek/deepseek-v4-flash-latest`, or, when nothing can replace it,
`this conversation was on deepseek-direct/deepseek-v4-pro and nothing else here can take it · connect a service or pick a model`.

## Model names carry the service they came from

The default service's model ids remain unchanged and unqualified. A model from another
service is written `<service>/<model id>`, such as
`deepseek-direct/deepseek-v4-pro`. That first segment is how the conversation remembers
where the model can be reached. With two or more connected services, `/model` shows a dim
heading for each service, default first, in the order shown in the Providers tab.

The status line uses the same spelling: an unqualified default-service id, and
`<service>/<model id>` for every other service. It does not shorten
`ollama/llama3.2:latest` to `llama3.2:latest`, because two services may publish the
same model name. `via <machine>` belongs only to a default-service model with router
lanes. A direct-service row and status line draw no `via` at all and open no lane
sheet; that service has one road, not a choice of serving machines.

## Why does my plan show no cost instead of unbilled or could not be priced?

Phase 1 records no cost for a direct service. Its calls therefore add nothing to the
spend page and show no invented `$0.00`. This does not mean the vendor charged nothing;
consult that account for its bill and limits.

When a direct stream ends before its usage block arrives, aforge asks for no OpenRouter
receipt and writes no ledger row for that unmeasured call. `/cost` stays silent about it
rather than saying a subscription call was charged but could not be priced. Direct calls
whose usage block does arrive still record their model call and token counts without an
invented price.

A direct service has one lane, so there is no serving-machine picker and nothing to
choose between. That is not a fault. Price caps, privacy negotiation and lane routing
belong to the default routed service and are not applied to a direct call.

## A local runner — Ollama, LM Studio, vLLM, llama.cpp

Choose **Ollama** in `/connect` to use its usual local OpenAI-compatible address;
Ollama asks for no key. For LM Studio, vLLM, llama.cpp, or an Ollama address that is not
the usual one, choose **Something else**, then enter its base URL and any key that server
requires.

The connection check asks the local runner for its model list first. When it answers,
its models appear under the service's heading in `/model`; when that address is absent,
the runner can still connect and its group asks for a model id. A local
service has one lane, so there is nothing to choose between and that is not a fault.

## Something else — a proxy, a gateway, or your own endpoint

The **Something else** row in `/connect` accepts an OpenAI-compatible base URL and key.
Use it for a proxy, gateway, self-hosted endpoint, or vendor not already named. Aforge
checks the address before saving it and uses a short written name derived from its host;
if that name is already taken, the message offers a `-direct` spelling.

That written host name is the row's name everywhere. A refusal from a localhost row says
`localhost refused that key — …`, and a success says `localhost is connected · 2 models`;
neither switches back to `custom`.

In Phase 1 a **Something else** service must provide the compatible chat path. Aforge
tries `GET <base>/models` first; the models from an answered list fill its picker group.
When that address is absent, aforge connects the service without inventing rows and the
picker asks you to type a model id. Direct calls record no cost in Phase 1 and have one lane.
