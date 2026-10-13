# Standing orders and reminders in the app

## Propose a standing order, ratify, change or decline

Say what should happen whenever a condition occurs. The agent uses `stand` to
save a proposed card containing when it wakes, what it does, what it won't do,
where it answers and its budget per run. Nothing runs before **Ratify**. The
proposal survives a restart. **Change…** edits the saved card in place;
**Not now** declines it and leaves a receipt. Every decision returns a receipt.

The app reads standing orders at `GET /app/v1/agents/{id}/orders`; decisions use
`POST /app/v1/orders/{oid}/ratify`, `/change` or `/not-now`. Optional `expect`
state/version values refuse stale decisions with 409. An Idempotency-Key replays
the same decision during this engine launch; different input under that key is
refused. Stopped and declined orders stay on disk and leave the active list.

## Answers in this chat, this crew, everywhere or not here

Scope selects where the order speaks, never where it looks. **This chat** sends
news to its originating chat. **This crew** sends it to the saved crew's current
members and is refused for an agent without a crew. **Everywhere** sends it to
all non-archived app chats, including closed chats. **Not here** sends it only to
the Inbox. Every scope keeps an Inbox copy, including completed reports. Notifications do not start a model
turn in the receiving chat. Moving a member out of a crew stops delivery to it.

The scope wire values are `this_chat`, `this_crew`, `everywhere`, `not_here`.
Existing orders start with this-chat delivery when first changed through the app.

## Asks first, earning trust, five approved runs and Not right

Every app order starts at **Asks first**, with zero approvals. A trigger saves
its exact evidence and asks **Go ahead · Skip this one · Pause order**. One
approval counts once; one through four approvals are **Earning trust**. At five
it is **Trusted alone**, running subsequent triggers without another run card.
Trust counts approvals rather than model assessments of success. Existing tool
permissions and spending rails still apply to trusted orders.

**Skip this one** changes neither trust nor approval count. **Not right** on any
saved run resets the track to zero. Pausing preserves the count. Changing the
trigger, action or its stated limits resets the track and consumes an old pending
run, so approval cannot execute changed instructions against old evidence.

Run decisions use `POST /app/v1/orders/{oid}/runs/{rid}` with `go`, `run_now`,
`skip`, `pause` or `not_right`. `runs` and `version` accompany each order; probe
evidence stays in the binary. A repeated decision on a consumed run is refused.

## Budget per run, pause, stop and skipped triggers

The default budget per run is $0.40; saved amounts are preserved. At the cap the
run stops and asks for a raise. The API's order waiting set includes the budget
ask. Raising `budget_per_run` through `/change` clears that hold; the next
trigger still follows the trust rule. A model call already underway can finish
before its billed cost is known, so the budget stops the next step.

**Pause**, **Resume**, **Stop** use `/pause`, `/resume`, `/stop`. Paused triggers
never execute their action and leave **Skipped while paused** lines with the
trigger and time. Pause consumes an existing pending trigger and preserves
trust. Stop is permanent for that item. **Pause all** pauses active recurring
orders for the agent, leaving reminders scheduled.

The app keeps checking with every chat closed. Its ticker uses the same native
store, pass lock, model routing and spending ledger as other binary doors.

## Remind me later, change a reminder time or cancel

A reminder is a one-shot standing order. The agent saves it directly and confirms
it with a notice; it has no ratification card or trust step. At its due time it
lands in the Inbox and its delivery chat, then ends. **Change** and **Cancel**
use `POST /app/v1/reminders/{oid}/change` with an RFC3339 `at`, or `/cancel`.

A past time is refused with 422 and **“That time has passed. Pick another.”**
Reminders cannot use run-approval verbs. Cancelling removes the reminder from its
list and keeps its record and receipt.

## Schedule work once instead of setting a reminder

A reminder says a line later. Scheduled work can run a task later, so it still
needs a proposed card and **Ratify**. At its due time it follows the standing
run approval rule, then ends after its one firing. It belongs in standing
orders, and cannot be changed or cancelled through the reminder-only routes.
App proposal questions offer **Ratify** and **Not now**; they do not offer a
once-now shortcut. Automatic app runs use deterministic names without AI titles.
