# Personal app definitions

## List, open and inspect a personal app

The desktop engine uses `apps.list` for the app shelf. `apps.get` reads an app's
definition, plan, screen and run names, and agent instructions. `apps.screen`
opens one screen from the same checked definition. `apps.kit` returns the
version 2 registry, with descriptions, schemas and examples for parts, fields,
views, content, actions, run steps, triggers, policies and connectors.
A client rendering an older app can request its original catalog with
`apps.kit {"kit":0}`.

## Which kit version checks my app and its custom parts?

The `kit` setting in app.json selects the rules for that app. Screens address
each part by a stable node ID. Kit 0 definitions keep their original parts and
limits, and the engine adds IDs to old screens when it reads them. Kit 2 definitions can name a custom part
with a manifest beside its source in the custom folder. The engine checks the
manifest, props, bindings and events, then saves those files with the app.
Rendering a custom part requires the desktop sandbox.

## Which gallery app features can the engine check?

Kit 2 can check Markdown, sheets, timelines, graphs, maps and slide content.
It also checks declarations for file references, record links, reasoned reveals,
sign-off, evidence, timers and output templates. Check matches a render Action
to its declared output and checks the visible merge fields.
The data store, run engine and desktop each implement their own execution and
rendering, including carrying out a run in a repository.

## Fix or revert a broken app and read its versions

`apps.versions` shows each saved version, when it was made, and its request
and summary. `apps.revert` copies a chosen working version forward as a new
version. It keeps all earlier versions. An invalid change stays in its draft.
The data store records which saved version is published and commits its data
grants with that version. If the published snapshot becomes unreadable,
revert to a working version to publish its definition and grants together.
Older definitions that have yet to be adopted can still open a valid saved
version if their current copy is damaged.

## Archive or delete an app and keep its records

`apps.archive` marks an app as archived. Its definition and history are still
available. `apps.delete` hides the app and closes access to its definition.
Both actions keep the person's records and the app's saved files on disk.

## Make an app from a description

Open **+ New app** in the desktop app and describe what you want to track
and do. The plan shows data, screens, automations and what the app needs.
Send a message to change any of it, then choose **Build**. Leave the tab
while it works and come back to the same conversation. An Inbox item opens
the app when it's ready to try or needs your input.

Try the screens with made-up records before choosing **Apply**. If the plan
offers to use an existing type, ask to use it to share those records.
The model that makes the app sees type names, fields and counts. It has
no access to your records.

## Change an app, try an earlier step and keep the change

Open **Edit app** and say what to change. Select an element in the preview
to ask about that exact part. Use an earlier step in the conversation to
preview it or choose **Go back to this** to put it back as a new step.
**Apply** publishes the checked files as the next version. **Discard**
drops the unpublished change and returns to your saved app.

The engine uses `apps.plan` to make or revise the plan, `apps.build` to start
work, `apps.change` to open editing, `apps.say` for conversation, and
`apps.build_state` to reopen the work. `apps.checkpoints` lists earlier steps
and `apps.restore_checkpoint` brings one back. `apps.preview` opens a
preview, `apps.apply` publishes it, and `apps.discard` drops the work.

## Build an app from an assistant or crew conversation

Use the `workaf-apps` skill and the `codeaf app` command. A build starts a
separate conversation that receives the request and any marked screen
element. Its commands can read app definitions and type shapes, and can add
types, fields, and screens. The app's records are separate.

`codeaf app parts` lists names from the active kit. For a specific view or
markup part, use `codeaf app parts --for add-view/board` or
`codeaf app parts --for RecordList`. In an app session, `app context` shows
the outline, manifest and file names. Ask for an individual definition
with `app context {"file":"screens/deals.json"}`.

`app look` uses the open desktop to draw the selected screen on example
records. When the desktop renderer is connected, Check draws every screen
in Dark and Light at 800 and 1120 pixels. Clipping, overlap, truncation,
empty bound columns, contrast problems and competing primary actions must
be fixed before a step can be kept. If the connected renderer times out,
try Check again.

With the CLI alone, Check still validates the app. The Check result and
change card show `look: skipped (desk not running)`. You can keep the
checkpoint and choose **Apply**. The standalone `app look` command returns
`desk_unavailable` until the renderer connects.

If a build stops, reopen it to see the reason in the build progress. If
the saved app changed during editing, discard the stale draft to start
from the latest version.

## How app commands describe a screen and the work it does

App authoring can compile an intent for a list, board, table, calendar, form,
detail, chart or triage queue. Markup can arrange the same parts on a screen.
The compiler gives each part a stable ID and checks the result against the kit
registry before the change can be saved. Collection screens declare their
query so the desk requests a view by ID.

A lifecycle declares the states of a record and which transitions people,
agents or runs may request. A board grouped by that lifecycle moves cards
through those transitions. Guards can read record fields, check for a current
sign-off with `signed()` or `signed("review")`, and count open work with
`open_work()`. The engine can check declarations for work
items, multistep forms, due timers and process templates. Executing a declared
process depends on the corresponding data and run support.

For kit 2, put connector operations that leave the machine in an approved
run step. A screen can start the run and show its approval card.

## Subscribe to record changes and refetch after reset

`data.subscribe` returns the first page of a native list or the watched
record in `snapshot`, with a `cursor` from the same data state. Use
`data.changed` to apply row changes after that cursor. To resume, send the
last cursor you applied. When the engine sends `data.reset`, clear what
the subscription displayed and refetch under your current access. Set
`invalidate_only` for a derived list or aggregate to receive resets with
a cursor. Refetch that view to get its values.
