// One hover preview is open at a time PER WORKSPACE. The store holds which tab it belongs to and whether the card
// arrived by swapping from a neighbour (3a: moving across tabs swaps instantly, with no delay and no animation).
// It is made once by each Workspace mount (see createPreviewStore), never module-global: a second mount, a remount
// or a test must not inherit another's warm state, open card or pending close timer.
type Shown = { id: string; swapped: boolean } | null;

export type PreviewStore = {
  /** True while a card is open: the next tab the pointer reaches shows its card at once. */
  isWarm: () => boolean;
  open: (id: string) => void;
  close: (id?: string) => void;
  /** Leaving a tab or the card closes after a short grace, so the pointer can cross the gap or reach the card. */
  scheduleClose: (id: string) => void;
  keepOpen: () => void;
  /** Cancels any pending close; called when the owning workspace unmounts. */
  dispose: () => void;
  subscribe: (listener: () => void) => () => void;
  get: () => Shown;
};

/** `closeDelay` is the grace in ms (closeDelay); passed in so this stays pure for node --test. */
export function createPreviewStore(closeDelay: number): PreviewStore {
  let shown: Shown = null;
  let closeTimer: ReturnType<typeof setTimeout> | undefined;
  const listeners = new Set<() => void>();
  const emit = () => listeners.forEach(listener => listener());
  const close = (id?: string) => {
    clearTimeout(closeTimer);
    if (!shown || (id !== undefined && shown.id !== id)) return;
    shown = null;
    emit();
  };
  return {
    isWarm: () => shown !== null,
    open: id => {
      clearTimeout(closeTimer);
      if (shown?.id === id) return;
      shown = { id, swapped: shown !== null };
      emit();
    },
    close,
    scheduleClose: id => { clearTimeout(closeTimer); closeTimer = setTimeout(() => close(id), closeDelay); },
    keepOpen: () => clearTimeout(closeTimer),
    dispose: () => { clearTimeout(closeTimer); shown = null; },
    subscribe: listener => { listeners.add(listener); return () => { listeners.delete(listener); }; },
    get: () => shown,
  };
}
