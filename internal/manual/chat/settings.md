# Settings

## Find a setting — categories, search and advanced controls

Open `/settings` or press `ctrl+,`. Settings is organized into General, Models,
Memory, Tasks, AI teams, Permissions, Spending, Connections, Devices and Privacy. Wide
terminals keep the categories beside the controls; narrow terminals show a
category bar. Left and right change category, up and down choose a row, and
Enter changes it. The pointer can select the same controls.

Type to search across categories, including advanced controls, connection
services and paired devices. Old setting names and configuration keys remain searchable. Clear the
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

## Change settings by asking in chat

You can ask codeaf to change supported preferences: “hide hints,” “expand completed
tool details,” “turn timestamps off,” or “turn memory on.” It reads the
`settings` tool first, then uses `change_setting` to save the requested value
through the same registry as the panel. If these tools are not loaded, codeaf
loads the `settings` capability group. It does not need to edit config files.

Chat discovery uses the same categories and names as the panel, including old
names as aliases. Tool details pair readable choices with the exact values to
write; those raw values can differ from their labels. In particular, the legacy
`ui.hints` row means **disable hints** to the registry, so its raw `on` hides tips
and raw `off` shows them. codeaf should follow the returned mapping, not guess
from the positive “show hints” label.

Saved preferences appear when Settings is opened again. Interface preferences
changed by chat refresh when the turn finishes; changes made directly in Settings
refresh the interface immediately. Startup-only settings still need the CLI
restart stated in their receipt. Chat-written routing, speed guard and pinned-host
preferences also recommend restarting the CLI to ensure already-open chats use
them; the panel has separate live controls for those settings. The memory
preference needs no restart: it applies from the next turn, in every open chat.

Some controls remain yours to change in Settings: approvals, spending limits,
task concurrency, review safeguards and credentials. Task workers do not have
the settings tools. A refusal should be explained rather than bypassed with a
file edit. Over `--host`, the chat changes its host's profile; the local Settings
panel and remote AI team defaults retain the machine boundaries described in
the commands manual.

## Did the update reset my settings — old profiles and existing preferences

No configuration migration is needed for the new categories. Existing profile
keys, saved values, project overrides and environment precedence keep their
meaning. The same reader, validator and writer still apply each registry setting.
For example, `show hints` presents the positive choice while preserving the
existing stored preference. Renaming or moving a control does not reset it.

Some controls previously visible in chat settings belonged to the separate
resident's background routine: practice idle time, practice spending, arrival
briefs and tenure. That routine was removed and the rows went with it; a value an
older version saved stays in the profile, read by nothing, and is never reported
as an unknown setting. Internal model slots that this surface cannot
change are omitted; supported model controls remain in Models.

## General — interface preferences, hints and share teams between paired computers

**General** contains mouse interaction, completed tool details, tool icons, chat
switching, the task sidebar and updates. There is no background reminders row: codeaf
installs no timer, and automations run only while a codeaf window is open
(*Automations*). **Show hints** is
positive: on shows contextual tips, off hides them. Keyboard instructions and
setting explanations stay visible either way. **Completed tool details** offers
**collapsed** or **expanded** without changing the meaning of an existing profile.

**Settings → General → share teams** is on by default for paired computers. On the hosted sync service sharing needs sync storage approval (Settings → Devices → sync storage). It
shares teams, their members and settings, and recent Traffic as read-only history.
The choice stays on each computer. Turning it off on either computer stops sharing
between them: that computer stops sending and receiving teams and Traffic. What
was already exchanged stays, including encrypted copies in the vault. Received
history is hidden while sharing is off and is kept for when it is turned back
on. Unpaired computers are unaffected. A missing or empty choice reads on; a
damaged configuration or invalid value reads off, as shown in Settings.

## AI teams settings versus an individual AI team

**AI teams** contains defaults for groups of AI chats, including questions to an
AI manager, turns started by team messages, spending and subteam behavior. It
does not configure human members or shared user accounts. A team may have no
manager, and a chat can belong to several teams.

An individual team's own Settings controls its supported overrides. Global
defaults do not erase explicit team overrides. New-subteam budget share applies
when a subteam is created; it does not redistribute existing budgets.

## Memory settings — turn memory on, why isn't it learning, what does memory cost

Memory is **on by default** when no choice has been saved. In Settings → Memory,
**learn from chats and jobs** controls `memory.enabled`. An explicit saved off
choice stays off. In the desktop app you can also do
it from a Factory job's spend area, under Spent: "Memory is off · Turn on…". This
one switch per home covers noticing useful lines, learning from saved chats, and
job memory, whose calls count in the job's cost. A saved on or off choice is kept.
Terminal **Build knowledge base** also saves on and imports saved chats.

When on, memory learns from your chats and jobs so later work can use it, and
makes extra model calls on your key. Costs vary with your chats and jobs;
Spending shows actual recorded costs rather than a forecast or a percentage
from an unrelated example.

Off stops automatic memory model calls from the next memory step, with no restart.
A call already in flight finishes and is billed. Saved knowledge stays readable;
reading it costs nothing. Kept lines and `/remember` still work. Use `/memory`
or `/memories` to inspect, fix or forget saved lines.

