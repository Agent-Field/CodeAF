# What the relay stores and what it can see

## What does the relay store — what is on the relay's disk

The relay holds two things for each identity, kept apart from every other identity: a **directory** (one record for each chat, one for each of your computers) and a **store** of chat content.

The content is in the store as **encrypted chunks**. Each chunk is sealed under a key that only your own computers have. The relay cannot open a chunk. It keeps the chunks you send, and your saved keys too: they travel as one more sealed chunk, like a chat.

Two things in the directory are sealed before they leave your computer: **the title of every chat** and **the name of every computer**. The relay stores them as sealed text and cannot read them.

There is no account on the relay. No email, no password, no name. Your identity is an id that starts with `id_`, made from your key.

## What can the relay see — the metadata, my IP address, and what is not hidden from it

**The relay cannot read:** your messages, your files, the titles of your chats, the names of your computers, your saved keys, the pairing code, the three words.

**The relay can see, and keeps or logs:**

- your identity id, and the id of each computer you use (an id, never a name);
- for each chat: its id, how big it is, when it last saved, which computer holds it, and the chat it was split from;
- for each computer: its operating system, processor type, and whether it has a sandbox or a copy-on-write disk;
- for each chunk: its size, and the ids of the parts inside it;
- when you connect, how often, and how much you send;
- the network address each request came from.

A relay you run yourself logs only the first 8 characters of the identity id, the kind of request, the status and two byte counts. It never logs a body. An operator of a relay you do not run can do more than that; you cannot check it.

The relay can also delete what it holds, or refuse to serve it. Keep a copy you care about on a computer of your own.

## Does the relay see my code, my prompts or my files

No. Turns, tool output and files are sealed on your computer before they are sent. The relay gets ciphertext and sizes.

Your saved keys are sealed too, as said above.

What sync does not do: it does not send the files of your working folder. The chat follows you; the files stay on the machine they are on. See "Sync my folder to the other machine" for that.
