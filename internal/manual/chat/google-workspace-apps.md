# Google Workspace in Personal apps

## Connect Gmail, Calendar or Drive to an app

Run `codeaf app connect google gmail`, `codeaf app connect google calendar`,
or `codeaf app connect google drive`. The command opens the Google sign-in
page and prints its local address. Add `--no-browser` to open the address
yourself. With no source name, the command asks for Gmail and Calendar
read access in two browser grants. Each grant is separate from chat's Google tools.

The read permissions are `openid` plus `gmail.readonly`,
`calendar.events.readonly`, or `drive.readonly` under
`https://www.googleapis.com/auth/`. The OpenID subject keeps separate read
and write grants tied to the same account. Google refreshes access through
the profile's stored connection.

If Google says “Your organization blocked this app,” ask your Workspace
administrator for approval or your organization's OAuth registration. Put
the installed-app client ID and secret in `google_oauth_client` and
`google_oauth_secret` in your personal profile, or set
`GOOGLE_OAUTH_CLIENT` and `GOOGLE_OAUTH_SECRET`. These use the same settings as
Google in chat. A client restricted to its organization requires a matching
Google account. Reconnect after changing registrations.

## Show my mail and calendar in Daily brief

Run `codeaf app connections daily-brief` after connecting Gmail and Calendar,
then open **Daily brief** in Desk's Apps shelf. It reads today's inbox and
the next seven days on your primary calendar. Use `--fixture` on the install
command to see synthetic messages and meetings with no Google sign-in.
`codeaf app connections google-binding APP CONNECTION live` switches that
same app from fixture data to live. Use `codeaf app connections list APP` to
find its connection name.

## Read a Gmail body or export a Drive document

Google sources read under the connected account. Set `google_index: true` on
a Gmail read to retain a bounded header and metadata index in the private
profile database. Its history cursor refreshes changed headers and labels.
Mail lists carry MIME
headers, labels, snippet and attachment metadata. A message body stays on
Google until a point read. `codeaf app connections content APP CONNECTION READ ID`
takes the app's record ID and returns personal text to the local terminal.
For a Drive Google Doc it asks Google to export plain text, subject to the
file's current access and download rights. The transcripts object finds
Google Docs with “transcript” in the title. Drive sharing information reflects
the metadata Google returns to your account.

Personal source data is PII for app model policy. A Google app can grant
`google_content` to its own codeaf conversation to read a selected body or
exported Doc. The model route must pass the app model policy. Outside
harnesses cannot call `google_content`. The terminal command uses the local
operator's authority. Content reads write a policy log entry without text.

To map mail addresses or meeting attendees to canonical Person IDs, an
operator sets `google_link_identities: true` and maps those columns to a
Person ref or refs field. Gmail's `sender_domain` can map to Company, whose
common type key is `organization`. Bind attachment metadata as Documents
with the `attachments` read on the same Gmail connection.

## Draft Gmail, change a Calendar event or comment on Drive

Connect a separate write grant with `codeaf app connect google SOURCE --write`.
Google asks for `openid` plus `gmail.compose`, `calendar.events`, or
`drive.file` under `https://www.googleapis.com/auth/`. Google bundles send
permission into `gmail.compose`. The app effect only calls Gmail's draft
creation endpoint. In Drive, `drive.file` limits comments to files authorized
for this OAuth client. The registered operations are `create-draft`,
`create-event`, `update-event`, and `comment`.

Declare the connection operation as
`connections.CONNECTION.OPERATION` in a run's tool grant. The run pauses for
approval and records the source receipt. An update uses the event's
`_source_version` as its expected version. If Google may have accepted a
write but did not confirm it, the run leaves the outcome for your review.

## Trigger a run on matching mail, an upcoming meeting or a transcript

Use a connector trigger with the registered Google source and event. Put
the profile connection ID and read name in `match.connection` and
`match.read`. Grant the event in the matching connector's capabilities.
Use `gmail` with `message.received` and an optional Gmail search in
`match.query`. Use `calendar` with `event.starts` and the advance notice
in `match.minutes` (1 to 1440). Use `drive` with `transcript.created` and
a transcripts read. Runs receive the canonical record ID and its app
record type. The Gmail and Drive feeds start at the current cursor. Calendar
keeps a sync token and upcoming start times. It applies cancellations and
moves before starting runs after a restart.
