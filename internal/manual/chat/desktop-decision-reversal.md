# Desktop decision reversal

## Undo an automatic decision — revoke consent or stop the task it started

The decision engine can overturn an automatic decision by running its stored undo handle through the engine that made it. For example, that owner can revoke standing consent or stop the task the decision started. The decision is marked overturned only after undo succeeds.

A decision that is irreversible, has no undo handle, or whose owner can no longer undo it is refused. The default refusal is "This decision cannot be undone." An owner can return a more specific explanation. A failed undo leaves the decision and its learning state unchanged. Desktop delivery requires the bridge to connect this operation to the action owner; a ledger stamp alone does not undo work.

## Next time always ask me or keep deciding after an overturn

The reversal applies to this decision's kind of question and subject class only. "Always ask me" sets that kind to always ask. "Keep deciding" sends that kind back to learning and clears its recent answers, so it must earn the right to decide again. Other kinds keep their settings and history.

Repeating the same reversal does not execute undo again, change the first reversal time, or erase answers learned afterward. A separate settings change is needed to change the mode after a completed reversal.

## Which tasks and decisions used this decision — notify or pause without cascading

The engine returns the tasks and decisions created after this decision that directly referenced it. Two tasks read "2 tasks used this". It offers notify or pause; listing those choices does not act on the dependent work. Nothing is automatically stopped, notified, or overturned. Indirect descendants are not included, and an unknown or zero count draws nothing.

The dependency reader must report real references from the work's owners. If it cannot read them, the reversal fails before undo runs. Choosing how to handle dependents is a separate explicit action.
