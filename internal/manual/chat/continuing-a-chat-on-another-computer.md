# Continuing a chat that one of your machines holds

To move a chat to this computer, select it on home and choose `move here` (the default while `idle handoff` is off; when **Settings → General → idle handoff** or `CODEAF_IDLE_HANDOFF=1` is turned on in a paired set, a released chat opens here automatically, while a live hold or a lapsed hold on another computer still offers `move here`). When your paired computer is offline or has let go of its latest chat, home can ask `Move tower's latest chat here?`: `alt+c` moves it immediately when there is no local choice to make, and `alt+x` dismisses the offer. Moving chats is available by default on Linux, macOS and native Windows with a compatible engine. Older chats become movable through independent copies after pairing.

## Continue on another computer — moving chats and pairing availability

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Older chats move through an independent copy. Once paired, codeaf prepares chats active in the last 30 days in the background, one at a time; opening another older chat prepares its copy then. This build shows and writes to the copy. The original files stay untouched and remain usable in older codeaf builds. While hosted relay storage is off, a chat still moves by blind pass-through between your paired computers when both are running codeaf; see *Blind pass-through*.

## Move here: also called "continue here", "take over" or "continue on this machine"

**Move here** is the one verb for carrying a chat from another computer to this one. If you are asking what happened to the continue button, or where did it go: nothing was removed, it was renamed. Older builds, older notes and older versions of this manual used the three names in this section's heading for the same act, on Home's rows, on the card that asks first, on the devices row and in the desktop app. Only the word changed. The card, its `1 move here` and `2 leave it there` answers, the `alt+m` chord on the devices row and the `alt+c` chord on the resume offer all work as they did.

If you remember one of those names, read it as `Move here` in every section of this page and on every page that names it. The sections below keep what the act does and what it refuses to do: *Move here says the computer that has this chat is not on*, *Move here does nothing* and *why move here failed* are the ones people reach most often.

## Blind pass-through — move a chat without relay storage (it used to be called sync storage), pass-through mode, move a chat while relay storage is off, does my other computer need to be on, move here without relay storage approval

On the hosted relay, relay storage is off for an account until it is approved (**Settings → Devices → relay storage**), and the service keeps no chat or file content while it is off. You can still move a chat between your own paired computers without it. This is **blind pass-through**, also called pass-through mode: the computer that has the chat sends its already-encrypted bytes straight through the relay to the computer taking it, and the service, which cannot read them, stores nothing. It is on by default; beyond pairing there is nothing to set up. Everything stays encrypted end to end.

Blind pass-through needs **both computers running codeaf**: a terminal, the app, or a background engine for that computer. It covers ordinary chats and their files, including a chat's `.env` and other secret files (*Do my .env files and secrets move to my other computer without relay storage*), and applies only when you have at least one other paired computer. Choose `move here` as usual. If the computer that has the chat is not on, see *Move here says the computer that has this chat is not on*.

Taking a chat on a second computer remembers it there, so that computer can serve it in turn: a third computer can take it from the second while the first is off.

Still waiting for relay storage approval, even with blind pass-through: saved provider keys and connections, memories, teams, shared preferences, idle handoff (an idle chat handing itself over) and task copies. They behave exactly as before while storage is off. A chat's own secret files are not in that list: they move with the chat.

When relay storage is approved later, chats that only lived on computers upload in the background by themselves. Until that finishes they can still be taken from a computer that is on.

Blind pass-through does nothing when storage is approved or when you use a self-hosted relay: those behave as they always did. *Turn off blind pass-through* says how to switch it off.

## Do my .env files and secrets move to my other computer without relay storage — will my secrets come with the chat, git shows my .env files deleted after moving a chat

Yes. While hosted relay storage is off and a chat moves by blind pass-through, the files codeaf keeps out of a chat's saved history move with it: every `.env` and `.env.*` file anywhere in the folder, committed templates such as `.env.example` included, and any other file that looks like it holds a secret, such as a private key or a README with a token in it. They arrive in the same step that brings the chat's files, byte for byte at the same path with the same file permissions, so git on the other computer does not show them as deleted.

They leave the computer that has the chat sealed under your key, inside the encrypted pipe, and nothing about them is stored anywhere but the two folders. Only the chat's own secret files move this way: saved provider keys, connections and credentials, preferences, shared memories and teams still wait for relay storage approval.

Where it stops:

- Both computers must run codeaf and be your own paired computers; a computer you removed gets nothing more.
- If the computer that has the chat is off, nothing moves, secret files included, and `move here` ends with the sentence in *Move here says the computer that has this chat is not on*.
- `CODEAF_PASSTHROUGH=off` turns blind pass-through off, and these files then wait for relay storage like the rest. With relay storage approved they travel through the key vault, as always.
- A file you edit on the second computer comes back when you take the chat back there. The file saved last wins, so a copy saved later on the receiving computer is kept.
- A secret file you delete on one computer is not deleted on the other by this road. A file too large for the key vault stays on the computer it is on (*A large file that looks like a secret did not arrive*).
- A secret file the computer that has the chat has not saved a turn since creating may not have been noticed yet, so it can miss the move until the chat's next saved turn.

## Move here says the computer that has this chat is not on — open codeaf there, then move here

When the computer that has the chat is not running codeaf, `move here` fails with this sentence, naming that computer:

`<computer name> has this chat and is not on right now; open codeaf there, then move here`

It says so within a few seconds. Nothing was moved and nothing is lost, and the chat's record still names the computer that has it. If you had typed words before choosing `move here`, they stay in the box and the sentence ends `; your words are still in the box`; a chat that opens by itself at launch has no typed words, so that ending is left off. It works as soon as codeaf runs there again: open a terminal, the app or its background engine on that computer, then choose `move here` again.

This happens while hosted relay storage is off, because blind pass-through carries the chat from the computer that has it. A chat that another of your computers has already taken can be served by that computer instead, so a third computer can take it from the second while the first is off.

If the other computer goes off after the chat has opened but before its files finish arriving, the chat stays open and the status line says `files couldn't arrive: <reason> · retry`. Once codeaf runs on that computer again, retrying fetches the rest.

A computer with blind pass-through turned off that meets such a chat on its list says so in a plain sentence instead of waiting: turn blind pass-through back on there. An older codeaf does not know blind pass-through and cannot fetch the chat; it ends `move here` with its usual could-not-continue line, and your words stay in the box. Update codeaf there.

## Why a chat shows as untitled on my other computer — blind pass-through and chat titles

While blind pass-through is carrying chats, WorkAF reads the title directly from the computer that has the chat. The relay keeps neither the title nor the folder name. If that computer is offline or runs an older version, Devices can show `untitled` until a title is available. After you move the chat, Devices reads the title from your local copy.

In WorkAF’s Devices page, **Preview** reads a short excerpt of the last saved message and reply. When the saved chat is unavailable, **Details** shows its device and move status. You can choose **Move here** from either view when the move is available.

## Turn off blind pass-through — CODEAF_PASSTHROUGH=off, pass-through mode off switch

Set the environment variable `CODEAF_PASSTHROUGH=off` before starting codeaf to turn blind pass-through off. Chats then stay on the computer that has them while hosted relay storage is off, exactly as before blind pass-through existed: see *Continue a chat on its own computer while relay storage is off*. Any other value, or no value, leaves blind pass-through on. `codeaf help env` lists it.

It changes nothing when relay storage is approved or when you use a self-hosted relay: those behave exactly as before. It is not a Settings row, and it does not turn off pairing, `--at` or anything else. With sync off (`CODEAF_SYNC_URL=off`) nothing leaves the computer, so there is no blind pass-through either.

A computer with blind pass-through off that meets a chat living only on another computer says so in a plain sentence instead of waiting. The switch holds on a computer that already shared chats by blind pass-through too: while it is off, that computer neither answers for those chats nor fetches a chat from another computer. Set it on each computer where you want the previous behaviour.

## Idle chats release their hold — open on another paired computer, even when the first is off

**Settings → General → idle handoff** (`idle_handoff`, or `CODEAF_IDLE_HANDOFF=1`) is **off by default**: while off, open idle chats keep renewing their hold and a released remote chat still uses **Move here**. When you turn `idle handoff` on with two or more paired computers (and relay storage on), an idle chat lets go of its hold about 2 seconds after it goes idle, once its saved turns, journal, working files, shared memory and settings are on the relay. The open window stays as a reader. A running turn, queued or steering work, a running task or job, unsaved tail writes or an upload failure keeps the hold. Rebuildable folders such as `node_modules` follow the usual transfer exclusions.

