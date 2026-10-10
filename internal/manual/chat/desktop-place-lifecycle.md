# Desktop place archive, deletion and undo

## What happens when I archive, restore, delete or undo a desktop place?

Archive removes a desktop place from the rail and chat context, preserving
its memberships and children. The archived list still includes it.
Restore makes its memberships contribute context again, without repinning it.

Place deletion preserves every chat and transcript. Chats retain their other
places or become unplaced. Direct children replace the deleted parent with
its parents, deduplicated; children of a top-level place become top-level.
This happens in one save. Confirmation counts direct chat memberships and
direct children, including archived children, rather than descendants.

Undo restores the place, parent edges, memberships and pins exactly. The
rail's current Open working set is preserved. Receipts last for 20 structural
writes in this bridge, without surviving restart. Undo later writes first.
Otherwise: "That can't be undone now because the places changed afterwards."
A missing receipt says: "That change can no longer be undone."
