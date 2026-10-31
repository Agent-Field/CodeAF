# Continuing a chat that one of your machines holds

To continue on this machine, select the chat on home and choose `continue here`. When your paired computer is offline or has let go of its latest chat, home can ask `Continue tower's latest chat here?`: `alt+c` continues immediately when there is no local choice to make, and `alt+x` dismisses the offer. Moving chats is available by default on Linux, macOS and native Windows with a compatible engine. Older chats become movable through independent copies after pairing.

## Continue on another computer — continuing a chat on another computer, why can't I type in this chat, my message did not send and home opened with a question

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Older chats move through an independent copy. Once paired, codeaf prepares chats active in the last 30 days in the background, one at a time; opening another older chat prepares its copy then. This build shows and writes to the copy. The original files stay untouched and remain usable in older codeaf builds.

A chat can be worked on by one device at a time. If you open a chat here that another of your devices is running right now, and you type a message and press enter, the message does not go to the chat. Home opens instead, with that chat's row under the cursor and the question up:

```
Continue this chat here?
running on tower; last saved turn 4s ago
1 continue here
2 leave it there
```

Your words stay in the box. Nothing was sent, and no branch nobody asked for was made. This card came up because you sent a message, not because you chose to continue, so `leave it there` is already chosen and leaning on enter changes nothing. Press `1`, then enter, to continue the chat on this device.

An attended window keeps following your other devices while the chat is open, so a move is shown without returning to home first. Slash commands such as `/help` always work.

If you continue it, the other device's window stops taking new turns and says `<device> continued this chat; this window now only shows it`. The page *Home* has the rest of that card.

## Where an older chat's copy lives — the .moving folder, README.txt, can I delete it

An older chat's copy is kept in a folder called `.moving` beside the original, in codeaf's state folder for that project. The name is historical: nothing in it is a move in progress. It holds the live copies this build writes to, so deleting it deletes those chats. The folder carries a `README.txt` that says so. The originals beside it stay untouched for older codeaf builds.

## My typed message after continue here — is it kept, does it send by itself

before showing this question for a send, codeaf checks which computer holds the chat now. an older listing alone cannot raise it. if that check fails, nothing is sent and the whole message stays in the box, including pasted text and attachments. the chat says `<host> has not answered; your message is still in the box, press enter to send it`. press enter to check again. changing the draft while a check waits keeps the current words unsent; the chat says `draft changed before sending; press enter to send your current words`.

a chat this window already holds, or one that has never been shared, sends without asking the service. an ordinary check for a shared copy waits up to five seconds. immediately after this window's own successful move, a message can wait up to 30 seconds for the chat to finish opening, then sends once. a window that lost the chat, or another window holding it, cannot give this window permission to send.

if Enter raised the continue card, a successful move restores those same words, pasted text and attachments and says `your message is still in the box; press enter to send it`. it waits for another enter. choosing `leave it there` or a failed move also keeps the whole draft.

## which draft comes back after continuing a chat

a move opens one conversation for that chat. sending clears the words from its saved drafts, including matching copies left by an earlier window. taking the chat back, reopening it or restarting codeaf then shows an empty box unless you typed a new draft.

taking a chat from Home restores that chat's own saved draft, or leaves its box empty. words typed for a different chat stay with that chat, even when both work in one place on disk. a draft held behind a send's continuation card stays with the chat that raised it. continuing a chat this window already holds keeps its open conversation and typed words.

## Read a finished task's worker conversation after moving its chat

Open `/task` and choose the finished task. Its saved worker conversation and steps remain readable with its outcome after moving the chat, even when the original working copy is no longer available. A finished task offers no restart action; typing `r` or `R` writes those letters in the box.

## What does leave it there do

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

On `Continue this chat here?`, `leave it there` closes the question without moving the chat. Your typed words stay in the box and nothing is sent. If the question opened because you tried to send from an older window, this answer is already selected. Choose `1 continue here` to bring the chat here before sending.

## Esc on a continue card returns to my chat, and each card keeps its own draft