With `idle handoff` on, a chat explicitly released by its holder opens directly on another paired computer with no Move-here step: press Enter on terminal Home, or open its `can_open` row through `agents.open` in the app. The original computer can be off. A chat another computer still holds (`held on <device>` or `working on <device>`, including an idle chat when `idle handoff` is off or on an older build that never releases) still asks, with **Move here** as your choice. A hold that lapsed without a release also keeps that card, including one whose holder was working. This applies even when the last working state is unknown. Existing local edits use the ordinary kept-work rules during transfer.

Sending new work in the window that released its idle hold takes the unchanged saved chat back in place, with no prompt, before recording your words or calling a model. If another computer took it, the window gets the held-elsewhere refusal. A hold that merely lapsed (not released by its window) admits nothing until you reopen or continue it. Only one computer can win a simultaneous open and send. A released window needs a successful ownership check before it can accept work; an outage leaves that work unsent. Unpaired, sync-off, single-device and default (`idle handoff` off) homes keep their hold while open.

## Continue on another computer — continuing a chat on another computer, why can't I type in this chat, I cannot type in this chat, my message did not send and home opened with a question

While hosted relay storage is off, a chat still moves by blind pass-through between your paired computers when both are running codeaf; see *Blind pass-through*.

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Older chats move through an independent copy. Once paired, codeaf prepares chats active in the last 30 days in the background, one at a time; opening another older chat prepares its copy then. This build shows and writes to the copy. The original files stay untouched and remain usable in older codeaf builds.

A chat can be worked on by one device at a time. If you open a chat here that another of your devices is running right now, and you type a message and press enter, the message does not go to the chat. Home opens instead, with that chat's row under the cursor and the question up:

```
Move this chat here?
held on tower; last saved turn 4s ago
1 move here
2 leave it there
```

Your words stay in the box. Nothing was sent, and no branch nobody asked for was made. This card came up because you sent a message, not because you chose to continue, so `leave it there` is already chosen and leaning on enter changes nothing. Press `1`, then enter, to continue the chat on this device.

An attended window keeps following your other devices while the chat is open, so a move is shown without returning to home first. Slash commands such as `/help` always work.

If you continue it, the other device's window stops taking new turns and says `<device> moved this chat. This copy is read-only and may be behind`. The page *Home* has the rest of that card.

## Which folder does a continued chat use — repeated moves and long copy-folder names

A chat that already has a local folder keeps that folder when you bring it back.
A first incoming copy is filed under this computer's `v3/projects` folder.
Copy-folder names stay bounded and stable across repeated moves; long original
paths receive a short stable suffix. Existing short folder names stay as they
are. A long path on the other computer does not make the folder name grow on
every round trip.

## A tool finishes after the chat hold expires — late saves and kept turns

A finished tool call is saved locally even after this computer has noticed its
hold expired. That late save stays local and is kept as a separate copy when it
syncs, without replacing the shared chat's saved turn, and a fresh continuation
is needed for new work.

## Where an older chat's copy lives — the .moving folder, README.txt, can I delete it

An older chat's copy is kept in a folder called `.moving` beside the original, in codeaf's state folder for that project. The name is historical: nothing in it is a move in progress. It holds the live copies this build writes to, so deleting it deletes those chats. The folder carries a `README.txt` that says so. The originals beside it stay untouched for older codeaf builds.

## My typed message after move here — is it kept, does it send by itself

before showing this question for a send, codeaf checks which computer holds the chat now. an older listing alone cannot raise it. if that check fails, nothing is sent and the whole message stays in the box, including pasted text and attachments. the chat says `<host> has not answered; your message is still in the box, press enter to send it`. press enter to check again. changing the draft while a check waits keeps the current words unsent; the chat says `draft changed before sending; press enter to send your current words`.

a chat this window already holds, or one that has never been shared, sends without asking the service. an ordinary check for a shared copy waits up to five seconds. immediately after this window's own successful move, a message can wait up to 30 seconds for the chat to finish opening, then sends once. a window that lost the chat, or another window holding it, cannot give this window permission to send.

if Enter raised the move card, a successful move restores those same words, pasted text and attachments and says `your message is still in the box; press enter to send it`. it waits for another enter. choosing `leave it there` or a failed move also keeps the whole draft.

## which draft comes back after continuing a chat

a move opens one conversation for that chat. sending clears the words from its saved drafts, including matching copies left by an earlier window. taking the chat back, reopening it or restarting codeaf then shows an empty box unless you typed a new draft.

taking a chat from Home restores that chat's own saved draft, or leaves its box empty. while a continue started from Home is running, text typed on Home is held as the draft of the chat being moved and lands in its box when it opens. if that chat already had a saved draft, the box holds the saved draft first, then a new line, then the text typed during the move, and both are kept when you quit. it is never sent automatically, and pressing enter while the move is in flight never starts a new conversation from it. words typed for a different chat stay with that chat, even when both work in one place on disk. a draft held behind a send's continuation card stays with the chat that raised it. continuing a chat this window already holds keeps its open conversation and typed words.

## Read a finished task's worker conversation after moving its chat

Open `/task` and choose the finished task. Its saved worker conversation and steps remain readable with its outcome after moving the chat, even when the original working copy is no longer available. A finished task offers no restart action; typing `r` or `R` writes those letters in the box.

## What does leave it there do

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

On `Move this chat here?`, `leave it there` closes the question without moving the chat. Your typed words stay in the box and nothing is sent. If the question opened because you tried to send from an older window, this answer is already selected. Choose `1 move here` to bring the chat here before sending.

## Esc on a move card returns to my chat, and each card keeps its own draft

If Enter in a displaced chat raises the move card, `esc` or `2 leave it there` returns to that same chat window with its full draft still in the box. If you opened the card from a Home row, leaving the card keeps you on Home.

during an automatic continue, the first `esc` withdraws its opening and any waiting choice. the automatic chat is not opened: it finishes arriving and is let go, so it stays on this computer, free to continue from anywhere. a further `esc` navigates as usual while that finishes. if the abandoned move has not answered after two minutes, its request is cancelled before the waiting choice can begin. inspecting another chat's card and choosing `leave it there` or `esc` leaves the automatic arrival alone.

A move card previews only that chat's unsent words, or nothing when no draft is available. Its project foot belongs to the selected chat. Words typed in another chat never appear on this card. Saved drafts belong to their chat even when a copied chat moves to a different project folder; opening another copy cannot adopt them.

## slow chat listing — waiting for a service, a timeout or an unreachable host

at five seconds without a chat-list answer, Home says `waiting for <host>; still checking` and keeps its rows. the read continues for up to 25 seconds. a deadline says `<host> did not answer in 25s; still trying`; a failed connection says `<host> is unreachable; still trying`. background checks continue on their usual schedule. a successful answer clears the line.

these are Home's listing sentences. a message left unsent by a failed send check gets its own explanation: `<host> has not answered; your message is still in the box, press enter to send it`.

## Continue a chat on its own computer while relay storage is off

This section describes the road used while relay storage is off when blind pass-through is off (`CODEAF_PASSTHROUGH=off`) or unavailable, for example when this computer has no other paired computer. With blind pass-through on, a chat moves between your computers live while both are running codeaf: see *Blind pass-through*.

Reading history already stored for your account needs no storage approval. With no local work to keep, `1 move here` brings that recorded history here, even if this computer has no copy yet or its clean copy is older.

After a live `--at` session, come back to the original computer and choose `1 move here`. If its local history contains the last shared turn and any later local turns, or no turn was shared yet, the chat continues in place. Every local turn stays, the move card goes away, and you can send messages and use tools immediately. Nothing is uploaded while storage is off. Quit and reopen normally; after approval, opening the chat on this same computer publishes the preserved turns even if its earlier hold expired.

Keeping turns made here that another computer lacks waits for storage access only when they must be saved separately before replacing this copy. If those turns diverge from newer stored history, codeaf preserves them and says `cannot move this chat here while relay storage is off because this computer is missing saved turns; your local turns stay here`. Your typed message stays in the box. Request access in Settings → Devices → relay storage before continuing this divergent copy.

## Tools stop when the chat moves away while relay storage is off — a running turn, its tools and saved turns

This describes how tools are guarded while relay storage is off when blind pass-through is off or unavailable. With blind pass-through on, an ordinary chat is handled the way it is with relay storage on.

Before tools run in a shared chat while storage is off, codeaf checks who holds it. A reading is reused for at most 15 seconds with live updates, or two seconds without them. A change announced by the service refreshes it at once, even within those bounds. A failed reading is also reused for two seconds after it finishes.

When a reading finds another computer holding the chat, this window only shows the chat and refuses further tools, including commands and file edits. It stops saving new turns; saving work already accepted can finish. Choose `move here` explicitly to resume writing.

An expired hold belonging to this computer or a free chat still allows tools. An unreachable service also allows local work; a chat never shared makes no ownership request. No uploads start while storage is off.

## Why does move here say this chat was last held here — my only computer asks to continue

