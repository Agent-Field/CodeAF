# Continuing a chat that one of your machines holds

This page is about continuing a chat on another computer: the question home asks, the offer when a device is off, and the line that says how long the move took.

## Why can't I type in this chat — my message did not send and home opened with a question

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

Only chats started after moving chats was turned on can be continued on another computer. Older chats stay where they are, keep their original files, and keep working in every build.

A chat can be worked on by one device at a time. If you open a chat here that another of your devices is running right now, and you type a message and press enter, the message does not go to the chat. Home opens instead, with that chat's row under the cursor and the question up:

```
Continue this chat here?
running on spark; last saved turn 4s ago
1 continue here
2 leave it there
```

Your words stay in the box. Nothing was sent, and no branch nobody asked for was made. This card came up because you sent a message, not because you chose to continue, so `leave it there` is already chosen and leaning on enter changes nothing. Press `1`, then enter, to continue the chat on this device.

An attended window keeps following your other devices while the chat is open, so a move is shown without returning to home first. Slash commands such as `/help` always work.

If you continue it, the other device's window stops taking new turns and says `<device> continued this chat; this window now only shows it`. The page *Home* has the rest of that card.

## What does leave it there do

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

On `Continue this chat here?`, `leave it there` closes the question without moving the chat. Your typed words stay in the box and nothing is sent. If the question opened because you tried to send from an older window, this answer is already selected. Choose `1 continue here` to bring the chat here before sending.

## What does running on spark mean — a row on home and the verb it offers

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

Home lists chats from your other devices. A chat that a device is working on now says `running on <device>`. One that a device let go of, or that went off because its lid closed, says `<device> offline`.

Press enter on either row to continue it here. The verb on the card depends on the row: **Move here** for a chat another device is running right now, **Continue here** for one it let go of. You already chose to continue, so the cursor starts on `continue here` and enter completes the move; `esc` leaves it there. The card says how fresh the copy is, so you know how many turns may still be on the other device.

## How do I continue on this machine — Continue where you left off, alt+c and alt+x

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

If you open codeaf and a chat on another device was running or went off in the last 24 hours, and that device is offline now (its lid is closed) or has let go of the chat, home offers it first of all:

```
Continue spark's latest chat here?
alt+c continue here · alt+x not now
```

- `alt+c` opens the same card as above, with the reason and the cursor on `continue here`, so enter finishes it. Nothing is moved by this line alone.
- `alt+x` dismisses it for that device until you open codeaf again.

The line follows the devices row: it stands while the device is offline or has let go of the chat, even if that became true after you opened codeaf, and goes away if the device comes back. A device that is online and still working on the chat is never offered.

To bring work from a device that is online, use the devices row: `alt+m bring work here`. The page *Adding a device to your devices, approving it, and removing it* shows the row and the dots.

## What happens when the chat arrives — the banner Moved from spark, and what was not brought

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

When the chat opens on this device it starts with one line that says how long the move took:

```
Moved from spark in 3.2s. Everything as you left it.
```

If a command was running on the other device, the line goes on:

```
Moved from spark in 3.2s. Everything as you left it. What was running there can start again here.
```

When the other device's name is not known it reads `Moved here in 3.2s. Everything as you left it.`

Right after, a card opens that says where the chat stands. It is the first thing on the screen of the chat. When something has to be set up here, the card asks whether to bring this device to the state the other one was in (the page *Home* has that question). When nothing has to be set up, the card only reports, with one answer that closes it:

```
Moved from spark. Here is where it stands.
not committed yet: main.go, util.go · last tests: passed
got it
```

The lines under the heading are:

Each line is left out when it has nothing to say.

- `not brought along:` folders the copy left behind, and any log or saved output that was too big or looked like a secret, with the reason.
- `was running there:` commands that were running on the other device. They are listed so you can start them again.
- `also needed:` what this device also needs.
- `not committed yet:` the files with changes that are not committed, up to three names and then `and N more`.
- `last tests:` `last tests: passed`, `last tests: failed`, or `last tests: failed 2` when the run said how many. A line with no test run behind it is not shown.

On the set-up card, `set up` runs only the commands the card lists, and `not now` leaves it and says `/setup does this later`.

