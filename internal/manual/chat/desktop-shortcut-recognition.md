# Desktop shortcut recognition

## Why does desktop Undo leave my textarea alone?

⌘Z on macOS or Ctrl Z on Linux undoes a structural action when a feature can handle it. Inputs, textareas, editable text and terminal fields keep their own Undo, including when the field is empty. The desktop does not cancel a recognized chord unless a feature handles it.

## Which desktop shortcuts select places or open files?

⌘P opens Go to a place, ⌘⇧P opens All places, ⌘0 opens the current place's Home, and ⌘N opens a new window. Linux uses Ctrl in place of ⌘. Place slots 1–9 use ⌃ on macOS and Alt on Linux; ⌘1–9 or Ctrl 1–9 still select tabs. ⌘G or Ctrl G groups selected tabs.

⌘O or Ctrl O selects Open file… in the new-tab field. ⌃` or Ctrl ` opens a terminal when a terminal handler is available. A prose input or textarea containing words keeps these chords; the new-tab command field retains them even with a query.

## Does Space Quick Look interrupt typing or activate a button?

The registry recognizes bare Space as Quick Look outside inputs, textareas, editable text, terminal fields, selects and buttons, including a button's nested icon. A feature must supply the Quick Look handler; recognition alone does not open a preview or cancel the key.

⌘⇧C or Ctrl Shift C is Copy link where a tab link is available. A file or diff surface keeps Copy path, and a terminal keeps its copy action.

## Is Home Up the bracket shortcut or the up arrow?

Iteration 2 keeps ⌘[ and ⌘] as focus-history Back and Forward, including on Home. Linux uses Alt ← and Alt →. Home Up uses ⌘↑ or Ctrl ↑ outside editing fields. In a conversation the arrows retain turn navigation.
