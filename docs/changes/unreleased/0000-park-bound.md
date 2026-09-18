---
kind: fixed
title: a parked worker is handed back with a record at a third of its allowance
pr: 0000
surface: [engine]
invalidates:
  - "A task worker parked on a command it started (`Agent.parkOnOwedJob`) waited the node's whole allowance before anything ended the wait, and the turn then resumed with no account of what it had been waiting for. The park now arms a bound of its own — a third of the allowance — and on that bound posts the record `the park was not settled within its bound` onto the queue before returning, so the turn comes back to the model with a sentence well before the wall."
  - "The wait on a promoted command was ended only by the command's ending, a stop, or the allowance timer. The bound now ends the WAIT and never the WORK: the promoted command is left running, so a job still producing output is not cut, its log keeps filling, and its own ending still arrives as a note in front of the model."
---

A foreground `bash` call that the background-after clock promotes into a job is
still the call the worker is waiting for, and the park holds the turn until that
ending arrives. When the ending never came, that wait was the node's whole
allowance: the turn sat silent to the wall and then came back to the model with
nothing to say about it.

The park now waits under `jobParkBoundShare` of the allowance it was given — a
third. A share is what scales: whatever wall the node was handed, the bound is
always strictly inside it, and two thirds of the run remain when the model hears
that the wait ran long, so it can act on the news rather than be handed it as the
run ends.

It is safe for a command that is honestly still working because the bound ends
the WAIT and never the WORK. The promoted command is untouched — the registry is
not signalled and its process is not killed — so a job that is still producing
output keeps producing it, and its own ending is still handed to the model as a
note when it finally lands. Only the turn that was parked on it comes back early,
and it comes back with the record of why.