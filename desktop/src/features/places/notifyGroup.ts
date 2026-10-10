// Groups needs-you notifications by place (Interactions "Notifications").
//
// One question is one notification. Notifications for the same place share a
// thread id and a title, which is that place's name. A chat in no live place
// shares the Now thread: the rail's name for unplaced work, not a guessed
// product title. A failed item is included beside the questions. Running and
// finished work are not notifications, and a question marked not blocking is
// not one either (Iteration 2: only blocking needs-you and failed).

/** The thread id for a chat the graph does not file in a live place. Same spelling as the window's Now key. */
export const UNPLACED_THREAD_ID = 'now';

/** The thread title for that chat. The rail spells this place Now. */
export const UNPLACED_THREAD_TITLE = 'Now';

const NEEDS_YOU = new Set(['question', 'approval', 'needsYou']);

/** One attention record the world feed or a Home row can hand over. */
export type GroupAttention = {
  id: string;
  chatId: string;
  kind: string;
  /**
   * Absent means the question blocks. Older feeds omit the field, and those
   * questions stop a conversation.
   */
  blocking?: boolean;
};

/** A place the graph listed. An archived place is hidden, so it cannot name a thread. */
export type GroupPlace = { id: string; name: string; archived?: boolean };

/** A direct filing, in the engine's membership order. The first live one names the thread. */
export type GroupMember = { chatId: string; placeId: string };

export type NotificationThread = {
  /** Place id, or `now` when the chat is unplaced. */
  id: string;
  /**
   * The place's name, or Now. Absent when that place has no name: a blank
   * name is not a title, and Now is not borrowed for a place that exists.
   */
  title?: string;
};

/** One notification, already reduced to a single record per question or failure. */
export type GroupedNotification = {
  id: string;
  chatId: string;
  kind: 'needsYou' | 'failed';
  thread: NotificationThread;
};

export type NotifyDirectory = {
  nodes?: readonly GroupPlace[];
  members?: readonly GroupMember[];
};

/**
 * The notifications Interactions posts, each carrying the thread it belongs to.
 * Input order is kept. A repeated id is one question, so the first record wins.
 */
export function groupNotifications(items: readonly GroupAttention[], places?: NotifyDirectory | null): GroupedNotification[] {
  const nodes = new Map<string, GroupPlace>();
  for (const node of places?.nodes ?? []) {
    // A repeated id is damage. The first row keeps the name the list was built with.
    if (node.id && !nodes.has(node.id)) nodes.set(node.id, node);
  }
  const seen = new Set<string>();
  const grouped: GroupedNotification[] = [];
  for (const item of items) {
    if (!item.id || seen.has(item.id)) continue;
    const failed = item.kind === 'failed';
    const needsYou = NEEDS_YOU.has(item.kind);
    if (!failed && !needsYou) continue;
    // A question the engine marked non-blocking never notifies. A failure is not a question.
    if (needsYou && item.blocking === false) continue;
    seen.add(item.id);
    grouped.push({
      id: item.id,
      chatId: item.chatId,
      kind: failed ? 'failed' : 'needsYou',
      thread: threadFor(item.chatId, places?.members, nodes),
    });
  }
  return grouped;
}

function threadFor(chatId: string, members: readonly GroupMember[] | undefined, nodes: Map<string, GroupPlace>): NotificationThread {
  if (chatId && members) {
    for (const member of members) {
      if (member.chatId !== chatId) continue;
      const place = nodes.get(member.placeId);
      // Archived and unknown places are hidden. The next direct membership may still name the thread.
      if (!place || place.archived) continue;
      const title = place.name.trim();
      return title ? { id: place.id, title } : { id: place.id };
    }
  }
  return { id: UNPLACED_THREAD_ID, title: UNPLACED_THREAD_TITLE };
}
