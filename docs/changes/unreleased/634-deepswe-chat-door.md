---
kind: added
title: the DeepSWE rig runs the chat surface too, in tmux inside the task container, beside do
pr: 634
surface: [build, chat, engine]
invalidates:
  - "bench/deepswe measured `aforge do` only; every row in AUTOPSY.md from s1 to s14 is the headless door. `DOOR=chat` runs the same task through `aforge chat --yolo --one-model` in a real terminal — tmux on the host, `docker exec -it` into the task container — with the brief pasted verbatim, and grades it the same way. The 2026-09-04 campaign on dev b3922479 is the first DeepSWE number for the chat surface."
  - "The rig's corpus path `~/src/swe-pro/tools/deepswe-bench/tasks` is gone; the tasks are `github.com/datacurve-ai/deep-swe` (117 tasks in the v1.1 checkout), cloned wherever `CORPUS` points. task.toml now carries `agent.timeout_sec = 10800`; a run that wants the 5400 s wall every earlier sweep used sets `AGENT_SECONDS=5400`, and the rig records which it ran under."
  - "run.log carried do's JSON envelope and its stream mixed together. stdout is `do.json` and stderr is `run.log` now; spend is read from the store ledger, the call log and the envelope and all three are written into cost.json, and the run's AFORGE_HOME is copied out beside the result for both doors with the key scrubbed."
---

The chat door is the canary's driver (bench/canary/lib/chat.sh) carried across.
The home stays inside the container because a session host listens on a socket
under it, so the two witnesses — the journal's usage seal and the call log — are
copied out with `docker cp` on every poll and read by the canary's journal.py.
A card waiting on a person is not the end of a cell: the rig waits
CHAT_ASK_GRACE seconds for a monitor to answer, then ends it as `asked`, because
a person sitting in front of the chat would have answered.

The first measurement is AUTOPSY.md's s15: eight tasks, sixteen cells, no
reward 1 on either door; `chat` passes more hidden tests than `do` on five of
eight tasks at about twice the money and one and a half times the wall, and
`do` on the five tasks with history costs $2.74 where s13 cost $1.52.
