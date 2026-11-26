# Intake channels for apps

## Share a web form and turn a submission into a record

In a kept kit 2 app, add an entry to `app.json` under `channels` with
`kind: "form-link"`, `record` (the type), and `form` (an ID in
`process.forms`). Add `workItem` to create the named `process.work_items`
item for each submission. The engine checks the form fields and the record
type before saving. A rejected submission returns a plain message, such as
`email is an email address`. A successful one says `Submission received.`

Call `channels.list` for the link while the desktop engine is open. The link
opens on the machine running the engine and permits submissions to that
form. Share it with someone using the same machine. It expires when the app
version changes or the engine restarts. The local server allows 20 requests every 10 seconds, including page loads.
A rate-limited request says
`too many deliveries. Try again shortly`. Personal has no hosted public
form URL, and pairing does not forward the local link.

## Turn emails into tickets from a drop folder

Declare `kind: "email-in"`, `record` and `mapping` under `channels`.
Map `subject`, `from`, `body` and `attachments` to the record's fields,
for example `{"subject":"title","from":"sender","body":"description","attachments":"files"}`.
Use a `file` field for a single attachment or `files` for multiple.
The sender maps to the address and the body uses plain text when present.

Call `channels.list` to get the folder. Save an email as `.eml` and move
the complete file into that folder. The engine checks every five seconds
while open. It leaves the source file in place and accepts files up to
4 MiB. Personal has no inbound SMTP, per-app email address, or mailbox
login. Old connector-only email declarations are inactive.

## Drop a file and import CSV rows or extract fields from a PDF or image

Declare `kind: "file-drop"` and `record`. When you move a completed file
into the folder from `channels.list`, the open engine checks it within
five seconds. A CSV uses the regular CSV import to create records. Optional
`mapping` maps column headers to fields. Without it, headers match field
names or labels. Rows that fail validation are reported by line.

Set `fileField` to a `file` or `files` field to attach a PDF or image to
a new record. Optional `nameField` saves its filename in a text field.
Optional `accept` restricts MIME types, such as `text/csv`,
`application/pdf` or `image/*`. Each delivery accepts up to 4 MiB.

To extract text into a field, declare `run` on the channel. The run gets
`$record` and `$input.channel`. Its first `agent` step can read the uploaded PDF
or image when the run's model accepts that format, up to eight attachments.
A following
`records.update` step can set a field to `$steps.extract.output` (where
`extract` is the agent step's ID). Those updates carry AI provenance.

## Receive a connector event or webhook on this machine

Declare `kind: "connector-event"` (or `"webhook-in"`), `record`, a
`mapping` from top-level JSON keys to fields, and a `secret` name.
Store the shared secret in
`<codeaf home>/apps/_intake/secrets/<app ID>/<secret name>`. Give the
file mode `0600` and use at least 32 characters with no spaces.

POST the JSON object to the link returned by `channels.list`. Send the
secret as an `Authorization: Bearer …` header and the delivery key as
`Idempotency-Key`. The endpoint accepts requests up to 1 MiB. It is
available on the engine's loopback address while the engine is open.
Personal has no hosted public webhook and accepts no requests while the
engine is closed.

## List channels and post a delivery from an agent with codeaf app

Send these JSON requests on the authenticated `codeaf app` wire. Keep
the wire open for the reply. `codeaf app --schema` shows the method types.

```json
{"id":1,"method":"channels.list","params":{"app":"<app ID>"}}
{"id":2,"method":"channels.post","params":{"app":"<app ID>","channel":"request","dedupe_key":"submission-1","values":{"title":"Help"}}}
```

Send form field names or the connector's original keys in `values`.
For an email or file, send its basename in `name` and base64 in
`content`. A form or event needs a `dedupe_key`. For files, the engine
can derive it from the content, plus the filename for file drops.
Reusing a key for the same delivery returns the original receipt
with `replayed: true`. Each new record's history identifies the channel
and uses import provenance. A declared `run` is queued once per new
record, including each accepted CSV row.
