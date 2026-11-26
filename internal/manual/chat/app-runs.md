# App runs

## Start an app run and see its steps

A desktop app can start a run from a screen or command using its saved run definition. A Kit 2 command with a record type in `for` appears with that type's card,
row and record detail. Its run can bind the current record with
`$record.id`. Progress updates as each step runs. A run can create or update records the app has permission to write, render a declared output, ask the app agent for an answer, call an allowed connection, wait for a duration, or repeat steps up to a limit. Shell commands and repository workspaces are unavailable in app runs.

An approved standalone gate shows as done once the run passes it, including
after the run finishes. A new loop round can ask for its own approval.

## What happens to scheduled runs when codeaf is closed?

Runs advance while codeaf is running. Completed steps, approval requests and wait deadlines are saved. When you reopen codeaf, a schedule with missed slots starts one run marked “caught up”, then returns to its regular schedule. A send interrupted before its result was saved is shown as incomplete so the same message cannot be sent twice.

## Recover a run after a restart interrupted a local record write

When codeaf restarts, a run can finish a prepared local record create or update.
The data store saves the step's receipt with the write. If the write already
landed, the run uses that receipt and continues without writing again. If the
write had yet to land, the run applies the prepared inputs under the app's
current record permissions. Older interrupted writes prepared without a
receipt key stay incomplete. An outbound call with an uncertain result also
stays incomplete.

## Update a record created earlier in a run

To update records written by a step named `save`, set the update's `ids` to
`$steps.save.output.rows.id`. This takes the saved IDs directly from that
step, including when both steps run together.

## Start an app run when a record changes

Apps can start a run when a record is created, updated or deleted and its
trigger condition matches. This includes a source sync that commits a new
record revision. codeaf reads committed record changes from its
saved queue. Each matching trigger creates one run for the change, including
when codeaf restarts before it finishes reading the queue. The first time the
queue is connected, it starts with new changes. A trigger with an invalid
condition or run definition appears as incomplete.

A `refs` parameter can use `default: $input.record.links` to take records
from the triggering record's `links` field. The field must link to that
parameter's record type. A manual start can supply its own list of IDs.
A query string is also a valid default.

A declared record write runs as a flow. It can replace a value the person set
when the app has write access and the record's rules permit the change.

## Approve, decline or raise the budget for an app run

The Inbox shows “Approval needed: send” before a run sends mail, posts a message or makes another outbound connection call. Open its Inbox row to see the run’s steps, the change and the records it applies to. Open a record by its title. Choose **Approve** to let that step proceed or **Reject** to end the run. A pending approval returns to the Inbox after the engine and Desk restart. When a run reaches its spending limit, the Inbox shows “Per-run budget reached”. Raise the limit in dollars to continue or choose Stop.

## Check refuses a run parameter default for multiple records

Use a record query string or a list of record IDs for a `refs` parameter's
default. For example, `"stage:Qualified"` selects matching records. Check
names the affected parameter when its default has a different shape.

## Bind names in a draft or run template

Use a declared record reference in the run prompt. For example, a deal
parameter with a contact reference can use `{{params.deal.contact.name}}`.
Use the actual field names in the app. `{{owner.name}}` uses the local
account's display name when available. Check flags markers such as
`[Name]` left in run templates or example replies. Bind the missing value
or remove the placeholder before applying the app.

## Read a document in an app run

A run model works from the content supplied with its step. Pass document
text from a record or a previous source step. A link can be kept as a source
citation. Check flags prompts that tell a step to retrieve the link itself.

## Draft a follow-up message or another text artifact in a run

A run's `agent` step receives its declared inputs, including the selected
record and readable relationships. Write a step prompt for the artifact you
want. A step with its own prompt uses that instruction. A step without one
uses the app agent's default instructions. The text alone is available at
`$steps.<id>.output`. The engine checks the response and asks once more if
it receives tool-call syntax or narration.
If the second response still fails the contract, the run follows its error
handling without saving that response. Tools require a declaration on the
step. An `ai` step uses its declared JSON output schema and also gets one
repair attempt. Keep the approval on the save step to see the exact note
before it is written.

## Run a meeting brief before a calendar event

Use a kit 2 `record.before` trigger with a readable record type, `at`
naming that type's datetime field and a positive `lead` such as `30m`.
Set `when` to a condition like `external == true` to limit it to external
meetings. The app checks upcoming records while the engine is open.
It starts one run per record and start time in the lead window. The run
sees the selected record through `$record`. Check uses the app's example
records to try the trigger without waiting for their real dates.
