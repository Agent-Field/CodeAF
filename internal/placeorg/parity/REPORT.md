# Places parity

Go organiser against the owner's saved embeddings (389 chats). Cluster ids are arbitrary, so the score is the adjusted Rand index. Names are a model's and are not compared. The saved reference is the single-anchor run; this port also scores an end anchor when a chat has one, and a chat earns every home at or above the floor and within the delta, with no cap.

Wall: 187ms. Space: `openai/text-embedding-3-small@256`.

Summary: 21 places

## Levels

| Level | ARI | Floor |
| --- | --- | --- |
| root | 0.986 | 0.85 |
| mid | 0.963 | 0.85 |
| leaf | 0.885 | 0.85 |

## Invariants

11 of 11 passed.

- depth within 6 — pass
- fan-out within 9 — pass
- every place has at least 4 chats — pass
- names unique tree-wide, one or two words — pass
- every extra home is earned, and in a branch of its own — pass
- forest: one parent per place — pass
- every op in the plan is in the journal and replays to this tree — pass
- a home for every chat, duplicates beside their representative — pass
- no probe is a place or counts toward one — pass
- the person's own places are intact in add-on mode — pass
- no model text became an id, a path or a permission — pass

## Op list diff

Counts are kinds. The reference list is the lines its run printed, including lines that are not tree operations. The Go list is the plan.

| Kind | Reference | Go |
| --- | --- | --- |
| COLLAPSE | 11 | 11 |
| CONSTRAINT | 1 | 0 |
| CORRECT | 1 | 0 |
| DEMOTE | 1 | 0 |
| DISSOLVE | 14 | 11 |
| FANOUT | 1 | 0 |
| FILE | 0 | 83 |
| INBOX | 0 | 28 |
| LENS | 0 | 6 |
| LINK | 0 | 104 |
| NAME | 0 | 21 |
| PARK | 0 | 278 |
