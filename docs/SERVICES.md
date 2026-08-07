# Services — processes the user meant to keep

`docs/JOBS.md` Decision 3 says nothing outlives its leaf, and for task
work that stays true. But "run the app so I can see it" is not task
work — it is a request for a *persistent effect*, and a system with no
sanctioned door for that teaches its own executor to escape through
`nohup` (observed in the wild): an unjournaled orphan nobody tracks,
nobody can stop conversationally, and nothing re-adopts after restart.
The fix is not loosening teardown — it is a first-class object.

## Decision 1 — A service is a journaled, owned process

A service is `{name, command, cwd, health (port | url | cmd), log,
provenance (the job that started it), pid + start-time}`, journaled
like everything else. It runs in its own detached process group with
its log in the workspace, exactly like a background job — the
difference is ownership: at leaf end a promoted job transfers from the
leaf's registry to the **resident's service supervisor** instead of
being killed. The teardown rule survives intact: leaves still kill
everything they own; they just may not own a service.

## Decision 2 — Promotion is consent, not a flag

A leaf cannot unilaterally make something permanent. The `job` tool
gains `keep: {name, health}` — a *request* to promote. Two paths:

- The compiler marked the job **service-intent** (the user's ask was
  for a running thing: "start", "serve", "run the app", "so I can
  open it") → promotion is automatic and *declared*: the receipt says
  what will keep running and how to stop it.
- Otherwise → one confirm question (`▸ 1 keep it running · ▸ 2 stop
  at task end`, default stop) — the same consequence gate as all
  standing effects. `nohup`-style escapes remain what they always
  were: a detached child the teardown reports; the executor guidance
  now says plainly that `keep` is the door and nohup is not.

## Decision 3 — Ambient, restartable, honest

- **Rail**: one dim line per service above tasks, beside standing —
  `▸ dev-server · up 2h · :5173` — breathing never (services are
  furniture); click → log tail, stop / restart as options.
- **Conversation**: "stop the dev server", "restart it" resolve
  through the same reference machinery as charters and surgery.
- **Restart-survival**: services are detached from the chat process,
  so closing the terminal does not kill them. On open, the supervisor
  re-adopts by pid + start-time match; a stale pid is marked stopped
  honestly (never respawned silently — restarting is the user's word
  or a health-check policy they ratified).
- **Health**: the supervisor checks health on its ordinary tick; a
  failing service gets one attention line, not a respawn loop. Auto-
  restart is opt-in per service and capped (3 attempts, then it rests
  and says so).
- **Headless**: `aforge services` lists; `aforge services stop <name>`
  for scripts. The journal is the truth either way.

## What this is not

- Not a process manager. No dependency graphs, no runlevels — named
  processes with logs, health, and an owner.
- Not an escape from the rail: a service spends no tokens, but its
  *creation* remains part of a railed job like any other effect.
- Not silent: every start, adoption, failure, and stop is journaled
  and visible where you talk.
