// A closing tab stays in the strip for one base duration so its slot can shrink and the tabs beside it slide.
// The reducer has already dropped it; this module only remembers where it stood.
import type { Tab, TabGroup } from './types.ts';

/** A tab the strip still draws after it has left the workspace, long enough for the width collapse. */
export type TabDeparture = {
  tab: Tab;
  /** Index in the visible strip at the moment it left, so it stays between those neighbours. */
  index: number;
  /** The group it belonged to, kept even when closing it emptied the group and the reducer dropped the group. */
  group?: TabGroup;
};

/** Tabs present in `before` and absent from `after`, in the order they stood. */
export function departuresFrom(before: readonly Tab[], after: readonly Tab[], groups: readonly TabGroup[]): TabDeparture[] {
  const stay = new Set(after.map(tab => tab.id));
  return before.flatMap((tab, index) => {
    if (stay.has(tab.id)) return [];
    return [{ tab, index, group: tab.groupId ? groups.find(group => group.id === tab.groupId) : undefined }];
  });
}

/** Reduced motion drops a closing tab on the commit. Otherwise it stays through the width transition. */
export function retainDepartures(reducedMotion: boolean, gone: readonly TabDeparture[]): readonly TabDeparture[] {
  return reducedMotion ? [] : gone;
}

/** Departures still on screen, plus any new ones, minus a tab that has come back. */
export function mergeDepartures(existing: readonly TabDeparture[], gone: readonly TabDeparture[], live: ReadonlySet<string>): TabDeparture[] {
  const kept = existing.filter(item => !live.has(item.tab.id));
  const seen = new Set(kept.map(item => item.tab.id));
  return [...kept, ...gone.filter(item => !seen.has(item.tab.id) && !live.has(item.tab.id))];
}

export function departureIds(list: readonly TabDeparture[]): string {
  return list.map(item => item.tab.id).join('\0');
}

/**
 * Puts each departing tab back at the index it left. Inserting from the left into the shorter list
 * restores the old order, so the slot that collapses is the one the neighbours are already beside.
 */
export function withDepartures(visible: readonly Tab[], departures: readonly TabDeparture[]): Tab[] {
  const live = new Set(visible.map(tab => tab.id));
  const pending = departures.filter(item => !live.has(item.tab.id)).sort((a, b) => a.index - b.index || a.tab.id.localeCompare(b.tab.id));
  const placed = visible.slice();
  for (const item of pending) placed.splice(Math.max(0, Math.min(item.index, placed.length)), 0, item.tab);
  return placed;
}

/** Groups the reducer already removed, while a departing member of them is still on screen. */
export function groupsForDepartures(groups: readonly TabGroup[], departures: readonly TabDeparture[]): TabGroup[] {
  const have = new Set(groups.map(group => group.id));
  const extra: TabGroup[] = [];
  for (const item of departures) {
    if (!item.group || have.has(item.group.id)) continue;
    have.add(item.group.id);
    extra.push(item.group);
  }
  return [...groups, ...extra];
}

/** `--dur-base` as milliseconds. A missing or non-duration token holds nothing. */
export function exitHoldMs(raw: string): number {
  const value = raw.trim();
  const milliseconds = /^([\d.]+)ms$/.exec(value);
  if (milliseconds) return Number(milliseconds[1]);
  const seconds = /^([\d.]+)s$/.exec(value);
  if (seconds) return Number(seconds[1]) * 1000;
  return 0;
}
