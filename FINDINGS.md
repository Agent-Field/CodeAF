# c293 findings

This documentation step records the behavior delivered by c293 after reading the named design and implementation sources.

An approved hand-off now receives its own copy beneath the conversation's `trees`, cut by the existing ground ladder from the ground chosen by its resolved stand. The run's children all work in that same run-owned copy, while the person's checkout remains untouched until landing.

A second hand-off may join work already underway only when both hand-offs stand on the same ground; its receipt explicitly says that it joined. A hand-off on different ground is refused with the explanation that it cannot join the live run, rather than sharing that run's dirty copy or silently taking another road.

Finishing the workers leaves their changes in the run's copy. Landing is a later explicit action that uses the existing run landing path to commit the copy's work and bring it home. The landing note reports the destination branch and number of files moved; when the run changed nothing, it instead reports that there is nothing to land because the run's working copy holds no change.