If Enter in a displaced chat raises the continue card, `esc` or `2 leave it there` returns to that same chat window with its full draft still in the box. If you opened the card from a Home row, leaving the card keeps you on Home.

during an automatic continue, the first `esc` withdraws its opening and any waiting choice. the automatic chat is not opened: it finishes arriving and is let go, so it stays on this computer, free to continue from anywhere. a further `esc` navigates as usual while that finishes. if the abandoned move has not answered after two minutes, its request is cancelled before the waiting choice can begin. inspecting another chat's card and choosing `leave it there` or `esc` leaves the automatic arrival alone.

A continue card previews only that chat's unsent words, or nothing when no draft is available. Its project foot belongs to the selected chat. Words typed in another chat never appear on this card. Saved drafts belong to their chat even when a copied chat moves to a different project folder; opening another copy cannot adopt them.

## slow chat listing — waiting for a service, a timeout or an unreachable host

at five seconds without a chat-list answer, Home says `waiting for <host>; still checking` and keeps its rows. the read continues for up to 25 seconds. a deadline says `<host> did not answer in 25s; still trying`; a failed connection says `<host> is unreachable; still trying`. background checks continue on their usual schedule. a successful answer clears the line.

these are Home's listing sentences. a message left unsent by a failed send check gets its own explanation: `<host> has not answered; your message is still in the box, press enter to send it`.

## Why does continue here say this chat was last held here — my only computer asks to continue

A chat this computer held last, whose hold expired with no other computer holding it, opens without a question: there is nothing to continue from. Enter sends, and turns this computer saved while it could not reach your other devices are sent to them when the chat opens. Earlier builds asked `Continue this chat here?` on the only computer that used the chat.

The card still says `this chat was last held here; last saved turn 2h ago` when this window's own earlier move failed. Choose `continue here` to resume it. The landing says `Moved here` and never names this computer as somewhere the chat moved from.

If unpublished work needs to be kept separately, the number of kept turns counts conversation turns. Intermediate saves and tool records within one question and answer do not become extra turns.

## What does running on tower mean — a row on home and the verb it offers

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Home lists chats from your other devices. A chat that a device is working on now says `running on <device>`. One that a device let go of, or that went off because its lid closed, says `<device> offline`.

Press enter on either row to continue it here. The verb on the card depends on the row: **Move here** for a chat another device is running right now, **Continue here** for one it let go of. You already chose to continue, so the cursor starts on `continue here` and enter completes the move; `esc` leaves it there. The card says how fresh the copy is, so you know how many turns may still be on the other device.

## Continue on this machine — Continue where you left off, alt+c and alt+x

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

If you open codeaf and a chat on another device was running or went off in the last 24 hours, and that device is offline now (its lid is closed) or has let go of the chat, home offers it first of all:

```
Continue tower's latest chat here?
alt+c continue here · alt+x not now
```

- `alt+c` continues immediately. The card asks first only when there is a real choice, such as keeping unsaved local edits as a branch.
- `alt+x` dismisses it for that device until you open codeaf again.

The line follows the devices row: it stands while the device is offline or has let go of the chat, even if that became true after you opened codeaf, and goes away if the device comes back. A device that is online and still working on the chat is never offered.

Home never offers this computer's own chat as work to continue from another
computer, even when an older record says this computer went offline.

To bring work from a device that is online, use the devices row: `alt+m bring work here`. The page *Adding a device to your devices, approving it, and removing it* shows the row and the dots.

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

## Why does the project label say copy from another machine

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

A chat arriving here for the first time works in its own copy. It is filed under the folder it came from, apart from conversations in this computer's own folder of the same name. Home and the keys-row label say `proj-a (copy from another machine)`. This remains a copy even if a folder with the original path happens to exist on this computer. Taking back a chat that already works in your own project restores that project in place. An unknown origin is left unnamed.

Removing this computer from your devices does not change those project
labels. Switching away and back keeps the same copy label or ordinary project
name.

## This chat is not syncing — other machines unreachable, working offline

