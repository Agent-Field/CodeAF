# Giving a password, token or API key

## How do I give you a password or token safely

When the work needs a credential from you — a GitHub token, a deploy key, an API key,
a password — the chat asks for it with `request_secret` instead of asking you to paste
it into a message. A card appears with a heading saying what it is, a sentence saying
exactly how to create it and what it is for, and one masked box.

Type or paste the value into that box and press **Save securely** (in the terminal,
`enter`). The box shows a bullet per character and how many characters arrived, never
the value itself. **Decline** (key `1`) says no; nothing is kept and the chat carries
on without it.

Never paste a credential into an ordinary message. A message is part of the
conversation: it is written to the transcript and sent to the model provider on every
later request.

## Where a secret you typed is kept and for how long

The value is written to a file of its own in the `secrets` folder of codeaf's state
folder (`~/.codeaf/secrets/`, or under `CODEAF_HOME` when that is set). The folder is
private to your user (0700) and the file is too (0600). It is never put in the
workspace, so it is never committed, synced or shared with a team.

The file is readable for **24 hours**. After that codeaf deletes it: a timer in the
running program, and a sweep whenever another secret is typed in or codeaf starts. To remove
one sooner, delete the file.

## What the chat sees when you type a secret

The chat never sees the value. Its tool call comes back with `saved`, the file's path,
how many characters arrived and the hour it stops being readable — nothing else. Its
commands read the value from the file, for example
`GH_TOKEN="$(cat ~/.codeaf/secrets/runner-token-3f9a…)" gh api …`, so the value goes to
the program that needs it and not into the conversation, the decision record, a
`--host` connection or the model provider.

If a command prints the value anyway, codeaf takes it out of the output before the chat
reads it: the chat sees `[redacted]` where the value was. That holds after a restart for
every secret still inside its 24 hours.

**Only the whole value is caught.** A command that prints PART of it — the first eight
characters, the last four — is not, so the chat is told never to print any part of a
secret and to check one by using it. The character count on the card is the
confirmation that it arrived.

## When a secret cannot be asked for

`request_secret` exists only where somebody can answer it. A run with nobody at the
screen — `--once`, a scheduled run, a headless worker — does not have it, and a model
`ask` with a secret box there comes back `your call: … (nobody to ask, so nothing was
asked for)`. A factory step's or a team member's secret question is never routed to a
manager: only you can type a credential. Asking for the same secret twice is allowed,
because a token that expired or was wrong is not a decision already made.

On a full belt the tool waits in the `secrets` group and the chat loads it with
`load_capability` when it needs it.
