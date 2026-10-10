# Desktop place selectors

## Why is a child place the same colour as its parent

A top-level place picks one of tide, iris, rose, sand or sage. A place inside it shows that colour, and so do the places inside that, until one of them picks a colour of its own. The places under that one then show the new colour. A top-level place with no colour chosen shows graphite, which is also Now's colour. A new top-level place is given the colour fewest top-level places already use. Ties go in that same order, starting at tide. Graphite is never assigned that way.

## Why a place in two parents does not mix colours

A place can sit in more than one parent. It keeps the colour of the first parent, the one it was created in. It does not blend the two colours. Moving the first parent changes the colour; the other parents do not. The name beside it in the rail is that first parent's name.

## What does 4 inside mean, and what does 28 chats mean

On the place tree, a place with other places directly in it says how many, as "4 inside". A place with none says its chats, as "28 chats" or "1 chat". A tile uses the same place count as "47 places · 212 chats": the places directly in it, then the chats in it and in the places under it. A chat filed in two of those places is counted once. A place that is also in another parent adds that, as "6 chats · also in Software". Zero places and zero chats say nothing.

## What does 2 need you in Config parser mean

The dot on a place is amber when a chat there, or in a place under it, is waiting on you, and red when a chat failed. Work that is only running draws no dot. The words name the count and the place: "2 need you in Config parser", "1 needs you in Config parser", or "1 failed task in Config parser". When the dot is on a parent because of one child, the words name that child. When more than one place is the source, the words name the place the dot is on.

## Why is a closed place still in the sidebar

Closing a place takes it out of Open in this window. If something in it is still running, or still waiting on you, the row stays and reads "closed · still running" until that settles. Then it leaves on its own. An Open place nobody has touched for 12 hours also leaves, unless that work is still going. Pinned places stay, in the order you set, and a pinned parent does not pull its children in. Open is the places you have gone to in this window, newest at the top.

## What do control 1 to 9 do for places

Control 1 through 9 (Alt 1 through 9 on Linux) follow the rail: pinned places first, in your order, then Open, newest first. The tenth place has no number. Now is control 0, and it is not one of those nine.
