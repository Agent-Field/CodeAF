# Microsoft 365 apps

## Connect Outlook, Calendar or OneDrive with Microsoft sign-in

Register a **Mobile and desktop applications** public client in Microsoft Entra with the redirect URI
`http://localhost`. Set `CODEAF_MICROSOFT_CLIENT_ID` to its application ID.
Leave `CODEAF_MICROSOFT_TENANT_ID` unset for a multi-tenant registration or set it to your organization's
directory tenant ID. Use Microsoft 365 in Connections or run `codeaf app connections signin microsoft-365`
and open its local URL. The browser uses a proof key and the profile renews the grant.

The delegated Graph scopes are `User.Read`, `User.ReadBasic.All`, `Mail.ReadWrite`, `Calendars.ReadWrite`
and `Files.Read.All`, plus `offline_access` for renewal. Organization directory lookups need a work or
school account with directory access. If Microsoft requires administrator consent or Conditional Access
blocks the grant, codeaf reports it. The organization decides whether to allow access.

## Use Outlook messages, meetings, files and directory identities as app data

Register `"kind":"microsoft-365"` and `"credential_mode":"person"` with
`codeaf app connections register connection.json`. Use your kept app's ID in `app`. The registration
uses the profile's browser grant. It takes no token on standard input. Add declared reads with
`table`, an app record `type`, `key: "id"`, an optional `version: "version"` and a `fields` mapping.

| Graph object (`table`) | Common type | Source selection |
| --- | --- | --- |
| `outlook` | Message (`message`) | Outlook Inbox metadata, optionally since an absolute RFC 3339 `graph_start` |
| `calendar` | Meeting (`event`) | Absolute RFC 3339 `graph_start` and `graph_end`, or declared `from` and `to` arguments |
| `drive` | Document (`document`) | Your OneDrive or an explicitly selected SharePoint/OneDrive drive ID in `graph_drive` |
| `person` | Person (`person`) | Exact email or UPN in a declared `email` argument |
| `company` | Company (`organization`) | Exact domain in a declared `domain` argument, backed by a visible directory member |

The connection uses Graph access checks on every read. It keeps local canonical IDs without copying source
rows. Types with local row policies require a source that can enforce those policies. Message senders
and meeting attendees can map to common Person references. Use `graph_fixture: true` only for supplied
synthetic examples in a test profile.

## Refresh Microsoft 365 with delta cursors

Read with `codeaf app connections read APP CONNECTION READ [JSON_ARGS] [CURSOR]` or `connections.read`.
Follow each returned `next` until the round returns a `checkpoint`. Save the checkpoint with the changes
and pass it as the cursor for the next round. Supply `{}` before the cursor when there are no arguments.
A page can be empty and still return `next`. `removed` lists known canonical IDs withdrawn from the feed.
A cursor belongs to the account grant, read and arguments. When it expires, start a fresh round.
One read is bounded to 500 rows and 1 MiB. Tighten the selected calendar window or drive when needed.

## Open an Outlook body, Word document text or Teams transcript

Map `body` for an Outlook read or `text` for a file or transcript read. List a record first, then run
`codeaf app connections content APP CONNECTION READ ID` with its canonical ID. This is an audited
person-only content review. The result is PII by default. Outlook lists and change feeds carry headers
without fetching message bodies. File text is supported for plain text and Word documents. Teams text uses
WebVTT. Content reads honor the live provider grant, local field rules and a 16 KiB text limit.
Outside harnesses cannot obtain this content or use a Microsoft-connected app as a model input.

## Connect Teams transcripts and link them to meetings

For work or school accounts, an administrator can grant `OnlineMeetings.Read` and
`OnlineMeetingTranscript.Read.All`. Launch codeaf with `CODEAF_MICROSOFT_TRANSCRIPTS=1` and sign in again.
Use a `transcripts` read with `graph_meeting_read` naming a Calendar read. Declare a `meeting` argument and
pass the calendar row's canonical ID. A `meeting` reference field on the Document links it to the Meeting.
The connection lists only transcripts Graph gives to that account. If the tenant has disabled Graph
transcript access, the connection reports the reason and drops the transcript read from available reads.
A meeting with no accessible transcript yields no transcript rows or a source permission refusal.

## Start a run on new Outlook mail, an upcoming meeting or a transcript

Use `"on":"connector"`, `"connector":"microsoft-365"` and one of `message.received`,
`event.starts` or `transcript.created` in `event`. Grant that event in the connector's `can` list.
Under `match`, name the registered `connection` and `read`. For mail, use `query` for a
case-insensitive subject or sender substring, and map the message date. For Calendar, set `minutes`
between 1 and 1440 and map the meeting start, with `from` and `to` arguments declared. Transcript triggers
use the linked calendar and scan meetings from the last 24 hours while the engine is running.
Runs receive the matching canonical `id` and `type` in their input. They can load permitted metadata separately.

## Create an Outlook draft or write a Calendar event after approval

Declare `create-draft`, `create-event` or `update-event` in the connection's `writes`. Grant the exact
tool `connections.CONNECTION.OPERATION` in the app. Run it behind `gate: {approval: send}`.
Provide `set` with the permitted fields. For a draft use `to`, `subject`, `body` and optionally `cc`.
For a new event use `title`, `start` and `end` with absolute RFC 3339 times. An event update also needs
the canonical `id` and its current `_source_version` as `version`. Graph can send calendar invitations
if you supply attendees. Outlook creates a draft in the mailbox. Sending Outlook mail is unavailable.
The run records the effect receipt. An unknown outcome calls for checking the source before retrying.
