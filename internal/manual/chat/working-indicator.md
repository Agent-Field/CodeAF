# The moving logo while codeaf works

## Why is a little logo moving on the left while I wait for an answer?

The single-line chevron and gold dot beside the activity words mean the conversation is still
working. The same indicator appears inside a running task's page and an adaptive
run's page. It remains visible while an answer streams or the work is waiting on
a model. The words beside it describe the known state; the motion is not a
percentage, a promise of success, or a claim that a particular tool is running.

Each new conversation turn chooses one of ten animations at random and avoids
immediately repeating the previous choice. Steering or retrying that turn keeps
its choice. A task page chooses its own animation when opened, independently of
the parent conversation. Rally, ping-pong, dribble, slingshot, juggle, backflip,
cradle, ripple, accordion and infinity all mean the same thing: work is ongoing.

## When does the working animation stop?

The indicator disappears when the work finishes, is interrupted, or needs your
answer. A task that is paused, finished, failed, or no longer being read through
a working connection does not animate. It also steps aside in copy mode.

Screen-reader mode, monochrome or ASCII terminals, and windows too small for the
single-line mark keep the existing text and compact status indicators. The mark
uses ordinary terminal characters; it needs no special font or image support.

There is no new keyboard command to choose an animation. The choice is automatic;
the component's named selections are available to application code.
