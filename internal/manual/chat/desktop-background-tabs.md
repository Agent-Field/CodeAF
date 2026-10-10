# Background tabs on the desktop

## How a background tab knows it needs you

On the desktop, a tab you are not looking at does not keep asking the engine for a full copy of that conversation. The window has one world stream. That stream's row for the conversation says whether work is still running, how many things need you, and the title the engine gave it.

The tab you are looking at is the only one that holds that conversation's own stream. Switching to a background tab is what loads the transcript. Until you do, the row is enough for the mark and the title.

A row that has not arrived yet changes nothing: the tab stays as it was the last time it was open. A needs-you count above zero shows the amber mark. Running stays quiet on the tab. A landed failure shows the red mark only when nothing is running and nothing needs you. Needs you outranks running, and running outranks a failure.

The row does not carry the last reply or the words of a question. A reply you already read stays on the tab. Questions you already read stay while the count is still above zero, and leave when the count is zero. A new question's wording shows when you open the tab.

## Does every open tab keep its own connection

No. A browser only allows a handful of connections to one host. If every saved tab held its own stream, a new tab could not send. Background tabs do not call `GET /sessions/{id}` on a timer, and they do not hold a per-session stream. One world stream covers them. The open tab is the one conversation stream.
