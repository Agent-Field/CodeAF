// The one toast channel. A feature calls `toasts.show(...)`; the single <ToastRegion/> draws whatever is current.
// Kept free of React so node tests can drive it and so any lane can post without importing a component.
//
// UNDO IS A SLOT OF THE TOAST, NOT A BUTTON A FEATURE DRAWS. A toast that can be taken back carries `undo`, and the
// region draws it last, in the same place, with the same label, every time. A feature never spells "Undo" itself.

/** Words of a toast; `{ strong }` is the subject (a tab's name) and is drawn medium. */
export type ToastPart = string | { strong: string };
export type ToastAction = { label: string; onSelect: () => void };
export type ToastTone = 'info' | 'danger';

export type ToastSpec = {
  message: readonly ToastPart[];
  tone?: ToastTone;
  /** Buttons before Undo, e.g. "Stop it". Choosing one dismisses the toast. */
  actions?: readonly ToastAction[];
  /** Takes the change back. Present means the region draws Undo; choosing it dismisses the toast. */
  undo?: () => void;
  /** Runs once when the toast leaves without Undo being chosen (timeout, replacement, or an action), so a held undo can be released. */
  onSettled?: () => void;
};

export type Toast = ToastSpec & { id: number; tone: ToastTone };

export type Toasts = ReturnType<typeof createToasts>;

export function createToasts() {
  let current: Toast | null = null;
  let next = 0;
  const listeners = new Set<() => void>();
  const emit = () => listeners.forEach(listener => listener());
  const settle = (toast: Toast | null) => toast?.onSettled?.();

  return {
    getToast: () => current,
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    /** Shows a toast, replacing the one on screen (which settles). Returns its id. */
    show(spec: ToastSpec): number {
      const replaced = current;
      current = { ...spec, tone: spec.tone ?? 'info', id: ++next };
      settle(replaced);
      emit();
      return current.id;
    },
    /** Leaves without undoing. A stale id (already replaced or gone) is ignored. */
    dismiss(id: number) {
      if (current?.id !== id) return;
      const gone = current;
      current = null;
      settle(gone);
      emit();
    },
    /** Chooses Undo: runs the undo, then leaves. `onSettled` is not run, because the change was taken back. */
    undo(id: number) {
      if (current?.id !== id) return;
      const gone = current;
      current = null;
      emit();
      gone.undo?.();
    },
    /** Chooses one of the toast's own actions. */
    act(id: number, index: number) {
      if (current?.id !== id) return;
      const gone = current;
      current = null;
      emit();
      gone.actions?.[index]?.onSelect();
      settle(gone);
    },
  };
}

/** The window's single channel. */
export const toasts = createToasts();
