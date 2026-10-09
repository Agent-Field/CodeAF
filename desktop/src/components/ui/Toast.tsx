import { useCallback, useEffect, useRef, useState, type FocusEvent, type KeyboardEvent } from 'react';
import { Button } from './Button';

// The shapes are spelled here, beside the primitive, so the shared library depends on no feature. They match the Places
// shell's contract (features/places/shell/contracts.ts) field for field, and TypeScript's structural typing lets either
// be passed where the other is asked for.

export type ToastAction = { label: string; onSelect: () => void | Promise<void>; primary?: boolean };
/** A toast (Components "Overlays"): bottom-centre on sh-2 for 6s, and it always offers Undo when there is something to undo. */
export type ToastModel = {
  id: string;
  text: string;
  /** The thing the sentence names, drawn in medium weight where the sentence spells it. */
  subject?: string;
  /** Accent dot for news, amber for a refusal the person should read. */
  tone?: 'info' | 'warning';
  actions: readonly ToastAction[];
  /** Milliseconds before it leaves by itself; it stays while hovered or focused. */
  durationMs?: number;
};

/** The design's "6s" (Components "Overlays"). Delete passes its own ten seconds (Interactions "Delete"). */
export const TOAST_DURATION_MS = 6000;
/** At most this many toasts are drawn at once; showing a fourth lets the oldest go. */
export const TOAST_LIMIT = 3;

const messageOf = (error: unknown) => (error instanceof Error && error.message ? error.message : 'That did not work.');

/** The sentence with its subject in medium weight. The subject is only bolded where the sentence spells it literally;
 * otherwise it leads the sentence, so a subject is never silently dropped. */
function Sentence({ text, subject }: { text: string; subject?: string }) {
  if (!subject) return <>{text}</>;
  const at = text.indexOf(subject);
  if (at < 0) return <><strong className="toast-subject">{subject}</strong> {text}</>;
  return <>{text.slice(0, at)}<strong className="toast-subject">{subject}</strong>{text.slice(at + subject.length)}</>;
}

/** One toast. It leaves by itself after its duration, but never while the pointer is over it or focus is inside it, so a
 * person reaching for Undo is never raced. An action is awaited and then the toast leaves; an action that is refused
 * keeps the toast and puts the refusal in its place, amber, for a fresh full duration. Escape inside it dismisses it. */
export function Toast({ toast, onDismiss }: { toast: ToastModel; onDismiss: (id: string) => void }) {
  const duration = toast.durationMs ?? TOAST_DURATION_MS;
  const [failure, setFailure] = useState<string>();
  const [pending, setPending] = useState<string>();
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const remaining = useRef(duration);
  const dismiss = useRef(onDismiss);
  dismiss.current = onDismiss;

  // Showing the same id again with new words is a new toast in the same place: its failure clears and its clock restarts.
  useEffect(() => { setFailure(undefined); remaining.current = duration; }, [toast, duration]);

  const paused = hovered || focused || pending !== undefined;
  useEffect(() => {
    // A duration of zero or Infinity means the owner dismisses it; nothing here does.
    if (paused || !Number.isFinite(duration) || duration <= 0) return;
    const started = Date.now();
    const timer = setTimeout(() => dismiss.current(toast.id), Math.max(0, remaining.current));
    return () => { clearTimeout(timer); remaining.current = Math.max(0, remaining.current - (Date.now() - started)); };
  }, [paused, toast, duration, failure]);

  const run = async (action: ToastAction) => {
    setPending(action.label);
    try {
      await action.onSelect();
      dismiss.current(toast.id);
    } catch (error) {
      remaining.current = duration;
      setFailure(messageOf(error));
      setPending(undefined);
    }
  };

  const tone = failure !== undefined ? 'warning' : toast.tone ?? 'info';
  const onBlur = (event: FocusEvent<HTMLDivElement>) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setFocused(false); };
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Escape') return;
    event.stopPropagation();
    dismiss.current(toast.id);
  };

  return <div className="toast" data-tone={tone} onPointerEnter={() => setHovered(true)} onPointerLeave={() => setHovered(false)}
    onFocus={() => setFocused(true)} onBlur={onBlur} onKeyDown={onKeyDown}>
    <span className="toast-dot" aria-hidden="true"/>
    {/* Keyed on the tone so a refusal mounts a fresh alert, which every screen reader announces; a role swapped in place is not. */}
    <span key={tone} className="toast-text" role={tone === 'warning' ? 'alert' : 'status'} aria-live={tone === 'warning' ? 'assertive' : 'polite'} aria-atomic="true">
      {failure !== undefined ? failure : <Sentence text={toast.text} subject={toast.subject}/>}
    </span>
    {toast.actions.map(action => <Button key={action.label} variant={action.primary ? 'quiet' : 'ghost'} className="toast-action"
      loading={pending === action.label} disabled={pending !== undefined} onClick={() => void run(action)}>{action.label}</Button>)}
  </div>;
}

/** Where toasts are drawn: one fixed stack at the bottom centre of the window, newest at the bottom. It is always on the page
 * (empty, it draws nothing and takes no pointer) so the live regions inside it are announced as they arrive. */
export function ToastRegion({ toasts, onDismiss }: { toasts: readonly ToastModel[]; onDismiss: (id: string) => void }) {
  return <section className="toast-region" aria-label="Notifications">
    {toasts.map(toast => <Toast key={toast.id} toast={toast} onDismiss={onDismiss}/>)}
  </section>;
}

export type ShowToast = Omit<ToastModel, 'id'> & { id?: string };

/** The toast list for one window. `show` returns the id (its own, or a fresh one) so the caller can dismiss it later;
 * showing an id that is already up replaces that toast and moves it to the bottom. */
export function useToasts(limit = TOAST_LIMIT) {
  const [toasts, setToasts] = useState<ToastModel[]>([]);
  const counter = useRef(0);
  const show = useCallback((model: ShowToast): string => {
    const id = model.id ?? `toast-${++counter.current}`;
    setToasts(previous => [...previous.filter(toast => toast.id !== id), { ...model, id }].slice(-limit));
    return id;
  }, [limit]);
  const dismiss = useCallback((id: string) => setToasts(previous => previous.filter(toast => toast.id !== id)), []);
  return { toasts, show, dismiss };
}
