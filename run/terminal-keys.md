# Terminal keys (tabs-audit finding 2)

Off a Mac the primary modifier is Ctrl, which is also the shell's editing modifier. The window-level capture listener in
`desktop/src/design/keyboard.ts` stole Ctrl+W/T/K/S/B/Y/1-9 from xterm.

## What changed
- `shortcutOf(event, platform | { mac, terminal })`: with `terminal: true` on a non-Mac, only these chords stay with the app:
  Ctrl+`, Ctrl+Tab / Ctrl+Shift+Tab, Ctrl+PageUp/PageDown, and Ctrl+Shift chords (T = new tab, W = close tab, A overview,
  K tasks, F focus). Ctrl+Shift+T therefore means "new tab" in a terminal, not "reopen closed tab".
- `dispatchShortcut` derives the context from `event.target.closest('.xterm')` (`isTerminalTarget`), so portals and several
  terminal tabs work; one registry, no per-feature guard.
- `TerminalScreen` custom key handler uses the same predicate (`isTerminalShortcut`), so xterm does not also emit those keys.
- Mac Cmd chords and Ctrl+Tab are unchanged. Outside a terminal Ctrl+W etc. still act on the app.
- Menu hint: the terminal tab's "Close tab" shows `Ctrl Shift W` on Linux (`terminalTabShortcuts`).

## Tests
- `src/design/keyboard.test.ts`: pure matcher, Linux terminal / Mac terminal.
- `tests/ui/terminal-tab.spec.ts`: real xterm textarea, Ctrl+W/T/K/S/B/Y/1/9 reach `/input` and change no tab; Ctrl+Shift+T/W
  open/close; outside-terminal Ctrl+W closes; Mac Cmd+W closes from a terminal. Chromium + WebKit, port 1765, workers=1.
- `npm run check` passes.

## Unresolved (user choice, default shipped)
- Reopen-closed-tab has no key inside a Linux terminal (Ctrl+Shift+T is new tab). Default: use the menu or leave the terminal.
- Ctrl+Alt+digit model pins and Ctrl+,/Ctrl+/ pass to the shell in terminals (they fall outside the allowed list).
- History worker's Cmd+Y addition lives in the same `shortcutOf`; both edits touch different lines, merge should be clean.
