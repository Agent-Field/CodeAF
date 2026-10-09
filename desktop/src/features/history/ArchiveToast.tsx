import { useEffect, useRef } from 'react';
import { Button, Icon } from '../../components/ui';
import design from '../../design/tokens.json';

/** How long the toast waits for an answer before it leaves (Components: toasts sit for 6s). Hovering or focusing it stops the clock. */
export const TOAST_MS = design.interaction.historyToastMs;

type Props = { count: number; onReview: () => void; onRestore: () => void; onDismiss: () => void };

/**
 * The one toast after tabs archived themselves (design 4c): "Archived 6 tabs idle for more than 12h", with
 * Review (opens History) and Restore all. It never blocks, and Escape dismisses it.
 */
export function ArchiveToast({ count, onReview, onRestore, onDismiss }: Props) {
  const timer = useRef<number>(undefined);
  const arm = () => { window.clearTimeout(timer.current); timer.current = window.setTimeout(onDismiss, TOAST_MS); };
  const hold = () => window.clearTimeout(timer.current);
  useEffect(() => { arm(); return hold; }, []);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape' && !document.querySelector('dialog[open]')) onDismiss(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onDismiss]);
  return <div className="history-toast" role="status" onPointerEnter={hold} onPointerLeave={arm} onFocus={hold} onBlur={arm}>
    <Icon name="archive" size="xs"/>
    <span className="history-toast-text">Archived {count} {count === 1 ? 'tab' : 'tabs'} idle for more than 12h</span>
    <Button variant="ghost" className="history-toast-action" onClick={onReview}>Review</Button>
    <Button variant="quiet" className="history-toast-action" onClick={onRestore}>Restore all</Button>
  </div>;
}