A chat this computer held last can resume sending after its hold expires when nobody else holds it. While relay storage is off and blind pass-through is not carrying the chat, Enter sends without a question if the local history contains every shared turn, including when later turns stayed only here or nothing was shared yet. The conversation stays open and the storage-off sentence is not repeated after idle time. After approval, opening the chat here sends its preserved turns to your other computers.

With storage on, differing saved turns still raise the question. Missing shared turns also require a choice or show the existing refusal while storage is off; local turns stay here.

The card still says `this chat was last held here; last saved turn 2h ago` when this window's own earlier move failed. Choose `move here` to resume it. The landing says `Moved here` and never names this computer as somewhere the chat moved from.

If unpublished work needs to be kept separately, the number of kept turns counts conversation turns. Intermediate saves and tool records within one question and answer do not become extra turns.

## What does held on tower or working on tower mean — a row on home and the verb it offers

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Home lists chats from your other devices. By default (`idle handoff` is off by default; turn it on in **Settings → General → idle handoff** or with `CODEAF_IDLE_HANDOFF=1`), an open idle chat keeps its hold and a released chat asks **Move here**. With `idle handoff` on in paired computers, an idle chat lets go about 2 seconds after it goes idle once everything is on the relay, and a released chat opens here directly: press enter on it to bring its latest saved copy here and open it directly (in the app, `agents.open` does the same on `can_open` rows). There is no Move-here card for a released chat when `idle handoff` is on, and a free row shows no holder label. A lapsed hold on another computer keeps the card, including when its holder was working.

A chat with a live hold on another device says `held on <device>` when it is open there with nothing running in it right now, and `working on <device>` while that device is in the middle of a turn; it changes back as soon as the turn ends. A relay too old to carry that word shows `held on <device>` either way, so a hold alone does not establish that work is running there. When a computer stops answering without letting go (for example its lid closed), its row says `<device> offline`; offline alone does not make a live hold free.

Opening a chat with a live hold still asks before moving it: the devices row offers **Move here** whether the chat is running there or went off, and Enter on Home's row opens the `Move this chat here?` card showing how fresh the copy is and how many turns may still be there (`esc` leaves it there). With remote control on and that computer online, Enter on a chat it is running opens the chat live instead, and the card is `alt+m` on Home (the page *Remote control* has the rest). Watching never takes a live hold.

## Continue on this machine — Continue where you left off, alt+c and alt+x

While hosted relay storage is off, a chat on a computer that is not running codeaf cannot be taken: `move here` says `<computer name> has this chat and is not on right now; open codeaf there, then move here`. See *Move here says the computer that has this chat is not on*.

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

If you open codeaf and a chat on another device was running or went off in the last 24 hours, and that device is offline now (its lid is closed) or has let go of the chat, home offers it first of all:

```
Move tower's latest chat here?
alt+c move here · alt+x not now
```

- `alt+c` continues immediately. The card asks first only when there is a real choice, such as keeping unsaved local edits as a branch.
- `alt+x` dismisses it for that device until you open codeaf again.

The line follows the devices row: it stands while the device is offline or has let go of the chat, even if that became true after you opened codeaf, and goes away if the device comes back. If the service stops supplying fresh presence while the card stands, a dim service-failure sentence stays in its place. The offer returns when fresh evidence supports it again. A device that is online and still working on the chat is never offered.

Home never offers this computer's own chat as work to continue from another
computer, even when an older record says this computer went offline.

To bring work from a device that is online, use the devices row: `alt+m move here`. The page *Adding a device to your devices, approving it, and removing it* shows the row and the dots.

## What happens when the chat arrives — the banner Moved from tower, and what was not brought

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

When the chat opens on this device it starts with one line that says how long the move took:

```
Moved from tower in 3.2s. Everything as you left it.
```

If a command was running on the other device, the line goes on:

```
Moved from tower in 3.2s. Everything as you left it. What was running there can start again here.
```

When the other device's name is not known it reads `Moved here in 3.2s. Everything as you left it.`

Right after, a card opens that says where the chat stands. It is the first thing on the screen of the chat. When something has to be set up here, the card asks whether to bring this device to the state the other one was in (the page *Home* has that question). When nothing has to be set up, the card only reports, with one answer that closes it:

```
Moved from tower. Here is where it stands.
not committed yet: main.go, util.go · last tests: passed
got it
```

## What was not brought along — folders, commands and tests on the arrival card

When a chat arrives on a paired computer, its arrival card can include these lines:

Each line is left out when it has nothing to say.

- `not brought along:` folders the copy left behind, and any log or saved output that was too big or looked like a secret, with the reason.
- `was running there:` commands that were running on the other device. They are listed so you can start them again.
- `also needed:` what this device also needs.
- `not committed yet:` the files with changes that are not committed, up to three names and then `and N more`.
- `last tests:` `last tests: passed`, `last tests: failed`, or `last tests: failed 2` when the run said how many. A line with no test run behind it is not shown.

On the set-up card, `set up` runs only the commands the card lists, and `not now` leaves it and says `/setup does this later`.

Typing while either arrival card is up goes into the message box, including the first character. Enter sends your words and leaves the card waiting. With an empty box, enter answers the card; use the arrows to choose an answer.

## Does a moved chat have the network — installs and access on this computer

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

A new chat is host-bound and keeps that class when moved. Its commands run on the computer that now holds it, with the network and the same access as any program you run there. Moving chats does not restrict commands to their workspace. `/setup` prepares missing tools and folders after a move; it does not enable ordinary network access. Existing cells declared sandboxed by an earlier build keep their declared class when moved or reopened.

## What folder does the moved chat work in — old absolute paths in earlier turns

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

A moved copy works in the workspace it arrived in. Before its next model request, the agent gets a session note naming the device it came from, the old folder and the new folder. Paths in earlier turns under the old folder now name the same relative paths under this workspace. The note is context for the agent, not a message you typed, and remains available after reopening or compacting.

The file tools also map an absolute path under the original folder to the same relative path in the new workspace. A shell command's old absolute path prefixes are mapped too. A mapped tool result says `Paths from the old folder <old> were mapped into the workspace here: <new>.` This applies even if the old folder exists on this computer. A mapped file path whose link leads outside the new workspace is refused. Paths to unrelated folders are unchanged.

Taking a chat back into its original project restores that project in place. The agent is told about the return too; old paths from the other computer's copy now map back into the original project.

## I deleted the project folder a chat worked in — will it still move here, or say could not be put in this folder

Yes. When the folder a chat worked in on this computer was deleted, `move here` and the automatic move at startup both open the chat. A folder that is not there holds no files to keep, so only the conversation is checked: the chat arrives the way a chat from another computer does, in a folder of its own under this computer's chats, with every file as the other computer left it, and the line `Moved from <device> in <T>. Everything as you left it.` The deleted folder is not made again and nothing is written to its old path.

If the conversation here holds turns that were never shared, the move asks first, as it does for any unsaved work, and keeps those turns as a separate copy of the chat before the files arrive; the deleted folder's files are not part of that copy. The engine log says `continuation: the chat's folder <path> is gone, so there are no local files to keep; checking only the conversation` each time the check meets a deleted folder.

A path that exists but is not a folder, or one this computer may not look at, is not treated as deleted. The move stops there and says `could not check this computer's copy before moving; your words are still in the box`.

## Why does the project label say copy from another machine

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

A chat arriving here for the first time works in its own copy. It is filed under the folder it came from, apart from conversations in this computer's own folder of the same name. Home and the keys-row label say `proj-a (copy from another machine)`. This remains a copy even if a folder with the original path happens to exist on this computer. Taking back a chat that already works in your own project restores that project in place, unless that folder was deleted here: the chat then works in a copy of its own. An unknown origin is left unnamed.

The same label appears with `codeaf chat --no-host`. The copy's own working folder does not change its label to `codeaf`.

Removing this computer from your devices does not change those project
labels. Switching away and back keeps the same copy label or ordinary project
name.

## Moving a chat with GIT_CEILING_DIRECTORIES set — an enclosing checkout and its ignore rules

codeaf's own capture policy follows Git's repository discovery when `GIT_CEILING_DIRECTORIES` is set. A ceiling equal to the workspace does not stop Git from finding an enclosing repository: its ignored files stay out of saved history. When an ancestor ceiling excludes that repository, codeaf's policy does not enable its ignore rules. A repository inside the boundary keeps its own ignore rules.

If the workspace's own `.furrowpolicy` contains `gitignore`, the native capture engine still uses its separate ignore discovery. It can find an enclosing checkout beyond the ceiling and omit the workspace's files. That case needs an engine fix.

If an older build saved an empty chat and the workspace does not enable `gitignore` in its own `.furrowpolicy`, open the original chat in this build, send another message and close it before trying `move here` again.

## This chat is not syncing — the relay is not answering, working offline

