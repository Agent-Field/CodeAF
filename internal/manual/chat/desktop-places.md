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

Limit: the decided model and permissions are shown in the Using list. The conversation does
not switch its running model or approvals because a place was added.

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

No. A place's folders are references, like attached folders: the conversation's working
directory, what a relative path means and where work may be written do not move.

A place's folders are **not** attached folders. Attached folders are the ones you attached
to this one conversation; a place's folders arrive because the conversation is filed under
the place, and are credited to it. Removing the conversation from the place removes them;
it never touches what you attached.
