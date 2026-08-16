# Background processes and services

## Background shells

A worker can start a long command in the background instead of waiting on it —
a build, a test suite, a dev server — and get on with something else. Each one
writes to its own log under `.aforge/jobs/`, so a chatty build can never block
anything, and the worker reads that log incrementally as it goes.

Background groups get **900 seconds** by default, are reniced to **+10** so they
yield your machine, and are killed cleanly when the task that started them ends.
That last part is deliberate: nothing survives its task by accident.

## Services: processes you meant to keep

Something you meant to keep running past the end of a task is a **service**, and
becoming one requires your consent.

When a task ends with a process it wants to keep, you get a blocking question:

> Keep the dev server running after this task?
>   1 yes — say "keep with auto-restart" to opt in
>   2 stop at task end

The default is stop. If you asked for a server in the first place, no question is
asked — you already said so. On yes, the receipt tells you how to end it:
`dev server keeps running · port 5173 — say 'stop the dev server' to end it`.

## Health checks

Every service declares how it can be checked: a **port**, a **URL** that must
return 200, or a **command** that must exit clean. Aforge probes it roughly
twice a second, after a two-second grace at startup, with a two-second timeout.
It also verifies the process is still the one it started, so a recycled process
id is never mistaken for a healthy service.

When one stops answering you are told plainly:
`dev server stopped answering on port 5173`.

## Restarts

Auto-restart is opt-in. When it is on, a failed service is restarted up to
**3 times**, backing off 250ms, then 500ms, then a second. At the limit it is
rested rather than thrashed:

> dev server rested after 3 restarts — say "restart it" when ready

Say "restart it" or "start it again" and it comes back. "auto-restart the api"
turns the policy on; "disable auto-restart" turns it off.

## Hygiene nudges

A service that has been up more than **3 days** while your session has been
quiet for more than **a day** earns one question — once, ever, for that service:

> dev server has run 4 days — still needed?

Keep it or stop it. It is asked once per service and never nagged again.

## Seeing and controlling them

- The **board** shows each service with its status, its log path, and the last
  ten log lines, with stop, restart and enable-auto-restart actions.
- `aforge services` lists them from a shell; `aforge services stop <name>` ends
  one.
- Plain sentences work: "what services are running", "stop the api", "restart
  the worker".

## "Shut it all down"

"Shut it all down", "stop everything", "kill everything" stops every live
service, unconditionally. Your in-flight *jobs* are a separate decision — you
get one question, defaulting to keeping them running, because stopping servers
and abandoning work are not the same wish.

If you say "stop everything **except** the api", that is not a blanket shutdown;
it is read as a targeted request and resolved against the live board.

## After a restart

When aforge starts again it re-adopts the services it was running, verifying
each is still the same process. One that died while it was away is reported
honestly rather than shown as healthy:

> dev server was not running anymore — say "start it again" to relaunch