If this computer holds the chat and renewals have failed for longer than one
renewal period, one dim line says
`the relay at 127.0.0.1:8901 is not answering; this chat keeps working here and catches up when it is back`.
It is said once per outage.
When renewals succeed again, one line says `this chat is syncing again`.

You can keep working here while offline. If another computer continued the
chat meanwhile, the existing `<device> moved this chat; this window now
only shows it` notice still applies.

## Reopen a kept copy after another computer takes the chat

When unsent turns were kept as a separate copy, reopening that copy checks who holds the copy itself. Another computer may still hold the original chat. The previous window finishes saving and stops syncing before the reopened copy starts its own sync, so a late save cannot change which copy it opens. Taking that copy elsewhere still stops new tool work in its older window.

## Move here does nothing — move here did nothing, the engine store is busy, or a chat is already moving here

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

while a chat you explicitly chose is moving here, Home's bottom line says `opening conversation…` with the bytes received so far (`opening conversation… 24 KB`, updated every 500 ms; or `copying to this computer: …` when `CODEAF_MOVE_FILES_FIRST=1` is set, and `moving here…` before byte progress begins). another explicit continue during that move says `a chat is already moving here; wait for it to open` on the card. an automatic startup move instead lets your explicit choice go next; see “my choice while an automatic chat is arriving”.

every failure stays where you are looking: on Home's card with `1 retry` and `2 leave it there`, or inside the open chat window. a fresh reading decides whether a different computer continued first. if the same computer picked the chat up again, the card asks again with `<holder> picked this chat up again; press enter to move it here`. The desktop's `agents.take` refusal says `<holder> picked this chat up again`, keeping its `conflict` code and `holder_changed` detail. Nothing is taken by either refusal.

## why move here failed — retry or leave it there

a failed move keeps the draft and shows `1 retry` and `2 leave it there`. retry confirms the holder and turn of ownership now displayed on that card.


- `another device moved this chat first`: another device was faster. The row goes back to `held on <device>`. Nothing changed.
- `the relay is not answering`: this device cannot reach the service. Nothing can be moved until it can.
- `<computer name> has this chat and is not on right now; open codeaf there, then move here`: relay storage is off and codeaf is not running on that computer. Open it there, then choose `retry`. Nothing changed.
- `could not move this chat here because the engine store is busy` or `could not move this chat here because the engine store is busy (held by codeaf chat, pid 4321)`: another `codeaf` process holds this computer's engine store writer. Close or finish that process, then choose `retry`.
- `could not check this computer's copy before moving; your words are still in the box`: the look at what this computer holds for the chat, which comes before anything is moved, could not finish. Nothing was put in any folder and the chat stays where it was; the engine log has the reason after `continue chat <id>`. A folder that was deleted is not a reason: see *I deleted the project folder a chat worked in*.
- `could not move this chat here`: the move failed. The row stays where it was; choose `retry` or try again.
- A move does not fail because another chat on this computer is saving its secrets to the relay at that moment. Its last step waits up to 30 seconds for that save to finish, then arrives.
- `could not move this chat here because the relay is not answering; your words are still in the box`: the service went away during the move. Nothing was sent and the chat stays where it was; send again once the other machines are reachable.

## Why can't I take an open chat from another computer — older relays and --host

- `taking an open chat from another computer waits for a relay update; all else works, so close it there and move it here` means the chat is open on another computer and the relay is too old to take it from there safely. Everything else works, including continuing a chat the other computer has closed. Close the chat on that computer, then choose `move here` again.
- Under `codeaf chat --host`, `move here` is not offered and a chat the remote host already holds never shows as held elsewhere. With no way to continue set up, an off row has no `move here`: enter says why.

## taking a chat back from my other computer, and two computers continuing the same chat — which one opens

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

A chat has one holder at a time. To take a chat back from the computer that took it, press enter on its row on that computer: choose `move here` by default (`idle handoff` is off by default; when turned on in **Settings → General → idle handoff** or with `CODEAF_IDLE_HANDOFF=1`, a released paired chat opens directly while a live hold or a lapsed hold on another computer still asks for `move here`). Nothing has to be returned or switched off first. A computer with the chat open when another takes it says `<device> moved this chat. This copy is read-only and may be behind`, stops taking new turns, and keeps what it had not yet sent as a branch (`<K> turns from <device>: open separately / discard`). A tool call the model tries there answers with that same line. Taking the chat back leaves a `node_modules` or other install folder that computer already has alone.

The choice covers the holder and turn of ownership shown on the card. If another computer continues the chat before you confirm or while the copy is arriving, your choice cannot take it from that new holder. The slower one sees `another device moved this chat first` on the card with `1 retry` and `2 leave it there`; its local copy is not replaced, and its row goes back to `held on <device>`. if the same holder picked it up again, the refreshed card says `<holder> picked this chat up again; press enter to move it here` and asks for a new confirmation instead. a complete chat already held here gets one silent recovery attempt; another disagreement keeps the retry card and draft. No turn is lost on either side: whatever the earlier holder had not sent is kept for you as the branch above. This build offers discard, which archives a branch, and opening the branch as a separate chat; merging it into the original chat is unavailable. Pressing `2` (`leave it there`) on the question, or `esc`, changes nothing and leaves the chat where it is.

## Can tools run after taking a chat back and restarting?

After a successful take-back through `move here`, tools run here again,
including after a restart. Earlier retained turns remain available as a separate
branch, and the original chat includes the other computer's saved work.

## This window only shows the chat — can I still send here

Once this window learns that another computer holds the chat, Enter opens
**Move this chat here?** and keeps your words unsent. This applies as soon
as the live update or the `<device> moved this chat; this window now only
shows it` notice arrives; it does not wait for the next background ownership
check. Enter cannot start a reply, publish a turn or create a separate chat
from this displaced window. With remote control on, a window whose chat another computer moved opens a live view of the computer that has it now, marked `on <device> · live`, instead of staying a read-only copy (the page *Remote control*). The computer that took it may still be opening the chat when this window first asks, so the window asks again a few times over the next half minute, and the read-only sentence stays until one of those asks gets through. Open the latest chat from Home to bring it back before sending (choose `move here` by default; with `idle handoff` on, a released paired chat opens directly while a live hold or a lapsed hold on another computer still asks for `move here`).

A chat this computer still holds sends on one Enter without an extra request
to the relay. A chat that has never been shared does too.

## Take my chat back after a crash, a killed process or a power loss

If this computer was killed and another computer continued the chat, reopen
codeaf and open the chat from Home: choose **move here** by default (`idle handoff` off), or with `idle handoff` on a released paired chat opens directly while a live hold or a lapsed hold on another computer still asks for **move here**, as does the question raised by Enter in the old local copy. The move brings the latest saved chat here and
says `Moved from <device>`. An old saved hold cannot keep the chat here or
make that copy send a message. The question uses the other computer and last
saved turn reported by the relay. An offline holder is named as
`<device> offline; last saved turn <N> ago`.

Turns and file edits that never reached the service are kept separately before
the newer chat arrives. Shared turns and unchanged closing bookkeeping do not
need another upload before the move.

## I reopened an old local chat after the other computer let go — where are the newer turns

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Reopening an older copy does not replace newer history on your other devices, even if the other computer has closed the chat. By default (`idle handoff` is off by default; turn it on in **Settings → General → idle handoff** or with `CODEAF_IDLE_HANDOFF=1`), this window only shows its local copy until you choose `move here`. On paired computers with `idle handoff` on, opening a released chat from Home brings the latest saved history here automatically before you can write, including when an older viewer tab is already open. An older local copy does not overwrite newer history. A live hold or a lapsed hold on another computer still asks for Move here, including when that holder was working.

An already-open older window remains a reader after another computer continues the chat. Sending there refuses before recording your words. Open the latest chat from Home to resume it; if its hold is still live elsewhere (or `idle handoff` is off), the move remains your choice.

Locally saved turns that never reached your other devices are kept as a branch before the newer copy replaces them, including turns already sealed on disk. Unsaved workspace edits are sealed and kept too. The move says `your unsaved edits here were kept as <K> turns from <device>`. Open that branch as a separate chat or discard it; merging is unavailable.

Home folds the kept turns onto the row as the dim note `1 copy`, so they have no row of their own to press enter on. Press `→` on the row, then `k` (`k kept turns`). The card asks **What should happen to these turns?** and offers `1 open separately`, `2 discard`, and `3 leave them`, with `leave them` selected. Choose a number, then enter. With several copies each `k` shows the next one, ending `(copy 2 of 3)`. `open separately` opens a chat with its own conversation id, the kept history and the files as those turns left them, in its own copy on this computer. The main chat's history and files stay as they are. Discard archives the kept turns; it does not delete them.

## Unsaved edits after taking a chat back — usage, cost, title failures and memory

