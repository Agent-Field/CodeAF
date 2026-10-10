# Desktop knowledge editing

## How do I edit or delete what a place knows?

The desktop bridge reads every saved knowledge line for a place, including
replaced lines and their source evidence. Adding a line records “You wrote”.
Editing keeps its identity, original creation date and source evidence, and
records the edit date so the new words count as newer information. Deleting a
line returns an Undo receipt. Undo restores both the words and any replacement
relationships, unless the graph changed afterwards or the receipt expired.
Archived places must be restored before their knowledge can be changed.

## What happens when edited knowledge lines contradict each other?

A bridge add or edit can explicitly name the line it supersedes. The replacement
and new words are saved together under one Undo receipt. The newer words replace
the older line, which remains available as history. When both statements are
personal words from the same day, the bridge returns both lines as a question
instead of striking either one. Editing counts as personal words dated today,
while the original source evidence remains available. The bridge does not infer
contradictions from arbitrary sentences; a replacement request names its target.
A replaced line cannot be edited or confirmed as live knowledge.

## Does saying yes to still true reset a knowledge line's last use?

A live knowledge line unused for 60 days becomes eligible for “still true?”.
Confirming yes records the current time as its last use and clears the previous
question date. The line is no longer due until another 60 unused days pass.
The bridge requires an explicit yes; a false or missing answer changes nothing.

## Does Using include edited knowledge and exclude replaced lines?

The desktop Using list and the engine's place context share one resolver. Live
knowledge lines supply the instructions for their place; replaced lines stay in
the knowledge history and are excluded from Using. Sources and model or
permission policies retain their place labels and existing budgets and refusal
rules. Unknown source dates, titles and answer counts are not invented. Changes
reach the engine at the next turn's start, rather than changing a running turn.
