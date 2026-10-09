# History

History is the desktop app's list of every conversation you have had, with a
one-line recap beside each, a way to read one back without opening it, and a
search that answers "what did we decide about that". It lives in the desktop
app only. The terminal has `/resume` and `/home` instead; the last section says
what they do.

## Where do I find past conversations in the desktop app — the History tab, Cmd+Y on a Mac, Ctrl+Y elsewhere

Open **History** with the keyboard shortcut **Cmd+Y on a Mac** and **Ctrl+Y on
Windows and Linux**. It lists every conversation on this machine, newest first
by when you last spoke in it. Each row has the conversation's name, a one-line
recap of what came of it, how long ago it was, and small facts when they apply:
tasks it ran, files it changed, decisions it reached.

A conversation that is working, or stopped on a question for you, says so on its
row, and the question it waits on is shown on the row. The filters
narrow the list to conversations that **decided** something, **changed files**,
ran **tasks**, or are **open** in a window now. Long lists load a page at a time.

A conversation nobody has summarised yet still appears. It has no recap line and
shows its name only. See the recap section for which conversations get one.

## Search what we decided — find a conversation by a decision, a file or a word

Type in History's search box. Searching asks no model anything and costs nothing:
it reads the names, recaps and messages already saved on this machine. It ignores
upper and lower case and small words such as "the" and "did", and it counts
newer conversations as a better match than old ones.

Results come in groups: **decisions**, **discussed** (conversations whose words
match), **files** that conversations changed, and **tasks** (shown as "Task in"
the conversation that ran it, matched on the task's title and result). Each
group shows its first few and says how many there are in total.

When one conversation clearly answers a question, for example "why did we rule
out JSON5", the top of the results shows that answer. It is **one of the
recap's own sentences, word for word**, with a link to the message that backs
it. codeaf never writes a fresh answer at search time. If no recap sentence
covers most of your words, or two conversations are about equally good, there is
no top answer and you get the groups.

**Archived conversations are always searched.** So are conversations with no
recap, by their name and their messages.

## What is the recap under each conversation — the one line, what was discussed, what was decided

A recap is a short account of one conversation that a model writes about it:

- **One line** for the list row: what was decided or found, or what was
  discussed and left open. For example "Ruled out JSON5, because it also allows
  comments" or "Discussed Load vs Open. No decision yet". It is never a message
  count or how long the work took.
- **What was discussed**, in two or three sentences.
- **What was decided**, each with who decided it: **you** or **codeaf**, and
  sometimes a word such as "accepted".
- **What came of it**, in a sentence.
- **Files changed** with lines added and removed. These come from the edits and
  writes in the transcript, not from the model. A file that already existed and
  was rewritten whole shows no counts, because the lines it replaced cannot be
  known from the call.

Only conversations held in the desktop app are summarised, from the first turn
that settles after this feature exists. Older conversations, conversations from
the terminal, and the steps of a task have no recap.

## Which model writes the recap — Titles and summaries, what it costs

The model role **Titles and summaries** writes the recaps. It is the same role
that names chats, tasks and background jobs, and it starts on
`deepseek/deepseek-v4.1-flash`. Change it in the desktop **Settings** page, in
the same row that names your chats; there is no separate setting for recaps.

The cost is small and bounded. A recap is written when a turn settles, and only
if the conversation changed since the last recap: one short call, with no tools,
over the first message and the latest forty, each cut short. Opening an old
conversation again, or a turn that said nothing new, pays nothing. The call runs
beside your turn and never delays it; if it fails, the conversation carries on
and simply keeps its older recap. Its spend is counted with the conversation's
other small calls, not as part of any turn.

## Will History archive my conversations — auto-archive after 12 hours idle, Restore all, un-archive

The desktop puts away an open **tab** that has been idle for **12 hours**, unless
it is pinned, still running, or waiting on a question for you. It happens when
the app starts: the idle tabs leave the tab strip, and one message at the bottom
says "Archived 6 tabs idle for more than 12h" with **Review** (opens History) and
**Restore all** (puts every one of those tabs back). It leaves after six seconds,
or at once with Escape; hovering it keeps it.

Archiving **deletes nothing**: the transcript, the recap and any tasks stay, the
conversation stays in History, and search always includes it. Its recap header
says "archived". Choosing **Continue** on an archived conversation takes it back
out of the archive. It is the same archive flag that home's `e` key sets in the
terminal.

You can also archive one yourself: right-click a row in History, or select it
and press Shift+F10 or the menu key, and choose **Archive**. A tab holding it
leaves the strip. Archive is not offered while the conversation is running or
waiting on you.

The desktop has **no delete** for conversations yet. To remove one for good,
use the terminal (see "Permanently deleting a conversation from Home").

## Read an old conversation without opening it, or continue it — messages in History, the recap going out of date

Selecting a conversation in History shows its recap and lets you **read its
messages** without opening it. Reading loads the words from the saved transcript
and starts nothing: no engine, no model call, no cost, and it works for
conversations no window has open. The messages are your words and codeaf's
replies; tool traffic and the notes codeaf writes to itself are left out, and a
very long message is cut at 20,000 bytes.

**Continue** opens the conversation in a window like any other: you can type
into it, it can run tasks, and a window already holding it is reused. It takes
the place of the History tab; Cmd-click (Ctrl-click elsewhere), a middle click,
or Cmd/Ctrl+Enter opens it in a new tab and keeps History.

## History keys and the row menu — arrows, Enter, Cmd+F, right-click

In History: **Up** and **Down** move the selection, **Home** and **End** jump to
the ends, **Enter** continues the selected conversation, and **Cmd+F** (Ctrl+F
elsewhere) goes to the search box; **Escape** clears a search. Clicking a row
selects it and shows its recap; double-clicking continues it.

Right-clicking a row, or Shift+F10 on the selected one, offers **Continue**,
**Read conversation** and **Archive**. The design also names "Add to place" and
"Delete"; the desktop does not have them yet, so they are not in the menu.

A recap says **out of date** when the conversation has moved on since the recap
was written, for instance after you spoke again. It is refreshed when that turn
settles.

## What History does not do — limits, the terminal, and /history in the terminal

- It is **desktop only**. The terminal has no History tab and writes no recaps.
  There, `/resume` opens an earlier conversation, `/home` lists every project and
  conversation on this machine, and typing on home searches your old chats. See
  "Searching from home".
- **The terminal's `/history` is a different thing**: it opens the activity page
  of tasks, not your conversations.
- Recaps are written when a turn settles, one short call per changed
  conversation. A conversation can briefly show an older recap, or none.
- A recap is a model's summary and can be wrong. The messages beside it are the
  record.
- Search reads words, not meaning: a question that shares none of the words in a
  conversation will not find it. The assistant's own `search_conversations` tool
  searches messages for the model; History is for you.
