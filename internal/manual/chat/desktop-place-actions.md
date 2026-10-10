# Desktop place actions

## How do I archive a place in the desktop app

Archive marks the place archived and takes it off the rail. That is one change.
The place is not deleted: it stays in the graph, so you can still find it from
Go to and from History. Chats filed in it stay filed, and they stay in History.
Nothing about those chats is removed.

Undo of that archive puts the place back as it was, including a rail pin it
had. Archiving a place that is already archived changes nothing, so there is
nothing to undo.

## What does undo do after I archive, move or delete a place on the desktop

Undo takes back the last structural place change: create, rename, tint, adding
a parent, a move, pin, unpin, reorder, archive, delete, or filing a chat.
Each change keeps the engine's receipt, and Undo sends those receipts back.
A window remembers up to 20 of them. A change that did not alter anything is
not one of the 20.

Delete removes the place only. The confirmation names the counts the engine
sends: how many places inside it move up a level, and how many chats are left
in no place or keep another place. No chat is deleted. The toast offers Undo
for 10 seconds. Other place changes use the ordinary toast.

A plain drop, or Add to another place, gives a place one more parent and
leaves the parents it already has. Holding Option while you drop moves it:
that target becomes its only parent. Option on a chat moves the chat out of
the place you dragged it from and into the one you dropped it on. A plain drop
of a chat adds it and leaves its other places.

Reordering the pinned places is one of those undoable changes. The toast says
"Reordered the pinned places."

## Why can't a place sit inside itself

Places can have several parents. A loop cannot: a place cannot contain itself,
and it cannot sit inside a place that is already inside it. codeaf refuses
that in words. A place dropped on itself says "A place can't be inside itself."
A loop through another place says which place would be put inside which, and
that the second is already inside the first. The refusal names the places. It
does not show an internal error.