If this computer holds the chat and renewals have failed for longer than one
renewal period, one dim line says
`this chat is not syncing because other machines are unreachable; it keeps working here`.
It is said once per outage.
When renewals succeed again, one line says `this chat is syncing again`.

You can keep working here while offline. If another computer continued the
chat meanwhile, the existing `<device> continued this chat; this window now
only shows it` notice still applies.

## Reopen a kept copy after another computer takes the chat

When unsent turns were kept as a separate copy, reopening that copy checks who holds the copy itself. Another computer may still hold the original chat. The previous window finishes saving and stops syncing before the reopened copy starts its own sync, so a late save cannot change which copy it opens. Taking that copy elsewhere still stops new tool work in its older window.

## Continue here does nothing — continue here did nothing, the engine store is busy, or a chat is already continuing here

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

while a chat you explicitly chose is moving here, the card says `continuing here…`. another explicit continue during that move says `a chat is already continuing here; wait for it to open` on the card. an automatic startup move instead lets your explicit choice go next; see “my choice while an automatic chat is arriving”.

every failure stays where you are looking: on Home's card with `1 retry` and `2 leave it there`, or inside the open chat window. a fresh reading decides whether a different computer continued first. if the same computer picked the chat up again, the card asks again with `<holder> picked this chat up again; press enter to continue it here`. it does not blame another device.

## why continue here failed — retry or leave it there

a failed move keeps the draft and shows `1 retry` and `2 leave it there`. retry confirms the holder and turn of ownership now displayed on that card.


- `another device continued this chat first`: another device was faster. The row goes back to `running on <device>`. Nothing changed.
- `other machines unreachable`: this device cannot reach the service. Nothing can be moved until it can.
- `could not continue this chat here because the engine store is busy` or `could not continue this chat here because the engine store is busy (held by codeaf chat, pid 4321)`: another `codeaf` process holds this computer's engine store writer. Close or finish that process, then choose `retry`.
- `could not continue this chat here`: the move failed. The row stays where it was; choose `retry` or try again.
- `could not continue this chat here because other machines are unreachable; your words are still in the box`: the service went away during the move. Nothing was sent and the chat stays where it was; send again once the other machines are reachable.
- `taking an open chat from another computer waits for a sync service update; all else works, so close it there and continue here` means the chat is open on another computer and the sync service is too old to take it from there safely. Everything else works, including continuing a chat the other computer has closed. Close the chat on that computer, then choose `continue here` again.
- Under `codeaf chat --host`, `continue here` is not offered and a chat the remote host already holds never shows as held elsewhere. With no way to continue set up, an off row has no `continue here`: enter says why.

## taking a chat back, and two computers continuing the same chat — which one opens

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

A chat has one holder at a time. To take a chat back from the computer that took it, continue it there the same way: press enter on its row on that computer and choose `continue here`. Nothing has to be returned or switched off first. A computer with the chat open when another takes it says `<device> continued this chat; this window now only shows it`, stops taking new turns, and keeps what it had not yet sent as a branch (`<K> turns from <device>: open separately / discard`). A tool call the model tries there answers with that same line. Taking the chat back leaves a `node_modules` or other install folder that computer already has alone.

The choice covers the holder and turn of ownership shown on the card. If another computer continues the chat before you confirm or while the copy is arriving, your choice cannot take it from that new holder. The slower one sees `another device continued this chat first` on the card with `1 retry` and `2 leave it there`; its local copy is not replaced, and its row goes back to `running on <device>`. if the same holder picked it up again, the refreshed card says `<holder> picked this chat up again; press enter to continue it here` and asks for a new confirmation instead. a complete chat already held here gets one silent recovery attempt; another disagreement keeps the retry card and draft. No turn is lost on either side: whatever the earlier holder had not sent is kept for you as the branch above. This build offers discard, which archives a branch, and opening the branch as a separate chat; merging it into the original chat is unavailable. Pressing `2` (`leave it there`) on the question, or `esc`, changes nothing and leaves the chat where it is.

