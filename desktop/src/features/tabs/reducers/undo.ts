// Reducer slice: taking back a structural change (Interactions, Undo: "⌘Z undoes the last structural action … up to
// 20 steps, for each window"). The inverse is an ordinary WorkspaceAction, so it is applied, saved and replayed over
// another window's tab set exactly like the change it takes back.
//
// AN UNDO NEVER ADOPTS AN OLD TAB SET. It carries two descriptions of the strip's STRUCTURE (which tabs, in what
// order, pinned or not, in which group, holding which panes), never of content: `expect` is how the change left the
// tabs it touched, `restore` is how they were before it. It applies only while every tab the change touched is still
// exactly as the change left it and the tabs it shares with that moment still stand in the same order; otherwise it
// is refused and changes nothing, so it can never take back what another window (or a later step here) did. Content
// always comes from the tab set as it is now: a draft typed, a title the engine sent or a route walked since the change
// is kept. Tabs another window opened since are kept where they stand; tabs it closed since stay closed.
import { arrange, focusedPane, makeSplit, normalize, paneOf, tabHolding, tabOf, withSplitTitle } from '../helpers.ts';
import type { Pane, SplitLayout, SplitRatios, Tab, TabGroup, TitleSource, WorkspaceState } from '../types.ts';

/** The structure of one strip tab: what Undo compares and rebuilds. A plain tab's `panes` is its own id. */
export type TabShape = {
  id: string; pinned: boolean; groupId?: string; panes: string[];
  /** A split tab's own fields, which no pane carries. */
  split?: { layout: SplitLayout; ratios?: SplitRatios; kind: Tab['kind']; title: string; titleSource?: TitleSource };
};
export type Structure = { order: string[]; tabs: TabShape[] };

export type UndoAction = { type: 'undo-structure'; expect: Structure; restore: Structure & { groups: TabGroup[] } };

export function shapeOf(tab: Tab): TabShape {
  const base = { id: tab.id, pinned: tab.pinned, ...(tab.groupId ? { groupId: tab.groupId } : {}) };
  if (!tab.split) return { ...base, panes: [tab.id] };
  const { layout, ratios, panes } = tab.split;
  return { ...base, panes: panes.map(pane => pane.id), split: { layout, ...(ratios ? { ratios } : {}), kind: tab.kind, title: tab.title, ...(tab.titleSource ? { titleSource: tab.titleSource } : {}) } };
}

/** Two shapes are the same structure: titles and divider ratios are content, not structure. */
export const sameShape = (a: TabShape, b: TabShape) =>
  a.id === b.id && a.pinned === b.pinned && a.groupId === b.groupId && a.panes.join('\n') === b.panes.join('\n') && a.split?.layout === b.split?.layout;

/**
 * The inverse of the change that turned `before` into `after`, or undefined when the change moved nothing structural
 * (a draft, a title, a collapse). Only the tabs whose structure changed are described; the order is the whole strip's.
 */
export function inverseOf(before: WorkspaceState, after: WorkspaceState): UndoAction | undefined {
  const was = new Map(before.tabs.map(tab => [tab.id, shapeOf(tab)]));
  const now = new Map(after.tabs.map(tab => [tab.id, shapeOf(tab)]));
  const changed = (id: string) => { const a = was.get(id); const b = now.get(id); return !a || !b || !sameShape(a, b); };
  const restoreTabs = [...was.values()].filter(shape => changed(shape.id));
  const expectTabs = [...now.values()].filter(shape => changed(shape.id));
  const order = { before: before.tabs.map(tab => tab.id), after: after.tabs.map(tab => tab.id) };
  if (!restoreTabs.length && !expectTabs.length && order.before.join('\n') === order.after.join('\n')) return undefined;
  const used = new Set(restoreTabs.flatMap(shape => (shape.groupId ? [shape.groupId] : [])));
  return {
    type: 'undo-structure',
    expect: { order: order.after, tabs: expectTabs },
    restore: { order: order.before, tabs: restoreTabs, groups: before.groups.filter(group => used.has(group.id)) },
  };
}

