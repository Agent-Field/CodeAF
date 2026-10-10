import { useEffect, useRef } from 'react';
import { Button } from '../../components/ui';
import { deleteQuestion } from './deleteFlow';

type Props = {
  count: number;
  busy?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
  /** The virtual list places the confirm where the row was. The specimen leaves it in normal flow. */
  placeRef?: (node: HTMLDivElement | null) => void;
  /** The live list focuses Cancel. A specimen must not steal focus from the design page. */
  autoFocus?: boolean;
};

/**
 * The inline delete confirm (Interactions "Delete"). It replaces one history row — never a dialog —
 * and names how many archived chats Delete will move: "Delete 1 chat?" or "Delete 14 chats?".
 * Cancel is the quiet button. Delete is the danger button. Escape cancels.
 */
export function DeleteConfirm({ count, busy = false, onCancel, onConfirm, placeRef, autoFocus = true }: Props) {
  const root = useRef<HTMLDivElement | null>(null);
  const question = deleteQuestion(count);
  useEffect(() => {
    if (!autoFocus) return;
    // The menu that opened this gives focus back to the row it replaced, which is already gone.
    // Cancel takes focus once that return has landed, and only from the document itself.
    const claim = () => root.current?.querySelector<HTMLButtonElement>('[data-cancel]')?.focus();
    const reclaim = (event: FocusEvent) => { if (event.target === document.body) claim(); };
    document.addEventListener('focusin', reclaim);
    const first = window.setTimeout(claim, 0);
    const done = window.setTimeout(() => document.removeEventListener('focusin', reclaim), 400);
    return () => { window.clearTimeout(first); window.clearTimeout(done); document.removeEventListener('focusin', reclaim); };
  }, [autoFocus]);
  const setNode = (node: HTMLDivElement | null) => { root.current = node; placeRef?.(node); };
  return <div ref={setNode} className="history-delete-confirm" role="group" aria-label={question} aria-busy={busy || undefined}
    onKeyDown={event => { if (event.key === 'Escape' && !busy) { event.preventDefault(); event.stopPropagation(); onCancel(); } }}>
    <span className="history-delete-question">{question}</span>
    <Button variant="quiet" data-cancel disabled={busy} onClick={onCancel}>Cancel</Button>
    <Button variant="danger" loading={busy} onClick={onConfirm}>Delete</Button>
  </div>;
}