Turns already shared do not become kept edits. Usage, reconciled task cost, naming failures and memory bookkeeping alone create no extra branch, kept-turns row or “kept as” notice. Their recorded usage stays in this computer's usage ledger. Opening an unchanged chat adds no empty turn, including when another computer holds it.

## Spending after a move

Saved calls missing from this computer’s ledger are added once. Shared call identities distinguish separate calls with identical prices and tokens. Remote charges keep their event identities alongside ordinary saved calls. Late receipts stay in the home where the chat opened.

Old unidentified calls use settled ledger rows as an ordered prefix. New live rows and remote events cannot cover that prefix. Unrelated unmarked rows, queued receipts and out-of-order old calls cannot be matched exactly. Old files load without migration.

## The chat is arriving on this computer — why it waits to open

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

When you move a chat here, codeaf fetches the conversation records and claims the hold first, so the chat opens and accepts typing in about a second without waiting for the workspace files to finish copying. While those conversation records are being swapped into place, pressing enter on its row says `this chat is arriving on this computer; it opens by itself in a moment`. Opening a second window during that brief swap is refused so no window reads the journal as it was before the move.

When the holder stops answering (for example, its lid is shut), the row may read `<device> offline`. While its hold is live, Move here still asks and the card says `last saved turn <N> ago; up to <K> turns may still be on <device>`. Once the holder releases the hold, Move here brings it here by default (`idle handoff` is off by default; when turned on in **Settings → General → idle handoff** or with `CODEAF_IDLE_HANDOFF=1`, opening the paired released chat brings it here directly). A hold that merely lapses keeps the Move-here card, including when its holder was working or its last working state is unknown.

Turns that never left that computer are not in the copy you get. If its window is still running when it reconnects, it keeps those turns as a branch. After a restart, opening the latest copy keeps unpublished local turns and workspace edits as a branch before replacing them. Open the branch separately or discard it. Merging into the original chat is unavailable.

## Why is it slow, files still arriving, can I type while it moves — background workspace files and CODEAF_MOVE_FILES_FIRST

When you continue a chat from another computer, Home's bottom line shows `opening conversation… N KB` (updated every 500 ms) from your keypress until the chat opens, and any text you type on Home during that move is held as the arriving chat's draft and placed in its message box when it opens (pressing Enter on Home during the move never starts a new conversation or sends the draft). The transcript opens and the message box accepts typing as soon as the conversation records and the hold arrive. It does not wait on workspace files, the key vault, task working copies, Git status or local indexes.

Workspace files keep arriving in the background after the chat opens. While they arrive, the status line says `files arriving` with the bytes copied so far (for example `files arriving · 40 of 112 MB` or `files arriving · 12 MB`, never `0 of 0`), and clears when the folder is in place. You can type and send messages while files are still arriving, and the model can answer in words immediately. Any tool or command that reads or writes the workspace or runs in it waits for the files to finish arriving (bounded by the 2-minute stall limit) rather than seeing a half-arrived folder.

Each object download is retried at least 3 times with backoff on transient errors before it counts as failed. If the background download still fails, the chat stays open, tools keep waiting rather than running on a partial tree, and the status line names the problem in plain words with a retry (`files couldn't arrive: <reason> · retry`). Retrying (`/retry` or the next tool call) keeps the objects already downloaded. The refusal holds in every window and process on this computer that works in that folder, each with the same reason, however long the failure is old: it does not run out on a timer, and it ends when a retry succeeds. If the window that was bringing the files closes first, the refusal stays, with the same reason and `the window that was fetching them has closed, so move the chat here again to fetch them`, because the folder is only part of the chat's files and nothing should use or save it as if it were the whole. Opening the chat again (Home, then the chat; `/resume`; resuming it from the terminal; or `agents.open` in the app) fetches the files again without moving the chat, so a chat this computer holds has nothing else to press: this computer keeps its hold, nothing is set aside or saved from the partial folder, the open is not held up while the files come, the status line says `files arriving` again, and the refusal clears when the files have landed. An open never starts a second fetch beside a window that is still bringing the files, and with the relay out of reach the chat opens as it was and the refusal stays. A chat that another computer holds now is not fetched by opening it and still refuses, with the same words; move it here from Home (or `agents.take` in the app) to fetch it then. Set `CODEAF_MOVE_FILES_FIRST=1` to restore the old order that downloads and puts all workspace files in place before opening the chat and shows the `copying to this computer` counter on Home while they copy.

## Were my own changes in the folder kept or written over when the files arrived — unsaved edits here, could not be kept yet

When a chat opens before its files arrive, this computer's older copy of the folder may hold a change that was never shared: an edited file, a new file, or something inside `.git` such as a half-written commit message (`.git/COMMIT_EDITMSG`). Before the arriving files replace that folder, codeaf keeps the change as a separate copy of the chat on the relay and says `your unsaved edits here were kept as <K> turns from <device>`. That sentence can come a few seconds after the chat opens, because the check for a change runs behind the opening when this computer has no saved record that the folder is untouched. The opening line `Moved from <device> in <T>. Everything as you left it.` is about the conversation and does not wait for it. Open or discard the kept turns with `→` then `k`, as described above.

If the change cannot be kept, for example because sync cannot be reached, the files from the other computer are not put in place. The folder here stays exactly as it is, tools that use the folder keep waiting, and the chat says `your unsaved edits here could not be kept yet (<reason>), so the files here are left as they are`. The status line shows `files couldn't arrive: your unsaved edits here could not be kept yet (<reason>) · retry`. Retrying tries to keep the change again, and once it is kept the files arrive. Nothing is replaced and nothing is said to be kept until the kept copy exists on the relay.

## Can the chat on the other computer still open the job logs it mentions — logs, saved tool output, task journals and the task history after a move

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Yes. A chat's messages name files that sit beside the chat and not in your project: the log of a background job (`log at …/logs/jobs/2.log`), the full output of a tool result that was cut short (`full: logs/stubs/9c2f.txt`), and the journals of its tasks. They move with the chat. The other computer puts the logs and saved output back in the chat's own folder, so the same names open there. The task journals (each task's transcript, trajectory and saved output) are part of the chat itself, so they arrive whole inside it at the paths the chat reads them from. The first message it receives after the move says where they are now (`… that earlier messages name under <old folder> were carried: they are under <new folder> here`). A path in an old message still shows the old folder; use the new one.

## Job log and saved tool output limits after a move — task journals stay whole

Three limits keep a move light, and they apply to job logs and saved tool output only. A single such file over 1 MiB stays behind, and so does whatever does not fit in 16 MiB together (the oldest go first). A task journal has no size limit and always moves whole. A file that looks like it holds a secret (a key, a token) stays behind too. Each one is named under `not brought along:` with the reason and `stayed on the machine that made it`; read it there by asking that machine, and never assume the other computer has it.

Two things are made again on the other computer and are not carried: the project's task history (the list of finished tasks home and the tasks tool show), which is rebuilt from the chat's own record, and the git bookkeeping inside a task's working copy. The program's own files inside a task's working copy (the `.codeaf` folder, where a task's job logs sit) are not carried either, because they are not your work: they never show up on the other computer as a change you did not make. A file you made in the working copy does move. A task that finished on a kept branch keeps that branch in your project on the other computer, at the same commit.

## What moves with a chat — is my git history sent, branches, secrets in my repo

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Your repository's `.git`, including its history, branches and settings, moves with the chat to your own paired devices. A task's commits travel too, and its working-copy bookkeeping is rebuilt on arrival. A key in an old commit remains in that history: codeaf does not remove or rewrite it. Git history travels sealed end to end; the relay sees only ciphertext, including when the history contains keys.

The current working files are checked separately. A `.env` or source file that the guard recognises as holding a key is left out of the chat's file capture and carried on its own: through the encrypted vault when relay storage is approved, and by blind pass-through with the chat while relay storage is off (*Do my .env files and secrets move to my other computer without relay storage*). If you committed that secret earlier, its historical Git copy still moves with the repository. An untracked `.env` with no committed copy arrives only by that separate road.

## How are secret files screened before saving or moving a chat — .env, ignored folders and changing files

Before each seal, codeaf asks which files will be captured and screens those candidates, including a full candidate list for a first reply or a capture that needs a full scan. If a file changes between screening and capture, saving plans and screens again. If repeated edits prevent a stable capture, it says `files kept changing while saving this chat; try again` and keeps the calls for a later save. A wholly git-ignored directory is neither entered nor screened unless it holds tracked files. An ignored file in an ordinary directory is still screened: root `.env`, `config/.env.local` and a private key in a `*.pem` file travel to paired computers on the separate road, the encrypted vault or blind pass-through. A withheld file stays out of local saved packs and the uploaded workspace capture. The chat does not mention a file withheld for a secret: the engine's log names it with its rule, and the cell's `.furrowpolicy` lists every withheld path. Tools still see a `.env` file's variables on the other computer once it has arrived. An unreadable folder stays out until its permissions recover; its newly eligible files are then screened with a fresh capture plan. Restores and arrivals also screen before an automatic safety capture.

