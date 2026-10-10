import { useCallback, useEffect, useId, useRef, useState, useSyncExternalStore, type FocusEvent, type KeyboardEvent } from 'react';
import { createPortal } from 'react-dom';
import design from '../../design/tokens.json';
import { toasts as windowToasts, TOAST_LIMIT, type Toast as ChannelToast, type ToastAction as ChannelAction, type ToastPart, type ToastTone, type Toasts } from '../../design/toasts';
import { Button } from './Button';
import { Icon } from './Icon';
import { toastQueueFor, type ToastOptions } from './toastQueue';
import './toast.css';

/** The design's "6s" (Components "Overlays"); Delete passes its own ten seconds (Interactions "Delete"). */
export const TOAST_DURATION_MS = design.interaction.toastDuration;
export { TOAST_LIMIT };

export type ToastAction = ChannelAction;

/** The Places shell's sentence shape: words with one subject, spelled where the sentence names it. */
export type ToastModel = {
  id: string;
  text: string;
  subject?: string;
  tone?: 'info' | 'warning';
  actions: readonly ToastAction[];
  undo?: () => void | Promise<void>;
  durationMs?: number;
};
export type ShowToast = Omit<ToastModel, 'id' | 'actions'> & { id?: string; actions?: readonly ToastAction[] };

type ViewToast = Pick<ChannelToast, 'content' | 'lead'> & { message: readonly ToastPart[]; tone?: ToastTone; actions?: readonly ToastAction[]; undo?: unknown };
type ViewProps = {
  toast: ViewToast;
  onAction?: (index: number) => void | Promise<void>;
  onUndo?: () => void | Promise<void>;
  onHold?: (held: boolean, source: 'hover' | 'focus') => void;
  onDismiss?: () => void;
  /** A refusal replaces the sentence; its leading dot supplies the amber state. */
  failure?: string;
  /** The label of the button whose work is still running. */
  pending?: string;
};

const messageOf = (error: unknown) => (error instanceof Error && error.message ? error.message : 'That did not work.');
const part = (item: ToastPart, index: number) => (typeof item === 'string' ? item : <strong key={index} className="toast-strong">{item.strong}</strong>);

/** The sentence with its subject in medium weight. The subject is only bolded where the sentence spells it literally;
 * otherwise it leads the sentence, so a subject is never silently dropped. */
export function sentenceParts(text: string, subject?: string): ToastPart[] {
  if (!subject) return [text];
  const at = text.indexOf(subject);
  if (at < 0) return [{ strong: subject }, ` ${text}`];
  return [text.slice(0, at), { strong: subject }, text.slice(at + subject.length)].filter(item => item !== '');
}

/** The toast itself, with no timer and no placement: what the region shows, and what the Design system page specimens. */
export function ToastView({ toast, onAction, onUndo, onHold, onDismiss, failure, pending }: ViewProps) {
  const tone = failure !== undefined ? 'warning' : toast.tone ?? 'info';
  const onBlur = (event: FocusEvent<HTMLDivElement>) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) onHold?.(false, 'focus'); };
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Escape' || !onDismiss) return;
    event.preventDefault();
    event.stopPropagation();
    onDismiss();
  };
  return (
    <div className="toast" data-tone={tone} data-native-cover="" onPointerEnter={() => onHold?.(true, 'hover')} onPointerLeave={() => onHold?.(false, 'hover')} onFocus={() => onHold?.(true, 'focus')} onBlur={onBlur} onKeyDown={onKeyDown}>
      {toast.lead && toast.lead !== 'dot' ? <span className="toast-icon" aria-hidden="true"><Icon name={toast.lead} size="xs"/></span> : <span className="toast-dot" data-tone={tone} aria-hidden="true"/>}
      {/* A refusal replaces the sentence while the persistent host announces it politely. */}
      <span key={tone} className="toast-text">
        {failure !== undefined ? failure : toast.content ?? toast.message.map(part)}
      </span>
      {toast.actions?.map((action, index) => <Button key={action.label} variant={action.primary ? 'quiet' : 'ghost'} className="toast-action"
        loading={pending === action.label} disabled={pending !== undefined} onClick={() => void onAction?.(index)}>{action.label}</Button>)}
      {toast.undo !== undefined && <Button variant="quiet" className="toast-action" loading={pending === 'Undo'} disabled={pending !== undefined} onClick={() => void onUndo?.()}>Undo</Button>}
    </div>
  );
}

/** One toast in the region. It leaves by itself after its duration, but never while the pointer is over it, focus is
 * inside it or its chosen work is running, so a person reaching for Undo is never raced. A refused choice keeps the toast
 * and puts the refusal in its place, amber, for a fresh full duration. Escape inside it dismisses it. */
