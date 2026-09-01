---
kind: changed
title: the crew has a worker seat, and its presets are open-weight models picked off the catalog's own scores
pr: 278
surface: [chat, engine, docs]
invalidates:
  - "The crew was FOUR tiers — reflex, low, high, mastermind — and none of them governed a chat task's worker: `defaultTaskModel` (internal/session/taskmodel.go) put a task on the `task.model` row, else the conversation's live model, so a person on `frugal` talking to a frontier model handed every task to that frontier model. It is FIVE tiers now: `worker` (`models.tiers.worker`, `roles.TierWorker`, `config.ModelTierWorker`) sits between low and high, and a task's model resolves: a model named in the ask → `task.model` → the crew's worker row → the conversation. A level on the worker row (`vendor/model:high`) is NOT carried onto the task."
  - "`hands` in the crew line (`crew → balanced · brain … · hands … · checks …`, from `config.CrewClasses`) named the LOW tier. It names the WORKER tier now. The low and reflex tiers no longer appear in that line at all, because they are the same models in every preset."
  - "`roles.RoleWorker` (the adaptive-run node) rode `TierLow`. It rides `TierWorker`, and so does the work seat of `aforge do` / `exec` / `plan` / `run` (`config.ResolveSeats` reads `ModelTierWorker`, not `ModelTierLow`). A test source that sets only `tiers.low` and expects a worker to land on it is wrong now."
  - "The shipped balanced crew was mistral-nemo / deepseek-v4-flash / qwen3.8-27b / kimi-k3:low. It is mistral-nemo / deepseek-v4-flash-0731 / glm-5.3-flash / qwen3.8-27b / glm-5.3:high (reflex / low / worker / high / mastermind). frugal is deepseek-v4-flash-0731 working with glm-5.3-flash thinking (`:high`) and checking; max is glm-5.3 working with kimi-k3 thinking (`:high`) and checking. `config.DefaultWorkerModel` exists. kimi-k3:low is in no preset."
  - "The low tier's default was `deepseek/deepseek-v4-flash`, which OpenRouter resolves to the April 2026 build. It is `deepseek/deepseek-v4-flash-0731` — the July build at the same price, thirteen coding-index points higher — in every preset."
  - "A profile that applied a crew before this change has four `models.tiers.*` keys on disk and reads `custom` (`config.CrewAt`) until a preset is applied again; the fifth row reads the shipped default meanwhile. That is the honest reading and not a bug."
  - "The settings panel's Providers tab had four class rows under crew; it has five, with a `worker` row (`does the work · every task, its parts, every run node — most of the bill`) between `small work` and `careful work`, and the `small work` row's line no longer claims run nodes. The manual's crew, task-model, `/crew`, settings and adaptive-run pages say all of it, and `models-and-cost.md` has a new section *Which model does a task run on*."
---

The worker is twenty or thirty tool turns against a handful of one-shot role calls
around it; it is where a task's money goes, and the one seat the cost dial could
not reach. Naming it `hands` in the summary was already what everyone read the
word to mean.

The ids were picked on 2026-09-01 from `/api/v1/models`, which now carries
`benchmarks.artificial_analysis` (intelligence, coding and agentic indexes) on
every row, filtered to models that publish weights, against blended price. The
open-weight pareto front on the agentic index is four models long —
deepseek-v4-flash-0731, glm-5.3-flash, glm-5.3, and kimi-k3 on coding only — and
the worker column climbs it one step per preset. The careful column is always a
different vendor from the worker and always sees images, because the vision role
rides it. Closed models that are cheaper on their own vendor's platform than
through the router are deliberately in no preset. The sliding, self-refreshing
pick was designed and parked; this is the hand-picked crew until then.
