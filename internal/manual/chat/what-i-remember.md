# What I remember

## Do you remember me between conversations?

Yes. A handful of durable things are carried from one conversation to the next:
something you asked to be remembered, a preference you stated, a correction you
made, a decision that still binds. It is **not** a copy of the transcript — the
conversation itself is not carried anywhere, and a new session starts with an
empty screen.

The memory commands have two postures:

- `/memory` — open the memory panel.
- `/memory <query>` or `/memories <query>` — print only matching memories into
  the conversation.
- `/memories` — print every memory into the conversation, preserving its older
  list posture.
- `/remember <text>` — keep one thing.
- `/forget <query>` — drop the one thing that best matches.

Memory can be turned off entirely. The `memory` row in `/settings` is on by
default; off, nothing is carried, nothing is written, and neither of the two
calls below is made. With it off, all three commands answer:

```
memory is off · turn it on under /settings
```

## How do I see what aforge remembers about me?

Open `/memory`. Its twelve-row list starts with the most recently updated
memories. Type to filter title, text and tags; the same prefix, substring
and fuzzy subsequence ranking as the model picker is applied to the list loaded
when the panel opened. A `*` marks a memory used at least five times. Press tab
to cycle the scope shown: all, user, project, env, then all again.

Enter expands the selected memory to show its full text, tags, use count, age,
and where it came from. Esc returns to the list; esc from the list closes the
panel. `/memory <query>` prints matching lines into the conversation.

## How do I edit a memory?

In `/memory`, press enter to expand a row, then enter again (or `e`) to edit.
The edit line is preloaded with the complete memory text. Enter saves it; esc
cancels without changing anything. The title and tags stay as they were.

## How do I forget a memory and undo forgetting one?

In the `/memory` list, delete (or ctrl+d) forgets the selected row immediately.
The footer says `forgot '<title>' — u to undo`; press `u` to restore it. Undo is
one-deep: only the most recent forget in this panel session can be restored.

## Where did a memory come from, and who says so?

Expand it with enter. The provenance line reads `learned <age> in '<session
title>'` when the source conversation is known. Older rows without provenance
say `learned <age> ago`. Provenance answers “says who?”: memory is inspectable,
forgetting is one key, and an accidental forget has one undo.

## How does it decide what to put in front of the model?

Before each message, a small model on its own cheap tier reads what you just
typed against an index of **titles only** — never the full text — and answers
which two or three remembered lines bear on this message. Only those are put in
front of the model, as a short `<memory>` block. Most messages need none, and an
empty answer is the ordinary one.

That is why a hundred remembered things do not make every message more
expensive: the index is titles, the block is what the router asked for, and
nothing else travels.

Two messages are never routed at all, because there would be nothing to match:
an empty message, and a continuation shorter than three words — `yes`, `go on`,
`that one` — unless it says `remember` or `forget`.

**If that small model is unreachable, the message goes out unchanged.** No
error, no warning, no memory in the prompt. A memory failure is never allowed to
break the thing you actually asked for.

## How does something get remembered without me asking?

After a message has been answered — off your path entirely, with nothing waiting
on it — the same cheap model reads the exchange and answers whether it held
anything worth carrying into another session. Most exchanges hold nothing, and
that is the answer it gives most of the time.

When it does find something, it is settled against what is already remembered
near it before anything is written. Four outcomes: it is added, it refines an
existing line, it replaces a line that has stopped being true, or it is skipped
because something already says it. That is what keeps telling aforge the same
preference in three sessions from leaving three near-identical lines behind.

Nothing about this is announced. There is no card and no line in the transcript
when a memory is written by this pass; `/memory` is how you see what it did.

## Can you remember this for me?

Yes — say so, and it is written down immediately. "remember that I deploy on
Fridays" is recognised as an instruction about memory rather than a message to
answer, and it is confirmed in one dim line:

```
remembered · deploys on Fridays
```

`/remember <text>` is the same thing typed as a command. So is the `remember`
tool, which aforge reaches for itself when you have stated something durable: it
takes the line and, optionally, how far the truth reaches — `user` for something
true about you everywhere (the default), `project` for something true only in
this project, `env` for something true only on this machine.

All three go through the same settling step, so saying it twice refines one
memory rather than making a second.

## Forgetting something

"forget what I said about the deploy" is recognised the same way `remember` is,
and so is `/forget <query>`. The query is matched against what is remembered and
**the single best match is dropped**, with its title said back:

```
forgot · deploys on Fridays
```

If nothing matches, that is the answer and nothing is changed:

```
nothing matched pineapples
```

One at a time is deliberate. A query that matched three memories and quietly
dropped all three would be losing two things you never named.

A dropped memory leaves a tombstone rather than a hole — the record that you
asked for it to be forgotten survives, and the memory itself is out of every
list, every search and every message from that instant.

## What does remembering cost?

Two calls per message, both on the cheapest of the four crew classes — the
`reflex` class, which exists precisely because a call made twice a turn is a
different economy from one made once a session. It ships pointed at
`nex-agi/nex-n2-mini`. Each goes out with a short
prompt, a 200-token ceiling and temperature 0, and each is asked to answer in a
few words of JSON. Neither is allowed to think.

Both are charged to the **session** total rather than to the message that
happened to trigger them, exactly as the session's own title and a compaction
summary are, so `/cost` and `/status` include them without any one message
reading as three times the price of its neighbours.

You can point that class at a different model — the **reflex** row in
`/settings` → Providers, or the whole crew in one word with `/crew` — or pin the
`reflex` role by itself under `pinned roles`. A change is live: the next turn's
pair uses it.

## Where is it kept, and does a task see it?

It is kept in `~/.aforge/graph.db`, which is per person rather than per
conversation or per project — so something remembered in one repository is
remembered in the next. `AFORGE_HOME` moves it with everything else aforge
keeps.

**A task gets the same treatment as a message.** When work is handed off to a
task, the router is asked once against that task's brief, and whatever it names
is put at the top of the task's own instructions. The task never writes memories
of its own: a family of eight tasks would otherwise be eight writers on one
brain, all blind to each other.

## I used to have a memory.md file

Earlier builds kept memory as one file of lines at `~/.aforge/v3/memory.md`,
written by a `note` tool and filtered by a `forget` tool. Neither tool exists any
more, and the file is no longer read on every message.

The first time a conversation starts with memory on and that file still there,
every non-empty line in it is imported as a remembered fact and the file is
**renamed** to `memory.md.imported` — never deleted, so your own copy of what you
wrote is still on disk. It says so once:

```
imported 12 memories from memory.md
```

After that the file is gone from aforge's view and the store is the only memory.
