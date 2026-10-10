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
  | { kind: 'structure'; action: WorkspaceAction };

/** What ⌘Z should do now. `refused` means the newest step's tabs changed since; it has been dropped. */
export type UndoPlan = { kind: 'apply'; actions: WorkspaceAction[] } | { kind: 'refused' } | { kind: 'empty' };

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
  return {
    get size() { return steps.length; },
    /** Records the step a change of this window's made, if it is structural. The oldest falls off past `limit`. */
    record(before: WorkspaceState, action: WorkspaceAction, after: WorkspaceState) {
      const step = stepFor(before, action, after);
      if (step) steps = [...steps, step].slice(-limit);
    },
    /**
     * Takes the newest step off and says how to undo it against `state`. A close whose tabs are already open again
     * (the closing toast's own Undo, or ⌘⇧T) is spent and skipped; a step that no longer applies is refused.
     */
    take(state: WorkspaceState): UndoPlan {
      while (steps.length) {
        const step = steps[steps.length - 1];
        steps = steps.slice(0, -1);
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
    clear() { steps = []; },
  };
}
export type StructuralUndo = ReturnType<typeof createStructuralUndo>;