## This window only shows the chat — can I still send here

Once this window learns that another computer holds the chat, Enter opens
**Continue this chat here?** and keeps your words unsent. This applies as soon
as the live update or the `<device> continued this chat; this window now only
shows it` notice arrives; it does not wait for the next background ownership
check. Enter cannot start a reply, publish a turn or create a separate chat
from this displaced window. Choose `continue here` to bring the latest chat
back before sending.

A chat this computer still holds sends on one Enter without an extra request
to the sync service. A chat that has never been shared does too.

## Take my chat back after a crash, a killed process or a power loss

If this computer was killed and another computer continued the chat, reopen
codeaf and choose **continue here** from Home or from the question raised by
Enter in the old local copy. The move brings the latest saved chat here and
says `Moved from <device>`. An old saved hold cannot keep the chat here or
make that copy send a message. The question uses the other computer and last
saved turn reported by the sync service. An offline holder is named as
`<device> offline; last saved turn <N> ago`.

Turns and file edits that never reached the service are kept separately before
the newer chat arrives. Shared turns and unchanged closing bookkeeping do not
need another upload before the move.

## I reopened an old local chat after the other computer let go — where are the newer turns

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Reopening an older copy does not replace newer history on your other devices, even if the other computer has closed the chat. This window only shows its local copy until you choose `continue here`. Submitting a message opens that question and keeps your words in the box. Continuing brings the newer history here before writing can resume.

Locally saved turns that never reached your other devices are kept as a branch before the newer copy replaces them, including turns already sealed on disk. Unsaved workspace edits are sealed and kept too. The move says `your unsaved edits here were kept as <K> turns from <device>`. Open that branch as a separate chat or discard it; merging is unavailable.

Press enter on the kept turns' row. The card offers `1 open separately`, `2 discard`, and `3 leave them`, with `leave them` selected. Choose a number, then enter. `open separately` opens a chat with its own conversation id, the kept history and the files as those turns left them, in its own copy on this computer. The main chat's history and files stay as they are. Discard archives the kept turns; it does not delete them.

## Unsaved edits after taking a chat back — usage, cost, title failures and memory

Turns already shared do not become kept edits. Usage, reconciled task cost, naming failures and memory bookkeeping alone create no extra branch, kept-turns row or “kept as” notice. Their recorded usage stays in this computer's usage ledger. Opening an unchanged chat adds no empty turn, including when another computer holds it.

## The chat is arriving on this computer — why it waits to open

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

While a chat is moving onto this computer, pressing enter on its row says `this chat is arriving on this computer; it opens by itself in a moment`. Nothing is wrong and nothing needs doing: the move takes a second or two, and the chat opens by itself when it ends, with the other computer's latest turns in it. Opening it earlier is refused on purpose, because a window opened halfway would show the chat as it was before the move and could not save your turns.

When the holder is off (its lid is shut), the row reads `<device> offline` and you can still continue the chat here: the card says `last saved turn <N> ago; up to <K> turns may still be on <device>`. Turns that never left that computer are not in the copy you get; if its window is still running when it reconnects, it keeps those turns as a branch. After a restart, the turns stay in the local copy until you choose `continue here`, which keeps them as a branch before replacing that copy. Open the branch separately or discard it. Merging into the original chat is unavailable.

