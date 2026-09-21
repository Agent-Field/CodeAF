---
kind: fixed
title: a sign-in and a harness ask say what they are waiting for, instead of only that somebody is needed
pr: 1315
surface: [engine, chat]
invalidates:
  - "A session stopped on a sign-in, a harness offer or a harness design card told every other window that a person was needed and gave no sentence saying what for, so a surface drew a mark nobody could act on. Both lanes now bank the question at the desk and the row says what it is waiting for."
---
personAskLanes decides THAT a person is needed from the lane books, while
waitingOnPerson takes the sentence from the presence desk. A lane counted in the
first and absent from the second leaves waiting true with an empty reason. The
sign-in lane and the harness lane, which is one book written by both the offer
and the design card, now raise through presenceAskingWhole, so the row carries
the question's own words. The lanes whose words come from another arm of
waitingOnPerson, the sub-harness offer, the fuel gate and landings, are
unchanged.
