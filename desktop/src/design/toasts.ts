// The one toast channel. A feature calls `toasts.show(...)`; the single <ToastRegion/> draws whatever is current.
// Kept free of React so node tests can drive it and so any lane can post without importing a component.
//
// UNDO IS A SLOT OF THE TOAST, NOT A BUTTON A FEATURE DRAWS. A toast that can be taken back carries `undo`, and the
// region draws it last, in the same place, with the same label, every time. A feature never spells "Undo" itself.
//
// The menus lane and the Places shell each arrived with a toast of their own; they are ONE channel now, so a closed
// tab's "Stop it" and a moved place's Undo share one region instead of two drawn over each other.
// One toast is on screen (SH-OQ6). A newer one replaces the older, which settles, so a person never has two to dismiss.

/** Words of a toast; `{ strong }` is the subject (a tab's or a place's name) and is drawn medium. */
export type ToastPart = string | { strong: string };
/** An action may be asynchronous. While it runs the toast stays; if it is refused the toast keeps standing and says why. */
export type ToastAction = { label: string; onSelect: () => void | Promise<void>; primary?: boolean };
// `primary` draws the button on field, as the design draws "Restore all"; the rest are bare in ink-2.
/** Accent for news, amber for a refusal the person should read, danger for work that did not happen. */
export type ToastTone = 'info' | 'warning' | 'danger';

export type ToastSpec = {
  message: readonly ToastPart[];
  tone?: ToastTone;
  /** Buttons before Undo, e.g. "Stop it". Choosing one dismisses the toast once it has run. */
  actions?: readonly ToastAction[];
  /** Takes the change back. Present means the region draws Undo; choosing it dismisses the toast once it has run. */
  undo?: () => void | Promise<void>;
  /** Runs once when the toast leaves without Undo being chosen (timeout, eviction, replacement, or an action), so a held undo can be released. */
  onSettled?: () => void;
  /** Milliseconds before it leaves by itself (`interaction.toastDuration`, the design's 6s, when absent). Zero or Infinity: its owner dismisses it. */
  durationMs?: number;
  /** Showing a toast with the key of one already up replaces that one in its place. */
  key?: string;
};

export type Toast = ToastSpec & { id: number; tone: ToastTone };

/** One toast is drawn. Showing another replaces it and settles the one that left. */
export const TOAST_LIMIT = 1;

export type Toasts = ReturnType<typeof createToasts>;

const thenable = (value: unknown): value is PromiseLike<void> => typeof (value as PromiseLike<void> | undefined)?.then === 'function';

export function createToasts(limit = TOAST_LIMIT) {
  let list: readonly Toast[] = [];
  let next = 0;
  const listeners = new Set<() => void>();
  const emit = () => listeners.forEach(listener => listener());
  const find = (id: number) => list.find(toast => toast.id === id);
  // Removing is idempotent: an action that dismissed its own toast, or a stale id, changes nothing and emits nothing.
  const remove = (id: number): Toast | undefined => {
    const gone = find(id);
    if (!gone) return;
    list = list.filter(toast => toast !== gone);
    emit();
    return gone;
  };

  // Runs a choice. A synchronous one leaves at once; an asynchronous one keeps the toast until it settles, and a refusal
  // keeps it standing and rejects, so the region can put the refusal where the sentence was.
  const choose = (id: number, run: (() => void | Promise<void>) | undefined, settles: boolean): Promise<void> => {
    if (!find(id)) return Promise.resolve();
    const result = run?.();
    const finish = () => { const gone = remove(id); if (gone && settles) gone.onSettled?.(); };
    if (!thenable(result)) { finish(); return Promise.resolve(); }
    return Promise.resolve(result).then(finish);
  };

  return {
    getToasts: () => list,
    /** The newest toast, or null. */
    getToast: (): Toast | null => list[list.length - 1] ?? null,
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    /** Shows a toast at the bottom of the stack, or in the place of the one with the same key (which settles). Returns its id. */
    show(spec: ToastSpec): number {
      const toast: Toast = { ...spec, tone: spec.tone ?? 'info', id: ++next };
      const replaced = spec.key !== undefined ? list.find(other => other.key === spec.key) : undefined;
      let settled: Toast[] = [];
      if (replaced) list = list.map(other => (other === replaced ? toast : other));
      else {
        const grown = [...list, toast];
        settled = grown.slice(0, Math.max(0, grown.length - limit));
        list = grown.slice(-limit);
      }
      emit();
      if (replaced) replaced.onSettled?.();
      settled.forEach(gone => gone.onSettled?.());
      return toast.id;
    },
    /** Leaves without undoing. A stale id (already replaced or gone) is ignored. */
    dismiss(id: number) { remove(id)?.onSettled?.(); },
    /** Chooses Undo. `onSettled` is not run, because the change was taken back. */
    undo(id: number): Promise<void> { return choose(id, find(id)?.undo, false); },
    /** Chooses one of the toast's own actions, then settles it. */
    act(id: number, index: number): Promise<void> { return choose(id, find(id)?.actions?.[index]?.onSelect, true); },
  };
}

/** The window's single channel. */
export const toasts = createToasts();
