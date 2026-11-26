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
to its declared output and checks the visible merge fields. Draft runs render
with example records so you can test the output before applying the app.
The data store, run engine and desktop each implement their own execution and
rendering, including carrying out a run in a repository.

## Fix or revert a broken app and read its versions

`apps.versions` shows each saved version, when it was made, and its request
and summary. `apps.revert` copies a chosen working version's files forward
unchanged. The result gets a new publication number in `meta.version`.
The restored manifest keeps its original version and earlier entries stay
in the history. An invalid change stays in its draft.
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
to ask about that exact part. The saved mark keeps the selected node ID and
checkpoint. After Check passes, a marked edit shows a **Proposed change**
card with the part and a before and after summary. The same card is
available after restarting. **Apply** publishes the checked files as the
next version. **Discard** drops the change and returns to your saved app.
Between proposals, use an earlier step in the conversation to preview it
or choose **Go back to this** to put it back as a new step.

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
with `app context {"file":"screens/deals.json"}`. An unchanged file page
can be reused from its earlier reply. In the scoped app session,
`app parts {"select":["add-view/board","RecordList"]}` fetches several
descriptors in one command. Use one `for` selector or a `select` list of up
to 12 selectors. The batch returns each match and lists misses in
`problems`.

`app look` uses the open desktop to draw the selected screen on example
records. When the desktop renderer is connected, Check draws every screen
in Dark and Light at 800 and 1120 pixels. Clipping, overlap, truncation,
empty bound columns, lists that have records but no visible fields, contrast
problems and competing primary actions must be fixed before a step can be kept. If the connected renderer times out,
try Check again.

Check also names missing screen controls. Each declared command and each
manual action in the plan's `actions` needs a screen control. A data line with
`create: true` needs a create button or form. These findings go back to the
Builder for repair, including when Desk is closed.

With the CLI alone, Check still validates the app. The Check result and
change card show `look: skipped (desk not running)`. You can keep the
checkpoint and choose **Apply**. The standalone `app look` command returns
`desk_unavailable` until the renderer connects.

If a build stops, reopen it to see the reason in the build progress.
Build and repair get a model call limit based on the record types, screens
and runs in the outline. When the Builder runs out of model calls,
completed definition changes stay in the draft. Use Build again to continue writing, or ask the Builder
to continue an unfinished plan. Check reports all definition findings
together before Apply. If the saved app changed during editing, discard the stale draft to start
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
run step. A screen can start the run. Open its approval from Inbox to approve or
decline the run.

## Subscribe to record changes and refetch after reset

`data.subscribe` returns the first page of a native list or the watched
record in `snapshot`, with a `cursor` from the same data state. Use
`data.changed` to apply row changes after that cursor. To resume, send the
last cursor you applied. When the engine sends `data.reset`, clear what
the subscription displayed and refetch under your current access. Set
`invalidate_only` for a derived list or aggregate to receive resets with
a cursor. Refetch that view to get its values.

## Auto assign work items on create or transition

Declare an `assignment` under `app.json.rules`. Set `record`, `workItem` (a
name from `process.work_items`), and `strategy`. An optional `transition`
limits the rule to that named move. Without it, the rule runs on create and
on each transition. A `when` condition can filter the resulting record's
values.

- `round-robin` rotates through `candidates` in order and remembers its place.
- `load` picks the candidate with the fewest open work items across the
  space. Ties go to the first candidate.
- `field` reads a person `field`, or looks up its text or enum value in
  `matches` to find the assignee.
- `linked-owner` follows a single record reference in `field` and reads the
  linked record's person `ownerField`.

Candidates can use `me` or `agent:<name>`. Declared `roles` resolve to
the local person in Personal. A missing field match or linked owner leaves
no assignment and adds the reason to record activity. The same writer
handles app screens, automation, and agent writes. An app needs read access
to the linked record type to see an assignment derived from its owner.
Skill assignment and approval delegation require a member directory.

## Who approves a record and can the creator sign?

An `approval-routing` rule names the `record`, `order` (`serial` or
`parallel`), and the list of approval `roles`. Use `when` for an amount
threshold or field condition, such as `amount >= 10000 && category == 'Travel'`.
Money comparisons use the field's stored number. The highest `priority`
wins, with equal priorities resolved by rule ID. `transition` limits the
route to records whose latest transition has that name.

`data.process` shows the selected rule and the approval steps. Each
`data.signoffs.create` signs the next ready step. Supply the rule's
`statement` if it declares one. A new record revision needs new sign-offs.
A `signed()` guard on the move out of review waits for the whole active
chain. The record timeline explains the route and shows each signed step.

Personal binds each role to the local person. With `makerCannotSign: true`,
the creator gets “The creator cannot approve their own record. This approval
requires a second person.” A Personal space has one person, so those steps
stay open. Agents can prepare work for review. Signing requires a person.

## Prevent double booking or limit bookings per person per day

Declare a resource in `app.json.resources` and give its `booking` the booking
record `type` and the `resource` field. For rooms or equipment, name the
resource record type in `record` and use a single reference field. A person
field needs no separate resource record type.

Use `start`, `end` and `overlap: "reject"` for a time span. A booking can
start exactly when the previous one ends. Use `day` and `maxPerDay` for a
daily limit. Datetime values count toward their UTC day. Set all booking
fields together. Clear them or delete the booking to free its slot.

The writer checks the limit before committing. If a slot is full, the
`booking_conflict` refusal names a conflicting record. Inspect that record
or choose another slot. Record activity shows why a committed booking was
accepted.


## App row policy commands in the terminal

With the app engine running, use
`codeaf app data-query '{"app":"APP","type":"account"}'` or
`codeaf app data-aggregate '{"app":"APP","type":"account","aggregate":[{"fn":"count"}]}'`.
`codeaf app data-types` shows each field's declared class and visibility.
