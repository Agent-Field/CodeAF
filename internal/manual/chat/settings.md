# Settings

## Find a setting — categories, search and advanced controls

Open `/settings` or press `ctrl+,`. Settings is organized into General, Models,
Memory, Tasks, AI teams, Permissions, Spending, Connections and Privacy. Wide
terminals keep the categories beside the controls; narrow terminals show a
category bar. Left and right change category, up and down choose a row, and
Enter changes it. The pointer can select the same controls.

Type to search across categories, including advanced controls and connection
services. Old setting names and configuration keys remain searchable. Clear the
query to return to browsing; Escape clears it first, then closes settings. Model
pickers have their own **Filter models** box. Each category keeps uncommon controls behind
`Advanced`; search can find them without opening that section first.

Selected-row help explains the setting. Scope and activation details appear
where known. A value controlled by an environment variable remains read-only;
change the variable at launch rather than trying to override it in Settings.
On/off controls change when activated. Settings with several values open a choice
list, with the saved value marked; opening or cancelling the list writes nothing.
Choose an option to save it. Text and number editors keep the setting explanation,
input and Save/Cancel controls together. Enter saves; Escape or Cancel discards
the draft. Invalid input stays beside its error without changing the saved value.
Any restart requirement remains visible while editing. A successful write shows
Saved. Remote AI team defaults wait for the host's acknowledgement; after Save
has been sent, the editor says Saving and offers Close. Closing that editor does
not undo the write already in progress.

## Did the update reset my settings — old profiles and existing preferences

No configuration migration is needed for the new categories. Existing profile
keys, saved values, project overrides and environment precedence keep their
meaning. The same reader, validator and writer still apply each registry setting.
For example, `show hints` presents the positive choice while preserving the
existing stored preference. Renaming or moving a control does not reset it.

Some controls previously visible in chat settings belonged to the separate
resident: practice idle time, practice spending, arrival briefs and tenure.
They are omitted here instead of suggesting that changing them teaches this chat.
Their stored values remain intact. Internal model slots that this surface cannot
change are omitted; supported model controls remain in Models.

## General — interface preferences and hints

**General** contains mouse interaction, completed tool details, tool icons, chat
switching, the task sidebar, updates and background reminders. **Show hints** is
positive: on shows contextual tips, off hides them. Keyboard instructions and
setting explanations stay visible either way. **Completed tool details** offers
**collapsed** or **expanded** without changing the meaning of an existing profile.

## AI teams settings versus an individual AI team

**AI teams** contains defaults for groups of AI chats, including questions to an
AI manager, turns started by team messages, spending and subteam behavior. It
does not configure human members or shared user accounts. A team may have no
manager, and a chat can belong to several teams.

An individual team's own Settings controls its supported overrides. Global
defaults do not erase explicit team overrides. New-subteam budget share applies
when a subteam is created; it does not redistribute existing budgets.

## Memory settings — saved versus active, inspect and forget

**Memory → remember across chats** controls the saved memory preference. It
supports learning useful preferences and corrections from conversations without
requiring every memory to be dictated. Use `/memory` or `/memories` to inspect
saved memories; the memory page offers its supported correction and forgetting
controls. A setting being on is not proof that any particular fact was saved.

Already-open chats keep the memory store they opened with. Restart the CLI to
apply a changed preference to those chats. Turning memory off does not delete
saved memories. Input history and unfinished drafts are separate Privacy choices.

## Permissions, task timers and automatic work

**Permissions** controls tool approvals and exceptions. Dangerous-command
exceptions still apply in permissive modes. An approval timer pauses when it
expires and keeps waiting; it does not silently approve a tool.

The proposed-task start timer has a different consequence: it starts the proposed
work when it expires. Read each control's consequence before changing its duration.
**Tasks** contains scope assessment, review, repair and concurrency; machine
resource limits are advanced controls. A one-off task does not require an AI team.
Under Advanced, **task scope assessment** offers **assess while working** or **skip assessment**.
Both start work immediately; assessment helps identify work to delegate.

**Concurrent tasks: no limit** means no user-set count cap, not disabled tasks.
A positive whole number caps simultaneous tasks, with the rest queued. Blank
restores no limit. CPU, available memory and provider limits can still hold new
work. Invalid input stays in the editor and does not change the saved preference.
The current engine reads the count cap at startup: restart the CLI to apply a
changed cap to already-open chats. Changing this preference does not stop a
running task. An unset task model follows the configured worker, falling back to
the current chat model when no worker model is set.

## Connect a service, change models or manage privacy

**Connections** contains service sign-in and provider credentials, web search
configuration and advanced remote-connection or custom OAuth application settings.
Ordinary account connection does not require you to create your own OAuth app.
**Models** selects supported models and reasoning effort; advanced controls hold
routing, internal role overrides and context limits.

**Privacy** separates input history, unfinished drafts, anonymous usage reporting
and **shared model recommendations** (formerly Model Pool). Recommendations can
**use and contribute**, **use only**, or remain **off**. Disabling one privacy
control is not a promise that all the others are disabled. Search accepts old
labels as well as the new organization.

Resident-only practice controls remain in resident configuration. They do not
appear in chat settings, and `/budget practice` cannot edit them from chat.
