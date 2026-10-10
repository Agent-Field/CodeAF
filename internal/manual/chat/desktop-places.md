# Desktop places

## What is a desktop place — work that belongs together, and what the AI is told about it

In the codeaf **desktop app**, a conversation can be **filed under places**: named
groupings such as `codeaf`, `Marketing` or `Q3 report`. A place can sit under one or more
other places, and a conversation can be filed under several places or under none (then it
is in **Now**). Places exist only in the desktop app. A conversation started in the
terminal is filed under no place, and nothing on this page applies to it.

A place can carry **instructions** (plain prose written on its Home) and **sources**
(folders, repositories, files, web pages, other conversations). A conversation filed under
a place is given what that place says, plus what the places above it say, **up to 2
levels up**: its parent and its grandparent. A place three levels up gives it nothing.

What is given depends only on which places the conversation is filed under. Which window
or strip its tab is open in, and which place is pinned, change nothing.

The model reads all of it at the top of its instructions, under the heading
`# Places this conversation belongs to`, with each instruction and source credited to the
place it came from.

## The Using list: why does this chat know about a file, or follow a rule I did not type here

If a conversation knows about `brand-voice.md` or writes "for customers, not engineers"
without being told so in the conversation, a place it is filed under said so. The conversation's
**Using** list shows every place it uses (filed here, or **inherited** through a parent),
every instruction, every source and the decided model and permissions, each labelled with
the place it came from. The chip reads like `Using 3 places · 4 sources`; the source count
is what the conversation is actually given, not what was trimmed or refused.

The Using list and the model are fed by one resolver, so the list never shows something the
model was not given, and the model is never given something the list hides.

To stop a conversation using a place, take it out of that place. Its next turn no longer
carries what the place says.

## Two places disagree — which instruction, model or permission wins

Sources never conflict: they add up. A **conflict** is two places setting the same thing
differently — the default model, the permissions, or instructions that contradict.

- **Model and permissions:** the **nearest common ancestor** with its own setting decides.
  A place counts as above itself, so a parent that disagrees with its child wins
  (`Release wanted Flash · codeaf decided`). A plain grouping place with no setting is
  skipped for the next one up. With no common place that decides, the conversation needs
  **one pick** from you; until then, **neither value is applied**. Your pick is remembered
  for that conversation only.
- **Instructions:** each place's words are kept under that place's own heading and never
  blended. The model is told: when two places' instructions contradict, follow what their
  nearest shared place above says; when none does, ask you once.

What a decided model or permissions setting then does to a conversation — and when it does
nothing — is the next two sections.

## A place's default model and permissions — a new chat starts on them

A place can set a **default model** and **permissions** (what runs without asking: `ask`,
`guardian`, `allow`, `deny`, or `auto` for "my settings decide"). Anything else is refused
when the place is saved.

- **New conversations.** A conversation started in a place, or filed before you said anything in it, takes
  the decided values when its **first turn opens**. A line says so:
  `Release set this conversation's model to deepseek/deepseek-v4.1-flash.` or
  `Release set what runs without asking in this conversation to ask.`
- **Later changes.** If the place's setting changes, a conversation still following it moves at the
  **start of its next turn** — never in the middle of a reply.
- **Your pick wins.** Choosing a model or permissions inside the conversation makes that field
  yours; no place changes it again. Changing the default model in Settings does not move a
  conversation whose place set its model.
- **A conversation that was already running** when it was filed keeps its model and permissions.
  The Using list says `This conversation was already running when a place gave it a model,
  so it keeps the one it has.` Taking the place's value there is your own pick.
- A conflict nobody decides applies **nothing** until you pick (see above).

Places exist only in the desktop app. A conversation opened in the terminal reads no
places, and the Using list says its settings are not applied to it.

## Why didn't the place's permissions apply — a place never loosens them by itself

A place's permissions are applied only when they let **no more** run without asking than
the conversation does now (`deny` < `ask` < `guardian` < `allow`; `auto` counts as whatever your
settings stand at). A place that would loosen them — say `allow` on a conversation that asks — is
**held**: the Using list says `This would let more run without asking than this
conversation does now, so it waits for you to choose it.` Nothing changes until you choose
it yourself, and then it is your pick.

This is on purpose: a conversation can be filed by the desktop's suggestions as well as by you, and
filing must never be a way to switch approvals off. Tighter permissions are applied, because
being asked more is never a surprise anybody pays for.

A word codeaf does not have, or a conversation with no control over approvals, is shown as not
applied, with the reason; it is never pretended.

## Some sources were left out — the source budget and the instruction budget

Filing a conversation under several big places could fill its prompt before you type, so
one conversation is given at most **12 sources** and **12 KiB** (12288 bytes) of place
instructions, across all its places.

The budget is spent **nearest place first**: places it is filed under, then parents, then
grandparents. Sources past the budget are **trimmed**: listed in the Using list as left
out, never given to the model, and the model is told only how many were left out.
Instructions past the budget are cut at a line where possible, and the model is told the
rest can be read on the place's Home. The same source listed by two places, or the same
instructions copied into two places, is given once and credited to both.

## Which sources a place can give — folders, files, web pages, other conversations, and what is refused

A source is a **reference**: codeaf names it and never reads, lists or fetches it for the
prompt. The model opens a file or folder itself, with its own tools, when the work needs it.

