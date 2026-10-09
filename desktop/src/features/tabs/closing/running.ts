// What "running" means for closing (design 3l): a pane the engine is working on, or holding for the person.
import type { TabSummary } from '../../conversation/tabSummary';
import { panesOf, type Tab } from '../model';

export type Summaries = Readonly<Record<string, TabSummary>>;

/** Working and waiting both keep going after the tab closes: a waiting pane is paused on a question, not finished. */
export const paneRunning = (summaries: Summaries, paneId: string): boolean => {
  const mark = summaries[paneId]?.mark;
  return mark === 'working' || mark === 'waiting';
};

export const tabRunning = (tab: Tab, summaries: Summaries): boolean => panesOf(tab).some(pane => paneRunning(summaries, pane.id));
