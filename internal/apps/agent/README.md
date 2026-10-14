# App agent

## Content tools and the persona

The app agent works on records and declared runs. It cannot write app files,
fields, screens, triggers, or the persona. Requests to change the app return
“That changes the app” with Shape this.

The tool list comes from `app.json` capabilities, independently of any `tools`
list in `agent.md`. A readable record type permits `records.query` and
`records.get`; a read-write type also permits `records.create`, `records.update`,
and `records.delete`. Each call still checks the caller and the target type.
Declared runs permit `runs.start`. Declared connector verbs and external tools
open approval items; the agent does not execute connectors directly.
`agent.md` supplies persona and instructions only.

## Refused tools

There is no card on which a person grants the app agent a tool the app does
not declare, so a permission ask never waits. A tool off the belt, or a call
the capability checker refuses, is answered at once as "Not allowed in this
app", and the model is told to say in one line what it could not do. If it
asks for a refused tool a second time, the turn ends with "I can't use <tool>
in this app." Connector approvals are not refusals and keep their own path.

## Replies and name lookups

Each model reply accepts the first complete JSON action, including replies
with surrounding prose or code fences. Additional actions are discarded and
the model gets a note saying to wait for the first action's result. Only the
accepted action enters assistant history. A reply with no valid action ends
the turn as plain text, with action fragments removed. Speech and tool
summaries are also stripped of action fragments at the stream boundary.

An empty exact-title lookup retries once with a case-insensitive contains query
and no unrelated filters. The model sees the retry result before answering.
The instructions ask it to try a shorter name if needed before requesting an
id, and to ask which named match when the result is ambiguous.

## Focused proof

`make test-focus PKGS=./internal/apps/agent RUN='.'` runs deterministic parser,
stream, capability, and lookup regressions, including the real record store.

`go test -tags live -run '^TestLiveCRMContactChanges$' -count=1 -timeout 5m -v
./internal/apps/agent` uses `deepseek/deepseek-v4.1-flash` and the configured
provider key to add and delete a contact through the app agent and real record
store. It skips if no key is configured.
