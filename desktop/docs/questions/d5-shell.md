# Shell decisions (t-d5-sh-decide-shell-questions)

Conservative answers for the fourteen open questions in the shell coverage
(`cov-shell.md` / `desktop/docs/DESIGN-COVERAGE.md` §7.2). Each row is also in
the Open table of `desktop/docs/DESIGN-QUESTIONS.md`. Cite the `SH-OQ` id.

Sources: Shell 2b, 2h, 3g, 3j, 3l; Interactions Shell and Shortcuts; Iteration 2
I2.1 (Next up replaces Inbox); design-shell ambiguities 1, 6, 7, 8, 9, 10, 22.
These rows do not move D1–D6, Q1–Q34, Q-P*, OV*, NT*, or R1–R4.

| # | Question | Assumption the app ships now | Governs |
|---|---|---|---|
| SH-OQ1 | Pinned first slot: place Home (2h, 3a–4c) or Inbox (2b, 2g, 3j, 3l)? Ambiguity 1. | Home is first. An Inbox tab, while one is still drawn, is the next pin, never ahead of Home. I2.1 replaces Inbox with Next up; after that, no Inbox chip is inserted. | SH-084, SH-085 |
| SH-OQ2 | ⌥⌘W on Close other tabs (3g) or Close and stop (3l, Interactions)? Ambiguity 7. | Close and stop. Close other tabs has no chord. Off a Mac the chord is Ctrl Alt W. ⌥⌘1–3 stay the pinned models (Q30, R1). Same as M1. | SH-129, SH-130, SH-143, SH-252 |
| SH-OQ3 | Copy link and Move to new window while unbacked: disabled or absent? | Absent, never disabled. Present only when backed: Copy link for a durable target (DL1–DL6), Move to new window in the desktop app (M5). | SH-126, SH-127, SH-278 |
| SH-OQ4 | Strip ⌘-click: background tab (Interactions) or selection for ⌘G (2h)? Ambiguity 6. | ⌘-click (Ctrl-click off a Mac) toggles selection. Middle-click closes an unpinned tab the way × does. A pin and a place Home stay. Overview cards stay OV14. Same pick rule as TI6. | SH-075, SH-076 |
| SH-OQ5 | ⌘K has no design. 3f/3k say one field. | ⌘K opens the New-tab field. The four-page command palette retires. ⌘P stays Go to (PLD-01). ⌘⇧K stays Tasks. Until the palette-retire lane lands, ⌘K still opens the command palette. | SH-240, SH-241 |
| SH-OQ6 | Toast stacking and keyboard. Ambiguity 22. | One toast. A newer one replaces it. Ordinary news is role=status; a refusal stays role=alert. The timer pauses on hover and focus. Escape dismisses. | SH-147, SH-148 |
| SH-OQ7 | Group suggestion ×: this session (2h) or 30 days (Interactions filing)? Ambiguity 9. | This session only. No 30-day memory for tab groups. The 30-day Not now stays place filing. Supersedes the provisional 30-day half of GO6. The offer store still writes 30 days until that lane drops it. | SH-111 |
| SH-OQ8 | ⌘W on a split: the merged tab or the focused pane? Ambiguity 10. | The whole merged tab. Close pane is the pane menu's. The × is the same close (TI13). | SH-210 |
| SH-OQ9 | When is the compressed tab live? | At 600px and below, inactive tabs only. Same rule as SH-072. | SH-072 |
| SH-OQ10 | Can Focus mode hide the macOS traffic lights? | No. They stay visible. A revealed strip keeps clear of them. Peek timing stays R4. | SH-013 |
| SH-OQ11 | Which workspace fields are per window? | Per window: active tab, recents, selection, overview, undo, rail collapsed, focus mode. Shared: tabs, groups, closed list, next number. Place pins stay engine-wide (PLD-13). | SH-043, SH-302, SH-310 |
| SH-OQ12 | Keep ⌘B beside ⌘S? | Yes, as R2. ⌘S stays the design's chord. | SH-035 |
| SH-OQ13 | Does pinch-out open the overview? | Not built, as OV2. The grid button and ⌘⇧\ open it. | SH-190 |
| SH-OQ14 | Inbox and Now right-click is "—". | Nothing opens. Those rows suppress the webview menu. Place rows keep theirs. | SH-028 |

Ambiguity 8 (whether Close N tabs stops running members) is not one of these
fourteen. M7 already says a group close keeps work running and offers Undo.
