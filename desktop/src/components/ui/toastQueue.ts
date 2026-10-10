import type { ReactNode } from 'react';
import type { IconName } from './Icon';
import design from '../../design/tokens.json' with { type: 'json' };
import { createToasts, toasts, type Toasts } from '../../design/toasts.ts';

export type ToastOptions = {
  lead: 'dot' | IconName;
  message: ReactNode;
  actions?: readonly { label: string; kind: 'ghost' | 'field'; onAction: () => void | Promise<void> }[];
  durationMs?: number;
  undo?: () => void | Promise<void>;
};
export type ToastClock = {
  now: () => number;
  setTimeout: (run: () => void, ms: number) => unknown;
  clearTimeout: (timer: unknown) => void;
};
const systemClock: ToastClock = {
  now: () => performance.now(),
  setTimeout: (run, ms) => globalThis.setTimeout(run, ms),
  clearTimeout: timer => globalThis.clearTimeout(timer as ReturnType<typeof setTimeout>),
};

/** Timing belongs to the window channel, so expiry is independent of React renders and can use a deterministic clock. */
export function createToastQueue(channel: Toasts = createToasts(), clock: ToastClock = systemClock) {
  let current: number | undefined;
  let timer: unknown;
  let remaining = 0;
  let started = 0;
  const holds = new Set<string>();
  const stop = () => {
    if (timer === undefined) return;
    clock.clearTimeout(timer);
    timer = undefined;
    remaining = Math.max(0, remaining - (clock.now() - started));
  };
  const start = () => {
    if (current === undefined || holds.size || !Number.isFinite(remaining) || remaining < 0) return;
    started = clock.now();
    const id = current;
    timer = clock.setTimeout(() => { timer = undefined; channel.dismiss(id); }, remaining);
  };
  const sync = () => {
    const next = channel.getToast();
    if (next?.id === current) return;
    stop();
    holds.clear();
    current = next?.id;
    const duration = next?.durationMs ?? design.interaction.toastDuration;
    remaining = duration <= 0 ? Infinity : duration;
    start();
  };
  const unsubscribe = channel.subscribe(sync);
  sync();
  return {
    channel,
    show(options: ToastOptions) {
      return channel.show({ message: [], content: options.message, lead: options.lead,
        durationMs: options.durationMs, undo: options.undo,
        actions: options.actions?.map(action => ({ label: action.label, primary: action.kind === 'field', onSelect: action.onAction })) });
    },
    dismiss: channel.dismiss,
    /** Each source holds independently: leaving the card cannot release focus or an action still running. */
    hold(id: number, source: string, held: boolean) {
      if (current !== id) return;
      stop();
      if (held) holds.add(source); else holds.delete(source);
      start();
    },
    restart(id: number) {
      if (current !== id) return;
      stop();
      remaining = channel.getToast()?.durationMs ?? design.interaction.toastDuration;
      if (remaining <= 0) remaining = Infinity;
      start();
    },
    dispose() { stop(); unsubscribe(); },
  };
}
export type ToastQueue = ReturnType<typeof createToastQueue>;
const queues = new WeakMap<Toasts, ToastQueue>();
/** Existing feature callers and the primitive post to the same channel, never two competing hosts. */
export function toastQueueFor(channel: Toasts): ToastQueue {
  let queue = queues.get(channel);
  if (!queue) { queue = createToastQueue(channel); queues.set(channel, queue); }
  return queue;
}
export const toast = toastQueueFor(toasts);
