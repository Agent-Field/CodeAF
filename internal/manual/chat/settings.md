# Settings

## Find a setting — categories, search and advanced controls

Open `/settings` or press `ctrl+,`. Settings is organized into General, Models,
Memory, Tasks, AI teams, Permissions, Spending, Connections and Privacy. Wide
terminals keep the categories beside the controls; narrow terminals show a
category bar. Left and right change category, up and down choose a row, and
Enter changes it. The pointer can select the same controls.

Type to search across categories, including advanced controls and connection
services. Old setting names and configuration keys remain searchable. Clear the
query to return to browsing. Each category keeps uncommon controls behind
`Advanced`; search can find them without opening that section first.

Selected-row help explains the setting. Scope and activation details appear
where known. A value controlled by an environment variable remains read-only;
change the variable at launch rather than trying to override it in Settings.
Escape backs out of an editor without applying its unfinished value.

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
**Tasks** contains task planning, review, repair and concurrency; machine resource
limits are advanced controls. A one-off task does not require an AI team.

## Connect a service, change models or manage privacy

**Connections** contains service sign-in and provider credentials, web search
configuration and advanced remote-connection or custom OAuth application settings.
Ordinary account connection does not require you to create your own OAuth app.
**Models** selects supported models and reasoning effort; advanced controls hold
routing, internal role overrides and context limits.

**Privacy** separates input history, unfinished drafts, anonymous usage reporting
and Model Pool participation. Disabling one is not a promise that all the others
are disabled. Search accepts the old labels as well as the new organization.

Search stays visible above the settings list and searches every category, including Advanced controls and connected apps. Model pickers have their own **Filter models** box. Escape clears a settings search first; another Escape closes settings. Hints stay under General as **show hints**: on shows contextual tips, off hides them. Keyboard instructions and setting explanations remain visible either way.

Display choices use plain language while existing configuration values keep their meaning. For example, **before starting a task** offers **assess the brief** or **start directly**; **completed tool details** offers **collapsed** or **expanded**. **Shared model recommendations** can **use and contribute**, **use only**, or remain **off**.

Resident-only practice controls remain in the resident configuration. They do not appear in chat settings, and `/budget practice` cannot edit them from chat.