## A large file that looks like a secret did not arrive — the key vault's size limit

The encrypted key vault carries secrets, not files of any size, and blind pass-through applies the same limit to a chat's secret files. A withheld file — one the guard kept out of the chat's saved files because it looks like it holds a key or a token — travels to your paired computers only when it is at most about 768 KiB, which encodes to the vault's limit of 1 MiB per entry. A `.env`, a credentials file or a private key is far below that.

A larger withheld file, such as a database, a git pack or a built binary that happens to contain a key-shaped string, is still kept out of every save. It stays on the computer that has it and does not travel. The engine's log says `it is too large for the key vault, so it stays on this computer` when it is first withheld. A copy of the same path from another computer never writes over it, and a removal on another computer never deletes it.

A vault that already held such a file from an earlier build drops that entry the next time it is opened and shrinks. The file itself is left where it is on every computer.

## Why does home appear while my chats are still syncing

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

The first screen draws while this computer publishes its device record, shares its saved keys and sends the first saved turn in the background. A slow relay does not leave the terminal blank. New tool work waits for the initial ownership check; local startup saves are sent once that check finishes.

## Does every chat copy my whole project — disk space, shared saved files, older chats

New chats on this computer share one store of saved file contents. Opening four chats in the same project does not make four private copies of every unchanged file. Each chat keeps its own history, call log and transport scratch; restoring one does not move another chat's saved head. A chat whose older private store already exists keeps using that store in place, without migration. Nothing is deleted automatically. `codeaf cell gc` is an explicit command and removes only files the saved copy can restore.

## Why did dist or node_modules not move — gitignored files, build output, ignored folders

Every chat's capture follows Git's ignore rules. Untracked ignored output stays on the computer that made it: it is neither stored in the local file history nor uploaded. Tracked files travel even if an ignore rule matches their name. A wholly ignored directory is not walked or screened. Lockfile rebuild folders still follow the existing rebuild rules.

The arrival card names omitted paths from the saved summary, for example `not brought along: dist/ (git ignores them), node_modules/ (git ignores them)`. The summary is bounded and can name only some omitted groups. Ignored secret files in ordinary directories are still screened and travel on their own road, the key vault or blind pass-through; an ignored `.env` inside a wholly ignored directory stays there with that directory.

If Git cannot read the project's ignore rules, saving stops with `cannot read which folders Git ignores here` and the cause. Completed calls stay in their local log for a later save. codeaf does not fall back to searching ignored directories or putting their secret files in the key vault or moving them by blind pass-through.

## Does saving slow my command — pending save, quit while saving, moving before the save finishes

A command's result returns after its call log is written, while its file capture finishes in the background. The next tool call waits until the previous capture has finished reading files. Continuing the chat on this computer, uploading it, closing it, quitting codeaf and `codeaf cell gc` also wait for both the call log and the saved files to reach durable storage. A storage failure is reported and stops that boundary. Opening a chat screens its folder in the background without sealing an empty turn. A command submitted before that screen finishes waits for it. A window that only views a chat held elsewhere does not seal turns. A failed capture keeps the finished call in its local log for the next save; codeaf never runs that command again to recover it.

## Quitting while saving — local confirmation and a slow upload

Quitting another chat does not invalidate a chat's earlier confirmed local save.

A save wait lasts at most 30 seconds. After the screen closes, a pending sync save says `saving this chat to your other devices…`. After about three seconds, a confirmed local save adds `still saving — ctrl+c leaves it saved here`; without confirmation, it says `still saving on this computer — ctrl+c leaves the written turns here`.

If the turns are saved locally but not uploaded, the final sentence is `saved here; it reaches your other devices when the relay at 127.0.0.1:8901 answers`, using your service’s host. If that pending upload was refused because storage is off, the successful receipt is `saved here; relay storage is off for this account, so it stays on this computer`. That is a successful exit. Ctrl+c during this wait leaves promptly with the same receipt when local saving is confirmed. Without confirmation, it says `saving on this computer has not finished; its written turns stay here` and exits unsuccessfully. Only a failed local write says `codeaf could not save this chat`; that failure may include the local recovery path.

If the last upload fails when you quit, for example because the connection dropped, this computer does not let the chat go. Your other computer keeps showing it as running here and does not open the older save by itself; the unsent turns stay on this computer until an upload succeeds.

## Final saving sentences when closing a chat, quitting, /new or /resume

After a chat finishes saving and closes, it no longer checks who holds it. A final save can find that the chat has moved to a different machine. Its sentence appears once in the window when you close one chat, or when `/new` or `/resume` closes the previous chat. On quit it appears once after the screen is restored, including when an earlier individual close was still saving. A sentence already shown in the window is not printed again after quitting. This also applies with `--no-host`.

A window that stops reading does not hold up another window closing the chat. A live saving sentence delivered after the window's last display check joins the restored-terminal report. Taking it for display or for that report consumes it once.

## I pressed alt+m before the other chat appeared — still arriving, home on a second computer

Device presence can arrive before its list of chats. If you press `alt+m` early, home says `chats are still arriving from your other computer; this opens when they are ready`. It keeps your request and opens the ordinary confirmation once the chat arrives; it never takes the chat without that confirmation. A recent chat left on another computer is offered when home opens, including one that computer closed cleanly.

## How long does moving a chat take — why a move is slow, moving back is quick

What a move costs depends on what this computer already has.

- **Moving a chat back** to a computer that held it before downloads only what the other computer changed. In a big folder, the rest of the move stays a second or two, plus the download.
- **A chat this computer never held** copies its whole saved folder. It takes as long as sending that many bytes through your connection to the server.
- **Taking a chat from a computer that is off** also restores this computer's own copy of the folder. On a large folder that adds a few seconds.

Before a move restores files into your folder, codeaf checks the files there for secrets. A file this running codeaf has already checked, and that has not changed since, is not read again. A file that changed is read again. After codeaf restarts, its first move in a folder reads every file once. It remembers at most 100,000 files. Files beyond that are read again on every move.

## a new chat brings its full saved contents; a returning chat brings its changes

a chat this computer has never held opens as soon as its conversation records and hold arrive, and brings its saved workspace contents in parallel in the background, even after other chats used this store. a returning chat downloads only what changed. tools that touch the workspace wait until all saved contents have arrived and been put in place (`CODEAF_MOVE_FILES_FIRST=1` waits for all files before opening instead). sharing saved file contents with other chats does not make a new chat count as one this computer already held.

## Does first start send anything — local identity before pairing

The first start may create a local identity for this computer. That file sends nothing: there is no sync traffic or network use until you pair or offer a pairing code. Reading the add-device instructions leaves it quiet. Offering a pairing code or running `codeaf pair` contacts the relay for that pairing only: chats start syncing once you approve the other computer, and a code that is taken back, runs out, is answered no or is used up by a wrong code leaves this computer quiet. `CODEAF_SYNC_URL=off` blocks sync traffic even after pairing.

## What survives a crash or power loss — durability, killed process, lost call records

If the codeaf process dies, written call records and finished results survive; recovery never runs those commands again. An interrupted external command is shown as incomplete rather than retried. A machine losing power can lose at most roughly the last second of call records and saves. Losing a record never causes its command to run again, and a torn final record is discarded without damaging earlier history.

Call records are written in order without waiting for a disk flush on each command. A background group flush runs about every 250 milliseconds. Moving or uploading the chat, becoming view-only, quitting and closing wait for the pending save and disk flush; a failure is reported. Saving flushes call records before relying on saved files. Recovery checks saved receipts before replaying completed call records. New calls have unique identities, so identical parallel calls stay separate even when they start in the same millisecond. Older records without identities still match by arguments and start time. If the saved log or a receipt is unreadable, recovery logs `recovery could not check saved receipts; keeping WAL replay unchanged` and replays the local call log as before; it cannot prevent duplicate receipt entries in that case. Recovery never runs external calls again.

## Do attached files keep their old paths when I move between Windows and Linux

A saved attachment reference keeps the source computer's path spelling with slashes, including Windows drive paths. It names where the file came from; it does not redirect work to that old folder. A job reads the saved attachment bytes and stages its copy in the workspace here, even if the original file moved or disappeared.

## Can I rewind an old chat on my new laptop — where does the copy start

Yes, after the older chat has a movable copy and you continue it on your paired laptop. Its first saved file state is the folder as it stood when the copy was made. Rewind can reach that first saved turn; file states from before the copy were never saved and cannot be reconstructed. The whole earlier conversation is still there to read.

Recent older chats are prepared in the background after pairing. If you explicitly open an older chat whose copy is not ready, opening that conversation waits for its first folder snapshot to be saved. Large folders can take longer; Home's initial display does not wait for the background batch.

