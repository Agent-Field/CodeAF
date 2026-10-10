# The Places rail in the desktop app

## How do I pin, reorder or close a place in the rail

In the desktop app the rail has two sections: pinned places, in the order you set, and open places, newest first. Visiting a place puts it at the top of the open section. Closing a place takes it out, unless work is still running in it or waiting on you; then it stays until that settles. Open places nobody has touched for twelve hours let go by themselves, checked every thirty seconds, and a place with work running or waiting on you is never let go.

## Why does a place change in every window at once

Every change to your places (a new place, a rename, a colour, filing or moving chats, pinning) is sent to all open desktop windows as one update, so they redraw from the same version of the graph. A change made from a stale window is refused with "Your places changed in another window. Reload and try again." and nothing is written.
