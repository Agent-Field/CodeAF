// The durable identity of a terminal tab. It lives on the pane itself (`sessionFile` plus `target.terminalId`),
// so it travels with every copy of the pane: a reload, a duplicate, a move to another window. The bridge's
// `sessionId` is opaque and dies with the page, so it is never part of it. The per-pane binding store in
// ./bindings still exists for tabs saved before this and for the engine's refusal sentence.
import type { Pane } from '../tabs/types.ts';
import type { TabView } from '../tabs/view-state.ts';
import { bindingOf, type TerminalBinding } from './bindings.ts';

export type TerminalTarget = { sessionFile: string; terminalId: string };

/** The view fields that name `target`. Dispatched through `view`, or passed to `newtab-become`. */
export const terminalView = ({ sessionFile, terminalId }: TerminalTarget): TabView => ({ sessionFile, target: { terminalId } });

type Carrier = Pick<Pane, 'sessionFile' | 'target'>;

/** The terminal the pane names for itself, or undefined unless BOTH halves are present. */
export function terminalTargetOf(pane: Carrier): TerminalTarget | undefined {
  const terminalId = pane.target?.terminalId;
  return pane.sessionFile && terminalId ? { sessionFile: pane.sessionFile, terminalId } : undefined;
}

/** What the pane shows: its own target first, else the legacy binding saved under its id. */
export function bindingFor(pane: Carrier & Pick<Pane, 'id'>): TerminalBinding | undefined {
  const own = terminalTargetOf(pane);
  return own ?? bindingOf(pane.id);
}

/**
 * The view that makes a pane saved before the durable target carry one, or undefined when it already does or
 * has no live terminal to name. It only reads; the caller applies it through a tab action.
 */
export function legacyTerminalView(pane: Carrier & Pick<Pane, 'id'>): TabView | undefined {
  if (terminalTargetOf(pane)) return undefined;
  const legacy = bindingOf(pane.id);
  return legacy?.sessionFile && legacy.terminalId ? terminalView({ sessionFile: legacy.sessionFile, terminalId: legacy.terminalId }) : undefined;
}
