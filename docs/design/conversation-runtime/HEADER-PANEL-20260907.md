# Aforge navigation panel — September 7, 2026

Design direction for the header refinement requested after trying revision
`0e095a9b1`. This document states the interaction contract; test receipts and
implementation status belong in the production audit.

## Purpose and hierarchy

The terminal is a place to operate and read. Its header should answer three
questions in order: which conversation, where inside its work, and what that
work is doing. The transcript remains the largest and highest-contrast reading
area. Preserve the existing restrained palette and terminal character grid.

The chat tabs are one group, the ancestry is a second, and execution facts are
secondary. A thin horizontal rule separates this navigation region from the
transcript and right-hand task list. A heavy surrounding border or a second
app-wide background is unnecessary.

Main chat: tab row, then separator. Task page: tab row, breadcrumb row, then
quiet status/metadata on the separator row. Model, cost and tool counts never
become breadcrumb segments. Only the current task receives primary emphasis;
known ancestors remain recognizable, clickable navigation targets.

## Four actions with distinct meanings

| Action | Meaning | Must preserve |
| --- | --- | --- |
| `+` New chat | Open a start page with a blank composer and recent chats. Create the conversation on first send. | The current conversation and its draft; no execution merely from opening or cancelling the page. |
| Chats | Find and open an existing conversation through the established switcher. | Stable identities, readable titles, explicit confirmation and existing ownership checks. |
| Tab `×` / Ctrl+w in Chats | Dismiss this view; keep the conversation discoverable in Chats. | Unsent text, caret, compact pastes, attachments and running work. |
| Stop | Explicitly stop the named work under the existing stop contract. | Retained artifacts and clear scope. |

The distinction between dismissing and stopping is substantive. A tab close
must not call an agent's Close or Interrupt method. Closing an inactive tab
must not select it first. Closing the last tab goes Home. With a shared engine
connection, dismissing the current tab goes Home without selecting another
conversation, because the current server still ends the previous conversation
on selection. This design does not claim server-side multiplexing exists.

Reopening a dismissed conversation restores its tab and owned draft. A redraw,
resize or new background event must not resurrect a deliberately dismissed tab.

## New-chat start page

Use the smallest existing start/composer machinery that can preserve recipient
ownership. Opening the page is a reversible navigation action, not a model call
or an empty session factory. Repeated presses reuse that page. Escape returns
to the same task/chat and draft. Choosing a recent chat resumes that chat.
Submitting a first message uses the existing creation and delivery contracts;
a failure retains the unsent message in the start page, never sends it into the
old chat. Files and compact pastes belong to the draft that received them.

Show a short list of recent chats with recognizable titles and enough project
context to distinguish duplicates. Defer predicted frequent items, favorites,
pinning and custom tab icons: each adds a separate organization concept and
needs evidence that recency and search are insufficient.

## Pointer and keyboard behavior

Tab labels have padded targets. Subtle separators are inert. Every tab responds
to hover, including the selected one; hover must differ from persistent
selection and must not move or resize labels. Reserve the close-control cells.
The close target highlights separately and consumes its own click. Moving the
pointer away restores the selected state, not an unselected default.

Chats is a labeled action at the right of the panel. The plus is a separate target at the right of the
header and means New chat, not overflow. Narrow layouts may use shorter words
and fold inactive tabs, but retain the active title and useful navigation.

Ctrl+w keeps its familiar word-delete behavior in the composer. In the Chats
picker it dismisses the selected tab.

Ctrl+k remains a preview until Enter, an explicit numbered selection, or a row
click. Escape cancels. Ordinary terminal protocols cannot reliably report Ctrl
release, so elapsed time never selects a conversation on the person's behalf.

Breadcrumb hover uses the same target boundaries as breadcrumb clicks. Root and
ancestors navigate; the current item remains the current item. A folded path
opens its nearest hidden ancestor. Guest or missing-owner data cannot invent a
navigation destination.

## Status and motion

Use one stable status slot only when the runtime can support its meaning:
needs the person's reply outranks working; idle has no decorative badge.
Internal checking, dependency waits and provider retries are not requests for a
person's response. A completed-but-unread indicator is useful only if it comes
from an actual unread receipt and clears when that conversation is read.
Unknown status stays absent. Never infer that a historical visited tab is still
running, or query a remote task index on every frame to animate a badge.

Click selection is immediate. Hover is immediate and confined to the target.
No sliding/reordering tabs, opacity animation on text, repeated pulses or new
frame clock for idle navigation. Existing work animation can continue within a
fixed cell where it represents actual activity. No-color selection and text
state remain understandable without relying on hue or motion.

## Adaptation and validation

Use a single header-height and row-coordinate contract for draw, hover, clicks,
text selection, body scrolling, sidebar and task controls. At short heights,
remove secondary chrome before sacrificing the composer and usable transcript.
At narrow widths, shorten metadata and fold ancestors before erasing the current
location. Long and same-named chats must retain their distinct destinations.

Validate main/task/new-chat, idle/working/needs-reply, local/shared, current/other/
last-tab close, failed create, repeated open, draft restoration and explicit
picker commit. Inspect real terminal frames together at 160, 80 and 60 columns,
plus no-color and short-height checks. Use one correction batch and at most one
visual confirmation pass; functional regressions must still be fixed and tested.

## Verification refinements

The final start-page footer names New chat and hides the previous conversation's telemetry. Task metadata reserves room for a visible divider even when a dependency has a long name. The task placeholder targets the actual draft row, preserving an attachment/effort tray above it. A guest task is reacquired from its exact owner on Escape; an unavailable connection uses the existing task-card refusal. An unnamed old conversation with unsent words blocks first-submit creation until the person returns to that draft, rather than transferring it to a different recipient.
