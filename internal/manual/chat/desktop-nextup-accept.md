# Desktop nextup accept

## What happens when I accept suggestions in Next up?

Accept suggestions waits, then answers every reversible question in the Next up queue that already has a suggestion. The answer is that suggestion. For about six seconds a toast reads "Accepted 3 suggestions", or "Accepted 1 suggestion" when there is one. Nothing is sent to those conversations while the toast is up. The questions that cannot be undone, the costly ones, and the ones with no suggestion are left waiting.

## Can I undo Accept suggestions before they are sent?

Undo on that toast cancels the whole batch. So does ⌘Z (Ctrl Z on Windows and Linux) while that toast is the latest one. Nothing is sent. After the toast leaves on its own, the answers have been sent and Undo is gone. Holding the pointer on the toast, or moving keyboard focus onto it, keeps it up so you can still take the batch back.

## What if accepting a suggestion fails?

The ones that did not land are named in one line: "Could not accept: Allow 3 git actions? · Skip 4 and continue?". That line is ordinary text, with Undo no longer offered. The ones that did land are not listed. When every answer lands, the line is not shown.

## Does Skip in Next up stay on this window only?

Skip sends the question in front to the back of this window's Next up queue. Skipping again sends the new front behind that one. The question is still waiting. It is not answered, and the count does not change. Another window keeps its own order. Skip is forgotten for a question that is no longer waiting.
