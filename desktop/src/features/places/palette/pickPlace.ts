// Pick mode for the place palette (Places 6f "Add to a place…", 8f "Add to another place…", Interactions History
// "Add to place"): a caller asks "which place?" and awaits the id. The palette is one dialog, so the question lives in a
// tiny module-level slot the mounted PlacePickerHost watches; the caller never touches React. React-free, so a node
// test pins every transition.

export type PickRequest = {
  /** What the field asks, e.g. "Add to a place". Shown as the palette's placeholder and its accessible name. */
  title: string;
  /** Place ids that are not offered: the places the thing is already in, or the place itself and its descendants. */
  exclude: ReadonlySet<string>;
  /** Makes a place from the typed words and returns its id; absent means the Create row is not offered. */
  create?: (name: string) => Promise<string | undefined>;
};

export type PickOptions = Omit<PickRequest, 'exclude'> & { exclude?: Iterable<string> };

type Pending = { request: PickRequest; settle: (placeId: string | undefined) => void };

let pending: Pending | undefined;
const listeners = new Set<() => void>();
const notify = () => listeners.forEach(listener => listener());

/**
 * Opens the palette in pick mode. Resolves the chosen place's id, or undefined when the person dismissed it (Esc, a click
 * outside) or another pick replaced this one. It never writes: what to do with the id is the caller's.
 */
export function pickPlace(options: PickOptions): Promise<string | undefined> {
  // One palette, one question: a newer ask means the older one is no longer on screen, so it ends as a dismissal.
  pending?.settle(undefined);
  return new Promise(resolve => {
    const entry: Pending = {
      request: { title: options.title, exclude: new Set(options.exclude ?? []), create: options.create },
      settle: placeId => {
        if (pending !== entry) return;
        pending = undefined;
        resolve(placeId);
        notify();
      },
    };
    pending = entry;
    notify();
  });
}

/** The question on screen right now, if any. Stable between changes, so it suits useSyncExternalStore. */
export function currentPick(): PickRequest | undefined {
  return pending?.request;
}

/** Answers the question on screen: a place id for a choice, undefined for a dismissal. A late answer is ignored. */
export function settlePick(request: PickRequest, placeId: string | undefined): void {
  if (pending?.request === request) pending.settle(placeId);
}

export function subscribePick(listener: () => void): () => void {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}