## Can the chat on the other computer still open the job logs it mentions — logs, saved tool output, task journals and the task history after a move

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Yes. A chat's messages name files that sit beside the chat and not in your project: the log of a background job (`log at …/logs/jobs/2.log`), the full output of a tool result that was cut short (`full: logs/stubs/9c2f.txt`), and the journals of its tasks. They move with the chat. The other computer puts the logs and saved output back in the chat's own folder, so the same names open there. The task journals (each task's transcript, trajectory and saved output) are part of the chat itself, so they arrive whole inside it at the paths the chat reads them from. The first message it receives after the move says where they are now (`… that earlier messages name under <old folder> were carried: they are under <new folder> here`). A path in an old message still shows the old folder; use the new one.

## Job log and saved tool output limits after a move — task journals stay whole

Three limits keep a move light, and they apply to job logs and saved tool output only. A single such file over 1 MiB stays behind, and so does whatever does not fit in 16 MiB together (the oldest go first). A task journal has no size limit and always moves whole. A file that looks like it holds a secret (a key, a token) stays behind too. Each one is named under `not brought along:` with the reason and `stayed on the machine that made it`; read it there by asking that machine, and never assume the other computer has it.

Two things are made again on the other computer and are not carried: the project's task history (the list of finished tasks home and the tasks tool show), which is rebuilt from the chat's own record, and the git bookkeeping inside a task's working copy. The program's own files inside a task's working copy (the `.codeaf` folder, where a task's job logs sit) are not carried either, because they are not your work: they never show up on the other computer as a change you did not make. A file you made in the working copy does move. A task that finished on a kept branch keeps that branch in your project on the other computer, at the same commit.

## What moves with a chat — is my git history sent, branches, secrets in my repo

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Your repository's `.git`, including its history, branches and settings, moves with the chat to your own paired devices. A task's commits travel too, and its working-copy bookkeeping is rebuilt on arrival. A key in an old commit remains in that history: codeaf does not remove or rewrite it. Git history travels sealed end to end; the sync service sees only ciphertext, including when the history contains keys.

The current working files are checked separately. A `.env` or source file that the guard recognises as holding a key is left out of the chat's file capture and carried through the encrypted vault. If you committed that secret earlier, its historical Git copy still moves with the repository. An untracked `.env` with no committed copy arrives only through the vault.

Before each seal, codeaf asks which files will be captured and screens those candidates, including a full candidate list for a first reply or a capture that needs a full scan. If a file changes between screening and capture, saving plans and screens again. If repeated edits prevent a stable capture, it says `files kept changing while saving this chat; try again` and keeps the calls for a later save. A wholly git-ignored directory is neither entered nor screened unless it holds tracked files. An ignored file in an ordinary directory is still screened: root `.env`, `config/.env.local` and a private key in a `*.pem` file travel through the encrypted vault to paired computers. A withheld file stays out of local saved packs and the uploaded workspace capture. Tools still see a `.env` file's variables through the vault. An unreadable folder stays out until its permissions recover; its newly eligible files are then screened with a fresh capture plan. Restores and arrivals also screen before an automatic safety capture.

## Why does home appear while my chats are still syncing

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

The first screen draws while this computer publishes its device record, shares its saved keys and sends the first saved turn in the background. A slow sync service does not leave the terminal blank. New tool work waits for the initial ownership check; local startup saves are sent once that check finishes.

## Does every chat copy my whole project — disk space, shared saved files, older chats

New chats on this computer share one store of saved file contents. Opening four chats in the same project does not make four private copies of every unchanged file. Each chat keeps its own history, call log and transport scratch; restoring one does not move another chat's saved head. A chat whose older private store already exists keeps using that store in place, without migration. Nothing is deleted automatically. `codeaf cell gc` is an explicit command and removes only files the saved copy can restore.

## Why did dist or node_modules not move — gitignored files, build output, ignored folders

Every chat's capture follows Git's ignore rules. Untracked ignored output stays on the computer that made it: it is neither stored in the local file history nor uploaded. Tracked files travel even if an ignore rule matches their name. A wholly ignored directory is not walked or screened. Lockfile rebuild folders still follow the existing rebuild rules.

The arrival card names omitted paths from the saved summary, for example `not brought along: dist/ (git ignores them), node_modules/ (git ignores them)`. The summary is bounded and can name only some omitted groups. Ignored secret files in ordinary directories are still screened and travel through the key vault; an ignored `.env` inside a wholly ignored directory stays there with that directory.

If Git cannot read the project's ignore rules, saving stops with `cannot read which folders Git ignores here` and the cause. Completed calls stay in their local log for a later save. codeaf does not fall back to searching ignored directories or putting their secret files in the key vault.

