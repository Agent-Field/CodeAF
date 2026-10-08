---
kind: added
title: ai& is offered as a provider of its own, asked for by key and nothing else
pr: 1737
surface: [chat, engine]
invalidates:
  - >-
    ai& was reachable only by hand, as a **Custom OpenAI-compatible API** row with
    `https://api.aiand.com/v1` typed into it, a name typed beside it and a key
    pasted in. It now has its own row in `/connect`, its own name in
    `codeaf connect aiand` and its own row in the Connections category of /settings.
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

One prepaid credit spends any model in ai&'s list, and that list reaches GLM,
DeepSeek, Qwen and Kimi at once, so a single key covers models a person would
otherwise open four accounts for. ai& is one row in `modelsource.Vendored()`,
and every surface a person touches is derived from that catalog — the connect
panel, the providers group, the picker group, the Connections category in
/settings and `codeaf connect aiand`.

ai& lists its models — `GET /v1/models` answers — so the connect line carries
the count that came back, for example `aiand is connected · 13 models`, and
`/model` fills its group from that list rather than asking for a typed id.

The row's preference was CHANGED by what that list says. It was the flagship,
`zai-org/glm-5.3`, whose vendor manifest answers `attachment: false` — a person
who dropped a screenshot into their first ai& conversation was refused by the
model codeaf had just seated them on. It is now `zai-org/glm-5.3-flash`: the
same family, `input: [text, image, video]`, and $0.15/$0.50 against the
flagship's $1.00/$4.00. The rule the row follows is the one `Source` already
states — the vendor's best model, not simply its flagship.

What makes ai& a row naming six vendors rather than one is that it is not one
lab, so the crew treats it as one route that spans families:
`crewVendors["aiand"]` names `deepseek`, `z-ai`, `moonshotai`, `qwen`, `openai`
and `google`, and one key can carry a planner and a checker sitting on different
labs in the same task. The rename in `crewVendorWire` is not a spelling
preference: asked on 2026-10-02 for the bare names the table avoids sending —
`glm-5.3-flash`, `deepseek-v4-flash` — api.aiand.com answered 404
`model_not_found` for every one, while the two-segment forms answered 200. So a
send without it is a call that fails.

The implementation is a base-URL swap: `Config.Direct` already switches off the
router's lane sheet, receipt fetch, price ceiling and routing vocabulary for a
vendor reached directly, `clientdoor.go` keys its adapter pool on the resolved
connection, and `internal/config/crew.go` carries the two tables above as its
only production code. Effort clamping for direct providers and one
crew-unreachable vendor (`motif-technologies/motif-3`, pickable by hand) are
recorded as future work, not acted on here. Nothing on the wire separates a
subscription from metered credit, so codeaf makes no plan claim — a run with
nothing left is answered by ai& with a `402` and `insufficient_credits`, the
balance speaking, not a bad key.
