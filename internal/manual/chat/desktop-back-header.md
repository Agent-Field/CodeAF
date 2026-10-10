# Desktop back header

## How do I go back from a task in the desktop app?

After entering a task in the same tab, the header starts with an arrow and the
parent's name, followed by `/` and the current task title. Activate the parent-name
button to go back one step. A top-level task names its conversation tab; a nested
task names its immediate parent. Older ancestors remain in navigation history.
Command+[ on Mac or Ctrl+[ elsewhere goes back; Command+] or Ctrl+] goes forward.
There is no permanent Back or Forward icon button.

## Why did the desktop Back chip disappear?

The temporary strip Back chip is intended for a jump you did not start yourself.
It names the previous destination as “Back to” followed by its real name. Activating
or using a key on the chip calls Back without dismissing it. Your next pointer activation or
key elsewhere dismisses it. Unknown destinations draw no chip.

The chip component is available for the window focus-history controller through
the strip's leading slot. This change does not yet connect notifications or Next
up to that controller, so those jumps do not yet produce a chip in the app.