- **Folders, repositories and files** must be absolute paths that exist. A folder inside a
  git repository keeps the folder you pointed at, and the repository around it is named.
  One that is not on the disk right now is still named, with `NOT on this disk right now`.
- **Web pages** must be `http` or `https` with a host. A URL carrying a user name or
  password is refused. Pages are named, never fetched.
- **Another conversation** is named by its id and title; its words are not included.

**Refused, whoever added them:** `~/.ssh`, `~/.gnupg`, `~/.aws`, other credential stores,
and the codeaf home itself — also when a symlink leads there. A refused source is listed
with its reason and never reaches the model.

## I added a place in the middle of a reply — it applies from the next turn

What a place gives is read **when a turn opens**, never in the middle of one. Filing a
conversation, editing a place's instructions or adding a source while a reply is running
changes nothing in that reply; the next message you send carries it.

Reading it costs nothing when nothing moved: the instructions at the top of the
conversation are rebuilt only when the places file changed, and only re-sent differently
when what this conversation uses actually changed.

## "Now also using …" and "No longer using …" lines in the conversation

When a place starts or stops reaching a conversation, a line is posted into it at the next
turn: `Now also using Release: brand-voice.md`, `Now also using codeaf (through Release)`,
or `No longer using Software`. The model sees the same line, so it knows its instructions
changed between two of its answers. The line stays in the conversation when it is reopened.

To take an addition back, remove the conversation from that place; the next turn says
`No longer using …`.

## Deleting or archiving a place — are my chats deleted

No. Deleting or archiving a place **never deletes a conversation**. Its conversations keep
their other places, or move to Now, and stay in History. An archived place gives its
conversations nothing until it is restored. A deleted place's child places move up to its
parents.

## Is a place's folder the working directory — places versus attached folders

A place's folders are **not** attached folders. Attached folders are ones you attached to this
one conversation. A place's folders arrive because the conversation is filed under the place.
Removing the conversation from the place removes those references. It never touches what you
attached.

A new conversation **started in a place** works in that place's **first** folder or repository
that can be used, in the order listed. Later folders stay references, not the working directory.
A relative path in a tool is read against that first folder.

A conversation **started in a place** opens in the desktop app's own working folder, not in one of
the place's folders: a place can list several folders, and picking one would be a guess.

## How desktop Place suggestions group a saved library

Saved chats are compared using their title, recap when present, and the first message you
wrote. Reading that opening message changes nothing in the conversation. Your home folder,
the filesystem root, and the app's shared working folder are not treated as project topics.
A real project folder can suggest a place named after the folder without a model call.

For topic groups, the Place suggestions model checks whether the chats belong together.
It also considers a bounded sample of up to 64 chats the folder and word rules could not
group, so related chats do not need identical vocabulary. Pasted instructions or a shared
kind of request are not reasons to group unrelated chats. A declined suggestion stays
snoozed; organizing again does not create duplicate open names under the same parent.

The organizing interval applies to one pass, rather than each question in that pass. Daily
call limits, minimum group size, confidence, pending offers and hierarchy limits still
apply. Nothing is moved until you accept. Accepting creates structural undo receipts.

With no conversation tab open, suggestions can ask through an existing saved conversation,
including one originally opened in the terminal. The reader joins that conversation's own
workspace host, never types a message or starts a turn, and bills the question to that
conversation. A missing workspace or unavailable engine leaves rules available and shows
why the model could not answer; it does not create a spare conversation to ask through.
The folder comes from the place's own list and is checked again when the chat opens. A link is
followed to the real directory. A link into a credentials folder or into codeaf's state folder
is refused. A folder that is missing, that this account cannot enter or read, or that is the
top of a disk, is skipped and the next one is tried.

With no folder source, the chat works where the desktop app was started and says nothing about
a folder. When every listed folder was skipped, it still opens there and says
`The place's folders can't be used, so this chat works where codeaf was started.`
A bridge that cannot open a chat in a place's folder says
`This engine can't open a chat in a place's folder, so it works where codeaf was started.`

A window cannot name the folder. A saved conversation reopens in the folder its own record
names, when that folder still qualifies and sits outside codeaf's state. Otherwise it reopens
where the desktop app was started.

A terminal opened in the conversation uses that same folder. One that is already running
keeps the directory it was given. The desktop process stays where it was launched.

The model does not follow the folder. A new chat still takes a place's model and permissions
when its **first turn** opens. A later change still applies at the **next turn**. A choice you
made in the chat still wins.

## Desktop question receipts — picked by codeaf or answered by you

An answered question leaves a quiet check and its picked label in the conversation.
A clock-picked answer reads, for example, `Keep strict` followed by
`picked by codeaf after 30s`. The seconds are the actual recorded wait, rounded
to the nearest whole second. Reopening the conversation keeps that wait.
Older records with no known wait say `picked by codeaf` without seconds.
An answer you give keeps its attribution and local time, for example
`Allow once · you · 14:02`.

## Review a question in an unfocused desktop split pane

An unfocused conversation in a desktop split shows a small card above its compact
reply field when the engine has a pending question. The card shows the question's
title and a `Review` button. Review focuses that pane and opens its full question
tray, returning to the latest content if you had scrolled away. With no pending
question, the card is absent. Review does not answer the question.
