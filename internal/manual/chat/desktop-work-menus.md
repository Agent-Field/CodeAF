# Desktop work block menus

## How do I copy the log of what codeaf did?

In the desktop app, right-click the work line ("Worked 1m 12s · 2 steps") or focus it and press the menu key or Shift+F10, then choose "Copy log". The clipboard gets plain text: each step's title, then each call's command and output, in the order the steps settled. An output the engine left out of the snapshot is fetched first; if that fails, the call's line is copied without it.

## How do I copy a command or a tool's output?

Right-click a step row (or press the menu key or Shift+F10 on it) and choose "Copy command or output". A command call copies the command alone; any other call copies its output, or the path or query it names when it has no output. A step with several calls copies each, one after another.
