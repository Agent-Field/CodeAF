// The words and the set a History Delete… confirms. Pure, so a node test can hold the count
// ("Delete 1 chat?" / "Delete 14 chats?") without mounting the row. The 10s wait lives in tokens.json
// (interaction.historyDeleteToastMs) and is read by the pane, so this file stays free of the JSON import.

/** The inline confirm. One chat is singular; every other count is plural, including zero, which the row never asks. */
export function deleteQuestion(count: number): string {
  return `Delete ${count} ${count === 1 ? 'chat' : 'chats'}?`;
}

/** The toast after the engine has moved the chats. Same singular rule as the confirm. */
export function deletedSentence(count: number): string {
  return `${count} ${count === 1 ? 'chat' : 'chats'} deleted`;
}

export type DeleteAsk = { ids: string[]; anchorId: string };

type Row = { id: string; archived: boolean };

/**
 * Which archived chats one Delete… covers, and which row the confirm replaces.
 * A row that sits inside a selection of more than one covers every archived chat in that selection,
 * and the confirm takes the place of the first of those (list order). A row outside the selection covers only itself.
 * An unarchived row is not deletable, so it is left out of the count and never becomes the confirm.
 */
export function deleteAsk(invokedId: string, selectedIds: readonly string[], items: readonly Row[]): DeleteAsk | undefined {
  const invoked = items.find(item => item.id === invokedId);
  if (!invoked?.archived) return undefined;
  const selected = new Set(selectedIds);
  const inSelection = selected.has(invokedId);
  const archived = items.filter(item => item.archived && (inSelection ? selected.has(item.id) : item.id === invokedId));
  if (archived.length === 0) return undefined;
  // A lone row, or a selection that only contains this one archived chat, confirms that chat on its own row.
  if (!inSelection || archived.length < 2) return { ids: [invoked.id], anchorId: invoked.id };
  return { ids: archived.map(item => item.id), anchorId: archived[0].id };
}

/** The inclusive run from the anchor row to the clicked row, in list order. A missing id selects only the click. */
export function idsBetween(items: readonly { id: string }[], fromId: string, toId: string): string[] {
  const from = items.findIndex(item => item.id === fromId);
  const to = items.findIndex(item => item.id === toId);
  if (from < 0 || to < 0) return [toId];
  const [start, end] = from < to ? [from, to] : [to, from];
  return items.slice(start, end + 1).map(item => item.id);
}