Typing while either arrival card is up goes into the message box, including the first character. Enter sends your words and leaves the card waiting. With an empty box, enter answers the card; use the arrows to choose an answer.

## Does a moved chat have the network — installs and access on this computer

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

A new chat is host-bound and keeps that class when moved. Its commands run on the computer that now holds it, with the network and the same access as any program you run there. Moving chats on or off does not restrict commands to their workspace. `/setup` prepares missing tools and folders after a move; it does not enable ordinary network access. Existing cells declared sandboxed by an earlier build keep their declared class when moved or reopened.

## What folder does the moved chat work in — old absolute paths in earlier turns

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

A moved copy works in the workspace it arrived in. Before its next model request, the agent gets a session note naming the device it came from, the old folder and the new folder. Paths in earlier turns under the old folder now name the same relative paths under this workspace. The note is context for the agent, not a message you typed, and remains available after reopening or compacting.

The file tools also map an absolute path under the original folder to the same relative path in the new workspace. A shell command's old absolute path prefixes are mapped too. A mapped tool result says `Paths from the old folder <old> were mapped into the workspace here: <new>.` This applies even if the old folder exists on this computer. A mapped file path whose link leads outside the new workspace is refused. Paths to unrelated folders are unchanged.

Taking a chat back into its original project restores that project in place. The agent is told about the return too; old paths from the other computer's copy now map back into the original project.

## Why does the project label say copy from another machine

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

A chat arriving here for the first time works in its own copy. It is filed under the folder it came from, apart from conversations in this computer's own folder of the same name. Home and the keys-row label say `proj-a (copy from another machine)`. This remains a copy even if a folder with the original path happens to exist on this computer. Taking back a chat that already works in your own project restores that project in place. An unknown origin is left unnamed.

## Continue here did nothing — another device continued first or the sync service is unreachable

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

If nothing happens when you press `Continue here`, the cause is one of these:

- `another device continued this chat first`: another device was faster. The row goes back to `running on <device>`. Nothing changed.
- `other machines unreachable`: this device cannot reach the service. Nothing can be moved until it can.
- `could not continue this chat here`: the move failed. The row stays where it was; try again.
- A sentence starting `the sync service must be updated before chats can move` means the service is too old to move chats safely. The operators update the hosted service; for your own service, follow the update command in that sentence on the terminal. Pairing, listing and viewing still work. Reopen codeaf after the service is updated.
- With no way to continue set up, an off row has no `continue here`: enter says why.

## Taking a chat back, and two computers racing for the same chat — who wins, and what the loser sees

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

A chat has one holder at a time. To take a chat back from the computer that took it, continue it there the same way: press enter on its row on that computer and choose `continue here`. Nothing has to be returned or switched off first. A computer with the chat open when another takes it says `<device> continued this chat; this window now only shows it`, stops taking new turns, and keeps what it had not yet sent as a branch (`<K> turns from <device>: open separately / discard`). A tool call the model tries there answers with that same line. Taking the chat back leaves a `node_modules` or other install folder that computer already has alone.

The choice covers the holder and turn of ownership shown on the card. If another computer continues the chat before you confirm or while the copy is arriving, your choice cannot take it from that new holder. The slower one sees `another device continued this chat first`, its local copy is not replaced, and its row goes back to `running on <device>`. Older clients that do not send this condition keep their earlier behavior. No turn is lost on either side: whatever the earlier holder had not sent is kept for you as the branch above. This build offers discard, which archives a branch, and opening the branch as a separate chat; merging it into the original chat is unavailable. Pressing `2` (`leave it there`) on the question, or `esc`, changes nothing and leaves the chat where it is.

## I reopened an old local chat after the other computer let go — where are the newer turns

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

Reopening an older copy does not replace newer history on your other devices, even if the other computer has closed the chat. This window only shows its local copy until you choose `continue here`. Submitting a message opens that question and keeps your words in the box. Continuing brings the newer history here before writing can resume.

Locally saved turns that never reached your other devices are kept as a branch before the newer copy replaces them, including turns already sealed on disk. Unsaved workspace edits are sealed and kept too. The move says `your unsaved edits here were kept as <K> turns from <device>`. Open that branch as a separate chat or discard it; merging is unavailable.

