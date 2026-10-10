import { useEffect, useRef } from 'react';
import { Button } from '../../components/ui';
import { placeDeleteQuestion, type PlaceDeleteImpact } from './deletePlaceCopy';
import './delete-place-confirm.css';

export type PlaceDeleteState = {
  id: string;
  name: string;
  phase: 'loading' | 'ready' | 'error';
  impact?: PlaceDeleteImpact;
  message?: string;
};

type Props = {
  state: PlaceDeleteState;
  busy?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
};

/**
 * Inline confirm for Delete place… (Places 6d, Interactions "Delete"). It sits on the Home
 * page, never in a dialog, and names how many chats become unplaced and how many child
 * places move up. Delete stays hidden until that preview has arrived, so a click cannot
 * run before the counts are known. Cancel is quiet. Delete place is the danger button.
 * Escape cancels. No chat is deleted.
 */
export function DeletePlaceConfirm({ state, busy = false, onCancel, onConfirm }: Props) {
  const root = useRef<HTMLDivElement>(null);
  const sentence = state.phase === 'ready' && state.impact ? placeDeleteQuestion(state.name, state.impact) : undefined;
  const checking = `Checking what deleting “${state.name}” would change`;
  const failed = state.message ?? 'Could not check what deleting would change.';
  const text = sentence ?? (state.phase === 'error' ? failed : checking);
  useEffect(() => {
    // The menu that opened this returns focus to the control it replaced, which is not this line.
    // Cancel takes focus once that return has landed, and only from the document or that opener.
    const claim = () => root.current?.querySelector<HTMLButtonElement>('[data-cancel]')?.focus();
    const reclaim = (event: FocusEvent) => {
      const target = event.target as Element | null;
      if (target === document.body || target?.hasAttribute('aria-haspopup') || target?.hasAttribute('data-places-tile-focusable')) claim();
    };
    document.addEventListener('focusin', reclaim);
    const first = window.setTimeout(claim, 0);
    const done = window.setTimeout(() => document.removeEventListener('focusin', reclaim), 1000);
    return () => { window.clearTimeout(first); window.clearTimeout(done); document.removeEventListener('focusin', reclaim); };
  }, []);
  return <div ref={root} className="place-delete-confirm" role="group" aria-label={sentence ?? `Delete ${state.name}`} data-phase={state.phase} aria-busy={busy || undefined}
    onKeyDown={event => { if (event.key === 'Escape' && !busy) { event.preventDefault(); event.stopPropagation(); onCancel(); } }}>
    <span className="place-delete-question" role={state.phase === 'error' ? 'alert' : 'status'}>{text}</span>
    <Button variant="quiet" data-cancel disabled={busy} onClick={onCancel}>Cancel</Button>
    {state.phase === 'ready' && <Button variant="danger" loading={busy} onClick={onConfirm}>Delete place</Button>}
  </div>;
}
