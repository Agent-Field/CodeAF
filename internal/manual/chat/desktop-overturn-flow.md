# Desktop overturn confirmation

## Overturn a decision that other tasks used — Notify them, Pause them, Leave them

Overturning a decision that other work built on opens a confirmation titled “Overturn this decision?”. It says how many tasks used the decision (“2 tasks used this”) and offers three ways to handle them: “Notify them”, “Pause them” or “Leave them”. Nothing is preselected. Pressing “Overturn” with no choice sends no handling at all, so no other task is touched; overturning never cascades to dependent tasks on its own. “Cancel” closes the confirmation without changing anything.

When no task used the decision, or the count is unknown, the confirmation shows only the title and the buttons, with no dependents line.

## Taking an overturn back — Undo on the “Decision overturned.” toast

A successful overturn shows the toast “Decision overturned.” at the bottom of the window. When the engine can reverse it, the toast carries Undo, like every other undoable change. If the engine refuses the overturn, the confirmation stays open and says why, and you can try again or cancel.

Limit: the engine's overturn route is not answering yet, and the Why? card does not open this confirmation yet, so today the confirmation exists as a component only.
