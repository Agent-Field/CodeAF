---
kind: fixed
title: the stopped frame is compared against a clock in the test's hand
pr: 1373
surface: [chat]
invalidates: []
---
The head draws the time of day on every frame, so a test that compares two
whole rendered frames was comparing the wall clock too, and failed whenever its
two captures straddled a minute boundary. One run in eighty one, and it was
read once on a pull request as a regression in a change that could not reach
the path it drives. The stopped turn's fixture now holds its own clock, which
the sibling fixture in the same subject already did. The clock is pinned there
rather than in the fixture every test in the package shares, because that one
would change observable time for tests asserting on elapsed durations. And
because narrowing the comparison buys the same green while costing the test
most of what it is for, the comparison has a name now and a second test that
goes red the moment it stops covering the head.