/** Every pane the tab set can give content for, open or closed, by pane id. */
type Source = { pane: Pane; holder?: string; closedId?: string };
function panesById(state: WorkspaceState): Map<string, Source> {
  const found = new Map<string, Source>();
  const add = (tab: Tab, from: Omit<Source, 'pane'>) => (tab.split ? tab.split.panes : [paneOf(tab)]).forEach(pane => found.has(pane.id) || found.set(pane.id, { pane, ...from }));
  state.tabs.forEach(tab => add(tab, { holder: tab.id }));
  state.closed.forEach(tab => add(tab, { closedId: tab.id }));
  return found;
}

/** Applies an Undo, or returns `state` itself when the tabs it would touch have changed since (a refusal). */
export function applyUndo(state: WorkspaceState, { expect, restore }: UndoAction): WorkspaceState {
  // 1. Every tab the change made or changed is still exactly as the change left it.
  for (const shape of expect.tabs) {
    const tab = state.tabs.find(t => t.id === shape.id);
    if (!tab || !sameShape(shapeOf(tab), shape)) return state;
  }
  // 2. Nothing the change left alone was moved since: the tabs it shares with that moment stand in the same order.
  const ids = state.tabs.map(tab => tab.id);
  const then = new Set(expect.order);
  const shared = ids.filter(id => then.has(id));
  const present = new Set(ids);
  if (shared.join('\n') !== expect.order.filter(id => present.has(id)).join('\n')) return state;
  // 3. Rebuild the tabs as they were, from the content they hold now.
  const content = panesById(state);
  const leaving = new Set(expect.tabs.map(shape => shape.id));
  const activePane = (() => { const tab = state.tabs.find(t => t.id === state.activeId); return tab ? focusedPane(tab).id : state.activeId; })();
  const usedClosed = new Set<string>();
  const rebuilt: Tab[] = [];
  for (const shape of restore.tabs) {
    const panes = shape.panes.map(id => content.get(id));
    // A pane the change took away must still be free to come back: in a tab the change made, or closed. A pane that
    // another tab holds now (it was reopened, or merged elsewhere, since) is never taken from that tab.
    if (panes.some(found => !found || (found.holder !== undefined && !leaving.has(found.holder)))) return state;
    panes.forEach(found => found!.closedId && usedClosed.add(found!.closedId));
    const own = { pinned: shape.pinned, groupId: shape.groupId };
    if (!shape.split) { rebuilt.push(tabOf(panes[0]!.pane, own)); continue; }
    const list = panes.map(found => found!.pane);
    const was = state.tabs.find(t => t.id === shape.id)?.split;
    const focusId = was ? was.panes[was.focus]?.id : list.some(pane => pane.id === activePane) ? activePane : undefined;
    const focus = Math.max(0, list.findIndex(pane => pane.id === focusId));
    const { kind, title, titleSource, layout, ratios } = shape.split;
    rebuilt.push(withSplitTitle({ id: shape.id, kind, title, titleSource, draft: '', ...own, split: makeSplit(list, focus, layout, ratios) }));
  }
  const kept = state.tabs.filter(tab => !leaving.has(tab.id));
  const byId = new Map<string, Tab>([...kept, ...rebuilt].map(tab => [tab.id, tab]));
  const tabs: Tab[] = [];
  const placed = new Set<string>();
  for (const id of restore.order) { const tab = byId.get(id); if (tab && !placed.has(id)) { tabs.push(tab); placed.add(id); } }
  // Tabs that appeared since (another window's) keep their place: just before the tab they now precede, or last
  // when nothing follows them (a tab opened at the end stays at the end).
  [...state.tabs].reverse().forEach(tab => {
    if (placed.has(tab.id) || leaving.has(tab.id)) return;
    const index = state.tabs.indexOf(tab);
    let at = tabs.length;
    for (let ahead = index + 1; ahead < state.tabs.length; ahead++) { const anchor = tabs.findIndex(t => t.id === state.tabs[ahead].id); if (anchor >= 0) { at = anchor; break; } }
    tabs.splice(at, 0, tab);
    placed.add(tab.id);
  });
  const groups = [...state.groups, ...restore.groups.filter(group => !state.groups.some(g => g.id === group.id)).map(group => ({ ...group, collapsed: false }))];
  const next: WorkspaceState = { ...state, tabs, groups, closed: state.closed.filter(tab => !usedClosed.has(tab.id)) };
  const activeId = tabs.some(tab => tab.id === state.activeId) ? state.activeId : (tabHolding(next, activePane) ?? tabs[0]).id;
  return normalize({ ...next, activeId });
}

