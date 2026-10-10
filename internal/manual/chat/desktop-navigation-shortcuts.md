# Desktop navigation shortcuts

## What do Command J, Command brackets and Command Up mean in the desktop app?

The desktop shortcut registry names Command J on Mac and Ctrl J elsewhere
`next-up`. Command [ and Command ] name `back` and `forward` in focus history.
On Linux and Windows, Back and Forward use Alt Left and Alt Right; Ctrl [ and
Ctrl ] remain available to editors. Command I and Ctrl I have no registry binding.

Command Up on Mac, or Ctrl Up elsewhere, names `up-level` when focus is inside
Home outside its composer. A Home text field keeps its caret keys even when
empty. In a chat, Command Up and Down (Ctrl Up and Down elsewhere) remain
previous and next message shortcuts; a field containing words keeps those keys.

These names route through the window's shared shortcut handlers. A chord is
consumed only when a mounted handler accepts it. Registering a name alone does
not create a Next up workflow or focus history. A Linux terminal keeps plain
Ctrl J for the shell.