function RegionToast({ toast, channel }: { toast: ChannelToast; channel: Toasts }) {
  const queue = toastQueueFor(channel);
  const [pending, setPending] = useState<string>();
  const [failure, setFailure] = useState<string>();
  const run = async (label: string, choose: () => Promise<void>) => {
    queue.hold(toast.id, 'action', true);
    setPending(label);
    try { await choose(); } catch (error) {
      queue.restart(toast.id);
      setFailure(messageOf(error));
      setPending(undefined);
      queue.hold(toast.id, 'action', false);
    }
  };
  return <ToastView toast={toast} failure={failure} pending={pending} onHold={(held, source) => queue.hold(toast.id, source, held)} onDismiss={() => channel.dismiss(toast.id)}
    onAction={index => run(toast.actions?.[index]?.label ?? '', () => channel.act(toast.id, index))}
    onUndo={() => run('Undo', () => channel.undo(toast.id))}/>;
}

/**
 * The window's one toast region (design 3l step 3, Components "Overlays"): one toast, bottom centre. A newer toast
 * replaces the one on screen. The region is always on the page (empty, it draws nothing and takes no pointer) so the
 * live region announces the sentence as it arrives, and it is a status, not a dialog, so it never takes focus. Mount
 * it ONCE per window; a specimen passes its own channel.
 */
export function ToastHost({ channel = windowToasts }: { channel?: Toasts }) {
  toastQueueFor(channel);
  const list = useSyncExternalStore(channel.subscribe, channel.getToasts);
  const [modal, setModal] = useState(() => document.querySelector<HTMLElement>('dialog:modal'));
  useEffect(() => {
    // Dialog owners can close without changing the toast channel or its parent. Follow that lifecycle independently.
    const update = () => setModal(document.querySelector<HTMLElement>('dialog:modal'));
    const containsDialog = (node: Node) => node instanceof Element && (node.matches('dialog') || node.querySelector('dialog') !== null);
    const observer = new MutationObserver(records => {
      if (records.some(record => record.type === 'attributes' ||
        [...record.addedNodes, ...record.removedNodes].some(containsDialog))) update();
    });
    observer.observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ['open'] });
    update();
    return () => observer.disconnect();
  }, []);
  const [shown, setShown] = useState(list);
  const exiting = list.length === 0 && shown.length > 0;
  useEffect(() => { if (list.length) setShown(list); }, [list]);
  const region = <section className="toast-region" aria-label="Notifications" role="status" aria-live="polite" aria-atomic="true"
    data-exiting={exiting || undefined} onTransitionEnd={event => {
      if (event.target === event.currentTarget && exiting) setShown([]);
    }}>
    {(list.length ? list : shown).map(toast => <RegionToast key={toast.id} toast={toast} channel={channel}/>)}
  </section>;
  useEffect(() => {
    if (!exiting) return;
    // Reduced motion has no transition event; the fallback also covers a host detached during an exit.
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const timer = window.setTimeout(() => setShown([]), reduced ? 0 : Number.parseFloat(design.foundation['dur-base']));
    return () => window.clearTimeout(timer);
  }, [exiting]);
  return createPortal(region, modal ?? document.body);
}

/** The Places shell's door onto the window channel: a sentence and its subject in, a toast in the one region out.
 * Showing an id already up replaces that toast in its place. `show` returns the id so the caller can dismiss it. */
export function useToasts(channel: Toasts = windowToasts) {
  const ids = useRef(new Map<string, number>());
  const scope = useId();
  const counter = useRef(0);
  const show = useCallback((model: ShowToast): string => {
    const id = model.id ?? `toast-${++counter.current}`;
    ids.current.set(id, channel.show({ key: `${scope}${id}`, message: sentenceParts(model.text, model.subject), tone: model.tone, actions: model.actions, undo: model.undo, durationMs: model.durationMs }));
    return id;
  }, [channel, scope]);
  const dismiss = useCallback((id: string) => { const at = ids.current.get(id); if (at !== undefined) channel.dismiss(at); ids.current.delete(id); }, [channel]);
  return { show, dismiss };
}

/** Compatibility keeps existing callers on the single primitive while the shell integrator adopts its name. */
export const ToastRegion = ToastHost;
/** The standalone card uses the same drawing as the host, for specimens and reusable overlay content. */
export function Toast(options: ToastOptions) {
  return <ToastView toast={{ message: [], content: options.message, lead: options.lead,
    actions: options.actions?.map(action => ({ label: action.label, primary: action.kind === 'field', onSelect: action.onAction })),
    undo: options.undo }} onAction={index => options.actions?.[index]?.onAction()} onUndo={options.undo}/>;
}
export { toast } from './toastQueue';
