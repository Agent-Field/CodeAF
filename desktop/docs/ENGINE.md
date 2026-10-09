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
3. Open a conversation and send. The first send creates the session; there is no Connect step.

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

Aside entries carry `TaskIDs`, the tasks they concern, so a task notice in the
conversation can open its task; this holds for run reports as well as task
landings. They also carry `AsideKind` (`"task"`, `"job"`, `"watch"` or `"resume"`,
absent when the note has no single author, as in a mixed "while you worked"
batch) and, for a job, `AsideTitle`, the job's own label or command. All three
come from the journal's own mark, identical live and on replay; older journals
simply omit them, and the UI draws nothing for an absent field. A tool call the
person's Stop cut short carries `Interrupted` and is never `Failed`: stopped is
not broken. A call cut by another door (a takeover, a closing session) keeps its
old marking. Tool entries carry `Failed` when present; the UI
marks only the step, never the whole group. The desktop creates a session on the
first send (`POST /sessions` with `{}`) and automatically reattaches a saved tab's
`sessionFile`; no model call happens before the person sends. Conversation titles
come from the engine title lane: one auxiliary call after the first message,
2 to 6 words in sentence case, delivered asynchronously through the snapshot
`title`. The tab label is manual name, then engine title, then the first line of
the first message, then `New conversation`. The UI makes no AI call of its own.

The preview uses OpenRouter `deepseek/deepseek-v4.1-flash` with OneModel enabled
for every text role, including auxiliaries and workers. It refuses a resumed
conversation with another model, missing OneModel, or a nonpersistent engine.
Changed-model sends are refused, never silently rerouted. Verify actual recorded
usage/model attribution in live tests, not just the UI picker label.

## Conversation endpoints (contract v2)

All are under `/sessions/{id}` and take the same bearer token. Refusals are
`{error}` carrying the engine's own sentence.

- POST `/tasks/{taskId}/{note|amend|pause|resume|cancel}` with `{text?}`. Text is
  required (non-blank) for note and amend, else 400. Engine refusals ("that task's
  run has ended") are 409. Success is `{accepted:true}` and a snapshot follows.
- GET `/files?path=` returns `{name,mime,size,hash,inline,dataBase64}` through the
  engine's confined FetchFile (16MB cap). `inline` is false for svg, html and xml
  (filedoor's allowlist): treat those as data to save or show as text, never render.
  Outside the workspace is 403, missing or not a file is 404.
- POST `/files/stat` with `{paths:[<=64]}` returns
  `[{path,exists,dir,size,modTime?,outside?}]`. `modTime` is RFC 3339. `outside`
  is true when the path does not exist because it lies beyond the workspace and
  the conversation folder (a lexical reading of the engine's refusal).
- POST `/turn` also takes `files:[{name,mime,dataBase64}]` with mode submit only.
  Images (png, jpeg, webp, gif) go as pictures, the rest as files. At most 10MB
  per picture and 20MB in total; larger is 413 with a sentence. Text may be blank
  when files are present.
- POST `/questions/hold` with `{kind,id,ref}` stops that question's clock; 409 when
  it is no longer open.
- GET `/favicon?domain=` returns `{dataUrl}` or `{}`. Only hosts that appear in
  this conversation's web_fetch or web_search arguments or results are fetched
  (`https://<domain>/favicon.ico`, 3 s, at most 64KB, image types except svg,
  same-host redirects only, public addresses only), cached per bridge.

Event `kind` names now cover caption, steerAccepted, steerConsumed,
steerFellThrough, toolAnnounced, toolForming, toolFinished, toolOutput, retrying,
notice, compacting, compacted, taskProposal, taskUpdate, taskPhase, jobUpdate,
questionWithdrawn, questionAnswered and titleChanged; any other stays `other`
with the raw kind number in `raw.Kind`. A steer that falls through (its turn
ended first) publishes the follow-up turn its words started.

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

## File and diff tabs

All reads go to the engine (remote `File.Text`, `File.Find`, `Diff.Changes`,
`Diff.File`), never to the app's disk, because the engine may be on another
machine. Paths are workspace-relative and slash separated. The boundary is the
workspace only (not the session folder); symlinks resolve before the check, so
traversal and links that leave the workspace are 403, missing is 404. Typed
client: `readEngineText`, `findEngineFiles`, `engineChanges`, `engineFileDiff`,
`engineEditorTarget` in `engine-client.ts`. The base is the commit the
conversation started on (recorded once, when the bridge first opens it in a git
workspace, and kept in the session folder so a reload keeps it); the diff shows
commits since then plus the working tree. With no record, or when history was
rewritten and the start is no longer reachable from HEAD, the base is HEAD (the
empty tree on an unborn branch, which records nothing). Both `/changes` and
`/diff` answer `base: {kind: 'start'|'head', sha?}` so the UI can say which one
was used. Outside git, `git:false` and no files.

- GET `/files/text?path=` returns `{path,name,dir,abs,size,hash,language,lines,text}`.
  Over 1MB or binary: `text` is empty and `refusal` is `too-large` or `binary`
  with a `message` sentence (status 200; offer Open in editor).
- GET `/files/find?q=&limit=` returns `{files:[{path,name,dir}],truncated?}`, best
  first, gitignore-aware (`git ls-files`, bounded walk outside git). Empty `q`
  matches nothing. Limit defaults to 20, at most 100.
- GET `/changes[?path=a&path=b]` returns `{git,base:{kind,sha?},branch,files:[{path,name,dir,
  status,added,deleted,binary?}],added,deleted,truncated?}`; `status` is modified,
  added, deleted or untracked. Repeated `path` narrows to the files a
  conversation touched (the app knows them from its tool calls).
- GET `/diff?path=` returns `{path,name,dir,abs,git,base?,status,added,deleted,binary?,
  lines,hunks,truncated?}`. A hunk is `{header,oldStart,oldLines,newStart,newLines,
  section,lines:[{kind:context|add|del,old?,new?,text}]}`; `old`/`new` are the
  two line-number columns. `lines` is the current file length: the "N unchanged
  lines" fold is the gap before a hunk (`newStart - 1 - previousEnd`) and after the
  last (`lines - end`). Header counts are `added`/`deleted`. The hover-preview diff
  head is the first del/add lines of `hunks[0]`. Capped at 5000 lines.
- GET `/files/locate?path=` returns `{path,abs,host,local}` for Open in editor.
  `abs` is a path on the ENGINE's disk. The app opens an editor only when `local`
  is true (the bridge started the engine as its own child) and `host` equals the
  app's own machine name; otherwise it hides the handoff or offers Copy path.
