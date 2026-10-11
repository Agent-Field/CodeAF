// Navigation changes views, never sessions, and places are libraries (owner decision 2026-10-11): going to a place
// opens its Home as a tab in the strip that is already showing, or focuses the one that is open. No tab is ever
// closed, moved or replaced by it, so drafts, groups and running conversations stay where they were.
import type { NativeControls, PlaceKey } from '../../design/nativeControls.ts';
import type { PlacesClient } from './client.ts';
import type { PlacesGraph, PlaceView } from './wire.ts';

export type NavigationDeps = {
  client: Pick<PlacesClient, 'graph' | 'visit' | 'railOp'>;
  windows: Pick<NativeControls, 'desktop' | 'openPlaceWindow'>;
  graph: () => PlacesGraph | undefined;
  current: () => Exclude<PlaceKey, 'root'>;
  /** Asks the strip for the place's Home as a tab, or for the one already open to come forward. */
  openHome: (key: string, title: string) => void;
  /** The place Focus is filtering the strip to, if any. */
  focused: () => string | undefined;
  setFocus: (key: string | undefined) => void;
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
    const target = await checked(key);
    // A slow canonical lookup must not send the window back after a newer navigation completed.
    if (turn !== arrival) return;
    // All places is not in any one place's Focus, so asking for it ends the Focus rather than hiding what was asked for.
    if (key === 'root') { deps.setFocus(undefined); deps.openRoot(); return; }
    // Now shows every open tab, unfiltered.
    if (key === 'now' || !target) { deps.setFocus(undefined); return; }
    // The Home just asked for is not in another place's Focus, and hiding it would look like nothing happened.
    if (deps.focused() !== key) deps.setFocus(undefined);
    deps.setClosed(key, false);
    deps.openHome(key, target.name);
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
    // Closing a place takes it off the rail; its tabs stay open, but a Focus on it has nothing left to name.
    if (deps.focused() === key) deps.setFocus(undefined);
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