export function reduceUndo(state: WorkspaceState, action: { type: string }): WorkspaceState | undefined {
  return action.type === 'undo-structure' ? applyUndo(state, action as UndoAction) : undefined;
}

/**
 * Snapshot undo for one window (IX Flows "Undo", S-3l-15). `withUndo(reducer)` remembers the shared strip
 * from before each structural action and `{ type: 'undo' }` puts that strip back. The stack lives on the state
 * (`undo`), so two windows that share one wrapped reducer still have two stacks, and it is not part of the
 * shared document a reload would read. The closing toast's Undo dispatches `undoAction`, the same action.
 */
export const undoStackLimit = 20;

/** Actions that push a snapshot. Drafts, titles, views and selection do not. */
const structuralUndoTypes = new Set([
  'close', 'close-others', 'close-right', 'close-group',
  'reorder', 'move-group', 'move-group-block',
  'group', 'ungroup', 'pin',
  'split-merge', 'split-unmerge', 'split-close-pane',
]);

export const isStructuralUndoAction = (type: string) => structuralUndoTypes.has(type);

/** The closing toast's Undo and the window's undo are this action. */
export const undoAction = { type: 'undo' } as const;

/** Drops this window's stack. Closing the window does the same by discarding the state that holds it. */
export const windowCloseAction = { type: 'window-close' } as const;

export type UndoStackAction = typeof undoAction | typeof windowCloseAction;

/** The shared strip a step remembers. Drafts and titles ride along so a tab that comes back still has them. */
export type SharedSnapshot = Pick<WorkspaceState, 'tabs' | 'groups' | 'closed' | 'nextNumber'>;

export type UndoStackState = { undo?: SharedSnapshot[] };

const snapshotOf = (state: WorkspaceState): SharedSnapshot =>
  structuredClone({ tabs: state.tabs, groups: state.groups, closed: state.closed, nextNumber: state.nextNumber });

/** Order, pin, group, split membership and who is closed. Content is not part of "did this step change the strip". */
function structuralKey(snapshot: SharedSnapshot): string {
  const tabs = snapshot.tabs.map(tab => {
    const panes = tab.split ? tab.split.panes.map(pane => pane.id).join(',') : '';
    return [tab.id, tab.pinned ? '1' : '0', tab.groupId ?? '', tab.split?.layout ?? '', panes].join(':');
  }).join('|');
  return `${tabs}#${snapshot.groups.map(group => group.id).join(',')}#${snapshot.closed.map(tab => tab.id).join(',')}`;
}

const paneIds = (tab: Tab): string[] => (tab.split ? tab.split.panes.map(pane => pane.id) : [tab.id]);

/** Open panes, so a draft typed since the step can be laid back over the restored strip. */
function openPanes(state: WorkspaceState): Map<string, Pane> {
  const found = new Map<string, Pane>();
  const add = (pane: Pane) => { if (!found.has(pane.id)) found.set(pane.id, pane); };
  for (const tab of state.tabs) {
    if (tab.split) tab.split.panes.forEach(add);
    else add(tab);
  }
  return found;
}

/** A pane's words and route, without the strip fields a snapshot is allowed to change. */
function asPane(value: Pane): Pane {
  const { pinned: _pinned, groupId: _group, split: _split, stood: _stood, ...pane } = value as Pane & { pinned?: boolean; groupId?: string; split?: unknown; stood?: unknown };
  return pane;
}

/**
 * Tabs opened since the step stay where they stand: just before the tab they now precede, or at the end when
 * nothing they precede was part of the step. A pane that belongs to a restored split is not a new tab.
 */
