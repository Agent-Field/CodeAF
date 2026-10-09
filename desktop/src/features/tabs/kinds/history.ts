import { HistoryPane } from '../../history/HistoryPane';
import type { KindDef } from './slots';

/** The History tab (Shell 4a-4d): backed by the engine's history routes (recaps, list, search). Opened with ⌘Y. */
export const historyKind: KindDef = { kind: 'history', label: 'History', icon: 'history', backed: true, pane: HistoryPane, preview: null };