## Does saving slow my command — pending save, quit while saving, moving before the save finishes

A command's result returns after its call log is written, while its file capture finishes in the background. The next tool call waits until the previous capture has finished reading files. Continuing the chat on this computer, uploading it, closing it, quitting codeaf and `codeaf cell gc` also wait for both the call log and the saved files to reach durable storage. A storage failure is reported and stops that boundary. Opening a chat screens its folder in the background without sealing an empty turn. A command submitted before that screen finishes waits for it. A window that only views a chat held elsewhere does not seal turns. A failed capture keeps the finished call in its local log for the next save; codeaf never runs that command again to recover it.

A wait lasts at most 30 seconds. A timeout says `still saving this chat on this computer: context deadline exceeded`; the pending save is not silently treated as done. The saving status reports failures. A successful quit stays quiet and waits up to 30 seconds for its final saving reply. An unanswered reply at that ceiling says the save is still finishing in the background; a definite saving failure reports an error. A final disk-flush failure says `codeaf: could not finish saving this chat:` followed by the cause and exits unsuccessfully.

If the last upload fails when you quit, for example because the connection dropped, this computer does not let the chat go. Your other computer keeps showing it as running here and does not open the older save by itself; the unsent turns stay on this computer until an upload succeeds.

## I pressed alt+m before the other chat appeared — still arriving, home on a second computer

Device presence can arrive before its list of chats. If you press `alt+m` early, home says `chats are still arriving from your other computer; this opens when they are ready`. It keeps your request and opens the ordinary confirmation once the chat arrives; it never takes the chat without that confirmation. A recent chat left on another computer is offered when home opens, including one that computer closed cleanly.

## a new chat brings its full saved contents; a returning chat brings its changes

a chat this computer has never held brings its saved contents in parallel, even after other chats used this store. a returning chat downloads only what changed. either path checks that all saved contents arrived before the move finishes. sharing saved file contents with other chats does not make a new chat count as one this computer already held.

## Does first start send anything — local identity before pairing

The first start may create a local identity for this computer. That file sends nothing: there is no sync traffic or network use until you pair or offer a pairing code. Reading the add-device instructions leaves it quiet. Offering a pairing code or running `codeaf pair` contacts the sync service for that pairing. `CODEAF_SYNC_URL=off` blocks sync traffic even after pairing.

## What survives a crash or power loss — durability, killed process, lost call records

If the codeaf process dies, written call records and finished results survive; recovery never runs those commands again. An interrupted external command is shown as incomplete rather than retried. A machine losing power can lose at most roughly the last second of call records and saves. Losing a record never causes its command to run again, and a torn final record is discarded without damaging earlier history.

Call records are written in order without waiting for a disk flush on each command. A background group flush runs about every 250 milliseconds. Moving or uploading the chat, becoming view-only, quitting and closing wait for the pending save and disk flush; a failure is reported. Saving also flushes the call records before relying on the saved files, and keeps the receipt before removing completed records from the local call log.

## Do attached files keep their old paths when I move between Windows and Linux

A saved attachment reference keeps the source computer's path spelling with slashes, including Windows drive paths. It names where the file came from; it does not redirect work to that old folder. A job reads the saved attachment bytes and stages its copy in the workspace here, even if the original file moved or disappeared.

## Can I rewind an old chat on my new laptop — where does the copy start

Yes, after the older chat has a movable copy and you continue it on your paired laptop. Its first saved file state is the folder as it stood when the copy was made. Rewind can reach that first saved turn; file states from before the copy were never saved and cannot be reconstructed. The whole earlier conversation is still there to read.

Recent older chats are prepared in the background after pairing. If you explicitly open an older chat whose copy is not ready, opening that conversation waits for its first folder snapshot to be saved. Large folders can take longer; Home's initial display does not wait for the background batch.

The original older chat is untouched. A turn typed into the original by an older codeaf after the copy was made stays in that original; it is not merged into the copy. This build lists only the copy in place of the original, with its title and ordering retained.

## I quit on my laptop and opened my desktop — do I need alt+c