Press enter on the kept turns' row. The card offers `1 open separately`, `2 discard`, and `3 leave them`, with `leave them` selected. Choose a number, then enter. `open separately` opens a chat with its own conversation id, the kept history and the files as those turns left them, in its own copy on this computer. The main chat's history and files stay as they are. Discard archives the kept turns; it does not delete them.

## The chat is arriving on this computer — why it waits to open

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

While a chat is moving onto this computer, pressing enter on its row says `this chat is arriving on this computer; it opens by itself in a moment`. Nothing is wrong and nothing needs doing: the move takes a second or two, and the chat opens by itself when it ends, with the other computer's latest turns in it. Opening it earlier is refused on purpose, because a window opened halfway would show the chat as it was before the move and could not save your turns.

When the holder is off (its lid is shut), the row reads `<device> offline` and you can still continue the chat here: the card says `last saved turn <N> ago; up to <K> turns may still be on <device>`. Turns that never left that computer are not in the copy you get; if its window is still running when it reconnects, it keeps those turns as a branch. After a restart, the turns stay in the local copy until you choose `continue here`, which keeps them as a branch before replacing that copy. Open the branch separately or discard it. Merging into the original chat is unavailable.

## Can the chat on the other computer still open the job logs it mentions — logs, saved tool output, task journals and the task history after a move

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

Yes. A chat's messages name files that sit beside the chat and not in your project: the log of a background job (`log at …/logs/jobs/2.log`), the full output of a tool result that was cut short (`full: logs/stubs/9c2f.txt`), and the journals of its tasks. They move with the chat. The other computer puts the logs and saved output back in the chat's own folder, so the same names open there. The task journals (each task's transcript, trajectory and saved output) are part of the chat itself, so they arrive whole inside it at the paths the chat reads them from. The first message it receives after the move says where they are now (`… that earlier messages name under <old folder> were carried: they are under <new folder> here`). A path in an old message still shows the old folder; use the new one.

Three limits keep a move light, and they apply to job logs and saved tool output only. A single such file over 1 MiB stays behind, and so does whatever does not fit in 16 MiB together (the oldest go first). A task journal has no size limit and always moves whole. A file that looks like it holds a secret (a key, a token) stays behind too. Each one is named under `not brought along:` with the reason and `stayed on the machine that made it`; read it there by asking that machine, and never assume the other computer has it.

Two things are made again on the other computer and are not carried: the project's task history (the list of finished tasks home and the tasks tool show), which is rebuilt from the chat's own record, and the git bookkeeping inside a task's working copy. The program's own files inside a task's working copy (the `.codeaf` folder, where a task's job logs sit) are not carried either, because they are not your work: they never show up on the other computer as a change you did not make. A file you made in the working copy does move. A task that finished on a kept branch keeps that branch in your project on the other computer, at the same commit.

## What moves with a chat — is my git history sent, branches, secrets in my repo

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

Your repository's `.git`, including its history, branches and settings, moves with the chat to your own paired devices. A task's commits travel too, and its working-copy bookkeeping is rebuilt on arrival. A key in an old commit remains in that history: codeaf does not remove or rewrite it. Git history travels sealed end to end; the sync service sees only ciphertext, including when the history contains keys.

The current working files are checked separately. A `.env` or source file that the guard recognises as holding a key is left out of the chat's file capture and carried through the encrypted vault. If you committed that secret earlier, its historical Git copy still moves with the repository. An untracked `.env` with no committed copy arrives only through the vault.

Every seal checks the whole working tree first, including the first turn when it only says a reply or runs a command without changing files. Files ignored by git are checked too. A withheld file stays out of the local saved packs and the uploaded workspace capture, and tools still see a `.env` file's variables through the vault. Restores and arrivals also check before any automatic safety capture.

## Why does home appear while my chats are still syncing

Moving chats and pairing are off on every build unless `CODEAF_CELLS=1` is set when codeaf starts on both computers. Add `export CODEAF_CELLS=1` to each computer's shell profile to keep it on. Windows does not have moving chats; use WSL.

The first screen draws while this computer publishes its device record, shares its saved keys and sends the first saved turn in the background. A slow sync service does not leave the terminal blank. New tool work waits for the initial ownership check; local startup saves are sent once that check finishes.
