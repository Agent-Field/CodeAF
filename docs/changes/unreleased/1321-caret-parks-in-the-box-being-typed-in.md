---
kind: fixed
title: the caret parks in the box the person is typing into, and hides on surfaces with no box
pr: 1321
surface: [chat]
invalidates:
  - "The connections key entry left the caret blinking in the resting search box at the foot while the person typed into the entry row 12 rows up, and its choice list left the caret in the resting box too. A caretRow hook on the place interface now answers 'where is your live box among the rows just built', and the frame parks the caret there, the column from the box's own renderer."
  - "The tasks filter parked the caret in the foot's 'type to filter this list' while the letters landed in the control row 17 rows up. The caret now parks on the control row, after the typed words, and at rest on the box's first cell."
  - "The task record card blinked a bar at (0,0) over the title on a boxless surface, though its own comment claimed the caret was hidden. It is hidden, by the same law the job page and home at rest follow."
---

Seven contracts pin the parks in internal/tui3/caretpark_test.go: each asserts the caret's row is the box's painted line and its column sits immediately after the typed words, including the settings search box with the foot line present and at rest. The settings search park itself was audited across widths 30 to 160 and heights 8 to 40 and was already right; the wrong parks were the three surfaces above.