A clean quit lets go after the last save is durable. Opening bare `codeaf` on your other paired computer within 24 hours continues the released chat automatically, with the usual `Moved from` banner and `not brought along:` lines. `esc` goes Home. A sleeping laptop has not let go: Home offers the chat and `alt+c` continues it, asking only if local work needs a choice.

If this computer is also coordinating, a clean terminal quit releases its idle chats and returns coordination to an anchor before stopping. With the anchors reachable, the other computer can continue immediately. Sleep, a crash or a lost connection still use the fallback described on the coordination and workers page.

after the chat arrives, enter can wait up to 30 seconds for this window to finish opening the chat before sending once. if the check fails, it says `<host> has not answered; your message is still in the box, press enter to send it`. the window keeps following changes, including renamed computers and a later move to another computer.

A running chat on the other computer never moves automatically. Neither does a named chat, `--host`, `--at` or `--session`, a chat with unsaved local edits, or a launch while another local window already has a chat open. if the service cannot be reached, Home names its host and keeps trying. if another computer got there first, the refreshed continue card explains the change and keeps the local chat as it was.

## my choice while an automatic chat is arriving — continue a different chat, join it or cancel

bare startup still chooses the most recently released eligible chat and checks local safety first. input before that check answers cancels the automatic choice.

if its move is already running, confirming a different chat on Home or using `alt+c` chooses what opens next. Home says `continuing <Y> next; <X> is still on its way here`, with the titles from the continue cards. the automatic move settles first and is let go with its files and kept work intact; it never opens over your choice. the next move uses exactly the computer and turn of ownership you confirmed. `esc` cancels the waiting choice and the automatic chat still settles and is let go.

confirming the automatic chat itself joins its move and opens it once. explicitly opening another local chat or starting a new one also prevents a late automatic arrival from replacing it. moving the cursor, opening Home or inspecting and declining another card does not choose a different chat. an explicit move already in progress still gives the usual busy sentence for another explicit pick.

## Why an older chat could not be copied — symbolic links, home folder

An older chat whose folder is a symbolic link, or whose saved conversation state contains symbolic links, is left untouched. codeaf refuses to copy those links because writing through one could change the original. The original still opens with an older codeaf.

An older chat that worked in your home folder, or in a folder that holds it, is not copied either. A copy saves the whole folder as it stands, and your home folder is not one project: saving all of it takes minutes and holds up every open chat's tools meanwhile. That chat keeps opening where it is, on this computer only, and it does not move to your other devices. Chats with their own folder are copied as usual.

## A save has not reached the other computer yet — upload failed or another device continued first

A saved chat reaches your other computers only after its encrypted objects have
been uploaded and its new saved head has been accepted. If an upload fails, the
other computer keeps seeing the previous saved head. Retry when the connection
returns. If the final save fails during quit, the work stays local and that computer does not announce a clean release; another computer does not automatically open that incomplete save. Uploaded objects are kept for reuse if another device continued the
chat first; this does not discard the work saved on your computer.

## Which computer coordinates: cluster status and cluster lead

On the hosted service, or a self-hosted service with persistent storage, an open paired home joins as a learner. Three persistent voters keep coordination available when your laptops sleep. `codeaf cluster status` shows this computer after joining. Continue chats through the ordinary continue door. See the coordination and workers page for command refusals and setup details.

## Can a worker access all my chats or approve pairing?

A delegated worker can access only its selected chats. It cannot approve pairing, rotate your identity, read your key vault or request leadership. Worker setup is not available as a terminal command yet. Personal computers keep their shared trust model.

## Worker restarts, draining and learner metadata visibility

A restarted worker uses a newer process number; delayed requests from its previous process cannot save work. Draining prevents new claims, while already held work can finish saving. Joining the full coordination log requires explicit consent and exposes identity-wide routing metadata, with chat contents and names still encrypted.

## How does a delegated worker claim an assignment or run parallel tasks?