The original older chat is untouched. A turn typed into the original by an older codeaf after the copy was made stays in that original; it is not merged into the copy. This build lists only the copy in place of the original, with its title and ordering retained.

## Turns typed in an older codeaf are not shown here — where are my rollback turns

If you return to an older codeaf after this build made a movable copy, turns you
type there remain in the original `transcript.jsonl`; they are not merged into the copy.
Opening the chat here shows a notice such as
`2 turns typed in an older codeaf are kept in <file> and are not shown here.`
`<file>` is the original transcript's location on the computer holding it, with
`~` used for that computer's home. The original is untouched. Open that file to
read the retained turns; nothing is merged automatically.

The notice appears in the conversation once and appears again only if the original
grows again with more person turns. The count is the total since the copy was made.
Moving the chat to another computer and taking it back preserves this notice
and its original count.
Copies made before this build may lack a reliable original-history count; those
show no warning rather than guessing. A headless open does not consume the notice.

## I quit on my laptop and opened my desktop — do I need alt+c

While hosted relay storage is off, `idle handoff` does not run, and a laptop where codeaf has quit cannot hand the chat over: the desktop can take it only while codeaf is running on the laptop (a terminal, the app or its background engine). See *Blind pass-through*.

A clean quit lets go after the last save is durable, and when **Settings → General → idle handoff** (`CODEAF_IDLE_HANDOFF=1`, off by default) is turned on, an idle paired chat also lets go about 2 seconds after it goes idle once everything is on the relay. Opening bare `codeaf` on your other paired computer within 24 hours continues the released chat automatically, with the usual `Moved from` banner and `not brought along:` lines. `esc` goes Home. A laptop that went to sleep while still holding the chat (for example mid-turn, before the save finished, while `idle handoff` is off, or on an older build) has not let go: while its hold is live, Home offers the chat and `alt+c` continues it, asking only if local work needs a choice; once the hold lapses, pressing Enter on its Home row still asks Move here.

If this computer is also coordinating, a clean terminal quit releases its idle chats and returns coordination to an anchor before stopping. With the anchors reachable, the other computer can continue immediately. Sleep, a crash or a lost connection still use the fallback described on the coordination and workers page.

after the chat arrives, enter can wait up to 30 seconds for this window to finish opening the chat before sending once. if the check fails, it says `<host> has not answered; your message is still in the box, press enter to send it`. the window keeps following changes, including renamed computers and a later move to another computer.

A running chat on the other computer never moves automatically. Neither does a named chat, `--host`, `--at` or `--session`, a chat with unsaved local edits, or a launch while another local window already has a chat open. if the service cannot be reached, Home names its host and keeps trying. if another computer got there first, the refreshed move card explains the change and keeps the local chat as it was.

## my choice while an automatic chat is arriving — continue a different chat, join it or cancel

bare startup still chooses the most recently released eligible chat and checks local safety first. input before that check answers cancels the automatic choice.

if its move is already running, confirming a different chat on Home or using `alt+c` chooses what opens next. Home says `moving <Y> next; <X> is still on its way here`, with the titles from the move cards. the automatic move settles first and is let go with its files and kept work intact; it never opens over your choice. the next move uses exactly the computer and turn of ownership you confirmed. `esc` cancels the waiting choice and the automatic chat still settles and is let go.

confirming the automatic chat itself joins its move and opens it once. explicitly opening another local chat or starting a new one also prevents a late automatic arrival from replacing it. moving the cursor, opening Home or inspecting and declining another card does not choose a different chat. an explicit move already in progress still gives the usual busy sentence for another explicit pick.

## Why an older chat could not be copied — symbolic links, home folder

An older chat whose folder is a symbolic link, or whose saved conversation state contains symbolic links, is left untouched. codeaf refuses to copy those links because writing through one could change the original. The original still opens with an older codeaf.

An older chat that worked in your home folder, or in a folder that holds it, is not copied either. A copy saves the whole folder as it stands, and your home folder is not one project: saving all of it takes minutes and holds up every open chat's tools meanwhile. That chat keeps opening where it is, on this computer only, and it does not move to your other devices. Chats with their own folder are copied as usual.

## A save has not reached the other computer yet — upload failed or another device continued first

While hosted relay storage is off, nothing is uploaded; the other computer reads the saved chat straight from this one by blind pass-through while both are running codeaf. See *Blind pass-through*.

A saved chat reaches your other computers only after its encrypted objects have
been uploaded and its new saved head has been accepted. If an upload fails, the
other computer keeps seeing the previous saved head. Retry when the connection
returns. If the final save fails during quit, the work stays local and that computer does not announce a clean release; another computer does not automatically open that incomplete save. Uploaded objects are kept for reuse if another device continued the
chat first; this does not discard the work saved on your computer.

## Which computer coordinates: cluster status and cluster lead

On the hosted service, or a self-hosted service with persistent storage, an open paired home joins as a learner. Three persistent voters keep coordination available when your laptops sleep. In this release, the coordinating computer cannot be chosen by hand; `codeaf cluster lead` is an unknown command. `codeaf cluster status` shows which computer coordinates and shows this computer after joining. Tracked in issue #2559. Continue chats through the ordinary continue door. See the coordination and workers page for command refusals and setup details.

## Can a worker access all my chats or approve pairing?

A delegated worker can access only its selected chats. It cannot approve pairing, rotate your identity, read your key vault or request leadership. Worker setup is not available as a terminal command yet. Personal computers keep their shared trust model.

## Worker restarts, draining and learner metadata visibility

A restarted worker uses a newer process number; delayed requests from its previous process cannot save work. Draining prevents new claims, while already held work can finish saving. Joining the full coordination log requires explicit consent and exposes identity-wide routing metadata, with chat contents and names still encrypted.

## How does a delegated worker claim an assignment or run parallel tasks?

An assignment names the selected chat and target worker. The worker downloads the saved chat, conditionally claims its observed ownership, then downloads the accepted saved head before opening it. Registration alone never starts work. Parallel tasks use separate child chats. Worker setup and execution are not available as a terminal command yet.

## Will an interrupted worker replay tools after a crash or reassignment?

No. The worker saves its claim and an execution intent before invoking tools. A lost reply retries the same coordination request. Once execution may have started, reopening reports `this work may be incomplete; its saved records have been kept` and keeps the records. It never runs that attempt again automatically. Reassignment and cancellation cannot undo an already running command.

## Why can an older computer list chats but not continue a chat that is still running?

Older builds can still list, save, publish and release chats through a compatible service. A service using quorum-backed coordination refuses an older request that tries to take a live chat without naming its observed holder and ownership number. It uses the existing refusal, `another device moved this chat first`; an old build does not necessarily show an extra update hint. Quit on the computer holding the chat before continuing it elsewhere, or update codeaf on the computer trying to continue. A released or expired chat can still be taken. A conditional choice never silently changes to a newer holder. An older codeaf also cannot take a chat that only lives on a computer (blind pass-through, see *Blind pass-through*): it ends `move here` with its usual could-not-continue line instead of waiting. Update codeaf on the computer trying to continue.


## Can an older build save a chat with large encrypted metadata?

Yes. Directory requests from older builds keep the existing 1 MiB request limit, including large encrypted metadata and frame hints. The service receives the complete signed request before making the change visible. A failed or interrupted save never exposes only part of that metadata. Reconnect and check the latest save before retrying. Encrypted names, key wraps and frame hints are preserved. The identity's storage capacity still applies.


## Opening a free chat from Home after restarting

By default (**Settings → General → idle handoff** is off), selecting a released remote chat on Home asks **Move here** and local chats open locally. Turn on **idle handoff** in `/settings` (or set `CODEAF_IDLE_HANDOFF=1`) on paired computers so selecting a released chat on Home brings its latest saved history here with a new hold, without **Move here**. This also works on the first open of this computer's local copy after a restart.

With `idle handoff` on, Home uses its shared listing when it already answers. If this window has no open copy and the listing is missing the chat or still shows this computer's old live hold, Home checks its current hold for up to two seconds. If that check fails or times out, the locally saved chat opens as usual, without a connection refusal. A chat already open in this window needs no extra check. A listed free chat still refreshes an older viewer with its latest history. A live hold or a lapsed hold elsewhere keeps the existing **Move here** card, including when its holder was working. Only explicitly released holds open directly on another computer when `idle handoff` is on. Unpaired, sync-off, single-device and default (`idle handoff` off) homes keep their local opening behavior.

If the relay is unreachable, Enter on a chat saved on this computer opens its local copy, including a listed free chat last held here. With `idle handoff` on, a free open's initial ownership read also waits at most two seconds: an unreachable service opens the local copy if one exists, or explains that the relay is not answering for a remote-only chat. Successful transfers keep their normal move budget. Opening locally keeps the chat's usual offline send restrictions.

