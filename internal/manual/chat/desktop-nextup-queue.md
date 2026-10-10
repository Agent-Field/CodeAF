# Desktop nextup queue

## What order does Next up walk questions?

Next up walks what is still waiting in other conversations. A stop on the turn,
or a named task that cannot continue, comes first. One that does not say
whether it blocks is walked with those. Anything that cannot be undone comes
last, even when it also stops the turn. Everything else sits between the two.
Inside each of those groups, the one asked first comes first. Two asked at the
same moment stay in the order they were listed. One with no asked time follows
the ones that have one.

## What does Skip do in Next up?

Skip sends the question in front to the back of the Next up queue. Skipping
again sends the new front behind that one. The question is still waiting. It is
not answered, and the count does not change.

## Does Next up count the conversation I am in?

No. The frame pill and the walk leave out the conversation on screen. That
conversation keeps its own "needs you here" count. The header chip reads "1 of M"
for the question in front, and M is how many questions are still in the queue.
When nothing elsewhere is waiting, there is no count and no "1 of M". A question
answered in another window drops out the next time the queue is built, and M
shrinks with it.

## Which questions does Accept suggestions answer in Next up?

Accept suggestions answers the reversible questions in that Next up queue that
already have a suggestion. The N in Accept N is how many of those there are. A
costly question, a question that cannot be undone, and a question with no
suggestion are not in that set. Skip does not remove a question from it.