An assignment names the selected chat and target worker. The worker downloads the saved chat, conditionally claims its observed ownership, then downloads the accepted saved head before opening it. Registration alone never starts work. Parallel tasks use separate child chats. Worker setup and execution are not available as a terminal command yet.

## Will an interrupted worker replay tools after a crash or reassignment?

No. The worker saves its claim and an execution intent before invoking tools. A lost reply retries the same coordination request. Once execution may have started, reopening reports `this work may be incomplete; its saved records have been kept` and keeps the records. It never runs that attempt again automatically. Reassignment and cancellation cannot undo an already running command.

## Why can an older computer list chats but not continue a chat that is still running?

Older builds can still list, save, publish and release chats through a compatible service. A service using quorum-backed coordination refuses an older request that tries to take a live chat without naming its observed holder and ownership number. It uses the existing refusal, `another device continued this chat first`; an old build does not necessarily show an extra update hint. Quit on the computer holding the chat before continuing it elsewhere, or update codeaf on the computer trying to continue. A released or expired chat can still be taken. A conditional choice never silently changes to a newer holder.


## Can an older build save a chat with large encrypted metadata?

Yes. Directory requests from older builds keep the existing 1 MiB request limit, including large encrypted metadata and frame hints. The service receives the complete signed request before making the change visible. A failed or interrupted save never exposes only part of that metadata. Reconnect and check the latest save before retrying. Encrypted names, key wraps and frame hints are preserved. The identity's storage capacity still applies.


## Why did Enter show Continue here instead of sending my message

Before sending, codeaf checks that this window still holds the chat. If another computer holds it, its latest saved turn is newer, or this window's earlier move failed, Enter opens **Continue this chat here?** or leaves the window showing the chat only. The words stay in the box. Choose **continue here** to bring the saved chat here, then press Enter to send.

Once this window says another computer continued the chat, its first Enter
raises that question without running a turn or making a branch. A saved hold
from an earlier process cannot bypass the question. A window whose writer
still holds the chat sends with one Enter without asking the service first.

A new chat can send its first message normally. A sleeping computer can also resume sending when nobody else has taken the chat and its local saved turn is still current. That safe return happens without another question. If the ownership check cannot reach your other computers, a chat this window already holds keeps working and its turns wait to be shared; any other message stays unsent.

## A move that failed partway — are my typed words safe, can the old copy still send

If something changes the chat's files while a move is putting them in place, codeaf stops the move and lets go of the chat. It says the move could not finish and that your words are still in the box. The window keeps showing the chat or returns to Home, and it cannot send on the old copy. Try **continue here** again.

Every failed move keeps the captured draft, including its whitespace, caret, pasted text and attachments, even if the old session closed during the move. Losing a race, a connection failure or a refused move does the same. A failed move never sends those words by itself.

## Repair sync — codeaf doctor and codeaf doctor --fix for unfinished continues, duplicate rows and a busy engine store

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Run `codeaf doctor` from a terminal to inspect this computer's engine store and cross-computer sync state without changing anything. When an issue is present, `engine store` and `sync` rows report:

- `busy · held by <command>, pid <pid>`: another live process holds the engine store writer.
- `unfinished continue · chat <id>`: a continue started on this computer never completed or let go.
- `duplicate rows · chat <id> (2 local folders)`: more than one local folder on disk shares one chat id.
- `duplicate rows · chat <id> (branch copy <branch>, all turns in main chat)` or `duplicate rows · chat <id> (branch copy <branch>, 3 turns not in main chat)`: branch copies exist for a chat.
- `lapsed hold on this computer · chat <id>`: an expired hold from this computer would raise a question on open.

Run `codeaf doctor --fix` to repair what is safe. It prints one line per repair:

- `fixed: released unfinished continue for chat <id>`
- `fixed: retired duplicate branch copy <branch> of chat <id>`

`codeaf doctor --fix` never touches another computer's live hold, never disturbs a live store writer (quit the program `busy` names, or wait for it, and continue again), and never removes a branch copy or `.taking` scratch folder that holds a turn not already in the main chat.
