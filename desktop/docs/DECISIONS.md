# Decisions

Calls made from the design and the code when a lane was asked to decide
rather than wait. The assumption the app ships is also a row in
`DESIGN-QUESTIONS.md`.

## t-d5-be-pg-merge-decision (2026-10-10)

**Question.** Places §6d (the card coverage calls P-6d-10) says “Delete or
merge a place. From the Home menu.” The sentences under that card describe
delete: chats keep their other places or become unplaced, nothing is lost,
history keeps everything, and children move up to the deleted place’s
parents. The 8f place menu drawing (coverage P-X-13) lists Go to, Quick Look,
Open in new window, Rename, Tint, Add to another place…, Pin to rail,
Archive, Delete place…. It does not draw Merge. The 60-day card says a quiet
suggestion to merge or archive, on Home and in ⌘P, and does not say which
place a merge lands in. 9d says a place inside two families keeps the tint of
the parent it was created in, and never blends colours. Nothing in the design
says how instructions or policy combine.

**Call.** Merge ships. The place the menu was opened on is absorbed. The
person picks the survivor. Nothing is deleted, and colours are not blended.

### Target

“Merge into…” opens the existing Go to sheet titled `Merge “Name” into…`.
The absorbed place, every place under it, and archived places are not
offered, because a place cannot sit inside itself. There is no suggested
target and no second confirmation. Picking a row merges at once. The toast
reads `Merged “Name” into “Survivor”` and offers Undo. Undo restores both
places, because the receipt puts the graph back to the snapshot from before
the write. Merging a place into itself is refused. A missing target is
refused with “Say which place to merge into.” An archived target is refused.

The 60-day suggestion’s Merge button opens that same chooser. It does not
pick a place. Archive and Not now on that line stay as they are.

### What combines

- **Instructions.** The survivor’s text stays first. The absorbed place’s
  instructions are appended after a blank line when they are non-empty and
  different. The same text is not repeated.
- **Sources.** United by kind and ref. The survivor’s copy of a duplicate
  stays.
- **Policy.** The survivor wins each field. A blank model or permissions
  field is filled from the absorbed place. The reserved manager blob follows
  the same empty-fill rule. No question is asked, and the two values are not
  mixed.
- **Tint.** The survivor keeps its own tint. A child that moves keeps the
  colour it was showing, pinned when inheriting from the survivor would
  change it. 9d’s “never blend” rule is the reason.
- **Chats.** Memberships move onto the survivor. A chat already in both keeps
  the survivor’s existing row. Other places that chat belongs to stay. No
  chat is deleted and none becomes unplaced only because this place went
  away. The delete sentences on the same 6d card stay the delete path.
- **Children.** They become children of the survivor. A child that already
  sits above the survivor takes the absorbed place’s parents instead, so the
  graph does not loop. Parents of the absorbed place are added to the
  survivor unless that edge would loop; those are reported as skipped and
  not applied.
- **Knows lines.** They move to the survivor. Their text is not rewritten
  and duplicates are not folded by this write.
- **Rail pin.** If the absorbed place was pinned and the survivor was not,
  the pin moves to the survivor. If the survivor was already pinned, it
  stays pinned.
- **Tabs.** The survivor keeps the tab set it already had. The absorbed
  place’s saved tabs are not copied onto it. They remain stored under the
  absorbed place and return with Undo.

### Where the item sits

One menu builder draws it, so it appears everywhere that builder is used
and the merge handler is wired:

1. The place right-click menu (8f), after “Add to another place…” and before
   Pin to rail.
2. The Home page’s ⋯ menu, in that same place. Archive and Delete stay on
   this menu.
3. The Home tab’s place menu, in that same place, then Close place. That
   tab menu does not carry Archive or Delete (PS9).

Offline, the write is absent, so the item is absent. Now and All places
have no place menu.

### Window

The window that ran the merge goes to the survivor. Another window that
still has the absorbed place selected, once its graph read shows the place
is gone, shows Now and says “That place no longer exists, so this window
shows Now.”

### What this does not invent

No blended tint, no model call to reconcile instructions, no default
survivor, no preview counts, and no confirm the design does not draw.
Delete’s confirmation stays on delete only.
