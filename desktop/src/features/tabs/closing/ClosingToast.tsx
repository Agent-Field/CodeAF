import { useEffect, useState } from 'react';
import { Button } from '../../../components/ui';
import design from '../../../design/tokens.json';
import './closing-toast.css';

export type ClosingToastModel = { id: number; tabId: string; title: string; kind: 'closed' | 'stop-failed' };

type ViewProps = { toast: ClosingToastModel; onStop?: () => void; onUndo?: () => void; onHold?: (held: boolean) => void };

/** The toast itself, with no timer and no placement: what the workspace shows, and what the Design system page specimens. */
export function ClosingToastView({ toast, onStop, onUndo, onHold }: ViewProps) {
  const failed = toast.kind === 'stop-failed';
  return (
    <div className="closing-toast" role="status" aria-live="polite" onPointerEnter={() => onHold?.(true)} onPointerLeave={() => onHold?.(false)} onFocus={() => onHold?.(true)} onBlur={() => onHold?.(false)}>
      <span className="closing-toast-dot" data-state={failed ? 'failed' : undefined} aria-hidden="true"/>
      {failed
        ? <span className="closing-toast-text">Could not stop <span className="closing-toast-title">{toast.title}</span></span>
        : <span className="closing-toast-text"><span className="closing-toast-title">{toast.title}</span> closed and still running</span>}
      <Button className="closing-toast-action" onClick={onStop}>{failed ? 'Try again' : 'Stop it'}</Button>
      {!failed && <Button variant="quiet" className="closing-toast-action" onClick={onUndo}>Undo</Button>}
    </div>
  );
}

type Props = { toast: ClosingToastModel; onStop: () => void; onUndo: () => void; onDismiss: () => void };

/**
 * Design 3l, step 3: "Config stack closed and still running   Stop it   Undo". It lasts 6s and holds while
 * the pointer or focus is on it. It is a status, not a dialog, so it never takes focus.
 */
export function ClosingToast({ toast, onStop, onUndo, onDismiss }: Props) {
  const [held, setHeld] = useState(false);
  useEffect(() => {
    if (held) return;
    const timer = window.setTimeout(onDismiss, design.interaction.toastDuration);
    return () => window.clearTimeout(timer);
  }, [toast.id, held]);
  return <div className="closing-toast-region"><ClosingToastView toast={toast} onStop={onStop} onUndo={onUndo} onHold={setHeld}/></div>;
}
