---
kind: changed
title: a desktop conversation is named from its opening intent by the configured titles model
pr: 0
surface: [engine]
invalidates:
  - "The desktop app named a conversation by rule, from the first words of the message the model was sent, and a message of files alone was named by the harness's own Attached file line. It is now named once from the person's own words and the names of the files they attached, by the title role's first rung (the pin, then the low tier), and carries those words cut to 40 characters until the answer lands."
  - "A name of the desktop was at most 41 characters, the ellipsis not counted. It is at most 40 characters, the ellipsis included, in the engine and in the app."
  - "launch.deterministicNames was how the app asked for rule naming. launch.intentNames separately opts into intent naming; the old flag retains free rule naming and never buys a title call."
  - "The desktop kept its first-word name permanently. Explicit intent naming can replace that provisional name once; the persisted opening name consumes the attempt even if the request fails, is denied, or is interrupted. The terminal namer keeps its existing policy."
---

One request per conversation, to the first rung of the title role's ladder and
no other (the conversation's own model is not asked in its place), through the
same auxiliary door as every other errand, so the spend guard prices it before
it is dispatched. The request carries at most 600 characters of the person's
words and six file names of 48 characters, asks for at most 40 characters, and
is given a 256-token ceiling. Nothing is read out of the words the model is
sent: attachments come from the door's own records, and a path the person typed
is their words and is kept. A rename by the person wins every race with the
answer, and a team member started by a brief keeps the name its brief gives it.

`docs/design/app-v3/PROTOCOL.md` says the same of `agents.create`.

With no configured title pin or low-tier route, the opening name remains and no
request is made. Explicit one-model mode may use the model the person selected.
Existing titles remain untouched: the stored literal-name mark covers both old
rule-generated titles and manual renames, so their origin cannot be inferred.
The app holds the existing standing title lane until it releases the connection,
so a title arriving after a short answer still updates the rail, header and tab.
