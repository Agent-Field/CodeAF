# Overview cards

## What does an overview card show?

The tab overview (the grid of open tabs) draws each tab as a readable card: its kind, a state, the title, then the one piece that matters, then how long ago it changed and the model when those are known. A conversation shows its last reply. A task that is being asked something shows that question. A running task shows the live command the engine sent, and nothing when it sent none. A terminal shows its last lines. A diff shows the first changed lines. A card with none of that shows no extra sentence.

## Why does an overview card say 4 running?

The state says "4 running" when the engine reports that many tasks running. With work in progress but no count, it says Working. When the tab needs you it says Needs you. When the work failed it says Failed. A task says "step 7" only when the engine sent that step number for that task. If the engine sent none of this, the card draws no state.

## How do I allow all from the tab overview?

A card that needs you shows Allow all when every question is a permission, or Allow when there is one, and Review. Allow all answers those permissions on the engine and leaves the overview open. Review opens that tab and closes the overview. A question that is not a permission offers Review only. If the answer does not go through, the card says why and the buttons stay.
