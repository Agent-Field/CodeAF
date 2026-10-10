/** How many decided rows a place Home shows before All (DEC-1). 12a draws two examples; the list is the top three. */
export const DECIDED_PREVIEW = 3;
/** P-8: All expands in place and stops at this many, newest first. */
export const DECIDED_CAP = 200;

/** One automatic decision on a place Home. Every string is the caller's; a blank one is not shown. */
export type DecidedWhy = {
  /** Who decided. Omitted from Why? when blank. */
  by?: string;
  /** The reason, in the words the decision recorded. */
  because?: string;
  /** A certainty the decision already spelled ("97%"). A missing one is left out. */
  sure?: string;
};

export type DecidedItem = {
  id: string;
  title: string;
  /** The quiet line under the title ("Launch post · you allowed it twice"). */
  detail?: string;
  /** A short age the caller already wrote ("1h"). The row does not format a clock. */
  age?: string;
  /** RFC 3339. When any row has one, the list is newest first. */
  at?: string;
  why?: DecidedWhy;
};

const titled = (item: DecidedItem): boolean => item.title.trim().length > 0;

/**
 * Newest first when any row carries `at`, otherwise the caller's order.
 * Rows with no title are dropped. The list stops at DECIDED_CAP.
 */
export function orderedDecided(items: readonly DecidedItem[]): DecidedItem[] {
  const kept = items.filter(titled);
  const stamped = kept.some(item => item.at);
  const sorted = stamped
    ? kept.map((item, index) => ({ item, index })).sort((a, b) => {
      if (a.item.at && b.item.at && a.item.at !== b.item.at) return a.item.at < b.item.at ? 1 : -1;
      if (a.item.at && !b.item.at) return -1;
      if (!a.item.at && b.item.at) return 1;
      return a.index - b.index;
    }).map(entry => entry.item)
    : kept;
  return sorted.slice(0, DECIDED_CAP);
}

/** The rows on screen: the preview, or the capped list once All has been opened. */
export function visibleDecided(items: readonly DecidedItem[], expanded: boolean): DecidedItem[] {
  const ordered = orderedDecided(items);
  return expanded ? ordered : ordered.slice(0, DECIDED_PREVIEW);
}

/** All is absent when the preview already holds every row (DEC-5). */
export function decidedShowsAll(items: readonly DecidedItem[]): boolean {
  return orderedDecided(items).length > DECIDED_PREVIEW;
}

/**
 * The number in the label. `total` is the engine's count when the caller did not
 * pass every row; it never undercuts the rows that are actually here.
 */
export function decidedCount(items: readonly DecidedItem[], total?: number): number {
  const present = items.reduce((count, item) => count + (titled(item) ? 1 : 0), 0);
  if (typeof total !== 'number' || !Number.isFinite(total) || total < present) return present;
  return Math.trunc(total);
}

/** The section label, counted the way 12a writes it: "Decided automatically · 9". */
export function decidedLabel(count: number): string {
  return `Decided automatically · ${count}`;
}

/** A string the row may draw. Blank and whitespace-only are nothing (the emptiness law). */
export function decidedText(value: string | undefined): string | undefined {
  const text = value?.trim();
  return text ? text : undefined;
}
