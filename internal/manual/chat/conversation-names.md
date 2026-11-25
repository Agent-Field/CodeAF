# Conversation names

## How a desktop conversation gets its name — the title in the chat header and the list

In the desktop app a conversation is named from what you opened it with. The moment your first message is sent it carries your own words, cut to fit; a moment later the title model replaces them with a short name of two to four words, at most 24 characters. An opening with nothing to name yet (`hi`, `help me`) keeps your words and may earn a name from one of the next three finished exchanges.

The terminal names a conversation once, from its first exchange, and does not rename it.

## Why did my chat's name change — renamed after I switched to a different subject

A desktop conversation can be renamed when you change subject in the same chat. If you were making launch images and then ask, in the same conversation, "help me calculate a fire plan for an NRI moving to Chennai", the name moves from the old subject to the new one (for example `NRI fire plan Chennai`), and the chat header and the conversation list show the new name.

It happens at most once per change of subject. Follow-ups on the new subject do not rename it again, and a follow-up on the old subject ("make the banner darker", "fix that slide", "update it") never renames anything.

## A name I gave the conversation myself is kept — my own conversation name never gets overwritten, renaming stops automatic names

A name you gave a conversation yourself is never replaced: not by its opening naming, not by a change of subject, and not after the app restarts. Rename it again to change it. Conversations named before this behaviour existed whose name is still your own opening words are treated as named by you and keep that name.

## What a rename on a change of subject costs — no extra call on an ordinary message

Nothing on an ordinary message. When a message arrives, codeaf compares its words with the conversation's name and its last few exchanges, in this process and without a model. Only a request of three or more meaningful words that points at nothing earlier ("it", "that", "update", "fix" and similar point back) and shares almost none of the conversation's words is a suspected change of subject. A suspicion costs one call to the title model, which is shown the current name, the recent exchange and the new message and may answer that it is the same subject; then the name stays. In measurement about one follow-up in twenty-five was suspected, each call cost about $0.0004, and no same-subject follow-up was renamed.
