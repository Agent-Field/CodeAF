# The Pier baseline arm (D3)

What ran, what it cost, and what is different about it. Written after the first
baseline roll on 2026-09-28.

## What ran

```
bench/frontiercode/pier-arm.sh jsonschema-log-warning \
    deepseek/deepseek-v4-flash-0731 s1 mini-swe-agent
```

Pier (`datacurve-pier`, the same harness that produced the DeepSWE baseline
rows) ran the fixture task end to end with its own `mini-swe-agent` install,
its own docker-compose environment build from the task's `environment/`, and
its own trajectory collection. The rig collected the graded diff from the kept
trial container and graded it with the SAME rubric, the SAME grader and the
SAME judge as the harness arm — that sameness is the whole point of the arm.

Pier needed three task-format accommodations, all made in the fixture:

- `task.authors` must be a list of author objects (`[]`, not a string).
- `verifier.collect` must be an array of tables (`[[verifier.collect]]`).
- `environment.network_mode` must be one of Pier's words (`public`); this rig's
  own proxy posture is defined by `run.sh`, not by this field.

The environment Dockerfile also grew what Pier's agents expect: the repository
lives in the `agent` user's home (owned by that user), `/root/repos/jsonschema`
is a symlink to the same tree so the harness arm's `--dir` still resolves, git
identity is set system-wide, and the Dockerfile's `ARG`s carry the pinned
commits as defaults because Pier's compose build passes no build args.

## The result

`results/jsonschema-log-warning-mini-swe-agent-pier-deepseek-deepseek-v4-flash-0731-s1/`:

- **score 1.00** — all twelve rubric criteria pass on mini-swe-agent's patch.
- **$0.0516**, 26,289 output tokens, 3.1M input tokens (2.5M of them cached),
  peak context 76,994 tokens, 21.5 minutes wall.
- Cost and tokens are Pier's own report (`agent_result.cost_usd`, litellm
  pricing); there is no credential guard on a Pier arm — Pier holds its own
  key, and the row's `cost_usd_guard` is null for that reason.

## The deviation, stated plainly

The baseline arm's egress posture is PIER'S: a squid allowlist derived from the
agent's model endpoint (`openrouter.ai`), not this rig's open-and-logged proxy
with transcript scanning. It is STRICTER than FrontierCode's open internet —
the agent could not browse or fetch anything but its inference endpoint. That
matches how Pier produced the official DeepSWE baseline rows, and the row
records `egress=pier-squid-allowlist` and carries no `flagged` value (there is
no open egress of ours to scan). Comparing arms on such a task is fair; a task
whose solution requires fetching from the internet would NOT be comparable in
this posture, and the pilot plan should either give Pier a broader allowlist
(it accepts one) or skip such tasks for cross-arm rows.