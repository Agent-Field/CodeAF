// The words of the inline place-delete confirm (Places 6d, Interactions "Delete").
// A place delete never deletes chats. The line names only the counts that change:
// chats whose only place this was, and child places that move up to its parents.
// A zero count is left out (the emptiness law). chatsHere is the engine's total
// filed here, including chats that keep another place; those are not a third clause.

export type PlaceDeleteImpact = {
  children: number;
  chatsHere: number;
  wouldBeUnplaced: readonly string[];
};

const count = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** The confirm line. The place is named so it cannot be read as a chat delete. */
export function placeDeleteQuestion(name: string, impact: PlaceDeleteImpact): string {
  const parts: string[] = [];
  const unplaced = impact.wouldBeUnplaced.length;
  if (unplaced > 0) parts.push(count(unplaced, 'chat becomes unplaced', 'chats become unplaced'));
  if (impact.children > 0) parts.push(count(impact.children, 'place moves up', 'places move up'));
  const detail = parts.length ? ` ${parts.join(' · ')}.` : '';
  return `Delete “${name}”?${detail} No chat is deleted.`;
}