function insertSince(restored: Tab[], current: WorkspaceState): Tab[] {
  const tabs = [...restored];
  const placed = new Set(tabs.map(tab => tab.id));
  const known = new Set(tabs.flatMap(paneIds));
  const currentTabs = current.tabs;
  [...currentTabs].reverse().forEach(tab => {
    if (placed.has(tab.id) || paneIds(tab).some(id => known.has(id))) return;
    const index = currentTabs.indexOf(tab);
    let at = tabs.length;
    for (let ahead = index + 1; ahead < currentTabs.length; ahead++) {
      const anchor = tabs.findIndex(item => item.id === currentTabs[ahead].id);
      if (anchor >= 0) { at = anchor; break; }
    }
    tabs.splice(at, 0, tab);
    placed.add(tab.id);
    for (const id of paneIds(tab)) known.add(id);
  });
  return tabs;
}

function restoreTab(snap: Tab, livePanes: Map<string, Pane>, current: WorkspaceState): Tab {
  if (!snap.split) {
    const live = livePanes.get(snap.id);
    if (!live) return snap;
    return { ...asPane(live), pinned: snap.pinned, ...(snap.groupId ? { groupId: snap.groupId } : {}) };
  }
  const panes = snap.split.panes.map(pane => { const live = livePanes.get(pane.id); return live ? asPane(live) : pane; });
  const liveTab = current.tabs.find(tab => tab.id === snap.id);
  const samePanes = !!liveTab?.split && liveTab.split.panes.map(pane => pane.id).join() === panes.map(pane => pane.id).join();
  const focus = samePanes ? liveTab!.split!.focus : snap.split.focus;
  const ratios = samePanes && liveTab!.split!.layout === snap.split.layout ? liveTab!.split!.ratios : snap.split.ratios;
  const shell: Tab = {
    ...snap,
    pinned: snap.pinned,
    ...(snap.groupId ? { groupId: snap.groupId } : { groupId: undefined }),
    ...(liveTab ? { draft: liveTab.draft, title: liveTab.title, titleSource: liveTab.titleSource } : {}),
    split: { ...snap.split, panes, focus, ...(ratios ? { ratios } : {}) },
  };
  return withSplitTitle(shell);
}

/** Puts the snapshot's strip back. Panes still open keep the draft, title and view they have now. */
function restoreSnapshot<S extends WorkspaceState>(current: S, snapshot: SharedSnapshot, undo: SharedSnapshot[]): S {
  const live = openPanes(current);
  const restored = snapshot.tabs.map(tab => restoreTab(tab, live, current));
  const tabs = insertSince(restored, current);
  const open = new Set(tabs.flatMap(paneIds));
  const have = new Map(current.groups.map(group => [group.id, group]));
  const groups = snapshot.groups.map(group => {
    const now = have.get(group.id);
    // A rename or a collapse since the step is not a step, so it stays.
    return now ? { ...group, title: now.title, collapsed: now.collapsed } : { ...group };
  });
  for (const tab of tabs) {
    if (tab.groupId && !groups.some(group => group.id === tab.groupId)) {
      const group = have.get(tab.groupId);
      if (group) groups.push({ ...group });
    }
  }
  const closed = snapshot.closed.filter(tab => !paneIds(tab).some(id => open.has(id)));
  const activeId = tabs.some(tab => tab.id === current.activeId) ? current.activeId : tabs[0]?.id ?? current.activeId;
  // arrange and normalize return the base workspace type; the stack field is already on this object.
  return normalize(arrange({ ...current, tabs, groups, closed, nextNumber: Math.max(snapshot.nextNumber, current.nextNumber), activeId, undo })) as S;
}

export function withUndo<S extends WorkspaceState, A extends { type: string }>(
  reducer: (state: S, action: A) => S,
): (state: S & UndoStackState, action: A | UndoStackAction) => S & UndoStackState {
  return (state, action) => {
    if (action.type === 'window-close') return state.undo?.length ? { ...state, undo: [] } : state;
    if (action.type === 'undo') {
      const stack = state.undo ?? [];
      if (!stack.length) return state;
      return restoreSnapshot(state, stack[stack.length - 1], stack.slice(0, -1));
    }
    // `undo` and `window-close` are handled above. What remains is an action the inner reducer knows.
    const inner = action as A;
    if (!structuralUndoTypes.has(inner.type)) return reducer(state, inner);
    const before = snapshotOf(state);
    const next = reducer(state, inner);
    if (next === state || structuralKey(before) === structuralKey(snapshotOf(next))) return next;
    const undo = [...(state.undo ?? []), before].slice(-undoStackLimit);
    return { ...next, undo };
  };
}
