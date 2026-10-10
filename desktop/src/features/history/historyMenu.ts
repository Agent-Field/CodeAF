import type { MenuEntry } from '../../components/ui/Menu';
import type { HistoryItem } from './types';

export type HistoryPress = { newTab: boolean; background?: boolean };
type Action = (item: HistoryItem) => void;

/** The host supplies capabilities so a row never offers an action with no engine behind it. */
export function rowMenu(item: HistoryItem, open: (item: HistoryItem, press: HistoryPress) => void, read?: Action, archive?: Action, confirmDelete?: Action, addToPlace?: (item: HistoryItem) => MenuEntry | undefined): MenuEntry[] {
  const entries: MenuEntry[] = [{ id: 'continue', label: 'Continue', onSelect: () => open(item, { newTab: false }) }];
  if (read) entries.push({ id: 'read', label: 'Read', onSelect: () => read(item) });
  const place = addToPlace?.(item);
  if (place) entries.push(place);
  if (archive || (item.archived && confirmDelete)) entries.push({ kind: 'separator', id: 'sep' });
  if (archive) entries.push({ id: 'archive', label: item.archived ? 'Unarchive' : 'Archive', icon: 'archive', disabled: !item.archived && item.state !== 'idle', onSelect: () => archive(item) });
  // ARCHIVED FIRST: the host opens confirmation; choosing this entry never deletes immediately.
  if (item.archived && confirmDelete) entries.push({ id: 'delete', label: 'Delete…', onSelect: () => confirmDelete(item) });
  return entries;
}
