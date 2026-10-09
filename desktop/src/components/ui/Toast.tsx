import { useEffect, useState, useSyncExternalStore } from 'react';
import { createPortal } from 'react-dom';
import design from '../../design/tokens.json';
import { toasts, type Toast as ToastModel, type ToastPart } from '../../design/toasts';
import { Button } from './Button';

type ViewProps = {
  toast: Pick<ToastModel, 'message' | 'tone' | 'actions'> & { undo?: unknown };
  onAction?: (index: number) => void;
  onUndo?: () => void;
  onHold?: (held: boolean) => void;
};

const part = (item: ToastPart, index: number) => (typeof item === 'string' ? item : <span key={index} className="toast-strong">{item.strong}</span>);

/** The toast itself, with no timer and no placement: what the region shows, and what the Design system page specimens. */
export function ToastView({ toast, onAction, onUndo, onHold }: ViewProps) {
  return (
    <div className="toast" role="status" aria-live="polite" onPointerEnter={() => onHold?.(true)} onPointerLeave={() => onHold?.(false)} onFocus={() => onHold?.(true)} onBlur={() => onHold?.(false)}>
      <span className="toast-dot" data-tone={toast.tone} aria-hidden="true"/>
      <span className="toast-text">{toast.message.map(part)}</span>
      {toast.actions?.map((action, index) => <Button key={action.label} className="toast-action" onClick={() => onAction?.(index)}>{action.label}</Button>)}
      {toast.undo !== undefined && <Button variant="quiet" className="toast-action" onClick={onUndo}>Undo</Button>}
    </div>
  );
}

/**
 * The window's one toast region (design 3l step 3). It lasts `interaction.toastDuration` and holds while the pointer
 * or focus is on it. It is a status, not a dialog, so it never takes focus. Mount it once. It draws inside the open
 * modal dialog, if there is one, because nothing behind a modal can be seen or pressed.
 */
export function ToastRegion() {
  const toast = useSyncExternalStore(toasts.subscribe, toasts.getToast);
  const [held, setHeld] = useState(false);
  const [, layer] = useState(0);
  // A dialog's `close` event does not bubble; when one closes the toast moves out of it, or it would stay in a closed dialog.
  useEffect(() => { const closed = () => layer(count => count + 1); document.addEventListener('close', closed, true); return () => document.removeEventListener('close', closed, true); }, []);
  const id = toast?.id;
  useEffect(() => {
    if (held || id === undefined) return;
    const timer = window.setTimeout(() => toasts.dismiss(id), design.interaction.toastDuration);
    return () => window.clearTimeout(timer);
  }, [id, held]);
  useEffect(() => { if (id === undefined) setHeld(false); }, [id]);
  if (!toast) return null;
  const region = <div className="toast-region"><ToastView key={toast.id} toast={toast} onAction={index => toasts.act(toast.id, index)} onUndo={() => toasts.undo(toast.id)} onHold={setHeld}/></div>;
  // A modal dialog (the tab overview) owns the top layer and makes the page inert, so a toast posted from inside it is drawn there.
  const modal = document.querySelector<HTMLElement>('dialog:modal');
  return modal ? createPortal(region, modal) : region;
}
