# PR 1604 diagnostic workflow — partial, not acceptance

Recorded runtime e556a5b21dadb6fbaf8808bfb625b69da459ecab on Spark, Fleet critical-extended-1569-v2. The original terminal recording was closed normally with Ctrl-C. All 29 recorded calls used OpenRouter deepseek/deepseek-v4.1-flash, including auxiliary roles; Model Pool was disabled for separately fixed #1608/#1610.

The practical goal was to implement and test a small duration parser/humanizer. The reading helper returned the requested read-only marker and a real task was created. The resulting duration implementation passed 11 artifact tests. This establishes useful task output but is not full acceptance: the worker followed an absolute source path in its brief and edited the source workspace despite retaining a separate working copy. This broad PR head lacks the latest separately owned #1562 grounding commits 780f22cb9 and 23d1ab588. No worker-isolation or complete #1569 closure is claimed here.

The recording also reproduces #1556: Alt+T then Enter opened the task room, typed text reached the room composer, but Enter left the full note unsent because the roster retained keyboard focus. Screenshot 1569-room-note-later.cast captures the unsent note. A later Alt+T/Enter recovery attempt occurred after the task had already ended; it is not evidence that the original note was delivered. A separate focused PR and fresh recording own the correction.

The source workspace's modified durations.py and untracked test_durations.py are copied only as diagnostic artifacts. The unmodified original cast preserves the failure and follow-up sequence. Preview video/GIF run at three times speed with idle intervals capped at three seconds. This diagnostic must not be presented as green end-to-end acceptance of PR 1604.
