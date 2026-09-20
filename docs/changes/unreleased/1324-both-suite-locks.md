---
kind: fixed
title: a heavy suite takes both the file lock and the older directory lock, so a checkout behind #1264 can see it
pr: 1324
surface: [build]
invalidates:
  - "Two heavy suites ran side by side and each believed it had the box: one took the file lock and one took the directory lock, and neither mechanism could see the other, so the reds and the timings that came out of the pile were attributable to nothing."
  - "Checking both locks when starting would have stopped us joining a stale holder and could never have stopped a stale runner joining us, because being unaware of the file lock is what makes a checkout stale."
---
A run takes the directory lock first, so there is no moment when it holds the
file lock and is still invisible to a checkout that reads only the directory.
The refusal is reported after the file lock has been tried, because two current
trees collide there and that refusal can say whether the recorded pid is visible
from this process namespace.

The directory is freed by whichever process holds the file lock, which is the
holder beside the suite rather than the wrapper. Killing the wrapper is allowed
by design and must not open the box while a suite is still running, so the two
locks share one lifetime.

The two paths are named independently in the configuration rather than one
trimmed from the other: a name spelled twice is a fact that can disagree with
itself, and a derived name would follow a test harness driving a private lock
straight to the real one.
