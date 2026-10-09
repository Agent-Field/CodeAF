// Long-history folding (design v3 conversation 1e, 1f). Pure rules: which turns
// fold, which fall into the "earlier turns" group, and where a jump lands.
// Nothing here invents text; a folded row shows the question and the digest.

import design from '../../design/tokens.json' with { type: 'json' };
import type { TurnV2 } from './types';

/** Beyond this many turns, the older ones fold into one group. */
export const TURN_LIMIT = design.turnFold.limit;
/** Distance from the top of the scroll pane a jump leaves the target at. */
export const JUMP_OFFSET = design.turnFold.jumpOffset;
/** The open-flag key of the earlier-turns group. */
export const EARLIER_KEY = 'earlier-turns';

/** A settled turn folds unless it is the latest; the reader's own choice always wins. */
export function isFolded(turn: TurnV2, index: number, count: number, chosen: Record<string, boolean>): boolean {
  const choice = chosen[turn.id];
  if (choice !== undefined) return choice;
  return turn.state === 'done' && index < count - 1;
}

/** The turns the group holds, and the ones that stay in the flow. */
export function splitEarlier(turns: TurnV2[], limit = TURN_LIMIT): { earlier: TurnV2[]; recent: TurnV2[] } {
  const cut = Math.max(0, turns.length - limit);
  return { earlier: turns.slice(0, cut), recent: turns.slice(cut) };
}

export function earlierLabel(count: number): string {
  return `${count} earlier ${count === 1 ? 'turn' : 'turns'}`;
}

/**
 * Where a step lands, given each turn's top edge relative to the pane. Up goes
 * to the last turn above the offset, down to the first below it. -1 means no
 * turn is left that way: the caller goes to the start or to the bottom.
 */
export function jumpIndex(tops: number[], direction: 1 | -1, offset = JUMP_OFFSET): number {
  const slack = 1;
  if (direction === -1) {
    for (let i = tops.length - 1; i >= 0; i--) if (tops[i] < offset - slack) return i;
    return -1;
  }
  return tops.findIndex((top) => top > offset + slack);
}
