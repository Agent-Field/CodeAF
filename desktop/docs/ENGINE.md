# Canonical engine preview

The desktop bundles the root `bin/codeaf`, built with `make build`. The historical
health-only module under `desktop/engine` is not the packaged engine. There is no
second agent loop: the local desktop transport dials `codeaf engine` through
`internal/remote`, the same persistent session host used by terminal connections.
Prompts, task execution, transcripts, plan reads and provider configuration remain
owned by that engine.

## Browser development

1. Run `npm run engine:dev` inside `desktop`.
2. Run `npm run dev` in a second terminal.
3. Explicitly connect a conversation before sending a real instruction.

The bridge binds loopback port 1423. Vite forwards `/api/engine` and injects the
process-local bearer token from an ignored, mode-0600 cache file. The browser
renderer never receives that token or a provider key. Override the workspace with
`CODEAF_DESKTOP_WORKSPACE` before starting the bridge. Remote previews may forward
that loopback port and use `CODEAF_DESKTOP_CONNECTION` for the local credential
file; do not copy provider keys. Native builds start the same transport through the
narrow `engine_connection` command and return its URL and local token.

## Session and rendering contract

POST `/api/engine/sessions` with `{}` creates a conversation in the configured
workspace; `{sessionFile}` reattaches its canonical transcript. Opaque connection
IDs last for one bridge lifetime. Persist sessionFile, not only ID, so a bridge
restart can reattach. A tab close releases its view, never Stop. A live stream can
reconnect with `GET /sessions/{id}/events?after=N`; records have sequence IDs and
canonical events. Completed history always comes from the engine transcript.

GET `/sessions/{id}` returns a snapshot with lower camel identity/state fields and
unchanged PascalCase canonical `DisplayEntry`, `PlanTaskRow` and `Usage` values.
GET `/sessions/{id}/tasks/{taskId}` returns the canonical read-only `PlanTaskPage`.
GET `/sessions/{id}/tools/{callId}` lazily reads a named canonical call's saved
output through the engine's existing file transfer. The client supplies no path;
only a recorded stub in this session's logs/stubs directory may select a file,
and symlink escapes are refused. Replies are `{output,full}` with a 1 MiB UTF-8
display cap. `full` is false for compact inline or capped output, never a claim
that a partial result is complete. Unavailable or pending results remain errors.
POST `/sessions/{id}/turn` takes `{text,mode}` where mode is submit, steer or queue;
POST `/sessions/{id}/stop` explicitly stops work. Those operations return
`{accepted:true}`; eventual state arrives through SSE/snapshot. External terminal
turns, asynchronous generated titles, background task updates and pending questions
use the existing remote subscriptions. While a view is subscribed, chat-scoped
plan rows also refresh every three seconds to catch worker CLI writes outside
the task notice lane. Reads make no AI call and do not fabricate activity times.
Failed plan reads expose planError and retain the last successful rows.

The preview uses OpenRouter `deepseek/deepseek-v4.1-flash` with OneModel enabled
for every text role, including auxiliaries and workers. It refuses a resumed
conversation with another model, missing OneModel, or a nonpersistent engine.
Changed-model sends are refused, never silently rerouted. Verify actual recorded
usage/model attribution in live tests, not just the UI picker label.

## Current limits

Pending questions and tool approvals retain their canonical Question objects in
snapshot.questions and NeedsPerson state. POST `/sessions/{id}/answer` accepts the
canonical Answer fields, including the offered option key and optional change
feedback. The transport matches a live question identity before delegating to the
engine resolver. Stale or rejected answers are errors; only accepted answers
return `{accepted:true}`. This does not change approval policy or enable yolo.
The workspace is selected at transport startup; native project selection is a
follow-up. A bounded 2048-record SSE tail falls back to canonical history after a
large replay gap; interrupted live partial output should not be presented as a
completed reply. Provider errors remain errors, not fixture-generated responses.
