---
kind: added
title: ai& is offered as a provider of its own, asked for by key and nothing else
pr: 1
surface: [chat, engine]
invalidates:
  - >-
    ai& was reachable only by hand, as a **Custom OpenAI-compatible API** row with
    `https://api.aiand.com/v1` typed into it, a name typed beside it and a key
    pasted in. It now has its own row in `/connect`, its own name in
    `codeaf connect aiand` and its own row on the Providers tab in `/settings`.
  - >-
    `/connect` said it held "the six built-in model providers" while the group
    already carried seven named rows. It now names the eight and spells them out,
    so the count can be checked against the list instead of remembered.
  - >-
    A provider that served more than one lab had no way to say so. Every direct
    connection was treated as one family, because one lab's own API takes one
    family. ai& is reached through six catalog vendors from the one key, and the
    crew can now put a planner and a checker on different labs through it.
  - >-
    A connection is seated on its vendor's flagship, on the assumption that the
    best model is the one to land a person on. On ai& that model takes text
    alone, so the first attachment a person tried was refused by the model
    codeaf had chosen for them. The preference is now the best model that also
    takes a file.
---

One prepaid credit spends any model in ai&'s list, and that list reaches several
labs at once, so a single key covers models a person would otherwise open four
different accounts for.

The PROVIDER is one row in `modelsource.Vendored()`, and every surface a person
touches is already derived from that catalog — the connect panel, the providers
group, the picker group, the Providers tab and `codeaf connect aiand`.
`Config.Direct` already switches off the router's lane sheet, receipt fetch, price
ceiling and routing vocabulary for a vendor reached directly, so a direct vendor
stays a base-URL swap. `internal/session` needed no change either:
`clientdoor.go` keys its adapter pool on the resolved connection and strips the
service segment from the id it sends, which is why ai&'s own two-segment ids
(`zai-org/glm-5.3-flash`, `deepseek-ai/deepseek-v4.1-flash`) already reach it
whole.

`internal/config/crew.go` is the only other file with production code in it, and
it takes two tables rather than any new machinery — described below.

ai& lists its models — `GET /v1/models` answers — so the connect line carries the
count that came back, for example `aiand is connected · 13 models`, and `/model`
fills its group from that list rather than asking for a typed id. The list is
organised by organisation and moves as the vendor adds and drops models, so the
number in the line is that day's answer and not something codeaf remembers.

The row's preference was CHANGED by what that list says. It was the flagship,
`zai-org/glm-5.3`, and the vendor's own manifest
(`https://api.aiand.com/v1/api.json`, read 2026-10-02) answers 200 for that model
with `attachment: false` and `input: [text]` — so a person who dropped a
screenshot into their first ai& conversation was refused by the model codeaf had
just seated them on. It is now `zai-org/glm-5.3-flash`: the same family, the same
million tokens, `input: [text, image, video]`, and $0.15/$0.50 against the
flagship's $1.00/$4.00. The rule the row follows is the one `Source` already
states — the vendor's best model, not simply its flagship.

TWO THINGS THE MANIFEST SAYS THAT THIS CHANGE DOES NOT ACT ON, both recorded so
they are not rediscovered as bugs.

The effort words are per-model and do not line up with codeaf's ladder. Only
`high` is universal across the thirteen: `moonshotai/kimi-k2.7-code` takes `high`
and nothing else, `qwen/qwen3.8-27b` takes `none,low,medium,xhigh` and so NOT
`high`, and `zai-org/glm-5.3` takes `low,high,max` and so neither `none` nor
`medium`. ai& publishes exactly the field that would settle this per model
(`reasoning_efforts` and `reasoning_effort_default` on every row of
`/v1/models`), but the listing parse keeps only the `id`, and the adapter's
`ReasoningProfile` seam is fed by the ROUTER's catalog — so for a direct provider
the profile reads unknown and the requested word goes out unclamped. Teaching a
direct provider to publish its own reasoning profile is a change to
`internal/catalog`, `internal/config` and `internal/provider` at once, and it
would alter effort handling for every direct connection, not just this one. It
wants its own change and a live key.

`motif-technologies/motif-3` is the one row of the thirteen that answers
`structured_output: false`. Nothing to do about it here: the crew cannot reach
that vendor at all (below), so it is only ever a model a person picks by hand.

It has one billing door and codeaf makes no plan claim about it, which is the same
honesty MiniMax's sentences already keep: nothing on the wire separates a
subscription from metered credit, and a money label codeaf cannot check is worse
than no label. A run with nothing left on the credit is answered by ai& with a
`402` and `insufficient_credits` — the balance speaking, not a bad key.

It collides with no model author, so it keeps the name `aiand` and never becomes
`aiand-direct`; that is the word `codeaf connect` takes and the first segment of
every model id it serves.

The crew router reaches it too, and that needed a second fact the catalog does not
give away. ai& is the first connection here that is ITSELF a router: its ids
already name the lab that built the model, so a send is the provider segment over
the vendor's OWN id rather than a bare name. `crewVendors` maps a connection to
the CATALOG's vendor words, because that is what the router weighs — and the
catalog says `deepseek/` and `z-ai/` where ai& says `deepseek-ai/` and
`zai-org/`. `crewVendorWire` carries that rename on the send.

WHAT MAKES AI& A ROW NAMING SIX VENDORS RATHER THAN ONE IS THAT IT IS NOT ONE
LAB. Its listing reaches GLM, DeepSeek, Qwen and Kimi at once, so a single key
buys the same families a person would otherwise open four accounts for. The crew
therefore treats it as one route that spans families, not as one more single-family
vendor: `crewVendors["aiand"]` names `deepseek`, `z-ai`, `moonshotai`, `qwen`,
`openai` and `google`, and one key can carry a planner and a checker sitting on
different labs in the same task. A vendor that served a single family would need
no row at all, because its own `Written` would already be that family.

It is not a tidiness fix. Asked on 2026-10-02 for the bare names that table exists
to avoid sending — `deepseek-v4-flash`, `glm-5.3-flash`, `kimi-k3` —
api.aiand.com answered 404 `model_not_found` for every one, while
`deepseek-ai/deepseek-v4-flash` and `zai-org/glm-5.3-flash` answered 200. So a
send without the rename is a call that fails, not a spelling somebody might
prefer.

Six of the seven organisations on that list are vendors the catalog already
carries, and all six are named in the crew row — including `google`, so
`google/gemma-4-31b-it` is reached as `aiand/google/gemma-4-31b-it` and the crew
may pick a Gemma for a seat. The seventh, `motif-technologies`, is a vendor no
catalog row names, so the crew cannot weigh it and never offers Motif 3 however
good its figures are; it stays pickable by hand, which is all a person can do
with a model the router has never heard of.
