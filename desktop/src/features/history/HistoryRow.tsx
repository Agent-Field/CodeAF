import type { MouseEvent } from 'react';
import { ContextMenu, Icon } from '../../components/ui';
import { rowMenu, type HistoryPress } from './historyMenu';
import { FileChips } from './FileChip';
import { rowLead, rowLine, rowTrail, type Lead } from './model';
import type { HistoryItem } from './types';

/** The leading mark: a 6px state dot for live work or a question (colour only on the dot), the conversation glyph otherwise. */
export function LeadMark({ lead }: { lead: Lead }) {
  return <span className="history-lead" data-lead={lead}>{lead === 'conversation' ? <Icon name="tab" size="xs"/> : <span className="history-dot" aria-hidden="true"/>}</span>;
}

type Props = {
  item: HistoryItem; now: number; selected: boolean; domId: string;
  onSelect: (item: HistoryItem) => void;
  /** Enter or a double click: continue the conversation. A command-click asks for a new tab. */
  onOpen: (item: HistoryItem, event: HistoryPress) => void;
  /** Read conversation: the saved messages, read-only, in this tab. */
  onRead?: (item: HistoryItem) => void;
  /** Archive, from the row menu. Absent when no workspace host can archive. */
  onArchive?: (item: HistoryItem) => void;
  /** Delete opens the host’s confirmation; absent until a canonical delete handler exists. */
  onDelete?: (item: HistoryItem) => void;
  /** Where the row sits in the virtual list (CSS variables are set by the list, never inline). */
  placeRef?: (node: HTMLDivElement | null) => void;
};

/** One conversation in the list (Components "History row"): title, one sentence of substance, up to three changed files, a stamp. */
export function HistoryRow({ item, now, selected, domId, onSelect, onOpen, onRead, onArchive, onDelete, placeRef }: Props) {
  const line = rowLine(item);
  const stateWord = item.state === 'needs-you' ? 'Needs you' : item.state === 'working' || (item.open && item.tasksRunning > 0) ? 'Working' : undefined;
  // A click selects; a command-click (or a middle click) opens the conversation in a new tab; a double click continues.
  const click = (event: MouseEvent) => {
    if (event.metaKey || event.ctrlKey) { onOpen(item, { newTab: true, background: true }); return; }
    onSelect(item);
    if (event.detail > 1) onOpen(item, { newTab: false });
  };
  const middle = (event: MouseEvent) => { if (event.button === 1) { event.preventDefault(); onOpen(item, { newTab: true, background: true }); } };
  return <ContextMenu label={`${item.title} actions`} items={rowMenu(item, onOpen, onRead, onArchive, onDelete)}>
    <div ref={placeRef} id={domId} role="option" aria-selected={selected} className="history-row" data-selected={selected || undefined} data-open={item.open || undefined} onClick={click} onAuxClick={middle}>
      <LeadMark lead={rowLead(item)}/>
      <div className="history-row-main">
        <span className="history-row-title">{item.title}</span>
        {stateWord && <span className="history-visually-hidden">({stateWord})</span>}
        {line && <span className="history-row-line">{line}</span>}
        {item.files.length > 0 && <FileChips files={item.files}/>}
      </div>
      <span className="history-row-stamp">{rowTrail(item, now)}</span>
    </div>
  </ContextMenu>;
}