## Why did Enter show Move here instead of sending my message

Before sending, codeaf checks that this window still holds the chat. If another computer holds it, its latest saved turn is newer, or this window's earlier move failed, Enter opens **Move this chat here?** or leaves the window showing the chat only. The words stay in the box. Open it from Home to bring the saved chat here, then press Enter to send (choose **move here** by default; with `idle handoff` on, a released paired chat opens directly while a live hold or a lapsed hold on another computer still asks for **move here**).

Once this window says another computer continued the chat, its first Enter
raises that question without running a turn or making a branch. A saved hold
from an earlier process cannot bypass the question. A window whose writer
still holds the chat sends with one Enter without asking the service first.

A new chat can send its first message normally. A hold that merely lapsed (not released by its window) is reopened conditionally on the next Enter when no other computer holds the chat, or continued with Move here; a window that released its own idle hold (when `idle handoff` is on) takes it back in place on new work when the saved head is unchanged. Reopening the chat acquires a new ownership fence. While storage is off and blind pass-through is not carrying the chat, a free chat last held here resumes quietly without reopening if its local history contains every shared turn, even with newer turns saved only here. If the ownership check cannot reach your other computers, a chat this window already holds keeps working and its turns wait to be shared; any other message stays unsent.

## A move that failed partway — are my typed words safe, can the old copy still send

If something changes the chat's files while a move is putting them in place, codeaf stops the move and lets go of the chat. It says the move could not finish and that your words are still in the box. The window keeps showing the chat or returns to Home, and it cannot send on the old copy. Try **move here** again.

Every failed move keeps the captured draft, including its whitespace, caret, pasted text and attachments, even if the old session closed during the move. Losing a race, a connection failure or a refused move does the same. A failed move never sends those words by itself.

## Repair sync — codeaf doctor and codeaf doctor --fix for unfinished continues, duplicate rows and a busy engine store

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Run `codeaf doctor` from a terminal to inspect this computer's engine store and cross-computer sync state without changing anything. When an issue is present, `engine store` and `sync` rows report:

- `busy · held by <command>, pid <pid>`: another live process holds the engine store writer.
- `unfinished continue · chat <id>`: a continue started on this computer never completed or let go.
- `duplicate rows · chat <id> (2 local folders)`: more than one local folder on disk shares one chat id.
- `duplicate rows · chat <id> (branch copy <branch>, all turns in main chat)` or `duplicate rows · chat <id> (branch copy <branch>, 3 turns not in main chat)`: branch copies exist for a chat.
- `lapsed hold on this computer · chat <id>`: a stale local hold receipt needs repair. With `idle handoff` on (`CODEAF_IDLE_HANDOFF=1`, off by default), current builds open an explicitly released paired chat directly; on Home, an unchanged expired hold last held here also opens without a question.

Run `codeaf doctor --fix` to repair what is safe. It prints one line per repair:

- `fixed: released unfinished continue for chat <id>`
- `fixed: retired duplicate branch copy <branch> of chat <id>`

`codeaf doctor --fix` never touches another computer's live hold, never disturbs a live store writer (quit the program `busy` names, or wait for it, and continue again), and never removes a branch copy or `.taking` scratch folder that holds a turn not already in the main chat.

## The chat banner says the relay is not answering — what is offline

The chat banner names the selected host, for example:
`the relay at 127.0.0.1:8901 is not answering; this chat keeps working here and catches up when it is back`.
When its address is unavailable, it says
`the relay is not answering; this chat keeps working here and catches up when it is back`.
Startup and renewal notices share one notice per outage.
The relay may be unreachable while your other computers are still
working. The chat stays usable on this computer and catches up when the service
answers. Home’s presence strip disappears when the service cannot provide fresh
presence; it does not guess which computers are online.

## Sync notices between older and current builds

A current window understands the earlier outage and removed-computer notices too. It uses one outage line while local work continues, and points to `/pair` for recovery in a chat. A current engine keeps the earlier notice spelling for an older window, so that window's existing outage and removal handling still works.

## Why can't I continue new turns from my offline computer — relay storage is off, and blind pass-through needs both computers on

Hosted relay storage is off by default while its privacy terms are being finished. Pairing alone does not enable it: saved keys, preferences, shared memories, teams, idle handoff and task copies wait. Chats do not wait: with blind pass-through a chat and its files move between your paired computers while both are running codeaf, so a chat on a computer that is off cannot be taken until codeaf runs there again (*Move here says the computer that has this chat is not on*). Pairing, device presence, remote control (once it is turned on) and live `--at` still work. After pairing, press enter on **Settings → Devices → relay storage** to ask for approval. A self-hosted relay with `--store` needs no request. Existing saved content remains readable; turning storage off deletes nothing.

While blind pass-through carries your chats, the notice about closed storage says `chats move between your computers by blind pass-through while both run codeaf; keys, memories, teams and preferences wait for relay storage (request access in Settings under Devices)`. When blind pass-through is off or unavailable (`CODEAF_PASSTHROUGH=off`, or no other paired computer), chats stay on their computer and the refusal is `relay storage is off for this account, so chats stay on this computer; request access in Settings under Devices`. Once approval is read in the background, waiting chats start within a minute without restarting.

## Changing thinking or model settings after idle release

With **Settings → General → idle handoff** (`CODEAF_IDLE_HANDOFF=1`, off by default) turned on in a paired set, changing this chat's name, thinking, model, automatic model choice, approval posture or attached folders in its released window first takes the hold back in place. The setting is saved with the chat before it is handed back again. `/remember` after the last turn also travels with the saved chat.

If another computer took the chat, the setting is refused through the usual held-elsewhere message. The terminal keeps the requested `/effort` or `/model` choice in the input box. Open the newest free chat before applying it there. Your already-held chat can still work offline, including starting a task.

## Renaming, rewinding or compacting a released chat

A manual name is saved in both the chat's title and journal. When `idle handoff` (`CODEAF_IDLE_HANDOFF=1`, off by default) has released this window's hold, renaming, rewinding, undoing a rewind, compacting, anchoring a workspace, changing project autonomy, editing a plan, changing a task's next model, reviewing, applying, resolving or deleting task work, landing a folder and moving a memory line first take back this window's released hold. If another computer continued the chat, these changes are refused before writing. Open its newest saved chat before applying the change there.

## Large workspaces keep their hold when the idle save check is too large

When `idle handoff` (`CODEAF_IDLE_HANDOFF=1`, off by default) is on, idle release requires a complete comparison. Its check allows 20,000 paths, 50,000 directory entries, 8 MiB per comparison output or diagnostic stream and 64 MiB of chat-file contents across the saved and current copies. It checks shared directories once. If any budget or the 30-second comparison deadline is exceeded, the chat keeps its hold and retries later. Ordinary work and saving keep their existing behavior; no incomplete comparison releases the chat.

## Opening a saved chat offline, asking again, and keeping unsent words

Opening a chat with no stopped reply to resume does not say `the reply stopped when this conversation moved — asking again` or refuse work through the resume door. This includes opening your released chat's local copy while the relay is unreachable.

If an unanswered reply was stopped by moving the chat, it asks again once when this window can work. If ownership refuses that resume, the refusal appears once without the asking-again line, and the unanswered question is kept for a later resume.

A new send in the window that released its chat (when `idle handoff` is on) needs to take the hold back. If the ownership check cannot reach sync, it says `the relay is not answering, so this chat cannot be taken back yet; your words are still in the box`. The draft stays in the box; the send records nothing and calls no model. When the service returns, press Enter again to send those words after taking a new hold. An uncertain take-back write keeps its separate retry sentence below.

Enter checks again even when Home previously read another computer's chats or still shows an outage. If this window's hold merely lapsed, Enter reopens the chat at a new fence and sends the kept draft without a card. The box stays intact until the send succeeds. Another holder still requires your continuation choice. A failed move from Home to a different chat also leaves this chat able to check again and conditionally reopen its unchanged own fence on the next Enter.

## Take-back timeout: retry a released chat when the relay is slow

When `idle handoff` (`CODEAF_IDLE_HANDOFF=1`, off by default) is on, taking back your released chat is a bounded ownership write, with up to 15 seconds to finish. The terminal can show `waiting for <service>; still checking` while it waits. An already-held chat keeps its ordinary short ownership check and offline behavior.

If the answer is lost, codeaf says `couldn't confirm taking this chat back from the relay; try again`. Your words stay unsent. Press Enter again to retry in the same window. The chat may be briefly unavailable: another hold on this computer does not prove which window acquired it. codeaf does not offer its own uncertain hold through “Move this chat here?”.

If no window adopted that uncertain hold, nobody renews it. It lapses within the ordinary 90-second hold, and a retry then takes a new hold before sending your words. A live hold belonging to another window or computer never grants this window permission to write.