Older homes with no saved choice also default on. An explicit saved off choice
stays off. Invalid saved values read off. Nothing saved is deleted.

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

**Run tasks on**, immediately after **Task model** in Tasks, chooses where a
task goes when you do not name a computer. It defaults to `this computer` and
reads eligible paired computers by shown name only when opened. The row
is absent on unpaired, quiet or Windows homes. An explicit `/task --on`, proposal-card choice or `computer`
argument wins. Quick tasks and work without an independent copy still run here.
Both ends need Linux or macOS, and the other computer must run `codeaf serve --take-tasks`. Plain `codeaf serve`
does not take tasks. The model's `computer` argument and a proposal's computer
line and cycle key appear only after the setting names a computer, or (for the
card) the proposal already names one.
See *Tasks on another computer* for limits and `run it here`.

## Connect a service, change models or manage privacy

**Connections** contains service sign-in and provider credentials, web search
configuration and advanced remote-connection or custom OAuth application settings.
Ordinary account connection does not require you to create your own OAuth app.
**Models** selects supported models and reasoning effort; advanced controls hold
routing, internal role overrides and context limits.

**Devices** lists your paired computers in the same words and order as `/devices`,
renames this device with `n`, removes another with `r`, and starts `/pair` from
`Add another device`.

**Privacy** separates input history, unfinished drafts, anonymous usage reporting
and **shared model recommendations** (formerly Model Pool). Recommendations can
**use and contribute**, **use only**, or remain **off** (the default).
With no saved choice, the pool makes no judge calls and writes no pool records;
each role still follows your task-model and tier settings, and in Auto the
router picks from your allowed models. A saved **use and contribute** choice
stays enabled. Disabling one privacy
control is not a promise that all the others are disabled. Search accepts old
labels as well as the new organization.

Anonymous token usage includes local models when their API returns token counts.
Streaming requests ask for those counts. A connected Ollama service is labeled
`ollama`; other local APIs are `other`. Missing text counts use a separately labeled byte-based estimate; media and unseen
output remain unknown. Opaque reasoning in the input also leaves its input estimate
unknown. Estimates do not enter billing or provider-reported totals. Provider
receipts replace estimates only for the token components they actually report.

There is no practice limit any more: codeaf no longer practises on its own, and
`/budget practice` answers with exactly that.

## Knowledge model for background learning

Settings → Models has a **knowledge** model choice (`knowledge.model`).
It controls the OpenRouter model used to extract, organize and review project
knowledge, separately from your conversation and task models. The default is
`deepseek/deepseek-v4.1-flash`. Clearing the choice restores that default.

A saved change applies to the next compilation without restarting your chats.
A compilation already running finishes with its original model. The compiler
verifies the model reported by the provider and rejects substitutions; it never
falls back to a different model. Existing notes stay intact when you change this
setting. Turn automatic learning on with the shared Memory setting.
A new profile whose first saved-chat inventory is empty enables capture automatically.
A profile with existing eligible history waits for **Build knowledge base** in
Memory before importing it; capture is profile-wide.
Compilation is paid model work recorded in Spend, including unusable answers.
It pauses at the daily limit and resumes automatically after you raise the
limit with `/budget day` or the local day changes.

## Unreadable profile settings and saved spending limits

If an existing `config.json` cannot be read, changing a setting refuses with
`write config: preserve existing file:` followed by the read error. The file is
left unchanged. Daily spending-limit reads return the read error instead of
substituting the default limit. Restore read access to resume these operations.
A missing file still uses new-profile defaults.

Preference and saved-provider-key sharing also refuses unreadable profile data.
An unreadable profile does not by itself block interactive startup: tool approvals
fall back to prompting, with saved profile exceptions withheld. Headless approval
resolution also falls back to prompting. A missing profile keeps its usual defaults;
malformed JSON still reports an error. Spending-limit reads and settings writes
continue to refuse unreadable files unless an explicit daily-limit override supplies
the spending authority.

## Devices — sync storage, off or on, and request access

Hosted sync storage is off by default while its privacy terms are being finished. Pairing alone does not enable it: new chats, files, saved keys, preferences, shared memories, teams and task copies wait. Pairing, device presence and live `--at` still work. After pairing, press enter on **Settings → Devices → sync storage** to ask for approval. A self-hosted sync service with `--store` needs no request.

A paired account whose service reports storage permission has a **sync storage** row showing **on** or **off**. While **off**, press Enter (`enter request access`) to open a pre-filled GitHub issue. The panel also shows the public request code and link if no browser opens. The code is your identity id (`id_` and 32 hex digits), safe to post and granting nothing by itself. Choosing this row in Settings search clears the search and opens Devices. While **off**, Enter also requests access. While **on**, it opens Devices without offering or making a request. The row is absent with sync off, before pairing, or when the service answers without storage permission. A window already open when you pair checks for the answer within about a minute; an open Settings page then updates its row automatically. The first `/status` after pairing shows the cached dim line `sync storage  off` while storage is off.

## Why Settings says held by an environment variable

A row controlled at launch says `held by <variable> — unset it to change this here`.
The name is the spelling that supplied the value. Unset that variable and start again
to change the row in Settings. See Starting codeaf, “The old name”, for older spellings.
