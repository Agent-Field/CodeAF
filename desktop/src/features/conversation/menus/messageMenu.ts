import type { MenuEntry } from '../../../components/ui';
import type { TurnV2 } from '../types';

/** Clipboard refusal stays silent, as it does on the hover Copy control. */
function copy(text: string): void {
  void navigator.clipboard.writeText(text).catch(() => undefined);
}

/** The bubble receives the same text that its hover Copy receives. */
export function messageMenu(text: string): MenuEntry[] {
  return [{ id: 'copy', label: 'Copy', icon: 'copy', disabled: !text, onSelect: () => copy(text) }];
}

/** Answers retain their Markdown source and record order, rather than copying the digest. */
export function foldedTurnMenu(turn: TurnV2, onOpen?: (anchor: string, background: boolean) => void): MenuEntry[] {
  const answer = turn.blocks.flatMap(block => block.kind === 'answer' && block.text ? [block.text] : []).join('\n\n');
  const items: MenuEntry[] = [{ id: 'copy-answer', label: 'Copy answer', icon: 'copy', disabled: !answer, onSelect: () => copy(answer) }];
  if (onOpen) items.push({ id: 'open-tab', label: 'Open in new tab', icon: 'arrowUpRight', onSelect: () => onOpen(turn.id, true) });
  return items;
}
