---
kind: fixed
title: the crew reaches the headless doors, and every run says which voice chose its models
pr: 186
surface: [engine, docs]
invalidates:
  - "A crew set with `/crew` held in the chat and nowhere else: `aforge do`, `exec`, `plan`, `run`, `revise` and `run subharness` resolved their two models from `--model`/`--plan-model` and `AFORGE_MODEL`/`AFORGE_PLAN_MODEL` alone and never opened the profile. They now climb one ladder — flag, environment, the profile's crew (mastermind plans, small work works), the build's default — so a benchmark no longer has to hand-translate a preset into slugs."
  - "The model flags said `(default AFORGE_MODEL)`, which named one rung of four. They now name the ladder: `flag › AFORGE_MODEL › crew › default`."
  - "A headless run said nothing about which models it was using unless a plan split happened to be in force. Every one of those doors now opens with a `models:` line on stderr naming both seats and the rung that chose each — `models: work deepseek/deepseek-v4-flash (crew frugal) · plan qwen/qwen3.8-27b (crew frugal)`."
  - "`aforge do --json` had no model fields. It now carries `model`, `plan_model`, `model_source` and `plan_model_source`; the two source fields name the rung (`--model`, `AFORGE_MODEL`, `crew frugal`, `default`), and a campaign should record them beside the score."
  - "The plan role's seed origin in the journal always read `AFORGE_PLAN_MODEL` or `--plan-model`. It now reads whatever actually named the model, which may be `crew frugal`."
  - "A model value carrying a thinking level missed every catalog lookup: `moonshotai/kimi-k3:low` matched no row, so its window read zero, its price read unpublished, its capabilities read unknown, and its measured history filed under a second identity. `catalog.normalizeID` — the one place a value becomes a lookup key — now takes the level off alongside the `~` alias marker, and `config.panelTierWord` does the same."
  - "A model id carrying a thinking level was sent to the provider whole, so `--plan-model moonshotai/kimi-k3:low` asked for a slug no provider publishes — a 404 on every planning call. `Config.providerConfig` and `ClientFor` now split the level off the slug; the seat, the plan role binding and the receipt keep the whole value, and the role ladder applies the level per call as it always has."
---

The crew is the product's own word for a model policy, and headless invented a
second one. Reported from a benchmark (#166) that ran the same task under two
presets and had to read `models.tiers.*` out of `config.json` by hand to do it —
which is exactly the one-source-of-truth violation the crew exists to prevent.

The resolution is one function beside `CrewAt`/`TierModelAt` — `config.ResolveSeats`
— and every door calls it rather than re-deriving a rung of its own; a structural
test in `cmd/aforge` holds that shape. The work seat takes the **low** tier and the
planning seat the **mastermind** tier because those are the two the chat's own
worker and planner ride (`roles.DefaultAssignment`), so the two surfaces now call
the same two models for the same two jobs.

One decision worth stating: an untouched profile falls to the **build's default**
rather than reading its own shipped tier values back as `crew balanced`. The four
defaults *are* the balanced row, so the alternative would make the bottom rung
unreachable and quietly change the default headless work model for everybody.

A crew value carrying a thinking level (`moonshotai/kimi-k3:low`) carries it the
way a flag does — whole into the seat and into the plan role's binding, split
into a model and an effort by the role ladder at the point of the call. Which
turned up two bugs older than this issue. Nothing between a seat and the wire
took the level off the slug, so `--plan-model kimi-k3:low` asked the provider for
a model id nobody publishes; and nothing took it off a catalog lookup key, so the
same value read zero context, no price and no capabilities. Each split now
happens once, at its own seam — the client, and the catalog's key.
