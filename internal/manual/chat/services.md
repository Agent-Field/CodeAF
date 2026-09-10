# Services — the places models come from

## Add a key — connect a service, add a provider, use a different model service

Open `/connect` or `/connections`. The `models` group lists DeepSeek, Z.ai, Moonshot,
Ollama and **Something else**, followed by any service already connected. Pick a row and
answer its fields. A successful listed service says `deepseek is connected · 6 models`;
one without a list says only `deepseek is connected`. The Providers tab in `/settings`
then shows the service, the safe spelling of its key, its region and its order.

The default service remains first. With two or more services, `/model` groups models by
service in that order; with only the default service, the picker remains ungrouped.

## Use my own DeepSeek key — connecting DeepSeek, GLM, Kimi, Qwen or MiniMax directly

Open `/connect`, choose the vendor in the `models` group, and enter the key. Z.ai asks
for a region before its key and is the direct door for GLM; Moonshot asks for a region
before its key and is the direct door for Kimi. Qwen or MiniMax can be added through
**Something else** when you have an OpenAI-compatible address and key for them.

A service name cannot be confused with the author part of a model already on the default
service. For example, `deepseek` collides, so aforge says
`deepseek is a model author on openrouter · connect this as deepseek-direct`. Connect it with that written name; its
models then read `deepseek-direct/<model id>`.

## Connect a service — what is asked for, and what aforge checks before it saves anything

Open `/connect` and choose a row in `models`. DeepSeek asks for a key. Z.ai and Moonshot
ask for a region and then a key. Ollama asks for nothing. **Something else** asks for a
base URL and a key. A key may also be the name of an environment variable, such as
`$DEEPSEEK_API_KEY`.

A key with the wrong shape is stopped before any call:
`that is not the shape of a deepseek key — they start with sk-`. A refusal carries the service's own answer, cut at
120 characters on a word boundary:
`deepseek refused that key — Authentication Fails, Your api key is invalid`. No answer is different:
`deepseek did not answer · nothing was saved`. Nothing is stored unless the service answers yes. A saved key lives in the
profile `config.json`, owner-readable only.

## What a service without a model list can and cannot do

A common reason for “why can't it make pictures any more?” is that the conversation now
uses a service without a model list. The answer depends on that service's empty catalog,
not on the picture tool itself.

A service that accepts its key but publishes no model list says `deepseek is connected`
with no count. Its picker group contains one dim row:
`no list from this service · type a model id`. Type a model id to use one; aforge does not invent a catalog.

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
`this conversation was on deepseek-direct/deepseek-v4-pro · it is now on ~deepseek/deepseek-v4-flash-latest`, or, when nothing can replace it,
`this conversation was on deepseek-direct/deepseek-v4-pro and nothing else here can take it · connect a service or pick a model`.

## Model names carry the service they came from

The default service's model ids remain unchanged and unqualified. A model from another
service is written `<service>/<model id>`, such as
`deepseek-direct/deepseek-v4-pro`. That first segment is how the conversation remembers
where the model can be reached. With two or more connected services, `/model` shows a dim
heading for each service, default first, in the order shown in the Providers tab.

The status line does not add another service label. The model id already carries it;
`via <machine>` still names a serving machine when one exists and draws nothing when it
does not.

## Why there is no price on a direct service yet

Phase 1 records no cost for a direct service. Its calls therefore add nothing to the
spend page and show no invented `$0.00`. This does not mean the vendor charged nothing;
consult that account for its bill and limits.

A direct service has one lane, so there is no serving-machine picker and nothing to
choose between. That is not a fault. Price caps, privacy negotiation and lane routing
belong to the default routed service and are not applied to a direct call.

## A local runner — Ollama, LM Studio, vLLM, llama.cpp

Choose **Ollama** in `/connect` to use its usual local OpenAI-compatible address;
Ollama asks for no key. For LM Studio, vLLM, llama.cpp, or an Ollama address that is not
the usual one, choose **Something else**, then enter its base URL and any key that server
requires.

The connection check must be able to reach the local runner and read its model list.
Once connected, its models appear under the service's heading in `/model`. A local
service has one lane, so there is nothing to choose between and that is not a fault.

## Something else — a proxy, a gateway, or your own endpoint

The **Something else** row in `/connect` accepts an OpenAI-compatible base URL and key.
Use it for a proxy, gateway, self-hosted endpoint, or vendor not already named. Aforge
checks the address before saving it and uses a short written name derived from its host;
if that name is already taken, the message offers a `-direct` spelling.

In Phase 1 a **Something else** service must provide both the compatible chat path and
`GET <base>/models`; a missing model list refuses the connection and saves nothing. The
models from that required list fill its picker group. Direct calls record no cost in Phase 1 and have one lane.
