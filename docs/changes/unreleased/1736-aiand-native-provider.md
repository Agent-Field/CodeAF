---
kind: added
title: ai& is offered as a provider of its own, asked for by key and nothing else
pr: 1736
surface: [chat, engine, docs]
invalidates:
  - >-
    ai& was reachable only by hand, as a **Custom OpenAI-compatible API** row with
    `https://api.aiand.com/v1` typed into it, a name typed beside it and a key
    pasted in. It now has its own row in `/connect`, its own name in
    `codeaf connect aiand` and its own row on the Providers tab in `/settings`.
  - >-
    A provider with one billing door had to be discovered by the person reading
    the code — MiniMax's page said why it made no plan claim and said nothing
    about which other providers were in the same position. ai& is now named
    wherever that shape is described, and it asks for `your key` with no region
    choice, exactly like DeepSeek and MiniMax.
  - >-
    Every other provider's models were reached by an id codeaf invented a prefix
    for. ai&'s own list already names the lab that built each model
    (`zai-org/glm-5.3`, `deepseek-ai/deepseek-v4.1-flash`), so those ids are kept
    whole behind the `aiand/` segment rather than shortened.
  - >-
    `/connect` said it held "the six built-in model providers" while the group
    already carried seven named rows. It now names the eight and spells them out,
    so the count can be checked against the list instead of remembered.
---

One prepaid credit spends any model in ai&'s list, and that list reaches several
labs at once, so a single key covers models a person would otherwise open four
different accounts for.

ai& lists its models — `GET /v1/models` answers — so the connect line carries the
count that came back, for example `aiand is connected · 13 models`, and `/model`
fills its group from that list rather than asking for a typed id. The list is
organised by organisation and moves as the vendor adds and drops models, so the
number in the line is that day's answer and not something codeaf remembers.

It has one billing door and codeaf makes no plan claim about it, which is the same
honesty MiniMax's sentences already keep: nothing on the wire separates a
subscription from metered credit, and a money label codeaf cannot check is worse
than no label. A run with nothing left on the credit is answered by ai& with a
`402` and `insufficient_credits` — the balance speaking, not a bad key.

It collides with no model author, so it keeps the name `aiand` and never becomes
`aiand-direct`; that is the word `codeaf connect` takes and the first segment of
every model id it serves.
