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
saved version it can find.

## Archive or delete an app and keep its records

`apps.archive` marks an app as archived. Its definition and history are still
available. `apps.delete` hides the app and closes access to its definition.
Both actions keep the person's records and the app's saved files on disk.
