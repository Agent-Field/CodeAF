# Continuing a chat that one of your machines holds

To continue on this machine, select the chat on home and choose `continue here`. When your paired computer is offline or has let go of its latest chat, home can ask `Continue tower's latest chat here?`: `alt+c` continues immediately when there is no local choice to make, and `alt+x` dismisses the offer. Moving chats is available by default on Linux, macOS and native Windows with a compatible engine. Older chats become movable through independent copies after pairing.

## Continue on another computer — why can't I type in this chat, my message did not send and home opened with a question

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

After `continue here` succeeds, those same words, pasted text and attachments are in the arrived chat's box. Press enter again when you want to send them. Choosing `leave it there` or a move that fails also keeps the whole draft.

An attended window keeps following your other devices while the chat is open, so a move is shown without returning to home first. Slash commands such as `/help` always work.

Before showing this question for a send, codeaf checks which computer holds the chat now. An older listing alone cannot raise it. If the check cannot reach your other devices and this window already knows the chat moved away, your words stay in the box and nothing is sent. Continuing a chat this computer already holds keeps its open conversation and your typed words.

If you continue it, the other device's window stops taking new turns and says `<device> continued this chat; this window now only shows it`. The page *Home* has the rest of that card.

## What does leave it there do

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

On `Continue this chat here?`, `leave it there` closes the question without moving the chat. Your typed words stay in the box and nothing is sent. If the question opened because you tried to send from an older window, this answer is already selected. Choose `1 continue here` to bring the chat here before sending.

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

## What was not brought along — the lines on the arrival card

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

## Reopen a kept copy after another computer takes the chat

When unsent turns were kept as a separate copy, reopening that copy checks who holds the copy itself. Another computer may still hold the original chat. The previous window finishes saving and stops syncing before the reopened copy starts its own sync, so a late save cannot change which copy it opens. Taking that copy elsewhere still stops new tool work in its older window.

## Continue here did nothing — another device continued first or the sync service is unreachable

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

If nothing happens when you press `Continue here`, the cause is one of these:

- `another device continued this chat first`: another device was faster. The row goes back to `running on <device>`. Nothing changed.
- `other machines unreachable`: this device cannot reach the service. Nothing can be moved until it can.
- `could not continue this chat here`: the move failed. The row stays where it was; try again.
- A sentence starting `the sync service must be updated before chats can move` means the service is too old to move chats safely. The operators update the hosted service; for your own service, follow the update command in that sentence on the terminal. Pairing, listing and viewing still work. Reopen codeaf after the service is updated.
- With no way to continue set up, an off row has no `continue here`: enter says why.

## Taking a chat back, and two computers racing for the same chat — who wins, and what the loser sees

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

A chat has one holder at a time. To take a chat back from the computer that took it, continue it there the same way: press enter on its row on that computer and choose `continue here`. Nothing has to be returned or switched off first. A computer with the chat open when another takes it says `<device> continued this chat; this window now only shows it`, stops taking new turns, and keeps what it had not yet sent as a branch (`<K> turns from <device>: open separately / discard`). A tool call the model tries there answers with that same line. Taking the chat back leaves a `node_modules` or other install folder that computer already has alone.

The choice covers the holder and turn of ownership shown on the card. If another computer continues the chat before you confirm or while the copy is arriving, your choice cannot take it from that new holder. The slower one sees `another device continued this chat first`, its local copy is not replaced, and its row goes back to `running on <device>`. No turn is lost on either side: whatever the earlier holder had not sent is kept for you as the branch above. This build offers discard, which archives a branch, and opening the branch as a separate chat; merging it into the original chat is unavailable. Pressing `2` (`leave it there`) on the question, or `esc`, changes nothing and leaves the chat where it is.

## I reopened an old local chat after the other computer let go — where are the newer turns

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Reopening an older copy does not replace newer history on your other devices, even if the other computer has closed the chat. This window only shows its local copy until you choose `continue here`. Submitting a message opens that question and keeps your words in the box. Continuing brings the newer history here before writing can resume.

Locally saved turns that never reached your other devices are kept as a branch before the newer copy replaces them, including turns already sealed on disk. Unsaved workspace edits are sealed and kept too. The move says `your unsaved edits here were kept as <K> turns from <device>`. Open that branch as a separate chat or discard it; merging is unavailable.

Turns already shared do not become kept edits. A closing or startup save that only refreshes unchanged bookkeeping creates no extra branch or kept-turns row.

Press enter on the kept turns' row. The card offers `1 open separately`, `2 discard`, and `3 leave them`, with `leave them` selected. Choose a number, then enter. `open separately` opens a chat with its own conversation id, the kept history and the files as those turns left them, in its own copy on this computer. The main chat's history and files stay as they are. Discard archives the kept turns; it does not delete them.

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

A command's result returns after its call log is written, while its file capture finishes in the background. The next tool call waits until the previous capture has finished reading files. Continuing the chat on this computer, uploading it, closing it, quitting codeaf and `codeaf cell gc` also wait for both the call log and the saved files to reach durable storage. A storage failure is reported and stops that boundary. Opening a chat starts its first screened capture in the background. A command submitted before that capture finishes waits for it; commands cannot change files while that first screen is being saved. A failed capture keeps the finished call in its local log for the next save; codeaf never runs that command again to recover it.

A wait lasts at most 30 seconds. A timeout says `still saving this chat on this computer: context deadline exceeded`; the pending save is not silently treated as done. The saving status reports failures, and quitting reports an error if saving could not finish. A final disk-flush failure says `codeaf: could not finish saving this chat:` followed by the cause and exits unsuccessfully.

If the last upload fails when you quit, for example because the connection dropped, this computer does not let the chat go. Your other computer keeps showing it as running here and does not open the older save by itself; the unsent turns stay on this computer until an upload succeeds.

## I pressed alt+m before the other chat appeared — still arriving, home on a second computer

Device presence can arrive before its list of chats. If you press `alt+m` early, home says `chats are still arriving from your other computer; this opens when they are ready`. It keeps your request and opens the ordinary confirmation once the chat arrives; it never takes the chat without that confirmation. A recent chat left on another computer is offered when home opens, including one that computer closed cleanly.

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

Once the chat arrives, you can send your next message immediately. The window keeps following changes, including renamed computers and a later move to another computer.

A running chat on the other computer never moves automatically. Neither does a named chat, `--host`, `--at` or `--session`, a chat with unsaved local edits, or a launch while another local window already has a chat open. If the service cannot be reached or another device got there first, Home says `other machines unreachable` or `another device continued this chat first` and keeps the local chat as it was.

## Why an older chat could not be copied

An older chat whose folder is a symbolic link, or whose saved conversation state contains symbolic links, is left untouched. codeaf refuses to copy those links because writing through one could change the original. The original still opens with an older codeaf.

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
