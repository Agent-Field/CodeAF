# Personal app definitions

## List, open and inspect a personal app

The desktop engine uses `apps.list` for the app shelf. `apps.get` reads an app's
definition, plan, screen and run names, and agent instructions. `apps.screen`
opens one screen from the same checked definition. `apps.kit` lists the parts a
screen can use and the properties and bindings each part accepts.

## Fix or revert a broken app and read its versions

`apps.versions` shows each saved version, when it was made, and its request
and summary. `apps.revert` copies a chosen working version forward as a new
version. It keeps all earlier versions. An invalid change stays in its draft.
If live definition files become invalid, the engine serves the latest valid
saved version it can find. When it serves an older version, revert to that
version before starting a new change.

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

`app look` uses the open desktop to draw the selected screen on example
records. If the desktop cannot draw it, the command replies with
`desk_unavailable`. Design changes stay in the editing tab until you
choose **Apply**.
