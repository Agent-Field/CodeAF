// The window's structural Undo stack (Interactions, Undo: "⌘Z undoes the last structural action (close, archive,
// move, delete, filing), up to 20 steps, for each window"). Pure: no React, no DOM, so node tests drive it.
//
// THE STACK HOLDS ONLY WHAT THIS WINDOW DID. It is fed by this window's own dispatches, never by a tab set another
// window wrote, so ⌘Z here can never take back a change made over there. Each step is an inverse made of ordinary
// actions (reducers/undo.ts, or Reopen for a close); a step whose tabs changed since is refused, never forced.
import { inverseOf } from '../reducers/undo.ts';
import { workspaceReducer, type WorkspaceAction, type WorkspaceState } from '../model.ts';

export const undoLimit = 20;

/** Changes ⌘Z takes back. Opening, typing, naming, selecting and collapsing are not structural and are never steps. */
const structural = new Set([
  'pin', 'group', 'group-picked', 'ungroup', 'move-group', 'reorder', 'reorder-group', 'move-group-block',
  'split-merge', 'split-unmerge', 'split-group', 'split-close-pane', 'split-swap', 'split-layout',
]);
/** Closing is taken back by Reopen, which puts each tab back where it stood (reducers/tabs.ts `restore`). */
const closing = new Set(['close', 'close-others', 'close-right', 'close-group']);

/** Whether ⌘Z could ever take this action back; anything else is dispatched without being predicted or recorded. */
export const isUndoable = (type: string) => structural.has(type) || closing.has(type);

export type UndoStep =
  | { kind: 'reopen'; ids: string[] }
  | { kind: 'structure'; action: WorkspaceAction }
  | { kind: 'external'; undo: () => Promise<void> };

/** What ⌘Z should do now. `refused` means the newest step's tabs changed since; it has been dropped. */
export type UndoPlan = { kind: 'apply'; actions: WorkspaceAction[] } | { kind: 'refused' } | { kind: 'empty' } | { kind: 'external'; undo: () => Promise<void> };

/**
 * The toast a running close shows, and the stack step for that close, are one Undo.
 * ⌘Z runs the toast (the tab comes back, the toast leaves) and the step is already spent,
 * so a second ⌘Z cannot open the tab again.
 */
export const tabCloseToastKey = 'tab-close';

/** True when the newest step is that close and the toast still offering it is the one on screen. */
export function toastOwnsClose(plan: UndoPlan, toastKey: string | undefined): boolean {
  return toastKey === tabCloseToastKey && plan.kind === 'apply' && plan.actions.length > 0 && plan.actions.every(action => action.type === 'reopen-id');
}

export function stepFor(before: WorkspaceState, action: WorkspaceAction, after: WorkspaceState): UndoStep | undefined {
  if (after === before) return undefined;
  if (closing.has(action.type)) {
    const open = new Set(before.tabs.map(tab => tab.id));
    const ids = after.closed.filter(tab => open.has(tab.id) && !before.closed.includes(tab)).map(tab => tab.id);
    return ids.length ? { kind: 'reopen', ids } : undefined;
  }
  if (!structural.has(action.type)) return undefined;
  const inverse = inverseOf(before, after);
  return inverse && { kind: 'structure', action: inverse };
}

export function createStructuralUndo(limit = undoLimit) {
  let steps: UndoStep[] = [];
  const listeners = new Set<() => void>();
  const emit = () => listeners.forEach(listener => listener());
  const append = (step: UndoStep) => { steps = [...steps, step].slice(-limit); emit(); };
  return {
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    getSnapshot: () => steps.length,
    /** A receipt joins the same chronology and capacity as tab inverses, never a second ring. */
    register(run: () => Promise<void>) {
      // Keyboard and toast can choose the same inverse before the engine responds, so they share one promise.
      let pending: Promise<void> | undefined;
      const forget = () => { steps = steps.filter(other => other !== step); emit(); };
      const step: UndoStep & { kind: 'external' } = { kind: 'external', undo: () => {
        if (pending) return pending;
        if (!steps.includes(step)) return Promise.resolve();
        pending = Promise.resolve().then(run).then(forget).finally(() => { pending = undefined; });
        return pending;
      } };
      append(step);
      return { undo: step.undo, forget };
    },
    /** The app-layer fallback only consumes the newest step if it belongs to an external owner. */
    async undoExternal() {
      const step = steps[steps.length - 1];
      if (step?.kind !== 'external') return false;
      await step.undo();
      return true;
    },
    get size() { return steps.length; },
    /** Records the step a change of this window's made, if it is structural. The oldest falls off past `limit`. */
    record(before: WorkspaceState, action: WorkspaceAction, after: WorkspaceState) {
      const step = stepFor(before, action, after);
      if (step) append(step);
    },
    /**
     * Plans the newest step against `state`; an external inverse stays until it succeeds so a failed connection
     * can be retried. A close whose tabs are already open again
     * (the closing toast's own Undo, or ⌘⇧T) is spent and skipped; a step that no longer applies is refused.
     */
    take(state: WorkspaceState): UndoPlan {
      while (steps.length) {
        const step = steps[steps.length - 1];
        if (step.kind === 'external') return { kind: 'external', undo: step.undo };
        steps = steps.slice(0, -1);
        emit();
        if (step.kind === 'reopen') {
          const closed = step.ids.filter(id => state.closed.some(tab => tab.id === id));
          if (closed.length) return { kind: 'apply', actions: [...closed].reverse().map(id => ({ type: 'reopen-id', id })) };
          if (step.ids.every(id => state.tabs.some(tab => tab.id === id))) continue;
          return { kind: 'refused' };
        }
        return workspaceReducer(state, step.action) === state ? { kind: 'refused' } : { kind: 'apply', actions: [step.action] };
      }
      return { kind: 'empty' };
    },
    clear() { steps = []; emit(); },
  };
}
export type StructuralUndo = ReturnType<typeof createStructuralUndo>;

// Each native webview has its own module realm, so this stack cannot cross windows or survive a reload.
export const windowStructuralUndo = createStructuralUndo();
