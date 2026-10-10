// Navigation changes views, never sessions. The workspace already saves each place's tabs under its own key;
// leaving that document intact is the stash that makes reopening restore drafts, groups and running conversations.
import type { NativeControls, PlaceKey } from '../../design/nativeControls.ts';
import type { PlacesClient } from './client.ts';
import type { PlacesGraph, PlaceView } from './wire.ts';

export type NavigationDeps = {
  client: Pick<PlacesClient, 'graph' | 'visit' | 'railOp'>;
  windows: Pick<NativeControls, 'desktop' | 'openPlaceWindow'>;
  graph: () => PlacesGraph | undefined;
  current: () => Exclude<PlaceKey, 'root'>;
  setPlace: (key: Exclude<PlaceKey, 'root'>) => void;
  /** Arrival focuses Home after the destination workspace mounts, even on a repeated visit. */
  focusHome: () => void;
  openRoot: () => void;
  setClosed: (key: string, closed: boolean) => void;
  refresh: () => Promise<void>;
  warn: (failure: unknown) => void;
};

export function createPlaceNavigation(deps: NavigationDeps) {
  let arrival = 0;
  const checked = async (key: string): Promise<PlaceView | undefined> => {
    if (key === 'now' || key === 'root') return;
    if (!/^pl_[0-9a-f]{16}$/.test(key)) throw new Error('That is not a place codeaf knows.');
    // A just-created place can precede the shell's refresh, so an absent target needs a canonical read.
    const target = deps.graph()?.places.find(place => place.id === key)
      ?? (await deps.client.graph({ archived: true })).places.find(place => place.id === key);
    if (!target) throw new Error('That place no longer exists.');
    if (target.archived) throw new Error(`“${target.name}” is archived. Restore it from All places first.`);
    return target;
  };
  const visit = async (key: string) => {
    if (key === 'now' || key === 'root') return;
    // Going there is a touch: POST /places/{id}/visit stamps lastOpenedAt, which is the date the
    // untouched-place suggestion reads. It moves no revision and has no receipt. The window has already
    // moved, so a refusal is reported and does not send it back.
    try { await deps.client.visit(key); } catch (failure) { deps.warn(failure); }
    // The rail's Open list is a separate soft write, with the same rule: keep the destination if it fails.
    try { await deps.client.railOp({ op: 'visit', place: key }); } catch (failure) { deps.warn(failure); }
    await deps.refresh();
  };
  const goTo = async (key: string, options: { newWindow?: boolean } = {}): Promise<void> => {
    if (options.newWindow && deps.windows.desktop) { await openInNewWindow(key); return; }
    const turn = ++arrival;
    await checked(key);
    // A slow canonical lookup must not send the window back after a newer navigation completed.
    if (turn !== arrival) return;
    if (key === 'root') { deps.openRoot(); return; }
    deps.setPlace(key as Exclude<PlaceKey, 'root'>);
    deps.setClosed(key, false);
    deps.focusHome();
    await visit(key);
  };
  const openInNewWindow = async (key: string): Promise<void> => {
    if (!deps.windows.desktop) { await goTo(key); return; }
    await checked(key);
    await deps.windows.openPlaceWindow(key as PlaceKey);
    await visit(key);
  };
  const closePlace = async (key: string): Promise<void> => {
    if (key === 'now' || key === 'root') return;
    await checked(key);
    // Only mark a place closed after the engine accepts it; a refused close keeps its tabs and rail row visible.
    await deps.client.railOp({ op: 'close', place: key });
    deps.setClosed(key, true);
    if (deps.current() === key) {
      ++arrival;
      deps.setPlace('now');
      deps.focusHome();
    }
    await deps.refresh();
  };
  const closeAllOthers = async (except?: string): Promise<void> => {
    // Snapshot the set before writes refresh the graph. Pinned places are outside the Open header's bulk action.
    const keys = deps.graph()?.rail.open.filter(place => place.id !== except && !place.pinned).map(place => place.id) ?? [];
    for (const key of keys) await closePlace(key);
  };
  const up = async (): Promise<void> => {
    const current = deps.current();
    const parent = deps.graph()?.places.find(place => place.id === current)?.parents[0];
    await goTo(parent ?? 'root');
  };
  return { goTo, closePlace, closeAllOthers, up, openInNewWindow };
}
